package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

type subscriptionClientVariant string

const (
	subscriptionClientVariantDefault  subscriptionClientVariant = "default"
	subscriptionClientVariantV2RayTun subscriptionClientVariant = "v2raytun"
)

type v2RayTunRoutingPayload struct {
	DomainStrategy string                `json:"domainStrategy"`
	ID             string                `json:"id"`
	Balancers      []map[string]any      `json:"balancers"`
	DomainMatcher  string                `json:"domainMatcher"`
	Rules          []v2RayTunRoutingRule `json:"rules"`
	Name           string                `json:"name"`
}

type v2RayTunRoutingRule struct {
	Domain      []string `json:"domain,omitempty"`
	ID          string   `json:"id"`
	OutboundTag string   `json:"outboundTag"`
	Type        string   `json:"type"`
	Name        string   `json:"__name__"`
	IP          []string `json:"ip,omitempty"`
}

func detectSubscriptionClientVariant(r *http.Request) subscriptionClientVariant {
	if r == nil {
		return subscriptionClientVariantDefault
	}
	clientOverride := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("client")))
	switch clientOverride {
	case "v2raytun":
		return subscriptionClientVariantV2RayTun
	case "default":
		return subscriptionClientVariantDefault
	}

	parsed := parseSubscriptionUserAgent(r.UserAgent())
	if strings.EqualFold(parsed.clientName, "v2raytun") {
		return subscriptionClientVariantV2RayTun
	}
	return subscriptionClientVariantDefault
}

func (v subscriptionClientVariant) cacheKeySuffix() string {
	return string(v)
}

func buildVariantRoutingHeader(variant subscriptionClientVariant, name string, destinations []string) (string, error) {
	if variant != subscriptionClientVariantV2RayTun {
		return "", nil
	}
	return buildV2RayTunRoutingHeader(name, destinations)
}

func buildV2RayTunRoutingHeader(name string, destinations []string) (string, error) {
	normalized, err := normalizeSubscriptionDefaultDestinations(destinations)
	if err != nil || len(normalized) == 0 {
		return "", err
	}

	domains := make([]string, 0, len(normalized))
	ips := make([]string, 0, len(normalized))
	seenDomains := make(map[string]struct{}, len(normalized))
	seenIPs := make(map[string]struct{}, len(normalized))

	for _, destination := range normalized {
		host, _, err := net.SplitHostPort(destination)
		if err != nil {
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			host = ip.String()
			if _, ok := seenIPs[host]; ok {
				continue
			}
			seenIPs[host] = struct{}{}
			ips = append(ips, host)
			continue
		}
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" {
			continue
		}
		if _, ok := seenDomains[host]; ok {
			continue
		}
		seenDomains[host] = struct{}{}
		domains = append(domains, host)
	}

	rules := make([]v2RayTunRoutingRule, 0, 2)
	if len(domains) > 0 {
		rules = append(rules, v2RayTunRoutingRule{
			Domain:      domains,
			ID:          stableSubscriptionRoutingID("domains:" + strings.Join(domains, ",")),
			OutboundTag: "direct",
			Type:        "field",
			Name:        "OGS Direct Domains",
		})
	}
	if len(ips) > 0 {
		rules = append(rules, v2RayTunRoutingRule{
			IP:          ips,
			ID:          stableSubscriptionRoutingID("ips:" + strings.Join(ips, ",")),
			OutboundTag: "direct",
			Type:        "field",
			Name:        "OGS Direct IPs",
		})
	}
	if len(rules) == 0 {
		return "", nil
	}

	payload := v2RayTunRoutingPayload{
		DomainStrategy: "AsIs",
		ID:             stableSubscriptionRoutingID("routing:" + strings.TrimSpace(name)),
		Balancers:      []map[string]any{},
		DomainMatcher:  "hybrid",
		Rules:          rules,
		Name:           strings.TrimSpace(name),
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func stableSubscriptionRoutingID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	raw := strings.ToUpper(hex.EncodeToString(sum[:16]))
	return raw[0:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:32]
}
