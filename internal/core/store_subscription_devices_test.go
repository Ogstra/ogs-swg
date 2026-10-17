package core

import (
	"context"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core/store"
)

func TestListSubscriptionDevices(t *testing.T) {
	ctx := context.Background()
	st := newMigrationTestStore(t)

	if _, err := st.RecordSubscriptionHWIDFirstSeen(ctx, 1, "hash-a", 100); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordSubscriptionHWIDFirstSeen(ctx, 1, "hash-b", 200); err != nil {
		t.Fatal(err)
	}
	for _, at := range []int64{300, 500, 400} {
		if err := st.Queries.InsertSubscriptionRequest(ctx, store.InsertSubscriptionRequestParams{
			SubID: 1, HwidHash: "hash-a", HwidPrefix: "AAAA1111", DeviceModel: "model-a",
			DeviceOs: "iOS", Country: "AR", RequestedAt: at,
		}); err != nil {
			t.Fatal(err)
		}
	}

	devices, err := st.ListSubscriptionDevices(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %#v; want 2", devices)
	}
	// Newest first-seen first: hash-b has no request rows yet.
	if devices[0].HWIDHash != "hash-b" || devices[0].HWIDPrefix != "" || devices[0].LastSeenAt != 0 {
		t.Fatalf("devices[0] = %#v", devices[0])
	}
	a := devices[1]
	if a.HWIDHash != "hash-a" || a.HWIDPrefix != "AAAA1111" || a.DeviceModel != "model-a" ||
		a.DeviceOS != "iOS" || a.Country != "AR" || a.FirstSeenAt != 100 || a.LastSeenAt != 500 {
		t.Fatalf("devices[1] = %#v; want latest request metadata", a)
	}

	if other, _ := st.ListSubscriptionDevices(ctx, 2); len(other) != 0 {
		t.Fatalf("sub 2 devices = %#v; want none", other)
	}
	if ok, _ := st.HasSubscriptionKnownHWID(ctx, 1, "hash-a"); !ok {
		t.Fatal("HasSubscriptionKnownHWID(hash-a) = false")
	}
	if ok, _ := st.HasSubscriptionKnownHWID(ctx, 2, "hash-a"); ok {
		t.Fatal("HasSubscriptionKnownHWID on another sub = true")
	}
}
