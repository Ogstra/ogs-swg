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
			sendSubResponse(w, c.Body, c.HeaderUp, c.HeaderDown, c.HeaderTot, c.HeaderExp)
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
	var totalUp, totalDown, totalLimit, earliestExp int64
	hasLimit := false

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

		userMeta, _ := s.store.GetUserMetadata(username)
		if userMeta != nil {
			if userMeta.QuotaLimit > 0 {
				hasLimit = true
				totalLimit += userMeta.QuotaLimit
			}
			// Add user traffic
			now := time.Now()
			start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
			samples, err := s.store.GetCombinedReport(username, start.Unix(), now.Unix())
			if err == nil {
				for _, smp := range samples {
					totalUp += smp.Uplink
					totalDown += smp.Downlink
				}
			}

			// Wait, expiry logic isn't explicitly in userMeta for ogs-swg (QuotaPeriod resets).
			// If there's no fixed expiry timestamp, we leave expire=0 initially
			// or we can calculate the end of the current period.
			if userMeta.QuotaPeriod != "" {
				// We don't have a direct 'expiry' field, so we just use 0 or leave it out if we want.
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

	if !hasLimit {
		totalLimit = 0
	}

	// Save to cache (TTL: 2 minutes to protect against flood, but fast enough for normal use)
	c := cachedSub{
		Body:       encoded,
		HeaderUp:   totalUp,
		HeaderDown: totalDown,
		HeaderTot:  totalLimit,
		HeaderExp:  earliestExp,
	}
	s.cache.SetWithTTL(cacheKey, c, 1, 2*time.Minute)

	sendSubResponse(w, c.Body, c.HeaderUp, c.HeaderDown, c.HeaderTot, c.HeaderExp)
}

func sendSubResponse(w http.ResponseWriter, body []byte, up, down, tot, exp int64) {
	// Subscription-Userinfo: upload=93568; download=2960655; total=10737418240; expire=1708848000
	var parts []string
	parts = append(parts, fmt.Sprintf("upload=%d", up))
	parts = append(parts, fmt.Sprintf("download=%d", down))
	parts = append(parts, fmt.Sprintf("total=%d", tot))
	if exp > 0 {
		parts = append(parts, fmt.Sprintf("expire=%d", exp))
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
