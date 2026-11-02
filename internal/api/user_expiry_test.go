package api

import (
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func TestApplyUserExpiry(t *testing.T) {
	srv, st := newNtfyTestServerWithStore(t)
	for _, name := range []string{"ann", "ben"} {
		if err := st.SaveUserMetadata(core.UserMetadata{Email: name, QuotaPeriod: "monthly", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	in := func(d time.Duration) *int64 { v := time.Now().Add(d).Unix(); return &v }
	days := func(n int64) *int64 { return &n }

	// New user: the request values are stored.
	date := in(48 * time.Hour)
	if err := srv.applyUserExpiry("ann", date, nil, core.UserExpiry{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetUserExpiry("ann"); got.ExpiresAt != *date {
		t.Fatalf("ann expiry = %+v", got)
	}

	// An edit that omits the fields keeps what the user had (also across a rename).
	prev, _ := st.GetUserExpiry("ann")
	if err := srv.applyUserExpiry("ben", nil, nil, prev); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetUserExpiry("ben"); got != prev {
		t.Fatalf("ben expiry = %+v; want the previous %+v", got, prev)
	}

	// Explicit zeros clear it.
	if err := srv.applyUserExpiry("ann", days(0), days(0), prev); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetUserExpiry("ann"); got != (core.UserExpiry{}) {
		t.Fatalf("ann expiry after clearing = %+v", got)
	}

	// Switching to "days after first use" is stored with no fixed date.
	if err := srv.applyUserExpiry("ann", days(0), days(30), core.UserExpiry{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetUserExpiry("ann"); got.ExpiresAt != 0 || got.ExpireAfterDays != 30 {
		t.Fatalf("ann expiry = %+v", got)
	}
}
