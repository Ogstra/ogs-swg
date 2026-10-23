package core

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestNtfyHWIDMigrationMessage(t *testing.T) {
	t.Run("confirmed", func(t *testing.T) {
		msg := NtfyHWIDMigrationMessage(NtfyHWIDMigrationInfo{SourceSubscription: "a", TargetSubscription: "b", HWIDPrefix: "abc12345"})
		if msg.Title != "Device moved to subscription: b" {
			t.Errorf("Title = %q", msg.Title)
		}
		if !msg.Markdown || msg.ClickPath != "/subscriptions" || msg.Priority != 3 {
			t.Errorf("Markdown/ClickPath/Priority = %v/%q/%d", msg.Markdown, msg.ClickPath, msg.Priority)
		}
		if !slices.Equal(msg.Tags, []string{"twisted_rightwards_arrows"}) {
			t.Errorf("Tags = %v", msg.Tags)
		}
		if !strings.Contains(msg.Message, "abc12345") {
			t.Errorf("Message = %q; want the HWID prefix", msg.Message)
		}
	})

	t.Run("failed", func(t *testing.T) {
		msg := NtfyHWIDMigrationMessage(NtfyHWIDMigrationInfo{SourceSubscription: "a", TargetSubscription: "b", HWIDPrefix: "abc12345", Failed: true})
		if msg.Title != "Device move failed: a -> b" {
			t.Errorf("Title = %q", msg.Title)
		}
		if msg.Priority != 4 || !slices.Equal(msg.Tags, []string{"warning"}) {
			t.Errorf("Priority/Tags = %d/%v", msg.Priority, msg.Tags)
		}
	})
}

func TestNtfyHWIDMigrationMessageNamesTheDevice(t *testing.T) {
	info := NtfyHWIDMigrationInfo{
		SourceSubscription: "a", TargetSubscription: "b", HWIDPrefix: "abc12345",
		DeviceModel: "iPhone 14 Pro", DeviceOS: "iOS", DeviceOSVersion: "17.4",
	}

	moved := NtfyHWIDMigrationMessage(info)
	if moved.Title != "iPhone 14 Pro · iOS 17.4 moved to subscription: b" {
		t.Errorf("Title = %q", moved.Title)
	}
	for _, want := range []string{"**iPhone 14 Pro · iOS 17.4**", "abc12345"} {
		if !strings.Contains(moved.Message, want) {
			t.Errorf("Message = %q; want %q", moved.Message, want)
		}
	}

	info.Failed = true
	failed := NtfyHWIDMigrationMessage(info)
	if !strings.Contains(failed.Message, "**iPhone 14 Pro · iOS 17.4**") || !strings.Contains(failed.Message, "abc12345") {
		t.Errorf("failed Message = %q; want the device and HWID prefix", failed.Message)
	}

	// No device info on the request: keep the generic wording.
	bare := NtfyHWIDMigrationMessage(NtfyHWIDMigrationInfo{TargetSubscription: "b", HWIDPrefix: "abc12345"})
	if bare.Title != "Device moved to subscription: b" || !strings.HasPrefix(bare.Message, "Device is now requesting") {
		t.Errorf("bare = %q / %q", bare.Title, bare.Message)
	}
}

func TestObserveHWIDMigration(t *testing.T) {
	info := NtfyHWIDMigrationInfo{SourceSubscription: "a", TargetSubscription: "b", HWIDPrefix: "abc123"}

	t.Run("enabled_publishes_once", func(t *testing.T) {
		s := baseSettings()
		s.EnableNewHwid = true
		n, recorded, _ := newTestNotifier(t, s)

		n.ObserveHWIDMigration(context.Background(), info)

		if len(*recorded) != 1 {
			t.Fatalf("published %d messages; want 1", len(*recorded))
		}
	})

	t.Run("toggle_off_publishes_nothing", func(t *testing.T) {
		s := baseSettings()
		s.EnableNewHwid = false
		n, recorded, _ := newTestNotifier(t, s)

		n.ObserveHWIDMigration(context.Background(), info)

		if len(*recorded) != 0 {
			t.Fatalf("published %d messages; want 0 (toggle off)", len(*recorded))
		}
	})
}
