package api

import (
	"crypto/sha256"
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

const happRoutingOffLink = "happ://routing/off"

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
			writeErr(w, http.StatusTooManyRequests, "Too Many Requests")
			return
		}
		if s.protectionRules.isTokenBlocked(token) {
			s.recordBlockedSubscriptionRequest(r, sub.ID, users, "token_block")
			writeErr(w, http.StatusTooManyRequests, "Too Many Requests")
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
			writeErr(w, http.StatusTooManyRequests, "Too Many Requests")
			return
		}
	}
	// UA checks run before recording the rate-limit token so that social-media
	// crawlers and browser requests do not consume quota slots.
	if s.config.SubscriptionProtection.SocialFetchersBlockEnabled && isSocialFetcherUA(r.UserAgent()) {
		s.recordBlockedSubscriptionRequest(r, sub.ID, users, "ua_social_fetcher")
		writeErr(w, http.StatusForbidden, "Forbidden")
		return
	}
	if s.config.SubscriptionProtection.UAFilterEnabled && isBrowserUA(r.UserAgent()) {
		s.recordBlockedSubscriptionRequest(r, sub.ID, users, "ua_browser")
		writeErr(w, http.StatusForbidden, "Forbidden")
		return
	}
	if !allowlisted {
		s.subscriptionLimiter.record(token)
	}

	// Load global Happ config once; reuse for routing profile, direct sites, and shadowrocket flag.
	var globalHappCfg core.SubscriptionHappConfig
	var globalHappCfgLoaded bool
	loadGlobalHappCfg := func() core.SubscriptionHappConfig {
		if !globalHappCfgLoaded {
			if cfg, cfgErr := s.store.GetSubscriptionHappConfig(r.Context()); cfgErr == nil {
				globalHappCfg = cfg
			}
			globalHappCfgLoaded = true
		}
		return globalHappCfg
	}

	happParams, happFlag := s.happSubscriptionParamsForRequest(r, token, sub.HappColorProfile)
	profileFlag := happFlag
	if profileFlag == "" && isShadowrocketRequest(r) {
		profileFlag = loadGlobalHappCfg().ProfileFlag
	}

	routingProfileJSON := ""
	if happParams != nil {
		if sub.HappRoutingProfile != "" {
			routingProfileJSON = sub.HappRoutingProfile
		} else {
			routingProfileJSON = loadGlobalHappCfg().RoutingProfile
		}
	}

	// Collect DirectSites from global config and per-sub field.
	var mergedDirectSites []string
	if happParams != nil && strings.TrimSpace(routingProfileJSON) != happRoutingOffLink {
		seen := make(map[string]struct{})
		addSites := func(raw string) {
			for _, s := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == ',' }) {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				if _, ok := seen[s]; !ok {
					seen[s] = struct{}{}
					mergedDirectSites = append(mergedDirectSites, s)
				}
			}
		}
		globalCfg := loadGlobalHappCfg()
		addSites(strings.Join(globalCfg.DirectSites, "\n"))
		addSites(sub.HappDirectSites)
	}

	cacheKey := "sub:" + token
	if happParams != nil {
		cacheKey += ":happ:" + happSubscriptionParamsCacheKey(happParams)
	}
	if routingProfileJSON != "" {
		cacheKey += ":r:" + routingProfileJSON
	}
	if sub.HappColorProfile != "" {
		cacheKey += ":cp:" + sub.HappColorProfile
	}
	if profileFlag != "" {
		cacheKey += ":flag:" + profileFlag
	}
	if len(mergedDirectSites) > 0 {
		cacheKey += ":ds:" + strings.Join(mergedDirectSites, ",")
	}
	if val, found := s.cache.Get(cacheKey); found {
		if c, ok := val.(cachedSub); ok {
			s.recordSubscriptionRequest(r, sub.ID, users, true)
			sendSubResponse(w, c.Body, c.HeaderName, c.HeaderUp, c.HeaderDown, c.HeaderTot, c.HeaderProfileInterval, c.HeaderUpdateAlways, c.HeaderHappParams)
			return
		}
	}

	members := make([]subscriptionMember, 0, len(memberRows))
	for _, member := range memberRows {
		members = append(members, subscriptionMember{UserName: member.UserName, Alias: member.Alias})
	}

	// Nil-typed-interface hazard: assigning a nil *core.Store/*core.Config
	// directly into an interface field produces a non-nil interface holding a
	// nil pointer, so these must be resolved to explicit nil interface values
	// first.
	var subUsers subscriptionUserSource
	if s.store != nil {
		subUsers = s.store
	}
	var subInbounds subscriptionInboundSource
	if s.config != nil {
		subInbounds = s.config
	}

	c := buildSubscription(buildSubscriptionInput{
		Members:               members,
		DisplayTitle:          subscriptionDisplayName(sub.Name, sub.Alias),
		Host:                  s.resolvePublicHost(r),
		ProfileFlag:           profileFlag,
		HappParams:            happParams,
		RoutingProfileJSON:    routingProfileJSON,
		MergedDirectSites:     mergedDirectSites,
		SubQuotaLimit:         sub.QuotaLimit.Int64,
		ProfileUpdateInterval: nullableInt64Ptr(sub.ProfileUpdateIntervalHours),
		UpdateAlways:          int64ToBool(sub.UpdateAlways),
		Now:                   s.now(),
		Users:                 subUsers,
		Inbounds:              subInbounds,
	})

	s.cache.SetWithTTL(cacheKey, c, 1, 2*time.Minute)

	s.recordSubscriptionRequest(r, sub.ID, users, false)
	sendSubResponse(w, c.Body, c.HeaderName, c.HeaderUp, c.HeaderDown, c.HeaderTot, c.HeaderProfileInterval, c.HeaderUpdateAlways, c.HeaderHappParams)
}

func (s *Server) happSubscriptionParamsForRequest(r *http.Request, token string, subColorProfile string) ([]happSubscriptionParam, string) {
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
	// Per-sub color-profile takes priority over global; seen map blocks the global below.
	if subColorProfile != "" {
		appendParam("color-profile", subColorProfile)
	} else {
		appendParam("color-profile", config.ColorProfile)
	}

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
		return "#" + param.Key + " " + param.Value
	}
	return "#" + param.Key + ": " + param.Value
}

func (s *Server) recordSubscriptionRequest(r *http.Request, subID int64, users []string, servedFromCache bool) {
	meta := extractSubscriptionRequestMetadata(r)
	if err := s.store.Queries.InsertSubscriptionRequest(r.Context(), store.InsertSubscriptionRequestParams{
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
		ViaWorker:       viaWorkerToInt64(r),
	}); err == nil {
		s.invalidateSubscriptionHistoryCache()
	}
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
	if err := s.store.Queries.InsertSubscriptionRequest(r.Context(), store.InsertSubscriptionRequestParams{
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
	}); err == nil {
		s.invalidateSubscriptionHistoryCache()
	}
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

func viaWorkerToInt64(r *http.Request) int64 {
	if r.Header.Get("X-CF-Client-IP") != "" {
		return 1
	}
	return 0
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
