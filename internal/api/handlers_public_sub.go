package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/Ogstra/ogs-swg/internal/core/store"
)

type cachedSub struct {
	Body                  []byte
	HeaderName            string
	HeaderUp              int64
	HeaderDown            int64
	HeaderTot             int64
	HeaderProfileInterval *int64
	HeaderUpdateAlways    bool
}

type subscriptionRequestMetadata struct {
	requestHost     string
	requestPath     string
	userAgent       string
	deviceModel     string
	deviceOS        string
	deviceOSVersion string
	appVersion      string
	country         string
	hwidHash        string
	hwidPrefix      string
}

type parsedSubscriptionUserAgent struct {
	clientName    string
	clientVersion string
	deviceModel   string
	deviceOS      string
	deviceOSVer   string
}

var (
	subscriptionUserAgentProductRE        = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9 _.-]{0,63})/([A-Za-z0-9._-]{1,64})`)
	subscriptionUserAgentiPhoneModelRE    = regexp.MustCompile(`\b(iPhone\d{1,2},\d+)\b`)
	subscriptionUserAgentiPadModelRE      = regexp.MustCompile(`\b(iPad\d{1,2},\d+)\b`)
	subscriptionUserAgentiPodModelRE      = regexp.MustCompile(`\b(iPod\d{1,2},\d+)\b`)
	subscriptionUserAgentMacModelRE       = regexp.MustCompile(`\b(MacBook(?:Air|Pro)?\d{1,2},\d+|Mac\d{1,2},\d+)\b`)
	subscriptionUserAgentSamsungModelRE   = regexp.MustCompile(`\b(SM-[A-Z0-9]+)\b`)
	subscriptionUserAgentAndroidModelRE   = regexp.MustCompile(`Android\s+[0-9][0-9A-Za-z._-]*\s*;\s*([A-Za-z0-9 _.-]{2,64}?)(?:\s+Build/|[;)])`)
	subscriptionUserAgentAndroidVersionRE = regexp.MustCompile(`Android\s+([0-9][0-9A-Za-z._-]*)`)
	subscriptionUserAgentWindowsRE        = regexp.MustCompile(`Windows NT\s+([0-9.]+)`)
)

func (s *Server) handlePublicSubscription(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		http.NotFound(w, r)
		return
	}

	cacheKey := "sub:" + token
	if val, found := s.cache.Get(cacheKey); found {
		if c, ok := val.(cachedSub); ok {
			sub, err := s.store.Queries.GetSubscriptionByToken(r.Context(), token)
			if err == nil {
				users, _ := s.store.Queries.GetUsersForSubscription(r.Context(), sub.ID)
				s.recordSubscriptionRequest(r, sub.ID, users, true)
			}
			sendSubResponse(w, c.Body, c.HeaderName, c.HeaderUp, c.HeaderDown, c.HeaderTot, c.HeaderProfileInterval, c.HeaderUpdateAlways)
			return
		}
	}

	sub, err := s.store.Queries.GetSubscriptionByToken(r.Context(), token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	users, err := s.store.Queries.GetUsersForSubscription(r.Context(), sub.ID)
	if err != nil || len(users) == 0 {
		http.NotFound(w, r)
		return
	}

	var links []string
	var totalUp, totalDown int64

	// Use subscription-level quota if set; otherwise fall back to summing individual user quotas.
	var totalLimit int64
	hasSubQuota := sub.QuotaLimit.Int64 > 0
	if hasSubQuota {
		totalLimit = sub.QuotaLimit.Int64
	}

	host := s.resolvePublicHost(r)

	// Fetch global inbound meta map for speedy lookup
	metaMap := make(map[string]*core.InboundMeta)
	if meta, err := s.store.GetAllInboundMeta(); err == nil {
		for k, v := range meta {
			metaCopy := v
			metaMap[k] = &metaCopy
		}
	}

	for _, username := range users {
		userInbounds, err := s.config.GetUserInbounds(username)
		if err != nil || len(userInbounds) == 0 {
			continue
		}
		// Legacy data may still have the same user in multiple inbounds. Subscription
		// generation now treats the first match as canonical to avoid broken bundles.
		userInbounds = userInbounds[:1]

		userMeta, _ := s.store.GetUserMetadata(username)
		if userMeta != nil {
			// Only accumulate individual quota when there is no subscription-level quota set.
			if !hasSubQuota && userMeta.QuotaLimit > 0 {
				totalLimit += userMeta.QuotaLimit
			}
			// Add user traffic
			now := s.now()
			start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
			samples, err := s.store.GetCombinedReport(username, start.Unix(), now.Unix())
			if err == nil {
				for _, smp := range samples {
					totalUp += smp.Uplink
					totalDown += smp.Downlink
				}
			}
		}

		for _, userInfo := range userInbounds {
			inboundView, err := s.config.GetSingboxInboundView(userInfo.Tag)
			if err != nil {
				continue
			}
			inbType := inboundView.Type
			if inbType == "" {
				inbType = "vless"
			}

			if inboundView.ListenPort <= 0 {
				continue
			}
			port := strconv.Itoa(inboundView.ListenPort)
			currentHost := host
			var inbMeta *core.InboundMeta
			if m, ok := metaMap[userInfo.Tag]; ok {
				inbMeta = m
				if m.ExternalPort > 0 {
					port = strconv.Itoa(m.ExternalPort)
				}
				if m.OverrideAddress != "" {
					currentHost = m.OverrideAddress
				}
			}

			sniFallback := ""
			if currentHost != host {
				sniFallback = host
			}

			var link string
			var buildErr error

			switch inbType {
			case "vless":
				link, buildErr = buildVlessLink(username, &userInfo, inboundView, currentHost, port, inbMeta, sniFallback)
			case "vmess":
				infoCopy := userInfo
				if userMeta != nil {
					if userMeta.VmessSecurity != "" {
						infoCopy.VmessSecurity = userMeta.VmessSecurity
					}
					if userMeta.VmessAlterID != 0 {
						infoCopy.VmessAlterID = userMeta.VmessAlterID
					}
				}
				link, buildErr = buildVmessLink(username, &infoCopy, inboundView, currentHost, port, inbMeta, sniFallback)
			case "trojan":
				link, buildErr = buildTrojanLink(username, &userInfo, inboundView, currentHost, port, inbMeta, sniFallback)
			case "hysteria2":
				link, buildErr = buildHysteria2Link(username, &userInfo, inboundView, currentHost, port)
			}

			if buildErr == nil && link != "" {
				links = append(links, link)
			}
		}
	}

	joined := strings.Join(links, "\n")
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(joined)))
	base64.StdEncoding.Encode(encoded, []byte(joined))

	// total=0 means "no limit" for most clients; only send if we have either sub or individual quotas.
	if !hasSubQuota && totalLimit == 0 {
		// no limit at all — leave totalLimit as 0
	}

	// Save to cache (TTL: 2 minutes to protect against flood, but fast enough for normal use)
	c := cachedSub{
		Body:                  encoded,
		HeaderName:            sub.Name,
		HeaderUp:              totalUp,
		HeaderDown:            totalDown,
		HeaderTot:             totalLimit,
		HeaderProfileInterval: nullableInt64Ptr(sub.ProfileUpdateIntervalHours),
		HeaderUpdateAlways:    int64ToBool(sub.UpdateAlways),
	}
	s.cache.SetWithTTL(cacheKey, c, 1, 2*time.Minute)

	s.recordSubscriptionRequest(r, sub.ID, users, false)
	sendSubResponse(w, c.Body, c.HeaderName, c.HeaderUp, c.HeaderDown, c.HeaderTot, c.HeaderProfileInterval, c.HeaderUpdateAlways)
}

func (s *Server) recordSubscriptionRequest(r *http.Request, subID int64, users []string, servedFromCache bool) {
	meta := extractSubscriptionRequestMetadata(r)
	_ = s.store.Queries.InsertSubscriptionRequest(r.Context(), store.InsertSubscriptionRequestParams{
		SubID:           subID,
		UserName:        strings.Join(users, ", "),
		RequestIP:       resolveSubscriptionRequestIP(r),
		RequestHost:     meta.requestHost,
		RequestPath:     meta.requestPath,
		UserAgent:       meta.userAgent,
		DeviceModel:     meta.deviceModel,
		DeviceOS:        meta.deviceOS,
		DeviceOSVersion: meta.deviceOSVersion,
		AppVersion:      meta.appVersion,
		Country:         meta.country,
		HwidHash:        meta.hwidHash,
		HwidPrefix:      meta.hwidPrefix,
		RequestedAt:     s.now().Unix(),
		ServedFromCache: servedFromCacheToInt64(servedFromCache),
	})
}

func extractSubscriptionRequestMetadata(r *http.Request) subscriptionRequestMetadata {
	if r == nil {
		return subscriptionRequestMetadata{}
	}

	ua := normalizeSubscriptionHeader(r.UserAgent(), 255)
	parsedUA := parseSubscriptionUserAgent(ua)
	rawHWID := normalizeSubscriptionHeader(r.Header.Get("X-Hwid"), 255)
	deviceModel := normalizeSubscriptionHeader(r.Header.Get("X-Device-Model"), 255)
	if deviceModel == "" {
		deviceModel = normalizeSubscriptionHeader(parsedUA.deviceModel, 255)
	}
	deviceModel = normalizeSubscriptionHeader(resolveSubscriptionDeviceModel(deviceModel), 255)
	deviceOS := normalizeSubscriptionHeader(r.Header.Get("X-Device-OS"), 64)
	if deviceOS == "" {
		deviceOS = normalizeSubscriptionHeader(parsedUA.deviceOS, 64)
	}
	deviceOSVersion := normalizeSubscriptionHeader(r.Header.Get("X-Ver-Os"), 64)
	if deviceOSVersion == "" {
		deviceOSVersion = normalizeSubscriptionHeader(parsedUA.deviceOSVer, 64)
	}
	appVersion := normalizeSubscriptionHeader(r.Header.Get("X-App-Version"), 64)
	if appVersion == "" {
		appVersion = normalizeSubscriptionHeader(parsedUA.clientVersion, 64)
	}
	return subscriptionRequestMetadata{
		requestHost:     normalizeSubscriptionHeader(r.Host, 255),
		requestPath:     sanitizeSubscriptionRequestPath(r.URL.Path),
		userAgent:       ua,
		deviceModel:     deviceModel,
		deviceOS:        deviceOS,
		deviceOSVersion: deviceOSVersion,
		appVersion:      appVersion,
		country:         normalizeSubscriptionCountry(r.Header.Get("CF-IPCountry")),
		hwidHash:        hashSubscriptionHWID(rawHWID),
		hwidPrefix:      prefixSubscriptionHWID(rawHWID),
	}
}

func parseSubscriptionUserAgent(userAgent string) parsedSubscriptionUserAgent {
	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		return parsedSubscriptionUserAgent{}
	}

	parsed := parsedSubscriptionUserAgent{}
	if matches := subscriptionUserAgentProductRE.FindStringSubmatch(ua); len(matches) == 3 {
		name := strings.TrimSpace(matches[1])
		if !strings.EqualFold(name, "Mozilla") && !strings.EqualFold(name, "Dalvik") {
			parsed.clientName = name
			parsed.clientVersion = strings.TrimSpace(matches[2])
		}
	}

	switch {
	case subscriptionUserAgentiPhoneModelRE.MatchString(ua):
		parsed.deviceModel = subscriptionUserAgentiPhoneModelRE.FindString(ua)
		parsed.deviceOS = "iOS"
	case subscriptionUserAgentiPadModelRE.MatchString(ua):
		parsed.deviceModel = subscriptionUserAgentiPadModelRE.FindString(ua)
		parsed.deviceOS = "iPadOS"
	case subscriptionUserAgentiPodModelRE.MatchString(ua):
		parsed.deviceModel = subscriptionUserAgentiPodModelRE.FindString(ua)
		parsed.deviceOS = "iOS"
	case subscriptionUserAgentMacModelRE.MatchString(ua):
		parsed.deviceModel = subscriptionUserAgentMacModelRE.FindString(ua)
		parsed.deviceOS = "macOS"
	case strings.Contains(ua, "Macintosh"):
		parsed.deviceModel = "Mac"
		parsed.deviceOS = "macOS"
	}

	if parsed.deviceModel == "" && subscriptionUserAgentSamsungModelRE.MatchString(ua) {
		parsed.deviceModel = subscriptionUserAgentSamsungModelRE.FindString(ua)
		parsed.deviceOS = "Android"
	}

	if parsed.deviceModel == "" {
		if matches := subscriptionUserAgentAndroidModelRE.FindStringSubmatch(ua); len(matches) == 2 {
			model := strings.TrimSpace(matches[1])
			if model != "" && !strings.EqualFold(model, "wv") && !strings.EqualFold(model, "Mobile") {
				parsed.deviceModel = model
			}
		}
	}

	if parsed.deviceOS == "" && strings.Contains(ua, "Android") {
		parsed.deviceOS = "Android"
	}
	if parsed.deviceOS == "" && strings.Contains(ua, "Windows NT") {
		parsed.deviceOS = "Windows"
	}

	if matches := subscriptionUserAgentAndroidVersionRE.FindStringSubmatch(ua); len(matches) == 2 {
		parsed.deviceOSVer = strings.TrimSpace(matches[1])
	}
	if parsed.deviceOSVer == "" {
		if matches := subscriptionUserAgentWindowsRE.FindStringSubmatch(ua); len(matches) == 2 {
			parsed.deviceOSVer = strings.TrimSpace(matches[1])
		}
	}

	return parsed
}

func normalizeSubscriptionHeader(value string, maxLen int) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if maxLen > 0 && len(trimmed) > maxLen {
		return trimmed[:maxLen]
	}
	return trimmed
}

func sanitizeSubscriptionRequestPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "/s/") {
		return "/s/[token]"
	}
	if len(trimmed) > 255 {
		return trimmed[:255]
	}
	return trimmed
}

func normalizeSubscriptionCountry(value string) string {
	trimmed := strings.ToUpper(strings.TrimSpace(value))
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > 8 {
		return trimmed[:8]
	}
	return trimmed
}

func hashSubscriptionHWID(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func prefixSubscriptionHWID(value string) string {
	if value == "" {
		return ""
	}
	trimmed := strings.ToUpper(strings.TrimSpace(value))
	if len(trimmed) > 8 {
		return trimmed[:8]
	}
	return trimmed
}

func resolveSubscriptionRequestIP(r *http.Request) string {
	if isTrustedProxy(r.RemoteAddr) {
		if ip := firstPublicForwardedIP(r.Header.Get("X-Forwarded-For")); ip != "" {
			return ip
		}
		if ip := firstHeaderToken(r.Header.Get("X-Real-IP")); ip != "" {
			return stripPort(ip)
		}
		if ip := firstHeaderToken(r.Header.Get("X-Forwarded-For")); ip != "" {
			return stripPort(ip)
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func firstPublicForwardedIP(value string) string {
	for _, part := range strings.Split(value, ",") {
		candidate := stripPort(strings.TrimSpace(part))
		if candidate == "" {
			continue
		}
		ip := net.ParseIP(candidate)
		if ip == nil {
			continue
		}
		if !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() {
			return candidate
		}
	}
	return ""
}

func servedFromCacheToInt64(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func sendSubResponse(w http.ResponseWriter, body []byte, profileTitle string, up, down, tot int64, profileUpdateInterval *int64, updateAlways bool) {
	var parts []string
	parts = append(parts, fmt.Sprintf("upload=%d", up))
	parts = append(parts, fmt.Sprintf("download=%d", down))
	parts = append(parts, fmt.Sprintf("total=%d", tot))

	if title := strings.TrimSpace(profileTitle); title != "" {
		w.Header().Set("Profile-Title", title)
	}
	if profileUpdateInterval != nil {
		w.Header().Set("profile-update-interval", strconv.FormatInt(*profileUpdateInterval, 10))
	}
	if updateAlways {
		w.Header().Set("update-always", "true")
	}
	w.Header().Set("Subscription-Userinfo", strings.Join(parts, "; "))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

// InvalidateSubCache clears the cached links in memory when a user config changes.
// To keep it simple and robust, we just clear the entire cache since settings/users update is rare.
func (s *Server) InvalidateSubCache() {
	if s.cache != nil {
		s.cache.Clear()
	}
}
