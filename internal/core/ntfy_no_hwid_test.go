package core

import (
	"strings"
	"testing"
)

func TestNtfyNoHWIDMessage(t *testing.T) {
	msg := NtfyNoHWIDMessage(NtfyNoHWIDInfo{
		SubscriptionName: "plan\nA",
		Username:         "alice",
		UserAgent:        "Happ/1.0",
		ClientIP:         "192.0.2.1",
	})
	if !strings.Contains(msg.Title, "plan A") {
		t.Fatalf("title = %q", msg.Title)
	}
	for _, want := range []string{"alice", "Happ/1.0", "192.0.2.1", "Country: -"} {
		if !strings.Contains(msg.Message, want) {
			t.Fatalf("message missing %q: %q", want, msg.Message)
		}
	}
	if msg.ClickPath != "/subscriptions" {
		t.Fatalf("click path = %q", msg.ClickPath)
	}
}
