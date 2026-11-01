package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func sampleShareLinks() []string {
	ss := base64.StdEncoding.EncodeToString([]byte("2022-blake3-aes-128-gcm:server-key:user-key"))
	vmess := base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"vm","add":"vm.example.test","port":"443","id":"11111111-1111-1111-1111-111111111111","aid":"0","net":"ws","type":"none","scy":"auto","path":"/ws","host":"cdn.example.test","tls":"tls","sni":"vm.example.test"}`))
	return []string{
		"vless://22222222-2222-2222-2222-222222222222@r.example.test:443?security=reality&encryption=none&pbk=PUBKEY&fp=chrome&type=tcp&sni=front.example.test&sid=ab12&flow=xtls-rprx-vision#Reality+Node",
		"vless://33333333-3333-3333-3333-333333333333@w.example.test:443?security=tls&sni=w.example.test&type=ws&path=%2Fws&host=cdn.example.test&allowInsecure=1#WS+Node",
		"vmess://" + vmess,
		"trojan://trojan-pass@t.example.test:443?security=tls&sni=t.example.test&type=grpc&serviceName=svc#Trojan",
		"hysteria2://hy-pass@h.example.test:8443?sni=h.example.test&obfs=salamander&obfs-password=obfs-pass#Hy2",
		"ss://" + ss + "@s.example.test:8388#SS",
		"anytls://any-pass@a.example.test:443?sni=a.example.test&alpn=h2#AnyTLS",
		"naive+https://user:naive-pass@n.example.test:443?sni=n.example.test#Naive",
	}
}

func TestParseShareLink_RoundTripsEverySupportedScheme(t *testing.T) {
	nodes := parseShareLinks(sampleShareLinks())
	if len(nodes) != 8 {
		t.Fatalf("parsed %d nodes; want 8", len(nodes))
	}
	byName := map[string]proxyNode{}
	for _, n := range nodes {
		byName[n.Name] = n
	}
	if n := byName["Reality Node"]; n.User != "22222222-2222-2222-2222-222222222222" || n.param("pbk") != "PUBKEY" || n.Port != 443 {
		t.Fatalf("reality node = %#v", n)
	}
	if n := byName["vm"]; n.Scheme != "vmess" || n.param("net") != "ws" || n.param("path") != "/ws" {
		t.Fatalf("vmess node = %#v", n)
	}
	if n := byName["SS"]; n.Method != "2022-blake3-aes-128-gcm" || n.Password != "server-key:user-key" {
		t.Fatalf("ss node = %#v", n)
	}
	if n := byName["Naive"]; n.Scheme != "naive" || n.User != "user" || n.Password != "naive-pass" {
		t.Fatalf("naive node = %#v", n)
	}
}

func TestBuildClashConfig_MapsProtocolsAndSkipsNaive(t *testing.T) {
	out := string(buildClashConfig(sampleShareLinks(), nil))
	for _, want := range []string{
		`"type":"vless"`, `"reality-opts":{"public-key":"PUBKEY","short-id":"ab12"}`, `"flow":"xtls-rprx-vision"`,
		`"type":"vmess"`, `"ws-opts"`, `"grpc-service-name":"svc"`, `"type":"hysteria2"`, `"obfs-password":"obfs-pass"`,
		`"type":"ss"`, `"type":"anytls"`, `"skip-cert-verify":true`, "proxy-groups:", "MATCH,Proxy",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("clash config misses %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Naive") {
		t.Fatal("clash config includes the Naive node, which Clash cannot use")
	}
}

func TestBuildSingboxConfig_IsValidJSONWithOutbounds(t *testing.T) {
	var cfg struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
		Route     map[string]interface{}   `json:"route"`
	}
	if err := json.Unmarshal(buildSingboxConfig(sampleShareLinks(), nil), &cfg); err != nil {
		t.Fatal(err)
	}
	types := map[string]int{}
	for _, o := range cfg.Outbounds {
		types[o["type"].(string)]++
	}
	for _, want := range []string{"vless", "vmess", "trojan", "hysteria2", "shadowsocks", "anytls", "selector", "urltest", "direct"} {
		if types[want] == 0 {
			t.Fatalf("outbound type %q missing: %v", want, types)
		}
	}
	if types["vless"] != 2 || types["naive"] != 0 {
		t.Fatalf("outbound counts = %v", types)
	}
	if cfg.Route["final"] != "proxy" {
		t.Fatalf("route = %v", cfg.Route)
	}
}

func TestBuildFormats_DuplicateNamesStayUnique(t *testing.T) {
	link := "trojan://p@t.example.test:443?security=tls#Same"
	var cfg struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal(buildSingboxConfig([]string{link, link}, nil), &cfg); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, o := range cfg.Outbounds {
		tag := o["tag"].(string)
		if seen[tag] {
			t.Fatalf("duplicate tag %q", tag)
		}
		seen[tag] = true
	}
}

func TestPublicSub_FormatParameterSwitchesTheBody(t *testing.T) {
	server, _ := newSubscriptionGoldenServer(t)
	get := func(query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/s/"+migrationTestSrcToken+query, nil)
		req.SetPathValue("token", migrationTestSrcToken)
		req.Header.Set("User-Agent", "Happ/2.7.1")
		rec := httptest.NewRecorder()
		server.handlePublicSubscription(rec, req)
		return rec
	}

	plain := get("")
	if ct := plain.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("default content type = %q", ct)
	}
	clash := get("?format=clash")
	if clash.Code != http.StatusOK || !strings.HasPrefix(clash.Header().Get("Content-Type"), "text/yaml") || !strings.Contains(clash.Body.String(), "proxies:") {
		t.Fatalf("clash response code=%d type=%q body=%q", clash.Code, clash.Header().Get("Content-Type"), clash.Body.String())
	}
	sb := get("?format=singbox")
	if sb.Code != http.StatusOK || !json.Valid(sb.Body.Bytes()) || !strings.HasPrefix(sb.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("sing-box response code=%d type=%q", sb.Code, sb.Header().Get("Content-Type"))
	}
	if again := get(""); again.Body.String() != plain.Body.String() {
		t.Fatal("a format request changed what the default response returns from the cache")
	}
	if unknown := get("?format=nonsense"); unknown.Body.String() != plain.Body.String() {
		t.Fatal("an unknown format must fall back to the default body")
	}
}

func TestDirectSites_BecomeRulesInBothFormats(t *testing.T) {
	sites := []string{"panel.example.test", "domain:bank.example.test", "full:exact.example.test", "10.0.0.0/8", "192.0.2.7", "geosite:cn", "*.wild.example.test"}

	clash := string(buildClashConfig(sampleShareLinks(), sites))
	for _, want := range []string{
		"- DOMAIN-SUFFIX,panel.example.test,DIRECT", "- DOMAIN-SUFFIX,bank.example.test,DIRECT", "- DOMAIN,exact.example.test,DIRECT",
		"- IP-CIDR,10.0.0.0/8,DIRECT,no-resolve", "- IP-CIDR,192.0.2.7/32,DIRECT,no-resolve", "- GEOSITE,cn,DIRECT", "- DOMAIN-SUFFIX,wild.example.test,DIRECT",
	} {
		if !strings.Contains(clash, want) {
			t.Fatalf("clash rules miss %q:\n%s", want, clash)
		}
	}
	if strings.Index(clash, "DOMAIN-SUFFIX,panel.example.test") > strings.Index(clash, "MATCH,Proxy") {
		t.Fatal("direct rules must come before the catch-all")
	}

	var cfg struct {
		Route struct {
			Rules []map[string]interface{} `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal(buildSingboxConfig(sampleShareLinks(), sites), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Route.Rules) != 3 {
		t.Fatalf("sing-box rules = %v; want domain, domain_suffix and ip_cidr rules (geosite skipped)", cfg.Route.Rules)
	}
	for _, rule := range cfg.Route.Rules {
		if rule["outbound"] != "direct" {
			t.Fatalf("rule %v does not go direct", rule)
		}
	}
}

func TestPublicSub_FormatsCarryDirectSitesForNonHappClients(t *testing.T) {
	server, st := newSubscriptionGoldenServer(t)
	if err := st.UpdateSubscriptionHappConfig(context.Background(), core.SubscriptionHappConfig{DirectSites: []string{"panel.example.test"}}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/s/"+migrationTestSrcToken+"?format=clash", nil)
	req.SetPathValue("token", migrationTestSrcToken)
	req.Header.Set("User-Agent", "ClashMeta/1.18")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)
	if !strings.Contains(rec.Body.String(), "DOMAIN-SUFFIX,panel.example.test,DIRECT") {
		t.Fatalf("clash body has no direct rule:\n%s", rec.Body.String())
	}
}

func TestOutboundFromShareLink(t *testing.T) {
	links := sampleShareLinks()
	want := map[int]string{0: "vless", 2: "vmess", 3: "trojan", 4: "hysteria2", 5: "shadowsocks", 6: "anytls"}
	for index, outboundType := range want {
		outbound, err := outboundFromShareLink("  " + links[index] + "\n")
		if err != nil {
			t.Fatalf("link %d: %v", index, err)
		}
		if outbound["type"] != outboundType || outbound["server"] == "" || outbound["tag"] == "" {
			t.Fatalf("link %d produced %v", index, outbound)
		}
	}
	reality, _ := outboundFromShareLink(links[0])
	tls, _ := reality["tls"].(kv)
	if tls == nil || tls["reality"] == nil {
		t.Fatalf("reality link lost its tls block: %v", reality)
	}

	if _, err := outboundFromShareLink(links[7]); err == nil {
		t.Fatal("a Naive link must be refused")
	}
	for _, bad := range []string{"", "not a link", "http://example.test", "vless://broken"} {
		if _, err := outboundFromShareLink(bad); err == nil {
			t.Fatalf("%q was accepted", bad)
		}
	}
}
