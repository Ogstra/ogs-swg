package api

// REGRESSION CONTRACT (phase 57, D-05/NTFY-11):
// internal/api/testdata/public_sub_hwid_golden.json is a byte-exact capture of
// handlePublicSubscription's output taken BEFORE any new-HWID tracking existed.
// Every plan in phase 57 that touches handlers_public_sub.go MUST re-run, and
// MUST see pass, all of:
//   go test ./internal/api -run TestPublicSubscriptionHWIDGolden -count=1
//   go test ./internal/api -run TestPublicSubscriptionGolden -count=1
// A mismatch is a hard failure, not a fixture to regenerate. Never re-run with
// -updatehwidgolden after this plan.

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"reflect"
	"testing"
)

// updateHWIDGolden rewrites internal/api/testdata/public_sub_hwid_golden.json
// from the current (unmodified) behavior of handlePublicSubscription. Run with:
//
//	go test ./internal/api -run TestPublicSubscriptionHWIDGolden -updatehwidgolden -count=1
var updateHWIDGolden = flag.Bool("updatehwidgolden", false, "rewrite internal/api/testdata/public_sub_hwid_golden.json")

func TestPublicSubscriptionHWIDGolden(t *testing.T) {
	server, _ := newSubscriptionGoldenServer(t)

	golden := map[string]interface{}{}

	golden["no_hwid_header"] = captureSubResponse(t, server, "/s/single-token", nil)
	golden["hwid_first_request"] = captureSubResponse(t, server, "/s/multi-token", func(r *http.Request) {
		r.Header.Set("X-Hwid", "hwid-device-aaa")
	})
	golden["hwid_same_repeat"] = captureSubResponse(t, server, "/s/multi-token", func(r *http.Request) {
		r.Header.Set("X-Hwid", "hwid-device-aaa")
	})
	golden["hwid_second_device_same_sub"] = captureSubResponse(t, server, "/s/multi-token", func(r *http.Request) {
		r.Header.Set("X-Hwid", "hwid-device-bbb")
	})
	golden["hwid_same_device_other_sub"] = captureSubResponse(t, server, "/s/single-token", func(r *http.Request) {
		r.Header.Set("X-Hwid", "hwid-device-aaa")
	})
	golden["hwid_full_metadata"] = captureSubResponse(t, server, "/s/single-token", func(r *http.Request) {
		r.Header.Set("X-Hwid", "hwid-device-ccc")
		r.Header.Set("X-Device-Model", "iPhone15,2")
		r.Header.Set("X-Device-OS", "iOS")
		r.Header.Set("X-Ver-Os", "17.4")
		r.Header.Set("X-App-Version", "2.7.1")
		r.Header.Set("CF-IPCountry", "AR")
		r.Header.Set("User-Agent", "Happ/2.7.1")
	})
	golden["hwid_empty_header"] = captureSubResponse(t, server, "/s/single-token", func(r *http.Request) {
		r.Header.Set("X-Hwid", "")
	})
	golden["hwid_hy2_single"] = captureSubResponse(t, server, "/s/hy2-token", func(r *http.Request) {
		r.Header.Set("X-Hwid", "hwid-device-ddd")
	})

	if !reflect.DeepEqual(golden["hwid_first_request"], golden["hwid_same_repeat"]) {
		t.Fatalf("hwid_first_request and hwid_same_repeat must be identical (repeated HWID request must stay byte-identical)")
	}

	got, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	got = append(got, '\n')

	const goldenPath = "testdata/public_sub_hwid_golden.json"

	if *updateHWIDGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -updatehwidgolden first): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden mismatch:\n%s", diffFirstNLines(got, want, 40))
	}
}
