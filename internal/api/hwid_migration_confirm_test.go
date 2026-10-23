package api

import (
	"context"
	"strings"
	"testing"
)

func TestPublicSub_ConfirmsMigrationWhenHWIDReachesTarget(t *testing.T) {
	f := newMigrationFixture(t, nil)
	ctx := context.Background()

	hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID)
	if live, _ := f.store.GetLiveHWIDMigration(ctx, f.srcID, f.hash); live == nil || live.Status != "delivered" {
		t.Fatalf("after first request = %#v; want delivered", live)
	}

	hitSub(t, f.server, migrationTestDstToken, migrationTestHWID)
	if live, _ := f.store.GetLiveHWIDMigration(ctx, f.srcID, f.hash); live != nil {
		t.Fatalf("migration still live after arrival: %#v", live)
	}

	again := hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID)
	if got := again.Header().Get("new-url"); got != "" {
		t.Fatalf("new-url re-sent after confirmation: %q", got)
	}
}

func TestPublicSub_OtherDeviceOnTargetDoesNotConfirm(t *testing.T) {
	f := newMigrationFixture(t, nil)

	hitSub(t, f.server, migrationTestDstToken, "hwid-someone-else")
	if live, _ := f.store.GetLiveHWIDMigration(context.Background(), f.srcID, f.hash); live == nil {
		t.Fatal("a different HWID on the target confirmed the migration")
	}
}

func TestPublicSub_MigratedDeviceDoesNotTriggerNewDeviceNotification(t *testing.T) {
	f := newMigrationFixture(t, nil)
	snapshot := recordMigrationNtfy(t, f)
	// A device being moved is already known on the source sub.
	if _, err := f.store.RecordSubscriptionHWIDFirstSeen(context.Background(), f.srcID, f.hash, 5); err != nil {
		t.Fatal(err)
	}

	hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID)
	hitSub(t, f.server, migrationTestDstToken, migrationTestHWID)

	msgs := snapshot()
	if len(msgs) != 1 || !strings.HasPrefix(msgs[0].Title, "Device moved to subscription:") {
		t.Fatalf("published = %#v; want only the migration-confirmed message", msgs)
	}

	// A later request is neither a new device nor a second confirmation.
	hitSub(t, f.server, migrationTestDstToken, migrationTestHWID)
	if got := len(snapshot()); got != 1 {
		t.Fatalf("published %d messages after a repeat request; want still 1", got)
	}
}

func TestPublicSub_UnrelatedNewDeviceStillNotifies(t *testing.T) {
	f := newMigrationFixture(t, nil)
	snapshot := recordMigrationNtfy(t, f)

	hitSub(t, f.server, migrationTestDstToken, "hwid-someone-else")

	msgs := snapshot()
	if len(msgs) != 1 || !strings.HasPrefix(msgs[0].Title, "New device on subscription:") {
		t.Fatalf("published = %#v; want the regular new-device message", msgs)
	}
}

func TestPublicSub_DeviceAlreadyOnTargetStillMigrates(t *testing.T) {
	f := newMigrationFixture(t, nil)
	ctx := context.Background()

	// The device already uses the target sub before the move is armed.
	hitSub(t, f.server, migrationTestDstToken, migrationTestHWID)
	if live, _ := f.store.GetLiveHWIDMigration(ctx, f.srcID, f.hash); live == nil || live.Status != "pending" {
		t.Fatalf("a target request before delivery changed the migration: %#v", live)
	}

	moved := hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID)
	if got := moved.Header().Get("new-url"); got == "" {
		t.Fatal("device already on the target was not told to move off the source")
	}

	hitSub(t, f.server, migrationTestDstToken, migrationTestHWID)
	if live, _ := f.store.GetLiveHWIDMigration(ctx, f.srcID, f.hash); live != nil {
		t.Fatalf("migration still live after the device came back to the target: %#v", live)
	}
}
