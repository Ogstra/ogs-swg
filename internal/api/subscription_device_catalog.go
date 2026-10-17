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

// Samsung names come from Google's Play supported-devices list; regenerate them
// with scripts/update-device-catalog.py when new models should be recognised.
//
//go:embed devicedata/samsung-device-names.json
var samsungDeviceNamesJSON []byte

var (
	subscriptionDeviceCatalogOnce sync.Once
	subscriptionAppleCatalog      map[string]string
	subscriptionSamsungCatalog    map[string]string
	// subscriptionSamsungFamily maps the first 7 characters of a model code
	// (e.g. "SM-S918") to a name, only when every known variant agrees on it.
	// It names regional variants that are not in the list yet.
	subscriptionSamsungFamily map[string]string
)

const samsungFamilyPrefixLen = 7

func loadSubscriptionDeviceCatalog() {
	subscriptionDeviceCatalogOnce.Do(func() {
		subscriptionAppleCatalog = make(map[string]string)
		loadSubscriptionDeviceCatalogJSON(appleIOSDeviceIdentifiersJSON)
		loadSubscriptionDeviceCatalogJSON(appleMacDeviceIdentifiersJSON)
		loadSubscriptionSamsungCatalog(samsungDeviceNamesJSON)
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

func loadSubscriptionSamsungCatalog(raw []byte) {
	subscriptionSamsungCatalog = make(map[string]string)
	subscriptionSamsungFamily = make(map[string]string)
	if len(raw) == 0 {
		return
	}
	var entries map[string]string
	if err := json.Unmarshal(raw, &entries); err != nil {
		return
	}
	ambiguous := make(map[string]bool)
	for key, value := range entries {
		code := strings.ToUpper(strings.TrimSpace(key))
		name := strings.TrimSpace(value)
		if code == "" || name == "" {
			continue
		}
		subscriptionSamsungCatalog[code] = name
		if len(code) <= samsungFamilyPrefixLen {
			continue
		}
		prefix := code[:samsungFamilyPrefixLen]
		if existing, ok := subscriptionSamsungFamily[prefix]; ok && existing != name {
			ambiguous[prefix] = true
		} else {
			subscriptionSamsungFamily[prefix] = name
		}
	}
	for prefix := range ambiguous {
		delete(subscriptionSamsungFamily, prefix)
	}
}

func resolveSamsungModel(model string) string {
	code := strings.ToUpper(model)
	name, ok := subscriptionSamsungCatalog[code]
	if !ok && len(code) > samsungFamilyPrefixLen {
		name, ok = subscriptionSamsungFamily[code[:samsungFamilyPrefixLen]]
	}
	if !ok {
		return "Samsung " + model
	}
	if strings.Contains(strings.ToLower(name), "galaxy") {
		return name
	}
	return "Samsung " + name
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

	if strings.HasPrefix(strings.ToUpper(trimmed), "SM-") {
		return resolveSamsungModel(trimmed)
	}

	return trimmed
}
