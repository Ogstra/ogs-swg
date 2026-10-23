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
	DeviceModel        string
	DeviceOS           string
	DeviceOSVersion    string
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
	// "iPhone 14 Pro · iOS 17.4"; empty when the request carried no device info.
	label := strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(info.DeviceModel))
	if os := strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(info.DeviceOS + " " + info.DeviceOSVersion))); os != "" {
		if label != "" {
			label += " · "
		}
		label += os
	}
	who := "Device"
	if label != "" {
		who = "**" + label + "**"
	}
	idLine := "\nHWID prefix: " + field(info.HWIDPrefix)
	if info.Failed {
		return NtfyMessage{
			Title:     fmt.Sprintf("Device move failed: %s -> %s", field(info.SourceSubscription), field(info.TargetSubscription)),
			Message:   who + " never requested the target subscription after the maximum number of attempts." + idLine,
			Tags:      []string{"warning"},
			Priority:  4,
			Markdown:  true,
			ClickPath: "/subscriptions",
		}
	}
	title := fmt.Sprintf("Device moved to subscription: %s", field(info.TargetSubscription))
	if label != "" {
		title = fmt.Sprintf("%s moved to subscription: %s", label, field(info.TargetSubscription))
	}
	return NtfyMessage{
		Title:     title,
		Message:   who + " is now requesting the new subscription." + idLine,
		Tags:      []string{"twisted_rightwards_arrows"},
		Priority:  3,
		Markdown:  true,
		ClickPath: "/subscriptions",
	}
}
