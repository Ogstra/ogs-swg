package core

import (
	"fmt"
	"strings"
)

// NtfyHWIDMigrationInfo describes a per-device subscription migration that
// either completed or gave up.
type NtfyHWIDMigrationInfo struct {
	SourceSubscription string
	TargetSubscription string
	HWIDPrefix         string
	Failed             bool
}

// NtfyHWIDMigrationMessage builds the notification for a finished device
// migration (confirmed on the target sub, or abandoned after max attempts).
func NtfyHWIDMigrationMessage(info NtfyHWIDMigrationInfo) NtfyMessage {
	field := func(v string) string {
		v = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(v))
		if v == "" {
			return "-"
		}
		return v
	}
	if info.Failed {
		return NtfyMessage{
			Title:     fmt.Sprintf("Device move failed: %s -> %s", field(info.SourceSubscription), field(info.TargetSubscription)),
			Message:   "Device **" + field(info.HWIDPrefix) + "** never requested the target subscription after the maximum number of attempts.",
			Tags:      []string{"warning"},
			Priority:  4,
			Markdown:  true,
			ClickPath: "/subscriptions",
		}
	}
	return NtfyMessage{
		Title:     fmt.Sprintf("Device moved to subscription: %s", field(info.TargetSubscription)),
		Message:   "Device **" + field(info.HWIDPrefix) + "** is now requesting the new subscription.",
		Tags:      []string{"twisted_rightwards_arrows"},
		Priority:  3,
		Markdown:  true,
		ClickPath: "/subscriptions",
	}
}
