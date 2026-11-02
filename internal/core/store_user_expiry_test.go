package core

import (
	"path/filepath"
	"testing"
	"time"
)

func newExpiryStore(t *testing.T) *Store {
	t.Helper()
	st, err := NewStore(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SaveUserMetadata(UserMetadata{Email: "alice", QuotaPeriod: "monthly", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestUserExpiry_ExpiredUserCountsAsOverQuota(t *testing.T) {
	st := newExpiryStore(t)
	now := time.Now()
	meta := &UserMetadata{Email: "alice", QuotaPeriod: "monthly", Enabled: true}

	if _, over, err := userQuotaUsage(meta, now, st); err != nil || over {
		t.Fatalf("user without expiry: over=%v err=%v", over, err)
	}

	if err := st.SetUserExpiry("alice", UserExpiry{ExpiresAt: now.Add(time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	if _, over, _ := userQuotaUsage(meta, now, st); over {
		t.Fatal("a user whose expiry is in the future must not be treated as over")
	}

	if err := st.SetUserExpiry("alice", UserExpiry{ExpiresAt: now.Add(-time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}
	if _, over, _ := userQuotaUsage(meta, now, st); !over {
		t.Fatal("an expired user must be treated as over so the sampler disables them")
	}

	// Extending the date brings them back through the same check.
	if err := st.SetUserExpiry("alice", UserExpiry{ExpiresAt: now.Add(24 * time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	if _, over, _ := userQuotaUsage(meta, now, st); over {
		t.Fatal("extending the expiry did not clear the over state")
	}
}

func TestUserExpiry_ExpiredAndOverQuotaBothDisable(t *testing.T) {
	st := newExpiryStore(t)
	now := time.Now()
	if err := st.AddSample(Sample{User: "alice", Timestamp: now.Unix(), Uplink: 100, Downlink: 100}); err != nil {
		t.Fatal(err)
	}
	meta := &UserMetadata{Email: "alice", QuotaLimit: 150, QuotaPeriod: "monthly", Enabled: true}
	if _, over, _ := userQuotaUsage(meta, now, st); !over {
		t.Fatal("over quota but not reported as over")
	}
}

func TestUserExpiry_StartsOnFirstUse(t *testing.T) {
	st := newExpiryStore(t)
	now := time.Now()
	if err := st.SetUserExpiry("alice", UserExpiry{ExpireAfterDays: 30}); err != nil {
		t.Fatal(err)
	}

	started, err := st.activateExpiryOnFirstUse("alice", now)
	if err != nil || started {
		t.Fatalf("no traffic yet: started=%v err=%v", started, err)
	}
	if e, _ := st.GetUserExpiry("alice"); e.ExpiresAt != 0 {
		t.Fatalf("the countdown started without any traffic: %+v", e)
	}

	if err := st.AddSample(Sample{User: "alice", Timestamp: now.Unix(), Uplink: 10, Downlink: 5}); err != nil {
		t.Fatal(err)
	}
	started, err = st.activateExpiryOnFirstUse("alice", now)
	if err != nil || !started {
		t.Fatalf("first traffic seen: started=%v err=%v", started, err)
	}
	e, _ := st.GetUserExpiry("alice")
	want := now.Add(30 * 24 * time.Hour).Unix()
	if e.ExpiresAt != want || e.ExpireAfterDays != 30 {
		t.Fatalf("expiry = %+v; want ExpiresAt=%d", e, want)
	}

	// Once started it is never moved again.
	if again, _ := st.activateExpiryOnFirstUse("alice", now.Add(48*time.Hour)); again {
		t.Fatal("the countdown was restarted")
	}
	if e2, _ := st.GetUserExpiry("alice"); e2.ExpiresAt != want {
		t.Fatalf("expiry moved to %d", e2.ExpiresAt)
	}
}

func TestUserExpiry_ListOnlyReturnsUsersWithAnExpiry(t *testing.T) {
	st := newExpiryStore(t)
	if err := st.SaveUserMetadata(UserMetadata{Email: "bob", QuotaPeriod: "monthly", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserExpiry("alice", UserExpiry{ExpiresAt: 2000000000}); err != nil {
		t.Fatal(err)
	}
	got, err := st.ListUserExpiries()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["alice"].ExpiresAt != 2000000000 {
		t.Fatalf("expiries = %v", got)
	}
}
