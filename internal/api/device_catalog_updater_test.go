package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

type fakeCatalogSettings struct{ values map[string]string }

func (f *fakeCatalogSettings) GetAppSetting(_ context.Context, key string) (string, bool, error) {
	v, ok := f.values[key]
	return v, ok, nil
}

func (f *fakeCatalogSettings) SetAppSetting(_ context.Context, key, value string) error {
	f.values[key] = value
	return nil
}

func encodeUTF16LEWithBOM(text string) []byte {
	units := utf16.Encode([]rune(text))
	out := []byte{0xFF, 0xFE}
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// samsungCSV builds a Google-style supported-devices CSV from a code -> name map.
func samsungCSV(models map[string]string) []byte {
	var b strings.Builder
	b.WriteString("Retail Branding,Marketing Name,Device,Model\n")
	codes := make([]string, 0, len(models))
	for code := range models {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		fmt.Fprintf(&b, "Samsung,%s,dev,%s\n", models[code], code)
	}
	return encodeUTF16LEWithBOM(b.String())
}

func restoreEmbeddedCatalog(t *testing.T) {
	t.Helper()
	currentDeviceCatalog()
	t.Cleanup(func() { deviceCatalogCurrent.Store(buildDeviceCatalog(nil, nil)) })
}

func jsonBytes(t *testing.T, v map[string]string) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// newCatalogSources serves plausible full-size sources (the embedded data plus
// one new model each) so the updater's size sanity checks pass.
func newCatalogSources(t *testing.T) (ios, mac, samsung string) {
	t.Helper()
	embedded := buildDeviceCatalog(nil, nil)
	iosData := parseDeviceCatalogJSON(appleIOSDeviceIdentifiersJSON)
	iosData["iPhone99,1"] = "iPhone 99"
	macData := parseDeviceCatalogJSON(appleMacDeviceIdentifiersJSON)
	samsungData := make(map[string]string, len(embedded.samsung)+1)
	for code, name := range embedded.samsung {
		samsungData[code] = name
	}
	samsungData["SM-Z9999"] = "Galaxy Z99"

	mux := http.NewServeMux()
	mux.HandleFunc("/ios.json", func(w http.ResponseWriter, _ *http.Request) { w.Write(jsonBytes(t, iosData)) })
	mux.HandleFunc("/mac.json", func(w http.ResponseWriter, _ *http.Request) { w.Write(jsonBytes(t, macData)) })
	mux.HandleFunc("/samsung.csv", func(w http.ResponseWriter, _ *http.Request) { w.Write(samsungCSV(samsungData)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL + "/ios.json", srv.URL + "/mac.json", srv.URL + "/samsung.csv"
}

func newTestCatalogUpdater(store deviceCatalogSettingsStore, ios, mac, samsung string) *deviceCatalogUpdater {
	u := newDeviceCatalogUpdater(store)
	u.appleIOSURL, u.appleMacURL, u.samsungURL = ios, mac, samsung
	u.requireHTTPS = false
	return u
}

func TestParseSamsungSupportedDevicesCSV(t *testing.T) {
	csvText := "Retail Branding,Marketing Name,Device,Model\n" +
		"Samsung,Galaxy S23 Ultra,dm3q,SM-S918B\n" +
		"Samsung,Elite,eliteltechn,SM-G1650\n" +
		"Samsung,Galaxy Folder2,fold2,SM-G1650\n" +
		"Samsung,SM-X999,x,SM-X999\n" +
		"Samsung,\u4e09\u661f W21 5G,w21,SM-W2021\n" +
		"Samsung,Gizmo Tablet,giz,GT-I9000\n" +
		"Xiaomi,Redmi Note,rn,2201123G\n"

	got, err := parseSamsungSupportedDevicesCSV(encodeUTF16LEWithBOM(csvText))
	if err != nil {
		t.Fatalf("parseSamsungSupportedDevicesCSV: %v", err)
	}
	want := map[string]string{"SM-S918B": "Galaxy S23 Ultra", "SM-G1650": "Galaxy Folder2"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for code, name := range want {
		if got[code] != name {
			t.Errorf("%s = %q want %q", code, got[code], name)
		}
	}

	if _, err := parseSamsungSupportedDevicesCSV([]byte("not utf-16 at all!")); err == nil {
		t.Error("garbage input was accepted")
	}
}

func TestDeviceCatalogUpdater_RefreshAddsNewModelsAndPersists(t *testing.T) {
	restoreEmbeddedCatalog(t)
	ios, mac, samsung := newCatalogSources(t)
	store := &fakeCatalogSettings{values: map[string]string{}}

	if got := resolveSubscriptionDeviceModel("iPhone99,1"); got != "iPhone99,1" {
		t.Fatalf("precondition: iPhone99,1 resolved to %q before the refresh", got)
	}

	u := newTestCatalogUpdater(store, ios, mac, samsung)
	if err := u.refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got := resolveSubscriptionDeviceModel("iPhone99,1"); got != "iPhone 99" {
		t.Errorf("new Apple model = %q want %q", got, "iPhone 99")
	}
	if got := resolveSubscriptionDeviceModel("SM-Z9999"); got != "Galaxy Z99" {
		t.Errorf("new Samsung model = %q want %q", got, "Galaxy Z99")
	}
	if got := resolveSubscriptionDeviceModel("SM-S918B"); got != "Galaxy S23 Ultra" {
		t.Errorf("known Samsung model = %q after refresh", got)
	}
	if store.values[deviceCatalogCacheKey] == "" {
		t.Fatal("the refreshed catalog was not persisted")
	}

	// A restart: back to the embedded data, then the cache restores the update.
	deviceCatalogCurrent.Store(buildDeviceCatalog(nil, nil))
	if got := resolveSubscriptionDeviceModel("iPhone99,1"); got != "iPhone99,1" {
		t.Fatalf("precondition: embedded catalog already knows iPhone99,1 (%q)", got)
	}
	restarted := newTestCatalogUpdater(store, ios, mac, samsung)
	restarted.loadCache(context.Background())
	if got := resolveSubscriptionDeviceModel("iPhone99,1"); got != "iPhone 99" {
		t.Errorf("after restart = %q want the cached name", got)
	}
}

func TestDeviceCatalogUpdater_BadDownloadsKeepTheCurrentCatalog(t *testing.T) {
	restoreEmbeddedCatalog(t)
	store := &fakeCatalogSettings{values: map[string]string{}}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failing.Close()
	tiny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".csv") {
			w.Write(samsungCSV(map[string]string{"SM-Z9999": "Galaxy Z99"}))
			return
		}
		w.Write([]byte(`{"iPhone99,1": "iPhone 99"}`))
	}))
	defer tiny.Close()

	for name, base := range map[string]string{"server error": failing.URL, "truncated lists": tiny.URL} {
		t.Run(name, func(t *testing.T) {
			u := newTestCatalogUpdater(store, base+"/ios.json", base+"/mac.json", base+"/samsung.csv")
			if err := u.refresh(context.Background()); err == nil {
				t.Fatal("refresh reported success for a bad download")
			}
			if got := resolveSubscriptionDeviceModel("iPhone99,1"); got != "iPhone99,1" {
				t.Errorf("a bad download changed the catalog: %q", got)
			}
			if got := resolveSubscriptionDeviceModel("iPhone15,3"); got != "iPhone 14 Pro Max" {
				t.Errorf("embedded name lost: %q", got)
			}
			if store.values[deviceCatalogCacheKey] != "" {
				t.Error("a bad download was persisted")
			}
		})
	}
}

func TestDeviceCatalogUpdater_RefusesPlainHTTPSources(t *testing.T) {
	restoreEmbeddedCatalog(t)
	ios, mac, samsung := newCatalogSources(t)
	u := newDeviceCatalogUpdater(&fakeCatalogSettings{values: map[string]string{}})
	u.appleIOSURL, u.appleMacURL, u.samsungURL = ios, mac, samsung // http:// test servers
	if err := u.refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "non-HTTPS") {
		t.Fatalf("refresh error = %v; want a non-HTTPS refusal", err)
	}
}

func TestDeviceCatalogUpdater_DueOnlyAfterAWeek(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	u := newTestCatalogUpdater(&fakeCatalogSettings{values: map[string]string{}}, "", "", "")
	u.now = func() time.Time { return now }

	if !u.due() {
		t.Error("never refreshed, but not due")
	}
	u.remote = remoteDeviceCatalog{AppleFetchedAt: now.Add(-time.Hour).Unix(), SamsungFetchedAt: now.Add(-time.Hour).Unix()}
	if u.due() {
		t.Error("refreshed an hour ago, but due")
	}
	u.remote.SamsungFetchedAt = now.Add(-8 * 24 * time.Hour).Unix()
	if !u.due() {
		t.Error("one source is 8 days old, but not due")
	}
}
