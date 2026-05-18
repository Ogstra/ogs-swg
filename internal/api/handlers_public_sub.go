package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
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
	HeaderHappParams      []happSubscriptionParam
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

type happSubscriptionParam struct {
	Key   string
	Value string
}

func subscriptionMemberDisplayName(username, alias string) string {
	if trimmed := strings.TrimSpace(alias); trimmed != "" {
		return trimmed
	}
	return username
}

func subscriptionDisplayName(name, alias string) string {
	if trimmed := strings.TrimSpace(alias); trimmed != "" {
		return trimmed
	}
	return name
}

var (
	subscriptionUserAgentClientPlatformVersionRE = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9 _.-]{0,63})/([A-Za-z][A-Za-z0-9 _.-]{0,31})/([A-Za-z0-9._-]{1,64})`)
	subscriptionUserAgentProductRE               = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9 _.-]{0,63})/([A-Za-z0-9._-]{1,64})`)
	subscriptionUserAgentiPhoneModelRE           = regexp.MustCompile(`\b(iPhone\d{1,2},\d+)\b`)
	subscriptionUserAgentiPadModelRE             = regexp.MustCompile(`\b(iPad\d{1,2},\d+)\b`)
	subscriptionUserAgentiPodModelRE             = regexp.MustCompile(`\b(iPod\d{1,2},\d+)\b`)
	subscriptionUserAgentMacModelRE              = regexp.MustCompile(`\b(MacBook(?:Air|Pro)?\d{1,2},\d+|Mac\d{1,2},\d+)\b`)
	subscriptionUserAgentSamsungModelRE          = regexp.MustCompile(`\b(SM-[A-Z0-9]+)\b`)
	subscriptionUserAgentAndroidModelRE          = regexp.MustCompile(`Android\s+[0-9][0-9A-Za-z._-]*\s*;\s*([A-Za-z0-9 _.-]{2,64}?)(?:\s+Build/|[;)])`)
	subscriptionUserAgentAndroidVersionRE        = regexp.MustCompile(`Android\s+([0-9][0-9A-Za-z._-]*)`)
	subscriptionUserAgentMacOSVersionRE          = regexp.MustCompile(`Mac OS X\s+([0-9_]+)`)
	subscriptionUserAgentWindowsRE               = regexp.MustCompile(`Windows NT\s+([0-9.]+)`)
	happSubscriptionParamKeyRE                   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

func (s *Server) handlePublicSubscription(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		http.NotFound(w, r)
		return
	}

	sub, err := s.store.Queries.GetSubscriptionByToken(r.Context(), token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	memberRows, err := s.store.Queries.GetSubscriptionMembers(r.Context(), sub.ID)
	if err != nil || len(memberRows) == 0 {
		http.NotFound(w, r)
		return
	}
	users := make([]string, 0, len(memberRows))
	for _, member := range memberRows {
		users = append(users, member.UserName)
	}

	clientIP := resolveSubscriptionRequestIP(r)
	allowlisted := s.protectionRules.isIPAllowed(clientIP)
	if !allowlisted {
		if s.protectionRules.isIPBlocked(clientIP) {
			s.recordBlockedSubscriptionRequest(r, sub.ID, users, "ip_block")
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		if s.protectionRules.isTokenBlocked(token) {
			s.recordBlockedSubscriptionRequest(r, sub.ID, users, "token_block")
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		window := time.Duration(s.config.SubscriptionProtection.WindowSeconds) * time.Second
		if blocked, retryAfter := s.subscriptionLimiter.check(token, s.config.SubscriptionProtection.MaxRequests, window); blocked {
			s.recordBlockedSubscriptionRequest(r, sub.ID, users, "rate_limit")
			retryAfterSeconds := int64(retryAfter.Seconds())
			if retryAfterSeconds < 1 {
				retryAfterSeconds = 1
			}
			w.Header().Set("Retry-After", strconv.FormatInt(retryAfterSeconds, 10))
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
	}
	// UA checks run before recording the rate-limit token so that social-media
	// crawlers and browser requests do not consume quota slots.
	if s.config.SubscriptionProtection.SocialFetchersBlockEnabled && isSocialFetcherUA(r.UserAgent()) {
		s.recordBlockedSubscriptionRequest(r, sub.ID, users, "ua_social_fetcher")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if s.config.SubscriptionProtection.UAFilterEnabled && isBrowserUA(r.UserAgent()) {
		s.recordBlockedSubscriptionRequest(r, sub.ID, users, "ua_browser")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if !allowlisted {
		s.subscriptionLimiter.record(token)
	}

	happParams, happFlag := s.happSubscriptionParamsForRequest(r, token)
	profileFlag := happFlag
	if profileFlag == "" && isShadowrocketRequest(r) {
		if cfg, cfgErr := s.store.GetSubscriptionHappConfig(r.Context()); cfgErr == nil {
			profileFlag = cfg.ProfileFlag
		}
	}

	// Determine routing profile for Happ clients.
	// Per-sub override takes priority; falls back to global config.
	routingProfileJSON := ""
	if happParams != nil {
		if sub.HappRoutingProfile != "" {
			routingProfileJSON = sub.HappRoutingProfile
		} else {
			if globalCfg, cfgErr := s.store.GetSubscriptionHappConfig(r.Context()); cfgErr == nil {
				routingProfileJSON = globalCfg.RoutingProfile
			}
		}
	}

	cacheKey := "sub:" + token
	if happParams != nil {
		cacheKey += ":happ:" + happSubscriptionParamsCacheKey(happParams)
	}
	if routingProfileJSON != "" {
		cacheKey += ":r:" + routingProfileJSON
	}
	if profileFlag != "" {
		cacheKey += ":flag:" + profileFlag
	}
	if val, found := s.cache.Get(cacheKey); found {
		if c, ok := val.(cachedSub); ok {
			s.recordSubscriptionRequest(r, sub.ID, users, true)
			sendSubResponse(w, c.Body, c.HeaderName, c.HeaderUp, c.HeaderDown, c.HeaderTot, c.HeaderProfileInterval, c.HeaderUpdateAlways, c.HeaderHappParams)
			return
		}
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

	proxyDisplayName := func(username, alias string) string {
		name := subscriptionMemberDisplayName(username, alias)
		if profileFlag != "" {
			return profileFlag + name
		}
		return name
	}

	for _, member := range memberRows {
		username := member.UserName
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
				link, buildErr = buildVlessLink(proxyDisplayName(username, member.Alias), &userInfo, inboundView, currentHost, port, inbMeta, sniFallback)
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
				link, buildErr = buildVmessLink(proxyDisplayName(username, member.Alias), &infoCopy, inboundView, currentHost, port, inbMeta, sniFallback)
			case "trojan":
				link, buildErr = buildTrojanLink(proxyDisplayName(username, member.Alias), &userInfo, inboundView, currentHost, port, inbMeta, sniFallback)
			case "hysteria2":
				link, buildErr = buildHysteria2Link(proxyDisplayName(username, member.Alias), &userInfo, inboundView, currentHost, port)
			case "shadowsocks":
				link, buildErr = buildShadowsocksLink(proxyDisplayName(username, member.Alias), &userInfo, inboundView, currentHost, port)
			case "anytls":
				link, buildErr = buildAnyTLSLink(proxyDisplayName(username, member.Alias), &userInfo, inboundView, currentHost, port, inbMeta, sniFallback)
			case "naive":
				link, buildErr = buildNaiveLink(proxyDisplayName(username, member.Alias), &userInfo, inboundView, currentHost, port, inbMeta, sniFallback)
			}

			if buildErr == nil && link != "" {
				links = append(links, link)
			}
		}
	}

	displayTitle := subscriptionDisplayName(sub.Name, sub.Alias)

	var responseLines []string
	if title := strings.TrimSpace(displayTitle); title != "" {
		responseLines = append(responseLines, "#profile-title: "+title)
	}
	for _, param := range happParams {
		responseLines = append(responseLines, happSubscriptionBodyLine(param))
	}
	// Embed standard subscription metadata as body lines for Happ clients so they
	// remain accessible even when HTTP response headers are stripped by proxies.
	if happParams != nil {
		userinfoParts := []string{
			fmt.Sprintf("upload=%d", totalUp),
			fmt.Sprintf("download=%d", totalDown),
			fmt.Sprintf("total=%d", totalLimit),
		}
		responseLines = append(responseLines, "#subscription-userinfo: "+strings.Join(userinfoParts, "; "))
		intervalVal := int64(0)
		if sub.ProfileUpdateIntervalHours.Valid {
			intervalVal = sub.ProfileUpdateIntervalHours.Int64
		}
		responseLines = append(responseLines, fmt.Sprintf("#profile-update-interval: %d", intervalVal))
		if int64ToBool(sub.UpdateAlways) {
			responseLines = append(responseLines, "#update-always: true")
		}
		if routingProfileJSON != "" {
			encoded := base64.StdEncoding.EncodeToString([]byte(routingProfileJSON))
			responseLines = append(responseLines, "#routing: happ://routing/onadd/"+encoded)
		}
	}
	responseLines = append(responseLines, links...)
	joined := strings.Join(responseLines, "\n")
	body := make([]byte, base64.StdEncoding.EncodedLen(len(joined)))
	base64.StdEncoding.Encode(body, []byte(joined))

	// total=0 means "no limit" for most clients; only send if we have either sub or individual quotas.
	if !hasSubQuota && totalLimit == 0 {
		// no limit at all — leave totalLimit as 0
	}

	// Save to cache (TTL: 2 minutes to protect against flood, but fast enough for normal use)
	c := cachedSub{
		Body:                  body,
		HeaderName:            displayTitle,
		HeaderUp:              totalUp,
		HeaderDown:            totalDown,
		HeaderTot:             totalLimit,
		HeaderProfileInterval: nullableInt64Ptr(sub.ProfileUpdateIntervalHours),
		HeaderUpdateAlways:    int64ToBool(sub.UpdateAlways),
		HeaderHappParams:      happParams,
	}
	s.cache.SetWithTTL(cacheKey, c, 1, 2*time.Minute)

	s.recordSubscriptionRequest(r, sub.ID, users, false)
	sendSubResponse(w, c.Body, c.HeaderName, c.HeaderUp, c.HeaderDown, c.HeaderTot, c.HeaderProfileInterval, c.HeaderUpdateAlways, c.HeaderHappParams)
}

func (s *Server) happSubscriptionParamsForRequest(r *http.Request, token string) ([]happSubscriptionParam, string) {
	if r == nil {
		return nil, ""
	}
	isHapp := isHappRequest(r)
	if !isHapp {
		return nil, ""
	}

	params := make([]happSubscriptionParam, 0, 8)
	seen := make(map[string]struct{}, 8)

	appendParam := func(key, value string) {
		key = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(key, "#")))
		value = normalizeHappSubscriptionParamValue(value)
		if value == "" || !happSubscriptionParamKeyRE.MatchString(key) {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		params = append(params, happSubscriptionParam{Key: key, Value: value})
	}

	config, err := s.store.GetSubscriptionHappConfig(r.Context())
	if err != nil {
		return nil, ""
	}

	appendParam("providerid", config.ProviderID)
	appendParam("hide-settings", config.HideSettings)
	appendParam("subscription-always-hwid-enable", config.AlwaysHWID)
	appendParam("subscription-auto-update-open-enable", config.AutoUpdateOnOpen)
	appendParam("subscription-ping-onopen-enabled", config.PingOnOpen)
	appendParam("color-profile", config.ColorProfile)

	for _, param := range config.AdvancedParameters {
		value := param.Value
		switch strings.ToLower(strings.TrimSpace(param.Key)) {
		case "fallback-url", "new-url":
			value = appendSubscriptionTokenToURL(value, token)
		}
		appendParam(param.Key, value)
	}
	return params, config.ProfileFlag
}

func isHappRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("client")), "happ") {
		return true
	}
	if strings.Contains(strings.ToLower(r.UserAgent()), "happ") {
		return true
	}
	for _, header := range []string{"X-Hwid", "X-Device-OS", "X-Ver-OS", "X-Device-Model"} {
		if strings.TrimSpace(r.Header.Get(header)) != "" {
			return true
		}
	}
	return false
}

func isShadowrocketRequest(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("client")), "shadowrocket") ||
		strings.Contains(strings.ToLower(r.UserAgent()), "shadowrocket")
}

func appendSubscriptionTokenToURL(value string, token string) string {
	base := strings.TrimSpace(value)
	token = strings.TrimSpace(token)
	if base == "" || token == "" {
		return base
	}
	if strings.HasSuffix(strings.TrimRight(base, "/"), "/"+token) {
		return base
	}

	parsed, err := url.Parse(base)
	if err == nil && (parsed.Scheme != "" || parsed.Host != "" || parsed.Path != "") {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + url.PathEscape(token)
		return parsed.String()
	}
	return strings.TrimRight(base, "/") + "/" + token
}

func normalizeHappSubscriptionParamValue(value string) string {
	value = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(value))
	if len(value) > 8192 {
		return value[:8192]
	}
	return value
}

func happSubscriptionParamsCacheKey(params []happSubscriptionParam) string {
	parts := make([]string, 0, len(params))
	for _, param := range params {
		parts = append(parts, param.Key+"="+param.Value)
	}
	return strings.Join(parts, "&")
}

func happSubscriptionBodyLine(param happSubscriptionParam) string {
	if param.Key == "providerid" {
		return "#providerid " + param.Value
	}
	return "#" + param.Key + ": " + param.Value
}

func (s *Server) recordSubscriptionRequest(r *http.Request, subID int64, users []string, servedFromCache bool) {
	meta := extractSubscriptionRequestMetadata(r)
	_ = s.store.Queries.InsertSubscriptionRequest(r.Context(), store.InsertSubscriptionRequestParams{
		SubID:           subID,
		UserName:        strings.Join(users, ", "),
		RequestIp:       resolveSubscriptionRequestIP(r),
		RequestHost:     meta.requestHost,
		RequestPath:     meta.requestPath,
		UserAgent:       meta.userAgent,
		DeviceModel:     meta.deviceModel,
		DeviceOs:        meta.deviceOS,
		DeviceOsVersion: meta.deviceOSVersion,
		AppVersion:      meta.appVersion,
		Country:         meta.country,
		HwidHash:        meta.hwidHash,
		HwidPrefix:      meta.hwidPrefix,
		RequestedAt:     s.now().Unix(),
		ServedFromCache: servedFromCacheToInt64(servedFromCache),
		Blocked:         0,
		BlockReason:     "",
	})
}

// blockedRecordDedupTTL is the window within which a duplicate blocked-request
// record (same sub + IP + reason) is suppressed to avoid multiple entries from
// parallel link-preview fetchers or rapid browser retries.
const blockedRecordDedupTTL = 10 * time.Second

func (s *Server) recordBlockedSubscriptionRequest(r *http.Request, subID int64, users []string, blockReason string) {
	clientIP := resolveSubscriptionRequestIP(r)
	dedupKey := fmt.Sprintf("%d|%s|%s", subID, clientIP, blockReason)

	s.blockedRecordDedupMu.Lock()
	now := s.now()
	if last, seen := s.blockedRecordDedup[dedupKey]; seen && now.Sub(last) < blockedRecordDedupTTL {
		s.blockedRecordDedupMu.Unlock()
		return
	}
	// Prune stale entries to keep the map from growing unbounded.
	for k, t := range s.blockedRecordDedup {
		if now.Sub(t) >= blockedRecordDedupTTL {
			delete(s.blockedRecordDedup, k)
		}
	}
	s.blockedRecordDedup[dedupKey] = now
	s.blockedRecordDedupMu.Unlock()

	meta := extractSubscriptionRequestMetadata(r)
	_ = s.store.Queries.InsertSubscriptionRequest(r.Context(), store.InsertSubscriptionRequestParams{
		SubID:           subID,
		UserName:        strings.Join(users, ", "),
		RequestIp:       resolveSubscriptionRequestIP(r),
		RequestHost:     meta.requestHost,
		RequestPath:     meta.requestPath,
		UserAgent:       meta.userAgent,
		DeviceModel:     meta.deviceModel,
		DeviceOs:        meta.deviceOS,
		DeviceOsVersion: meta.deviceOSVersion,
		AppVersion:      meta.appVersion,
		Country:         meta.country,
		HwidHash:        meta.hwidHash,
		HwidPrefix:      meta.hwidPrefix,
		RequestedAt:     s.now().Unix(),
		ServedFromCache: 0,
		Blocked:         1,
		BlockReason:     blockReason,
	})
}

func isSubscriptionClientUA(ua string) bool {
	trimmed := strings.TrimSpace(ua)
	if trimmed == "" {
		return false
	}

	lowered := strings.ToLower(trimmed)
	for _, knownClient := range []string{
		"v2rayn",
		"shadowrocket",
		"nekoray",
		"clash",
		"sing-box",
		"hiddify",
		"loon",
		"stash",
		"surge",
		"quantumult",
		"surfboard",
		"kitsunebi",
		"karing",
		"happ",
	} {
		if strings.Contains(lowered, knownClient) {
			return true
		}
	}
	return false
}

func isSocialFetcherUA(ua string) bool {
	trimmed := strings.TrimSpace(ua)
	if trimmed == "" {
		return false
	}
	if isSubscriptionClientUA(trimmed) {
		return false
	}

	lowered := strings.ToLower(trimmed)
	for _, marker := range []string{
		"facebookexternalhit",
		"facebookcatalog",
		"meta-externalagent",
		"meta-externalfetcher",
		"twitterbot",
		"slackbot-linkexpanding",
		"slack-imgproxy",
		"slackbot",
		"linkedinbot",
		"discordbot",
		"telegrambot",
		"skypeuripreview",
		"microsoftpreview",
		"whatsapp/",
		"instagram",
		"fban/",
		"fbav/",
		"messenger",
	} {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

func isBrowserUA(ua string) bool {
	trimmed := strings.TrimSpace(ua)
	if trimmed == "" {
		return false
	}
	if isSubscriptionClientUA(trimmed) {
		return false
	}

	parsed := parseSubscriptionUserAgent(trimmed)
	switch parsed.clientName {
	case "Chrome", "Firefox", "Safari", "Edge", "Opera":
		return true
	}
	if strings.HasPrefix(trimmed, "Mozilla/") {
		return true
	}
	for _, marker := range []string{"Chrome/", "Firefox/", "Safari/", "Edg/", "OPR/", "CriOS/", "FxiOS/"} {
		if strings.Contains(trimmed, marker) {
			return true
		}
	}
	return false
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
	if matches := subscriptionUserAgentClientPlatformVersionRE.FindStringSubmatch(ua); len(matches) == 4 {
		name := strings.TrimSpace(matches[1])
		platform := strings.TrimSpace(matches[2])
		version := strings.TrimSpace(matches[3])
		if !strings.EqualFold(name, "Mozilla") && !strings.EqualFold(name, "Dalvik") {
			parsed.clientName = name
			parsed.clientVersion = version
			switch {
			case strings.EqualFold(platform, "PC"), strings.EqualFold(platform, "Desktop"):
				parsed.deviceModel = "PC"
			case strings.EqualFold(platform, "Mac"), strings.EqualFold(platform, "macOS"):
				parsed.deviceModel = "Mac"
				parsed.deviceOS = "macOS"
			case strings.EqualFold(platform, "Windows"):
				parsed.deviceModel = "PC"
				parsed.deviceOS = "Windows"
			case strings.EqualFold(platform, "Linux"):
				parsed.deviceModel = "PC"
				parsed.deviceOS = "Linux"
			case strings.EqualFold(platform, "Android"):
				parsed.deviceOS = "Android"
			case strings.EqualFold(platform, "iOS"):
				parsed.deviceOS = "iOS"
			case strings.EqualFold(platform, "iPadOS"):
				parsed.deviceOS = "iPadOS"
			default:
				parsed.deviceModel = platform
			}
		}
	} else if matches := subscriptionUserAgentProductRE.FindStringSubmatch(ua); len(matches) == 3 {
		name := strings.TrimSpace(matches[1])
		if !strings.EqualFold(name, "Mozilla") && !strings.EqualFold(name, "Dalvik") {
			parsed.clientName = name
			parsed.clientVersion = strings.TrimSpace(matches[2])
		}
	}
	if parsed.clientName == "" {
		switch {
		case strings.Contains(ua, "Edg/"):
			parsed.clientName = "Edge"
			parsed.clientVersion = extractSubscriptionUserAgentVersion(ua, "Edg/")
		case strings.Contains(ua, "OPR/"):
			parsed.clientName = "Opera"
			parsed.clientVersion = extractSubscriptionUserAgentVersion(ua, "OPR/")
		case strings.Contains(ua, "Chrome/"):
			parsed.clientName = "Chrome"
			parsed.clientVersion = extractSubscriptionUserAgentVersion(ua, "Chrome/")
		case strings.Contains(ua, "Firefox/"):
			parsed.clientName = "Firefox"
			parsed.clientVersion = extractSubscriptionUserAgentVersion(ua, "Firefox/")
		case strings.Contains(ua, "Safari/") && strings.Contains(ua, "Version/"):
			parsed.clientName = "Safari"
			parsed.clientVersion = extractSubscriptionUserAgentVersion(ua, "Version/")
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
		if matches := subscriptionUserAgentMacOSVersionRE.FindStringSubmatch(ua); len(matches) == 2 {
			parsed.deviceOSVer = strings.ReplaceAll(strings.TrimSpace(matches[1]), "_", ".")
		}
	}
	if parsed.deviceOSVer == "" {
		if matches := subscriptionUserAgentWindowsRE.FindStringSubmatch(ua); len(matches) == 2 {
			parsed.deviceOSVer = strings.TrimSpace(matches[1])
		}
	}

	return parsed
}

func extractSubscriptionUserAgentVersion(userAgent string, marker string) string {
	idx := strings.Index(userAgent, marker)
	if idx < 0 {
		return ""
	}
	start := idx + len(marker)
	end := start
	for end < len(userAgent) {
		c := userAgent[end]
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '.' || c == '_' || c == '-' {
			end++
			continue
		}
		break
	}
	if end <= start {
		return ""
	}
	return userAgent[start:end]
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
	// X-CF-Client-IP is set by the Cloudflare Worker (subscription-proxy.js) to
	// the real client IP (from CF-Connecting-IP on the incoming Worker request).
	// Using a custom header avoids nginx overwriting X-Real-IP / CF-Connecting-IP
	// with the Worker egress IP before the request reaches the panel.
	if cfcIP := strings.TrimSpace(r.Header.Get("X-CF-Client-IP")); cfcIP != "" {
		if ip := net.ParseIP(cfcIP); ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() {
			return cfcIP
		}
	}
	// CF-Connecting-IP is set by Cloudflare to the real client IP for every
	// proxied request and cannot be injected by end-users (Cloudflare strips
	// and re-sets it at the edge). Check it for direct-CF requests (no Worker).
	if cfIP := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cfIP != "" {
		if ip := net.ParseIP(cfIP); ip != nil {
			return cfIP
		}
	}
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

func sendSubResponse(w http.ResponseWriter, body []byte, profileTitle string, up, down, tot int64, profileUpdateInterval *int64, updateAlways bool, happParams []happSubscriptionParam) {
	var parts []string
	parts = append(parts, fmt.Sprintf("upload=%d", up))
	parts = append(parts, fmt.Sprintf("download=%d", down))
	parts = append(parts, fmt.Sprintf("total=%d", tot))

	if title := strings.TrimSpace(profileTitle); title != "" {
		w.Header().Set("Profile-Title", title)
	}
	intervalVal := int64(0)
	if profileUpdateInterval != nil {
		intervalVal = *profileUpdateInterval
	}
	w.Header().Set("profile-update-interval", strconv.FormatInt(intervalVal, 10))
	if updateAlways {
		w.Header().Set("update-always", "true")
	}
	for _, param := range happParams {
		w.Header().Set(param.Key, param.Value)
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
