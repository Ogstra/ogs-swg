package api

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

// Apple identifiers are sourced from kyle-seongwoo-jun/apple-device-identifiers (MIT).
//
//go:embed devicedata/apple-ios-device-identifiers.json
var appleIOSDeviceIdentifiersJSON []byte

//go:embed devicedata/apple-mac-device-identifiers.json
var appleMacDeviceIdentifiersJSON []byte

var (
	subscriptionDeviceCatalogOnce sync.Once
	subscriptionAppleCatalog      map[string]string
)

var subscriptionSamsungPrefixCatalog = []struct {
	prefix string
	name   string
}{
	{prefix: "SM-F946", name: "Samsung Galaxy Z Fold 5"},
	{prefix: "SM-F956", name: "Samsung Galaxy Z Fold 6"},
}

func loadSubscriptionDeviceCatalog() {
	subscriptionDeviceCatalogOnce.Do(func() {
		subscriptionAppleCatalog = make(map[string]string)
		loadSubscriptionDeviceCatalogJSON(appleIOSDeviceIdentifiersJSON)
		loadSubscriptionDeviceCatalogJSON(appleMacDeviceIdentifiersJSON)
	})
}

func loadSubscriptionDeviceCatalogJSON(raw []byte) {
	if len(raw) == 0 {
		return
	}
	var entries map[string]string
	if err := json.Unmarshal(raw, &entries); err != nil {
		return
	}
	for key, value := range entries {
		identifier := strings.TrimSpace(key)
		name := strings.TrimSpace(value)
		if identifier == "" || name == "" {
			continue
		}
		subscriptionAppleCatalog[identifier] = name
	}
}

func resolveSubscriptionDeviceModel(model string) string {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return ""
	}

	loadSubscriptionDeviceCatalog()
	if resolved, ok := subscriptionAppleCatalog[trimmed]; ok {
		return resolved
	}

	if strings.HasPrefix(trimmed, "SM-") {
		for _, entry := range subscriptionSamsungPrefixCatalog {
			if strings.HasPrefix(trimmed, entry.prefix) {
				return entry.name
			}
		}
		return "Samsung " + trimmed
	}

	return trimmed
}
