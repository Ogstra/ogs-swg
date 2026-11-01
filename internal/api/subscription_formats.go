package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Subscription formats other than the default share-link list. They are built
// from the share links the builder already produces, so every protocol detail
// (Reality keys, SNI fallbacks, transports) stays defined in one place.

const (
	subFormatClash   = "clash"
	subFormatSingbox = "singbox"
)

// proxyNode is one share link parsed back into its parts.
type proxyNode struct {
	Scheme   string // vless, vmess, trojan, hysteria2, ss, anytls, naive
	Name     string
	Server   string
	Port     int
	User     string // vless/vmess uuid, naive username
	Password string
	Method   string // shadowsocks cipher, vmess security
	AlterID  int
	Params   map[string]string
}

func (n proxyNode) param(key string) string { return n.Params[key] }

func (n proxyNode) tlsEnabled() bool {
	switch n.Scheme {
	case "trojan", "hysteria2", "anytls", "naive":
		return true
	}
	security := n.param("security")
	return security == "tls" || security == "reality" || n.param("tls") == "tls"
}

func (n proxyNode) insecure() bool {
	return n.param("allowInsecure") == "1" || n.param("insecure") == "1"
}

func (n proxyNode) alpn() []string {
	raw := n.param("alpn")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

func (n proxyNode) transport() string {
	t := n.param("type")
	if t == "" {
		t = n.param("net")
	}
	if t == "" {
		return "tcp"
	}
	return t
}

func splitHostPort(hostport string) (string, int, bool) {
	idx := strings.LastIndex(hostport, ":")
	if idx < 0 {
		return "", 0, false
	}
	port, err := strconv.Atoi(hostport[idx+1:])
	if err != nil {
		return "", 0, false
	}
	return strings.Trim(hostport[:idx], "[]"), port, true
}

func fragmentName(u *url.URL) string {
	name, err := url.QueryUnescape(u.EscapedFragment())
	if err != nil {
		return u.Fragment
	}
	return name
}

func parseShareLink(link string) (proxyNode, bool) {
	scheme, rest, found := strings.Cut(link, "://")
	if !found {
		return proxyNode{}, false
	}
	if scheme == "vmess" {
		return parseVmessLink(rest)
	}
	u, err := url.Parse(link)
	if err != nil {
		return proxyNode{}, false
	}
	host, port, ok := splitHostPort(u.Host)
	if !ok {
		return proxyNode{}, false
	}
	node := proxyNode{Scheme: scheme, Name: fragmentName(u), Server: host, Port: port, Params: map[string]string{}}
	for key, values := range u.Query() {
		if len(values) > 0 {
			node.Params[key] = values[0]
		}
	}
	switch scheme {
	case "vless", "trojan", "hysteria2", "anytls":
		if u.User == nil {
			return proxyNode{}, false
		}
		secret := u.User.Username()
		if scheme == "vless" {
			node.User = secret
		} else {
			node.Password = secret
		}
	case "naive+https", "naive+quic":
		if u.User == nil {
			return proxyNode{}, false
		}
		node.Scheme = "naive"
		node.User = u.User.Username()
		node.Password, _ = u.User.Password()
		if scheme == "naive+quic" {
			node.Params["network"] = "udp"
		}
	case "ss":
		if u.User == nil {
			return proxyNode{}, false
		}
		raw := u.User.Username()
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(raw)
		}
		if err != nil {
			return proxyNode{}, false
		}
		method, password, ok := strings.Cut(string(decoded), ":")
		if !ok {
			return proxyNode{}, false
		}
		node.Method, node.Password = method, password
	default:
		return proxyNode{}, false
	}
	return node, true
}

func parseVmessLink(encoded string) (proxyNode, bool) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		return proxyNode{}, false
	}
	var payload map[string]string
	if err := json.Unmarshal(raw, &payload); err != nil {
		return proxyNode{}, false
	}
	port, err := strconv.Atoi(payload["port"])
	if err != nil || payload["add"] == "" || payload["id"] == "" {
		return proxyNode{}, false
	}
	alterID, _ := strconv.Atoi(payload["aid"])
	node := proxyNode{
		Scheme:  "vmess",
		Name:    payload["ps"],
		Server:  payload["add"],
		Port:    port,
		User:    payload["id"],
		Method:  payload["scy"],
		AlterID: alterID,
		Params:  map[string]string{},
	}
	for _, key := range []string{"net", "tls", "sni", "alpn", "allowInsecure", "path", "host"} {
		if value := payload[key]; value != "" {
			node.Params[key] = value
		}
	}
	if node.Params["net"] == "h2" {
		node.Params["net"] = "http"
	}
	return node, true
}

func parseShareLinks(links []string) []proxyNode {
	nodes := make([]proxyNode, 0, len(links))
	for _, link := range links {
		if node, ok := parseShareLink(link); ok {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// uniqueNodeNames keeps names distinct, which both Clash and sing-box require.
func uniqueNodeNames(nodes []proxyNode) []proxyNode {
	seen := map[string]int{}
	out := make([]proxyNode, len(nodes))
	for i, node := range nodes {
		name := strings.TrimSpace(node.Name)
		if name == "" {
			name = node.Server + ":" + strconv.Itoa(node.Port)
		}
		seen[name]++
		if seen[name] > 1 {
			name += " " + strconv.Itoa(seen[name])
		}
		node.Name = name
		out[i] = node
	}
	return out
}

type kv = map[string]interface{}

// ---- Clash (mihomo / Clash Meta) ----

func clashProxy(n proxyNode) (kv, bool) {
	p := kv{"name": n.Name, "server": n.Server, "port": n.Port, "udp": true}
	transport := n.transport()
	applyTransport := func() {
		switch transport {
		case "ws":
			p["network"] = "ws"
			opts := kv{}
			if path := n.param("path"); path != "" {
				opts["path"] = path
			}
			if host := n.param("host"); host != "" {
				opts["headers"] = kv{"Host": host}
			}
			if len(opts) > 0 {
				p["ws-opts"] = opts
			}
		case "httpupgrade":
			p["network"] = "ws"
			opts := kv{"v2ray-http-upgrade": true}
			if path := n.param("path"); path != "" {
				opts["path"] = path
			}
			if host := n.param("host"); host != "" {
				opts["headers"] = kv{"Host": host}
			}
			p["ws-opts"] = opts
		case "grpc":
			p["network"] = "grpc"
			if service := n.param("serviceName"); service != "" {
				p["grpc-opts"] = kv{"grpc-service-name": service}
			} else if path := n.param("path"); path != "" {
				p["grpc-opts"] = kv{"grpc-service-name": path}
			}
		case "http":
			p["network"] = "h2"
			opts := kv{}
			if path := n.param("path"); path != "" {
				opts["path"] = path
			}
			if host := n.param("host"); host != "" {
				opts["host"] = []string{host}
			}
			if len(opts) > 0 {
				p["h2-opts"] = opts
			}
		default:
			p["network"] = "tcp"
		}
	}
	applyTLS := func() {
		if sni := n.param("sni"); sni != "" {
			p["servername"] = sni
		}
		if alpn := n.alpn(); len(alpn) > 0 {
			p["alpn"] = alpn
		}
		if n.insecure() {
			p["skip-cert-verify"] = true
		}
	}

	switch n.Scheme {
	case "vless":
		p["type"], p["uuid"] = "vless", n.User
		applyTransport()
		if n.tlsEnabled() {
			p["tls"] = true
			applyTLS()
			if fp := n.param("fp"); fp != "" {
				p["client-fingerprint"] = fp
			}
		}
		if n.param("security") == "reality" {
			p["reality-opts"] = kv{"public-key": n.param("pbk"), "short-id": n.param("sid")}
		}
		if flow := n.param("flow"); flow != "" {
			p["flow"] = flow
		}
		if n.param("packetEncoding") != "" {
			p["packet-encoding"] = n.param("packetEncoding")
		}
	case "vmess":
		p["type"], p["uuid"], p["alterId"] = "vmess", n.User, n.AlterID
		p["cipher"] = firstNonEmpty(n.Method, "auto")
		applyTransport()
		if n.tlsEnabled() {
			p["tls"] = true
			applyTLS()
		}
	case "trojan":
		p["type"], p["password"] = "trojan", n.Password
		applyTransport()
		applyTLS()
	case "hysteria2":
		p["type"], p["password"] = "hysteria2", n.Password
		applyTLS()
		delete(p, "servername")
		if sni := n.param("sni"); sni != "" {
			p["sni"] = sni
		}
		if obfs := n.param("obfs"); obfs != "" {
			p["obfs"] = obfs
			p["obfs-password"] = n.param("obfs-password")
		}
	case "ss":
		p["type"], p["cipher"], p["password"] = "ss", n.Method, n.Password
	case "anytls":
		p["type"], p["password"] = "anytls", n.Password
		applyTLS()
		delete(p, "servername")
		if sni := n.param("sni"); sni != "" {
			p["sni"] = sni
		}
	default:
		// Clash has no Naive outbound.
		return nil, false
	}
	return p, true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// buildClashConfig renders a mihomo-compatible profile. Entries are JSON flow
// mappings, which are valid YAML and need no YAML dependency.
func buildClashConfig(links []string, directSites []string) []byte {
	var sb strings.Builder
	sb.WriteString("mixed-port: 7890\nallow-lan: false\nmode: rule\nlog-level: info\nproxies:\n")
	var names []string
	for _, node := range uniqueNodeNames(parseShareLinks(links)) {
		proxy, ok := clashProxy(node)
		if !ok {
			continue
		}
		raw, err := json.Marshal(proxy)
		if err != nil {
			continue
		}
		sb.WriteString("  - " + string(raw) + "\n")
		names = append(names, node.Name)
	}
	if len(names) == 0 {
		sb.WriteString("proxy-groups: []\nrules:\n  - MATCH,DIRECT\n")
		return []byte(sb.String())
	}
	var rules []string
	for _, entry := range directSites {
		if rule, ok := parseDirectSite(entry); ok {
			if line, ok := rule.clash(); ok {
				rules = append(rules, line)
			}
		}
	}
	selectGroup, _ := json.Marshal(kv{"name": "Proxy", "type": "select", "proxies": append([]string{"Auto"}, append(names, "DIRECT")...)})
	autoGroup, _ := json.Marshal(kv{"name": "Auto", "type": "url-test", "proxies": names, "url": "https://www.gstatic.com/generate_204", "interval": 300})
	sb.WriteString("proxy-groups:\n  - " + string(selectGroup) + "\n  - " + string(autoGroup) + "\nrules:\n")
	for _, line := range rules {
		sb.WriteString("  - " + line + "\n")
	}
	sb.WriteString("  - MATCH,Proxy\n")
	return []byte(sb.String())
}

// ---- sing-box ----

func singboxOutbound(n proxyNode) (kv, bool) {
	o := kv{"tag": n.Name, "server": n.Server, "server_port": n.Port}
	tls := func() {
		t := kv{"enabled": true}
		if sni := n.param("sni"); sni != "" {
			t["server_name"] = sni
		}
		if alpn := n.alpn(); len(alpn) > 0 {
			t["alpn"] = alpn
		}
		if n.insecure() {
			t["insecure"] = true
		}
		if fp := n.param("fp"); fp != "" {
			t["utls"] = kv{"enabled": true, "fingerprint": fp}
		}
		if n.param("security") == "reality" {
			t["reality"] = kv{"enabled": true, "public_key": n.param("pbk"), "short_id": n.param("sid")}
		}
		o["tls"] = t
	}
	transport := func() {
		switch n.transport() {
		case "ws", "httpupgrade", "http":
			t := kv{"type": n.transport()}
			if path := n.param("path"); path != "" {
				t["path"] = path
			}
			if host := n.param("host"); host != "" {
				if n.transport() == "ws" {
					t["headers"] = kv{"Host": host}
				} else {
					t["host"] = host
				}
			}
			o["transport"] = t
		case "grpc":
			t := kv{"type": "grpc"}
			if service := firstNonEmpty(n.param("serviceName"), n.param("path")); service != "" {
				t["service_name"] = service
			}
			o["transport"] = t
		}
	}

	switch n.Scheme {
	case "vless":
		o["type"], o["uuid"] = "vless", n.User
		if flow := n.param("flow"); flow != "" {
			o["flow"] = flow
		}
		if n.param("packetEncoding") != "" {
			o["packet_encoding"] = n.param("packetEncoding")
		}
		if n.tlsEnabled() {
			tls()
		}
		transport()
	case "vmess":
		o["type"], o["uuid"], o["alter_id"] = "vmess", n.User, n.AlterID
		o["security"] = firstNonEmpty(n.Method, "auto")
		if n.tlsEnabled() {
			tls()
		}
		transport()
	case "trojan":
		o["type"], o["password"] = "trojan", n.Password
		tls()
		transport()
	case "hysteria2":
		o["type"], o["password"] = "hysteria2", n.Password
		if obfs := n.param("obfs"); obfs != "" {
			o["obfs"] = kv{"type": obfs, "password": n.param("obfs-password")}
		}
		tls()
	case "ss":
		o["type"], o["method"], o["password"] = "shadowsocks", n.Method, n.Password
	case "anytls":
		o["type"], o["password"] = "anytls", n.Password
		tls()
	default:
		// Naive outbound needs a special sing-box build; leave it out.
		return nil, false
	}
	return o, true
}

func buildSingboxConfig(links []string, directSites []string) []byte {
	outbounds := []kv{}
	var tags []string
	for _, node := range uniqueNodeNames(parseShareLinks(links)) {
		if outbound, ok := singboxOutbound(node); ok {
			outbounds = append(outbounds, outbound)
			tags = append(tags, node.Name)
		}
	}
	all := []kv{}
	if len(tags) > 0 {
		all = append(all,
			kv{"type": "selector", "tag": "proxy", "outbounds": append([]string{"auto"}, tags...), "default": "auto"},
			kv{"type": "urltest", "tag": "auto", "outbounds": tags, "url": "https://www.gstatic.com/generate_204", "interval": "5m"},
		)
	}
	all = append(all, outbounds...)
	all = append(all, kv{"type": "direct", "tag": "direct"})
	final := "direct"
	if len(tags) > 0 {
		final = "proxy"
	}
	route := kv{"final": final}
	if len(tags) > 0 {
		if rules := singboxDirectRules(directSites); len(rules) > 0 {
			route["rules"] = rules
		}
	}
	raw, _ := json.MarshalIndent(kv{
		"log":       kv{"level": "info"},
		"outbounds": all,
		"route":     route,
	}, "", "  ")
	return raw
}

// formattedSubscriptionBody returns the converted body and its content type
// for an explicit ?format=, or ok=false to keep the default response.
func formattedSubscriptionBody(format string, links []string, directSites []string) (body []byte, contentType string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case subFormatClash:
		return buildClashConfig(links, directSites), "text/yaml; charset=utf-8", true
	case subFormatSingbox:
		return buildSingboxConfig(links, directSites), "application/json; charset=utf-8", true
	}
	return nil, "", false
}

func isSubscriptionFormat(format string) bool {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case subFormatClash, subFormatSingbox:
		return true
	}
	return false
}

// mergeDirectSiteLists flattens newline or comma separated lists into one
// de-duplicated list, keeping first-seen order.
func mergeDirectSiteLists(raws ...string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, raw := range raws {
		for _, entry := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == ',' }) {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			if _, ok := seen[entry]; ok {
				continue
			}
			seen[entry] = struct{}{}
			out = append(out, entry)
		}
	}
	return out
}

type directSiteKind int

const (
	directSuffix directSiteKind = iota
	directFull
	directCIDR
	directGeosite
	directGeoip
)

type directSite struct {
	kind  directSiteKind
	value string
}

// parseDirectSite reads a direct-site entry: a bare domain (matches subdomains
// too), domain:/full:/geosite:/geoip: prefixes, or an IP / CIDR.
func parseDirectSite(entry string) (directSite, bool) {
	e := strings.ToLower(strings.TrimSpace(entry))
	switch {
	case e == "":
		return directSite{}, false
	case strings.HasPrefix(e, "geosite:"):
		return directSite{directGeosite, strings.TrimPrefix(e, "geosite:")}, true
	case strings.HasPrefix(e, "geoip:"):
		return directSite{directGeoip, strings.TrimPrefix(e, "geoip:")}, true
	case strings.HasPrefix(e, "full:"):
		return directSite{directFull, strings.TrimPrefix(e, "full:")}, true
	case strings.HasPrefix(e, "domain:"):
		e = strings.TrimPrefix(e, "domain:")
	}
	if _, network, err := net.ParseCIDR(e); err == nil {
		return directSite{directCIDR, network.String()}, true
	}
	if ip := net.ParseIP(e); ip != nil {
		if ip.To4() != nil {
			return directSite{directCIDR, e + "/32"}, true
		}
		return directSite{directCIDR, e + "/128"}, true
	}
	e = strings.TrimPrefix(e, "*.")
	e = strings.TrimPrefix(e, ".")
	if e == "" {
		return directSite{}, false
	}
	return directSite{directSuffix, e}, true
}

func (d directSite) clash() (string, bool) {
	switch d.kind {
	case directSuffix:
		return "DOMAIN-SUFFIX," + d.value + ",DIRECT", true
	case directFull:
		return "DOMAIN," + d.value + ",DIRECT", true
	case directCIDR:
		if strings.Contains(d.value, ":") {
			return "IP-CIDR6," + d.value + ",DIRECT,no-resolve", true
		}
		return "IP-CIDR," + d.value + ",DIRECT,no-resolve", true
	case directGeosite:
		return "GEOSITE," + d.value + ",DIRECT", true
	case directGeoip:
		return "GEOIP," + d.value + ",DIRECT", true
	}
	return "", false
}

// singboxDirectRules groups the entries by match type. geosite/geoip need rule
// sets in current sing-box, so they are left out.
func singboxDirectRules(entries []string) []kv {
	var suffixes, domains, cidrs []string
	for _, entry := range entries {
		site, ok := parseDirectSite(entry)
		if !ok {
			continue
		}
		switch site.kind {
		case directSuffix:
			suffixes = append(suffixes, site.value)
		case directFull:
			domains = append(domains, site.value)
		case directCIDR:
			cidrs = append(cidrs, site.value)
		}
	}
	var rules []kv
	if len(domains) > 0 {
		rules = append(rules, kv{"domain": domains, "outbound": "direct"})
	}
	if len(suffixes) > 0 {
		rules = append(rules, kv{"domain_suffix": suffixes, "outbound": "direct"})
	}
	if len(cidrs) > 0 {
		rules = append(rules, kv{"ip_cidr": cidrs, "outbound": "direct"})
	}
	return rules
}

// outboundFromShareLink turns a share link (vless://, vmess://, trojan://,
// hysteria2://, ss://, anytls://) into a Sing-box client outbound, so another
// server can be used as a route target without writing its config by hand.
func outboundFromShareLink(link string) (map[string]interface{}, error) {
	node, ok := parseShareLink(strings.TrimSpace(link))
	if !ok {
		return nil, fmt.Errorf("not a supported share link")
	}
	outbound, ok := singboxOutbound(uniqueNodeNames([]proxyNode{node})[0])
	if !ok {
		return nil, fmt.Errorf("%s links cannot be used as an outbound", node.Scheme)
	}
	return outbound, nil
}
