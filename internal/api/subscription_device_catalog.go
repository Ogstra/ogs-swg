package api

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
)

// Apple identifiers are sourced from kyle-seongwoo-jun/apple-device-identifiers (MIT).
//
//go:embed devicedata/apple-ios-device-identifiers.json
var appleIOSDeviceIdentifiersJSON []byte

//go:embed devicedata/apple-mac-device-identifiers.json
var appleMacDeviceIdentifiersJSON []byte

// Samsung names come from Google's Play supported-devices list; regenerate them
// with scripts/update-device-catalog.py when the embedded copy should be
// refreshed (the running panel also refreshes itself, see device_catalog_updater.go).
//
//go:embed devicedata/samsung-device-names.json
var samsungDeviceNamesJSON []byte

const samsungFamilyPrefixLen = 7

// deviceCatalog is immutable once built, so readers never need a lock: the
// updater swaps in a whole new value atomically.
type deviceCatalog struct {
	apple   map[string]string
	samsung map[string]string
	// samsungFamily maps the first 7 characters of a model code (e.g.
	// "SM-S918") to a name, only when every known variant agrees on it. It
	// names regional variants that are not in the list yet.
	samsungFamily map[string]string
}

var (
	deviceCatalogOnce    sync.Once
	deviceCatalogCurrent atomic.Pointer[deviceCatalog]
)

func currentDeviceCatalog() *deviceCatalog {
	deviceCatalogOnce.Do(func() {
		deviceCatalogCurrent.Store(buildDeviceCatalog(nil, nil))
	})
	return deviceCatalogCurrent.Load()
}

// buildDeviceCatalog layers remote overrides on top of the embedded data, so a
// partial or empty download can only add names, never remove them.
func buildDeviceCatalog(remoteApple, remoteSamsung map[string]string) *deviceCatalog {
	apple := parseDeviceCatalogJSON(appleIOSDeviceIdentifiersJSON)
	for key, value := range parseDeviceCatalogJSON(appleMacDeviceIdentifiersJSON) {
		apple[key] = value
	}
	for key, value := range remoteApple {
		apple[key] = value
	}

	samsung := parseDeviceCatalogJSON(samsungDeviceNamesJSON)
	for key, value := range remoteSamsung {
		samsung[strings.ToUpper(strings.TrimSpace(key))] = value
	}
	return &deviceCatalog{apple: apple, samsung: samsung, samsungFamily: samsungFamilyNames(samsung)}
}

func parseDeviceCatalogJSON(raw []byte) map[string]string {
	out := make(map[string]string)
	if len(raw) == 0 {
		return out
	}
	var entries map[string]string
	if err := json.Unmarshal(raw, &entries); err != nil {
		return out
	}
	for key, value := range entries {
		identifier := strings.TrimSpace(key)
		name := strings.TrimSpace(value)
		if identifier == "" || name == "" {
			continue
		}
		out[identifier] = name
	}
	return out
}

func samsungFamilyNames(samsung map[string]string) map[string]string {
	family := make(map[string]string)
	ambiguous := make(map[string]bool)
	for code, name := range samsung {
		code = strings.ToUpper(code)
		if len(code) <= samsungFamilyPrefixLen {
			continue
		}
		prefix := code[:samsungFamilyPrefixLen]
		if existing, ok := family[prefix]; ok && existing != name {
			ambiguous[prefix] = true
		} else {
			family[prefix] = name
		}
	}
	for prefix := range ambiguous {
		delete(family, prefix)
	}
	return family
}

func resolveSamsungModel(catalog *deviceCatalog, model string) string {
	code := strings.ToUpper(model)
	name, ok := catalog.samsung[code]
	if !ok && len(code) > samsungFamilyPrefixLen {
		name, ok = catalog.samsungFamily[code[:samsungFamilyPrefixLen]]
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

	catalog := currentDeviceCatalog()
	if resolved, ok := catalog.apple[trimmed]; ok {
		return resolved
	}

	if strings.HasPrefix(strings.ToUpper(trimmed), "SM-") {
		return resolveSamsungModel(catalog, trimmed)
	}

	return trimmed
}
