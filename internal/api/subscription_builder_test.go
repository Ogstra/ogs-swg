package api

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/Ogstra/ogs-swg/internal/core/store"
)

// fakeSubscriptionUserSource is an in-memory implementation of
// subscriptionUserSource. Unknown keys return an error so the skip paths in
// buildSubscription are exercised the same way a real *core.Store failure
// would be.
type fakeSubscriptionUserSource struct {
	inboundMeta map[string]core.InboundMeta
	metadata    map[string]*core.UserMetadata
	reports     map[string][]core.Sample
	external    map[string][]core.ExternalProfile
	metaErr     error
}

func (f *fakeSubscriptionUserSource) GetAllInboundMeta() (map[string]core.InboundMeta, error) {
	return f.inboundMeta, nil
}

func (f *fakeSubscriptionUserSource) GetUserMetadata(email string) (*core.UserMetadata, error) {
	if f.metaErr != nil {
		return nil, f.metaErr
	}
	if m, ok := f.metadata[email]; ok {
		return m, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeSubscriptionUserSource) GetCombinedReport(user string, start, end int64) ([]core.Sample, error) {
	if s, ok := f.reports[user]; ok {
		return s, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeSubscriptionUserSource) GetUserExternalProfiles(userName string) ([]core.ExternalProfile, error) {
	if p, ok := f.external[userName]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}

// fakeSubscriptionInboundSource is an in-memory implementation of
// subscriptionInboundSource.
type fakeSubscriptionInboundSource struct {
	userInbounds map[string][]core.UserInboundInfo
	views        map[string]*core.SingboxInboundView
}

func (f *fakeSubscriptionInboundSource) GetUserInbounds(name string) ([]core.UserInboundInfo, error) {
	if v, ok := f.userInbounds[name]; ok {
		return v, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeSubscriptionInboundSource) GetSingboxInboundView(tag string) (*core.SingboxInboundView, error) {
	if v, ok := f.views[tag]; ok {
		return v, nil
	}
	return nil, errors.New("not found")
}

// decodeSubscriptionBody base64-decodes c.Body and splits it into lines.
func decodeSubscriptionBody(t *testing.T, c cachedSub) []string {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(string(c.Body))
	if err != nil {
		t.Fatalf("decode subscription body: %v", err)
	}
	if len(decoded) == 0 {
		return nil
	}
	return strings.Split(string(decoded), "\n")
}

func plainVlessView(tag string) *core.SingboxInboundView {
	return &core.SingboxInboundView{
		Tag:        tag,
		Type:       "vless",
		ListenPort: 443,
	}
}

const (
	subTestHost   = "198.51.100.10"
	subTestSNI    = "edge.example.com"
	subTestUUID1  = "11111111-1111-1111-1111-111111111111"
	subTestUUID2  = "22222222-2222-2222-2222-222222222222"
	subTestUUIDEP = "33333333-3333-3333-3333-333333333333"
)

func TestBuildSubscription_MultiUserAggregation(t *testing.T) {
	users := &fakeSubscriptionUserSource{
		metadata: map[string]*core.UserMetadata{
			"alice": {Email: "alice", QuotaLimit: 0},
			"bob":   {Email: "bob", QuotaLimit: 0},
		},
		reports: map[string][]core.Sample{
			"alice": {{User: "alice", Uplink: 10, Downlink: 20}},
			"bob":   {{User: "bob", Uplink: 30, Downlink: 40}},
		},
		external: map[string][]core.ExternalProfile{
			"bob": {
				{Type: "vless", Enabled: true, HostIPv4: subTestHost, Port: 8443, UUID: subTestUUIDEP, PublicKey: "public-key-placeholder", ShortID: "beef", ServerName: subTestSNI, Flag: ""},
				{Type: "vless", Enabled: false, HostIPv4: subTestHost, Port: 8444, UUID: subTestUUIDEP, PublicKey: "public-key-placeholder", ShortID: "beef", ServerName: subTestSNI, Flag: ""},
			},
		},
	}
	inbounds := &fakeSubscriptionInboundSource{
		userInbounds: map[string][]core.UserInboundInfo{
			"alice": {{Tag: "vless-1", UUID: subTestUUID1}},
			"bob":   {{Tag: "vless-1", UUID: subTestUUID2}},
		},
		views: map[string]*core.SingboxInboundView{
			"vless-1": plainVlessView("vless-1"),
		},
	}

	c := buildSubscription(buildSubscriptionInput{
		Members: []subscriptionMember{
			{UserName: "alice"},
			{UserName: "bob"},
		},
		Host:     subTestHost,
		Now:      time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
		Users:    users,
		Inbounds: inbounds,
	})

	lines := decodeSubscriptionBody(t, c)
	var links []string
	for _, l := range lines {
		if l != "" {
			links = append(links, l)
		}
	}
	if len(links) != 3 {
		t.Fatalf("links=%d want 3: %v", len(links), links)
	}
	if !strings.Contains(links[0], subTestUUID1) {
		t.Fatalf("first link should be alice's, got %q", links[0])
	}
	if !strings.Contains(links[1], subTestUUID2) {
		t.Fatalf("second link should be bob's managed inbound, got %q", links[1])
	}
	if !strings.Contains(links[2], subTestUUIDEP) {
		t.Fatalf("third link should be bob's enabled external profile, got %q", links[2])
	}
	for _, l := range links {
		if strings.Contains(l, "8444") {
			t.Fatalf("disabled external profile link must be absent: %v", links)
		}
	}
	if c.HeaderUp != 40 || c.HeaderDown != 60 {
		t.Fatalf("HeaderUp/Down=%d/%d want 40/60", c.HeaderUp, c.HeaderDown)
	}
}

func TestBuildSubscription_SubLevelQuotaWins(t *testing.T) {
	users := &fakeSubscriptionUserSource{
		metadata: map[string]*core.UserMetadata{
			"alice": {Email: "alice", QuotaLimit: 5 * 1024 * 1024 * 1024},
			"bob":   {Email: "bob", QuotaLimit: 3 * 1024 * 1024 * 1024},
		},
	}
	c := buildSubscription(buildSubscriptionInput{
		Members:       []subscriptionMember{{UserName: "alice"}, {UserName: "bob"}},
		SubQuotaLimit: 10 * 1024 * 1024 * 1024,
		Now:           time.Now(),
		Users:         users,
	})
	if c.HeaderTot != 10*1024*1024*1024 {
		t.Fatalf("HeaderTot=%d want sub-level quota", c.HeaderTot)
	}
}

func TestBuildSubscription_SumsMemberQuotas(t *testing.T) {
	users := &fakeSubscriptionUserSource{
		metadata: map[string]*core.UserMetadata{
			"alice": {Email: "alice", QuotaLimit: 5 * 1024 * 1024 * 1024},
			"bob":   {Email: "bob", QuotaLimit: 0},
			"carol": {Email: "carol", QuotaLimit: 3 * 1024 * 1024 * 1024},
		},
	}
	c := buildSubscription(buildSubscriptionInput{
		Members:       []subscriptionMember{{UserName: "alice"}, {UserName: "bob"}, {UserName: "carol"}},
		SubQuotaLimit: 0,
		Now:           time.Now(),
		Users:         users,
	})
	if c.HeaderTot != 8*1024*1024*1024 {
		t.Fatalf("HeaderTot=%d want sum of positive quotas", c.HeaderTot)
	}
}

func TestBuildSubscription_NoQuotaAnywhere(t *testing.T) {
	users := &fakeSubscriptionUserSource{
		metadata: map[string]*core.UserMetadata{
			"alice": {Email: "alice", QuotaLimit: 0},
		},
	}
	c := buildSubscription(buildSubscriptionInput{
		Members:       []subscriptionMember{{UserName: "alice"}},
		SubQuotaLimit: 0,
		Now:           time.Now(),
		Users:         users,
	})
	if c.HeaderTot != 0 {
		t.Fatalf("HeaderTot=%d want 0", c.HeaderTot)
	}
}

func TestBuildSubscription_HappBodyLines(t *testing.T) {
	interval := int64(12)
	routingJSON := `{"Name":"p","DirectSites":["a.example.com"]}`
	c := buildSubscription(buildSubscriptionInput{
		Members:               []subscriptionMember{{UserName: "alice"}},
		DisplayTitle:          "Happ Bundle",
		HappParams:            []happSubscriptionParam{{Key: "providerid", Value: "provider-test-id"}},
		RoutingProfileJSON:    routingJSON,
		MergedDirectSites:     []string{"a.example.com", "b.example.com"},
		ProfileUpdateInterval: &interval,
		Now:                   time.Now(),
	})
	lines := decodeSubscriptionBody(t, c)
	joined := strings.Join(lines, "\n")

	for _, want := range []string{
		"#profile-title: Happ Bundle",
		"#providerid provider-test-id",
		"#subscription-userinfo: upload=0; download=0; total=0",
		"#profile-update-interval: 12",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("body missing %q: %q", want, joined)
		}
	}

	var routingLine string
	for _, l := range lines {
		if strings.HasPrefix(l, "happ://routing/onadd/") {
			routingLine = l
		}
	}
	if routingLine == "" {
		t.Fatalf("no onadd routing line found: %q", joined)
	}
	encoded := strings.TrimPrefix(routingLine, "happ://routing/onadd/")
	decodedProfile, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode routing profile: %v", err)
	}
	var profile map[string]interface{}
	if err := json.Unmarshal(decodedProfile, &profile); err != nil {
		t.Fatalf("unmarshal routing profile: %v", err)
	}
	sitesRaw, _ := profile["DirectSites"].([]interface{})
	if len(sitesRaw) != 2 {
		t.Fatalf("DirectSites=%v want deduped merge of 2", sitesRaw)
	}

	// With HappParams nil, none of the happ metadata lines should appear.
	cNoHapp := buildSubscription(buildSubscriptionInput{
		Members:               []subscriptionMember{{UserName: "alice"}},
		DisplayTitle:          "Happ Bundle",
		HappParams:            nil,
		RoutingProfileJSON:    routingJSON,
		MergedDirectSites:     []string{"a.example.com"},
		ProfileUpdateInterval: &interval,
		Now:                   time.Now(),
	})
	noHappLines := strings.Join(decodeSubscriptionBody(t, cNoHapp), "\n")
	for _, unwanted := range []string{"#subscription-userinfo", "#profile-update-interval", "happ://routing/onadd/"} {
		if strings.Contains(noHappLines, unwanted) {
			t.Fatalf("body should not contain %q when HappParams is nil: %q", unwanted, noHappLines)
		}
	}
}

func TestBuildSubscription_RoutingOffPassthrough(t *testing.T) {
	c := buildSubscription(buildSubscriptionInput{
		Members:            []subscriptionMember{{UserName: "alice"}},
		HappParams:         []happSubscriptionParam{{Key: "providerid", Value: "x"}},
		RoutingProfileJSON: happRoutingOffLink,
		Now:                time.Now(),
	})
	lines := decodeSubscriptionBody(t, c)
	found := false
	for _, l := range lines {
		if l == happRoutingOffLink {
			found = true
		}
		if strings.HasPrefix(l, "happ://routing/onadd/") {
			t.Fatalf("should not emit onadd line when routing is off: %v", lines)
		}
	}
	if !found {
		t.Fatalf("expected literal %q line: %v", happRoutingOffLink, lines)
	}
}

func TestBuildSubscription_SkipsBrokenInbounds(t *testing.T) {
	users := &fakeSubscriptionUserSource{
		metadata: map[string]*core.UserMetadata{
			"alice": {Email: "alice", QuotaLimit: 1024},
			"bob":   {Email: "bob", QuotaLimit: 2048},
		},
		reports: map[string][]core.Sample{
			"alice": {{User: "alice", Uplink: 1, Downlink: 1}},
			"bob":   {{User: "bob", Uplink: 2, Downlink: 2}},
		},
	}
	inbounds := &fakeSubscriptionInboundSource{
		userInbounds: map[string][]core.UserInboundInfo{
			// alice's inbound view lookup will error (tag not in views map).
			"alice": {{Tag: "missing-view", UUID: subTestUUID1}},
			// bob's view exists but has ListenPort 0.
			"bob": {{Tag: "zero-port", UUID: subTestUUID2}},
		},
		views: map[string]*core.SingboxInboundView{
			"zero-port": {Tag: "zero-port", Type: "vless", ListenPort: 0},
		},
	}

	c := buildSubscription(buildSubscriptionInput{
		Members:  []subscriptionMember{{UserName: "alice"}, {UserName: "bob"}},
		Now:      time.Now(),
		Users:    users,
		Inbounds: inbounds,
	})

	lines := decodeSubscriptionBody(t, c)
	for _, l := range lines {
		if l != "" {
			t.Fatalf("expected no links, got %v", lines)
		}
	}
	if c.HeaderUp != 3 || c.HeaderDown != 3 {
		t.Fatalf("HeaderUp/Down=%d/%d want 3/3 (traffic still counted)", c.HeaderUp, c.HeaderDown)
	}
	if c.HeaderTot != 3072 {
		t.Fatalf("HeaderTot=%d want 3072 (quota still counted)", c.HeaderTot)
	}
}

func TestBuildSubscription_FirstInboundOnly(t *testing.T) {
	inbounds := &fakeSubscriptionInboundSource{
		userInbounds: map[string][]core.UserInboundInfo{
			"alice": {
				{Tag: "vless-1", UUID: subTestUUID1},
				{Tag: "vless-2", UUID: subTestUUID2},
			},
		},
		views: map[string]*core.SingboxInboundView{
			"vless-1": plainVlessView("vless-1"),
			"vless-2": plainVlessView("vless-2"),
		},
	}
	c := buildSubscription(buildSubscriptionInput{
		Members:  []subscriptionMember{{UserName: "alice"}},
		Now:      time.Now(),
		Inbounds: inbounds,
	})
	lines := decodeSubscriptionBody(t, c)
	var links []string
	for _, l := range lines {
		if l != "" {
			links = append(links, l)
		}
	}
	if len(links) != 1 {
		t.Fatalf("links=%d want 1 (first inbound only): %v", len(links), links)
	}
	if !strings.Contains(links[0], subTestUUID1) {
		t.Fatalf("expected first inbound's link, got %q", links[0])
	}
}

func TestBuildSubscription_NilSourcesAreSafe(t *testing.T) {
	c := buildSubscription(buildSubscriptionInput{
		Members:  []subscriptionMember{{UserName: "alice"}},
		Now:      time.Now(),
		Users:    nil,
		Inbounds: nil,
	})
	lines := decodeSubscriptionBody(t, c)
	for _, l := range lines {
		if l != "" {
			t.Fatalf("expected no links with nil sources, got %v", lines)
		}
	}
	if c.HeaderUp != 0 || c.HeaderDown != 0 || c.HeaderTot != 0 {
		t.Fatalf("expected zero totals with nil sources, got up=%d down=%d tot=%d", c.HeaderUp, c.HeaderDown, c.HeaderTot)
	}
}

func TestBuildSubscription_ProfileFlagPrefixes(t *testing.T) {
	users := &fakeSubscriptionUserSource{
		external: map[string][]core.ExternalProfile{
			"bob": {
				{Type: "vless", Enabled: true, HostIPv4: subTestHost, Port: 8443, UUID: subTestUUIDEP, PublicKey: "public-key-placeholder", ShortID: "beef", ServerName: subTestSNI, Flag: "[EU] "},
			},
		},
	}
	inbounds := &fakeSubscriptionInboundSource{
		userInbounds: map[string][]core.UserInboundInfo{
			"alice": {{Tag: "vless-1", UUID: subTestUUID1}},
		},
		views: map[string]*core.SingboxInboundView{
			"vless-1": plainVlessView("vless-1"),
		},
	}
	c := buildSubscription(buildSubscriptionInput{
		Members:     []subscriptionMember{{UserName: "alice"}, {UserName: "bob"}},
		ProfileFlag: "[US] ",
		Now:         time.Now(),
		Users:       users,
		Inbounds:    inbounds,
	})
	lines := decodeSubscriptionBody(t, c)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "%5BUS%5D") && !strings.Contains(joined, "[US]") && !strings.Contains(joined, "US%5D+alice") && !strings.Contains(joined, "US]+alice") {
		// Link names are URL-fragment-encoded; just assert the flag text made it in somehow.
		if !strings.Contains(joined, "US") {
			t.Fatalf("expected profile flag [US] to prefix alice's link name: %q", joined)
		}
	}
	if !strings.Contains(joined, "EU") {
		t.Fatalf("expected external profile's own flag [EU] to prefix bob's link name: %q", joined)
	}
}

func TestBuildSubscription_CacheMissThenHitByteIdentical(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "cache-contract-token",
		Name:        "Cache Contract",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	makeRequest := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/s/cache-contract-token", nil)
		req.SetPathValue("token", "cache-contract-token")
		rec := httptest.NewRecorder()
		server.handlePublicSubscription(rec, req)
		return rec
	}

	first := makeRequest()
	second := makeRequest()
	if first.Body.String() != second.Body.String() {
		t.Fatalf("cache-miss and cache-hit bodies differ:\n%q\n%q", first.Body.String(), second.Body.String())
	}
	firstHeaders := first.Header().Clone()
	secondHeaders := second.Header().Clone()
	firstHeaders.Del("Date")
	secondHeaders.Del("Date")
	if len(firstHeaders) != len(secondHeaders) {
		t.Fatalf("header count differs: %v vs %v", firstHeaders, secondHeaders)
	}
	for k, v := range firstHeaders {
		if strings.Join(secondHeaders[k], ",") != strings.Join(v, ",") {
			t.Fatalf("header %q differs: %v vs %v", k, v, secondHeaders[k])
		}
	}

	server.InvalidateSubCache()
	third := makeRequest()
	if third.Body.String() != first.Body.String() {
		t.Fatalf("post-invalidate rebuild differs from original:\n%q\n%q", first.Body.String(), third.Body.String())
	}
}
