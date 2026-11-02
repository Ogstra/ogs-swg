package core

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSubscriptionRequestIndexes(t *testing.T) {
	st, err := NewStore(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	for _, name := range []string{"idx_subscription_requests_requested_at", "idx_subscription_requests_device"} {
		var n int
		if err := st.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("index %s missing", name)
		}
	}

	// The two hot queries must use them instead of scanning the table.
	plan := func(query string, args ...any) string {
		rows, err := st.db.Query("EXPLAIN QUERY PLAN "+query, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := ""
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			out += detail + "\n"
		}
		return out
	}
	device := plan(`SELECT id FROM subscription_requests WHERE sub_id = ? AND hwid_hash = ? ORDER BY requested_at DESC LIMIT 1`, 1, "h")
	if !strings.Contains(device, "idx_subscription_requests_device") {
		t.Fatalf("latest-request-of-a-device plan does not use its index:\n%s", device)
	}
}
