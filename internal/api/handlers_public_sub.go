package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

type cachedSub struct {
	Body       []byte
	HeaderName string
	HeaderUp   int64
	HeaderDown int64
	HeaderTot  int64
	HeaderExp  int64
}

func (s *Server) handlePublicSubscription(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		http.NotFound(w, r)
		return
	}

	cacheKey := "sub:" + token
	if val, found := s.cache.Get(cacheKey); found {
		if c, ok := val.(cachedSub); ok {
			sendSubResponse(w, c.Body, c.HeaderName, c.HeaderUp, c.HeaderDown, c.HeaderTot, c.HeaderExp)
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
	var earliestExp int64
	hasSubQuota := sub.QuotaLimit.Int64 > 0
	if hasSubQuota {
		totalLimit = sub.QuotaLimit.Int64
		earliestExp = nextQuotaResetUnix(s.now(), sub.QuotaPeriod.String, int(sub.ResetDay.Int64))
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
				userExp := nextQuotaResetUnix(s.now(), userMeta.QuotaPeriod, userMeta.ResetDay)
				if earliestExp == 0 || (userExp > 0 && userExp < earliestExp) {
					earliestExp = userExp
				}
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
		Body:       encoded,
		HeaderName: sub.Name,
		HeaderUp:   totalUp,
		HeaderDown: totalDown,
		HeaderTot:  totalLimit,
		HeaderExp:  earliestExp,
	}
	s.cache.SetWithTTL(cacheKey, c, 1, 2*time.Minute)

	sendSubResponse(w, c.Body, c.HeaderName, c.HeaderUp, c.HeaderDown, c.HeaderTot, c.HeaderExp)
}

func sendSubResponse(w http.ResponseWriter, body []byte, profileTitle string, up, down, tot, exp int64) {
	// Subscription-Userinfo: upload=93568; download=2960655; total=10737418240; expire=1708848000
	var parts []string
	parts = append(parts, fmt.Sprintf("upload=%d", up))
	parts = append(parts, fmt.Sprintf("download=%d", down))
	parts = append(parts, fmt.Sprintf("total=%d", tot))
	if exp > 0 {
		parts = append(parts, fmt.Sprintf("expire=%d", exp))
	}

	if title := strings.TrimSpace(profileTitle); title != "" {
		w.Header().Set("Profile-Title", title)
	}
	w.Header().Set("Subscription-Userinfo", strings.Join(parts, "; "))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func nextQuotaResetUnix(now time.Time, period string, resetDay int) int64 {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "", "none":
		return 0
	case "daily":
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		return next.Unix()
	case "monthly":
		if resetDay <= 0 {
			resetDay = 1
		}
		candidate := monthlyResetAt(now.Year(), now.Month(), resetDay, now.Location())
		if !now.Before(candidate) {
			nextMonth := now.AddDate(0, 1, 0)
			candidate = monthlyResetAt(nextMonth.Year(), nextMonth.Month(), resetDay, now.Location())
		}
		return candidate.Unix()
	default:
		return 0
	}
}

func monthlyResetAt(year int, month time.Month, resetDay int, loc *time.Location) time.Time {
	if resetDay <= 0 {
		resetDay = 1
	}
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
	if resetDay > lastDay {
		resetDay = lastDay
	}
	return time.Date(year, month, resetDay, 0, 0, 0, 0, loc)
}

// InvalidateSubCache clears the cached links in memory when a user config changes.
// To keep it simple and robust, we just clear the entire cache since settings/users update is rare.
func (s *Server) InvalidateSubCache() {
	if s.cache != nil {
		s.cache.Clear()
	}
}
