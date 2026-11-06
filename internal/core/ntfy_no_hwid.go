package core

import (
	"fmt"
	"strings"
)

// NtfyNoHWIDInfo describes a request that reached a subscription requiring a
// device ID without one.
type NtfyNoHWIDInfo struct {
	SubscriptionName string
	Username         string
	UserAgent        string
	Country          string
	ClientIP         string
}

// NtfyNoHWIDMessage builds the notification for a request that came without a
// device ID and was answered with an empty subscription.
func NtfyNoHWIDMessage(info NtfyNoHWIDInfo) NtfyMessage {
	clean := func(v string) string {
		return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(v))
	}
	field := func(v string) string {
		if v = clean(v); v == "" {
			return "-"
		}
		return v
	}
	return NtfyMessage{
		Title: fmt.Sprintf("No device ID: %s", field(info.SubscriptionName)),
		Message: "A request without a device ID got an empty subscription." +
			"\nUser: " + field(info.Username) +
			"\nClient: " + field(info.UserAgent) +
			"\nCountry: " + field(info.Country) +
			"\nIP: " + field(info.ClientIP),
		Tags:      []string{"warning"},
		Priority:  3,
		ClickPath: "/subscriptions",
	}
}
