package api

import (
	"fmt"
	"testing"
	"time"
)

func TestDemoSelectActiveKeys_AlwaysTerminatesAndFillsTarget(t *testing.T) {
	for _, n := range []int{6, 8, 9, 12} {
		keys := make([]string, 0, n)
		for i := 0; i < n; i++ {
			keys = append(keys, fmt.Sprintf("key-%02d", i))
		}
		for bucket := int64(0); bucket < 400; bucket++ {
			now := time.Unix(bucket*300, 0)
			done := make(chan map[string]struct{}, 1)
			go func() { done <- demoSelectActiveKeys("wireguard-peer", append([]string(nil), keys...), now, 4, 5) }()
			select {
			case got := <-done:
				if len(got) < 4 || len(got) > 5 {
					t.Fatalf("n=%d bucket=%d: selected %d keys, want 4-5", n, bucket, len(got))
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("n=%d bucket=%d: demoSelectActiveKeys did not terminate", n, bucket)
			}
		}
	}
}
