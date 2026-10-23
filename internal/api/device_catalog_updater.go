package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	deviceCatalogCacheKey      = "device_catalog_cache_v1"
	deviceCatalogRefreshEvery  = 7 * 24 * time.Hour
	deviceCatalogCheckEvery    = 24 * time.Hour
	deviceCatalogFirstCheck    = time.Minute
	deviceCatalogMaxBodyBytes  = 16 << 20
	deviceCatalogRequestTimout = 45 * time.Second

	deviceCatalogAppleIOSURL = "https://raw.githubusercontent.com/kyle-seongwoo-jun/apple-device-identifiers/main/ios-device-identifiers.json"
	deviceCatalogAppleMacURL = "https://raw.githubusercontent.com/kyle-seongwoo-jun/apple-device-identifiers/main/mac-device-identifiers.json"
	deviceCatalogSamsungURL  = "https://storage.googleapis.com/play_public/supported_devices.csv"
)

type deviceCatalogSettingsStore interface {
	GetAppSetting(ctx context.Context, key string) (string, bool, error)
	SetAppSetting(ctx context.Context, key, value string) error
}

// remoteDeviceCatalog is what the updater persists: the last good download of
// each source, applied over the embedded data.
type remoteDeviceCatalog struct {
	Apple            map[string]string `json:"apple,omitempty"`
	Samsung          map[string]string `json:"samsung,omitempty"`
	AppleFetchedAt   int64             `json:"apple_fetched_at,omitempty"`
	SamsungFetchedAt int64             `json:"samsung_fetched_at,omitempty"`
}

// deviceCatalogUpdater keeps the device-name catalogs current without a
// release: it periodically downloads the public Apple and Samsung lists,
// validates them, stores the result and swaps the in-memory catalog. Any
// failure leaves the previous catalog (at worst the embedded one) in place.
type deviceCatalogUpdater struct {
	client       *http.Client
	appleIOSURL  string
	appleMacURL  string
	samsungURL   string
	requireHTTPS bool
	store        deviceCatalogSettingsStore
	now          func() time.Time
	remote       remoteDeviceCatalog
}

func newDeviceCatalogUpdater(store deviceCatalogSettingsStore) *deviceCatalogUpdater {
	return &deviceCatalogUpdater{
		client:       &http.Client{Timeout: deviceCatalogRequestTimout},
		appleIOSURL:  deviceCatalogAppleIOSURL,
		appleMacURL:  deviceCatalogAppleMacURL,
		samsungURL:   deviceCatalogSamsungURL,
		requireHTTPS: true,
		store:        store,
		now:          time.Now,
	}
}

// loadCache applies the last stored download, if any, so a restart does not
// fall back to the older embedded names while waiting for the next refresh.
func (u *deviceCatalogUpdater) loadCache(ctx context.Context) {
	raw, found, err := u.store.GetAppSetting(ctx, deviceCatalogCacheKey)
	if err != nil || !found {
		return
	}
	var cached remoteDeviceCatalog
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		return
	}
	u.remote = cached
	u.apply()
}

func (u *deviceCatalogUpdater) apply() {
	deviceCatalogCurrent.Store(buildDeviceCatalog(u.remote.Apple, u.remote.Samsung))
}

func (u *deviceCatalogUpdater) due() bool {
	oldest := u.remote.AppleFetchedAt
	if u.remote.SamsungFetchedAt < oldest || oldest == 0 {
		oldest = u.remote.SamsungFetchedAt
	}
	return u.now().Sub(time.Unix(oldest, 0)) >= deviceCatalogRefreshEvery
}

// refresh downloads every source independently: a source that fails or looks
// wrong is skipped without discarding the ones that worked.
func (u *deviceCatalogUpdater) refresh(ctx context.Context) error {
	embedded := buildDeviceCatalog(nil, nil)
	var errs []error
	changed := false

	apple := make(map[string]string)
	appleOK := true
	for _, url := range []string{u.appleIOSURL, u.appleMacURL} {
		body, err := u.fetch(ctx, url)
		if err != nil {
			appleOK = false
			errs = append(errs, fmt.Errorf("apple catalog: %w", err))
			break
		}
		for key, value := range parseDeviceCatalogJSON(body) {
			apple[key] = value
		}
	}
	// A truncated or wrong file must not replace good data.
	if appleOK && len(apple) >= len(embedded.apple)*8/10 {
		u.remote.Apple = apple
		u.remote.AppleFetchedAt = u.now().Unix()
		changed = true
	} else if appleOK {
		errs = append(errs, fmt.Errorf("apple catalog: only %d entries, expected about %d", len(apple), len(embedded.apple)))
	}

	body, err := u.fetch(ctx, u.samsungURL)
	if err != nil {
		errs = append(errs, fmt.Errorf("samsung catalog: %w", err))
	} else if samsung, err := parseSamsungSupportedDevicesCSV(body); err != nil {
		errs = append(errs, fmt.Errorf("samsung catalog: %w", err))
	} else if len(samsung) < len(embedded.samsung)*8/10 {
		errs = append(errs, fmt.Errorf("samsung catalog: only %d entries, expected about %d", len(samsung), len(embedded.samsung)))
	} else {
		u.remote.Samsung = samsung
		u.remote.SamsungFetchedAt = u.now().Unix()
		changed = true
	}

	if changed {
		u.apply()
		if encoded, err := json.Marshal(u.remote); err == nil {
			if err := u.store.SetAppSetting(ctx, deviceCatalogCacheKey, string(encoded)); err != nil {
				errs = append(errs, fmt.Errorf("store catalog: %w", err))
			}
		}
	}
	return errors.Join(errs...)
}

func (u *deviceCatalogUpdater) fetch(ctx context.Context, url string) ([]byte, error) {
	if u.requireHTTPS && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("refusing non-HTTPS source %q", url)
	}
	ctx, cancel := context.WithTimeout(ctx, deviceCatalogRequestTimout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ogs-swg-device-catalog")
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, deviceCatalogMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > deviceCatalogMaxBodyBytes {
		return nil, errors.New("response too large")
	}
	return body, nil
}

// run loads the cache and then checks daily whether a weekly refresh is due.
// It blocks; start it in a goroutine.
func (u *deviceCatalogUpdater) run(ctx context.Context) {
	u.loadCache(ctx)
	wait := deviceCatalogFirstCheck
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if u.due() {
			if err := u.refresh(ctx); err != nil {
				log.Printf("device catalog refresh: %v", err)
			}
		}
		wait = deviceCatalogCheckEvery
	}
}

// parseSamsungSupportedDevicesCSV reads Google's UTF-16 "supported devices" list
// and returns model code -> marketing name for Samsung SM- models. A few legacy
// codes map to several names; "Galaxy ..." wins, then alphabetical order.
func parseSamsungSupportedDevicesCSV(raw []byte) (map[string]string, error) {
	text, err := decodeUTF16LE(raw)
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}
	brandCol, nameCol, modelCol := -1, -1, -1
	for i, column := range header {
		switch strings.TrimSpace(column) {
		case "Retail Branding":
			brandCol = i
		case "Marketing Name":
			nameCol = i
		case "Model":
			modelCol = i
		}
	}
	if brandCol < 0 || nameCol < 0 || modelCol < 0 {
		return nil, errors.New("unexpected CSV header")
	}

	namesByCode := make(map[string]map[string]struct{})
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(record) <= brandCol || len(record) <= nameCol || len(record) <= modelCol {
			continue
		}
		code := strings.ToUpper(strings.TrimSpace(record[modelCol]))
		name := strings.TrimSpace(record[nameCol])
		if !strings.EqualFold(strings.TrimSpace(record[brandCol]), "samsung") || !strings.HasPrefix(code, "SM-") {
			continue
		}
		if name == "" || strings.EqualFold(name, code) || !isASCII(name) {
			continue
		}
		if namesByCode[code] == nil {
			namesByCode[code] = make(map[string]struct{})
		}
		namesByCode[code][name] = struct{}{}
	}

	out := make(map[string]string, len(namesByCode))
	for code, names := range namesByCode {
		list := make([]string, 0, len(names))
		for name := range names {
			list = append(list, name)
		}
		sort.Slice(list, func(i, j int) bool {
			gi, gj := strings.Contains(strings.ToLower(list[i]), "galaxy"), strings.Contains(strings.ToLower(list[j]), "galaxy")
			if gi != gj {
				return gi
			}
			return list[i] < list[j]
		})
		out[code] = list[0]
	}
	return out, nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func decodeUTF16LE(raw []byte) (string, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xFF, 0xFE})
	if len(raw)%2 != 0 {
		return "", errors.New("not UTF-16: odd byte count")
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	return string(utf16.Decode(units)), nil
}

// startDeviceCatalogUpdater keeps device names current in the background. It is
// off in demo mode and when OGS_DEVICE_CATALOG_AUTO_UPDATE=false (the embedded
// catalog is then used as is).
func (s *Server) startDeviceCatalogUpdater() {
	if s.config == nil || s.config.DemoMode || !s.config.DeviceCatalogAutoUpdate || s.store == nil {
		return
	}
	go newDeviceCatalogUpdater(s.store).run(context.Background())
}
