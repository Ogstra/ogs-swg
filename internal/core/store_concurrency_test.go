package core

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Readers and writers share a small pool. None of them may fail with
// "database is locked", including writers that read before they write.
func TestStore_ConcurrentReadersAndWritersDoNotLockEachOther(t *testing.T) {
	st, err := NewStore(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := st.CreatePanelUser("racer", "pw-racer-123", PanelUserPermissions{CanReadUsers: true}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	run := func(name string, work func(i int) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; time.Now().Before(deadline); i++ {
				if err := work(i); err != nil {
					errs <- fmt.Errorf("%s: %w", name, err)
					return
				}
			}
		}()
	}

	for w := 0; w < 3; w++ {
		w := w
		run(fmt.Sprintf("setting writer %d", w), func(i int) error {
			return st.SetAppSetting(ctx, fmt.Sprintf("k%d", w), fmt.Sprint(i))
		})
	}
	run("read-then-write transaction", func(i int) error {
		_, err := st.ConsumePanelUserRecoveryCode("racer", "no-such-hash")
		return err
	})
	run("totp step writer", func(i int) error {
		_, err := st.ConsumePanelUserTOTPStep("racer", int64(i+1))
		return err
	})
	for r := 0; r < 4; r++ {
		run(fmt.Sprintf("reader %d", r), func(i int) error {
			if _, err := st.ListUserTemplates(ctx); err != nil {
				return err
			}
			_, _, err := st.GetAppSetting(ctx, "k0")
			return err
		})
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
