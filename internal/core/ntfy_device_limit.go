package core

import (
	"fmt"
	"strings"
)

// NtfyDeviceLimitInfo describes a device refused for exceeding a subscription's device limit.
type NtfyDeviceLimitInfo struct {
	SubscriptionName string
	Username         string
	HWIDPrefix       string
	DeviceModel      string
	DeviceOS         string
	DeviceOSVersion  string
}

// NtfyDeviceLimitMessage builds the notification for a refused device.
func NtfyDeviceLimitMessage(info NtfyDeviceLimitInfo) NtfyMessage {
	clean := func(v string) string {
		return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(v))
	}
	field := func(v string) string {
		if v = clean(v); v == "" {
			return "-"
		}
		return v
	}
	label := clean(info.DeviceModel)
	if os := clean(info.DeviceOS + " " + info.DeviceOSVersion); os != "" {
		if label != "" {
			label += " · "
		}
		label += os
	}
	who := "A new device"
	if label != "" {
		who = "**" + label + "**"
	}
	return NtfyMessage{
		Title: fmt.Sprintf("Device limit reached: %s", field(info.SubscriptionName)),
		Message: who + " was refused. Remove a device in Subscriptions > Devices or raise the limit." +
			"\nUser: " + field(info.Username) +
			"\nHWID prefix: " + field(info.HWIDPrefix),
		Tags:      []string{"no_entry"},
		Priority:  3,
		Markdown:  true,
		ClickPath: "/subscriptions",
	}
}
