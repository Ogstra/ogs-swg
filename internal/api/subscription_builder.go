package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// subscriptionUserSource is the persistence surface the subscription builder
// needs. *core.Store satisfies it.
type subscriptionUserSource interface {
	GetAllInboundMeta() (map[string]core.InboundMeta, error)
	GetUserMetadata(email string) (*core.UserMetadata, error)
	GetCombinedReport(user string, start, end int64) ([]core.Sample, error)
	GetUserExternalProfiles(userName string) ([]core.ExternalProfile, error)
}

// subscriptionInboundSource is the sing-box config surface the subscription
// builder needs. *core.Config satisfies it.
type subscriptionInboundSource interface {
	GetUserInbounds(name string) ([]core.UserInboundInfo, error)
	GetSingboxInboundView(tag string) (*core.SingboxInboundView, error)
}

// subscriptionMember is one subscription member, already resolved from the DB row.
type subscriptionMember struct {
	UserName string
	Alias    string
}

// buildSubscriptionInput is everything the assembly needs. It carries no
// request, response-writer or server dependency.
type buildSubscriptionInput struct {
	Members               []subscriptionMember
	DisplayTitle          string // already resolved via subscriptionDisplayName
	Host                  string // already resolved via resolvePublicHost
	ProfileFlag           string
	HappParams            []happSubscriptionParam
	RoutingProfileJSON    string
	MergedDirectSites     []string
	SubQuotaLimit         int64 // 0 means "no subscription-level quota"
	ProfileUpdateInterval *int64
	UpdateAlways          bool
	Now                   time.Time
	Users                 subscriptionUserSource    // may be nil
	Inbounds              subscriptionInboundSource // may be nil
}

// buildSubscription assembles the subscription payload. It never returns an
// error: every data-source failure degrades exactly as the pre-refactor handler
// did (skip the member, the inbound or the traffic contribution).
func buildSubscription(in buildSubscriptionInput) cachedSub {
	var links []string
	var totalUp, totalDown int64

	// Use subscription-level quota if set; otherwise fall back to summing individual user quotas.
	var totalLimit int64
	hasSubQuota := in.SubQuotaLimit > 0
	if hasSubQuota {
		totalLimit = in.SubQuotaLimit
	}

	host := in.Host

	// Fetch global inbound meta map for speedy lookup
	metaMap := make(map[string]*core.InboundMeta)
	if in.Users != nil {
		if meta, err := in.Users.GetAllInboundMeta(); err == nil {
			for k, v := range meta {
				metaCopy := v
				metaMap[k] = &metaCopy
			}
		}
	}

	proxyDisplayName := func(username, alias string) string {
		name := subscriptionMemberDisplayName(username, alias)
		if in.ProfileFlag != "" {
			return in.ProfileFlag + name
		}
		return name
	}
	externalProfileDisplayName := func(username, alias string, ep core.ExternalProfile) string {
		name := subscriptionMemberDisplayName(username, alias)
		if flag := strings.TrimSpace(ep.Flag); flag != "" {
			return flag + name
		}
		return proxyDisplayName(username, alias)
	}

	for _, member := range in.Members {
		username := member.UserName
		var userMeta *core.UserMetadata
		if in.Users != nil {
			userMeta, _ = in.Users.GetUserMetadata(username)
		}
		if userMeta != nil {
			// Only accumulate individual quota when there is no subscription-level quota set.
			if !hasSubQuota && userMeta.QuotaLimit > 0 {
				totalLimit += userMeta.QuotaLimit
			}
			// Add user traffic
			now := in.Now
			start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
			if in.Users != nil {
				samples, err := in.Users.GetCombinedReport(username, start.Unix(), now.Unix())
				if err == nil {
					for _, smp := range samples {
						totalUp += smp.Uplink
						totalDown += smp.Downlink
					}
				}
			}
		}

		var userInbounds []core.UserInboundInfo
		var err error
		if in.Inbounds != nil {
			userInbounds, err = in.Inbounds.GetUserInbounds(username)
		}
		if err == nil && len(userInbounds) > 0 {
			// Legacy data may still have the same user in multiple inbounds. Subscription
			// generation now treats the first match as canonical to avoid broken bundles.
			userInbounds = userInbounds[:1]

			for _, userInfo := range userInbounds {
				inboundView, err := in.Inbounds.GetSingboxInboundView(userInfo.Tag)
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

		// Append external homelab profile links for this subscription member.
		if in.Users != nil {
			externalProfiles, epErr := in.Users.GetUserExternalProfiles(username)
			if epErr == nil {
				for _, ep := range externalProfiles {
					if !ep.Enabled {
						continue
					}
					var epLink string
					var epLinkErr error
					epDisplayName := externalProfileDisplayName(username, member.Alias, ep)
					switch ep.Type {
					case "vless":
						epLink, epLinkErr = buildExternalVlessLink(epDisplayName, ep)
					case "shadowsocks":
						epLink, epLinkErr = buildExternalShadowsocksLink(epDisplayName, ep)
					}
					if epLinkErr == nil && epLink != "" {
						links = append(links, epLink)
					}
				}
			}
		}
	}

	displayTitle := in.DisplayTitle

	var responseLines []string
	if title := strings.TrimSpace(displayTitle); title != "" {
		responseLines = append(responseLines, "#profile-title: "+title)
	}
	for _, param := range in.HappParams {
		responseLines = append(responseLines, happSubscriptionBodyLine(param))
	}
	// Embed standard subscription metadata as body lines for Happ clients so they
	// remain accessible even when HTTP response headers are stripped by proxies.
	if in.HappParams != nil {
		userinfoParts := []string{
			fmt.Sprintf("upload=%d", totalUp),
			fmt.Sprintf("download=%d", totalDown),
			fmt.Sprintf("total=%d", totalLimit),
		}
		responseLines = append(responseLines, "#subscription-userinfo: "+strings.Join(userinfoParts, "; "))
		intervalVal := int64(0)
		if in.ProfileUpdateInterval != nil {
			intervalVal = *in.ProfileUpdateInterval
		}
		responseLines = append(responseLines, fmt.Sprintf("#profile-update-interval: %d", intervalVal))
		if in.UpdateAlways && in.HappParams == nil {
			responseLines = append(responseLines, "#update-always: true")
		}
		if in.RoutingProfileJSON != "" {
			if strings.TrimSpace(in.RoutingProfileJSON) == happRoutingOffLink {
				responseLines = append(responseLines, happRoutingOffLink)
			} else {
				injected := in.RoutingProfileJSON
				if len(in.MergedDirectSites) > 0 {
					var profileMap map[string]json.RawMessage
					if err := json.Unmarshal([]byte(in.RoutingProfileJSON), &profileMap); err == nil {
						var existing []string
						if raw, ok := profileMap["DirectSites"]; ok {
							_ = json.Unmarshal(raw, &existing)
						}
						combined := make([]string, 0, len(existing)+len(in.MergedDirectSites))
						seenDS := make(map[string]struct{})
						for _, s := range append(existing, in.MergedDirectSites...) {
							if _, ok := seenDS[s]; !ok {
								seenDS[s] = struct{}{}
								combined = append(combined, s)
							}
						}
						if dsRaw, err := json.Marshal(combined); err == nil {
							profileMap["DirectSites"] = dsRaw
						}
						if injectedBytes, err := json.Marshal(profileMap); err == nil {
							injected = string(injectedBytes)
						}
					}
				}
				encoded := base64.StdEncoding.EncodeToString([]byte(injected))
				responseLines = append(responseLines, "happ://routing/onadd/"+encoded)
			}
		} else if len(in.MergedDirectSites) > 0 {
			minProfile := map[string]interface{}{"Name": "panel-direct", "DirectSites": in.MergedDirectSites}
			if minJSON, err := json.Marshal(minProfile); err == nil {
				encoded := base64.StdEncoding.EncodeToString(minJSON)
				responseLines = append(responseLines, "happ://routing/onadd/"+encoded)
			}
		}
	}
	responseLines = append(responseLines, links...)
	joined := strings.Join(responseLines, "\n")
	body := make([]byte, base64.StdEncoding.EncodedLen(len(joined)))
	base64.StdEncoding.Encode(body, []byte(joined))

	return cachedSub{
		Body:                  body,
		HeaderName:            in.DisplayTitle,
		HeaderUp:              totalUp,
		HeaderDown:            totalDown,
		HeaderTot:             totalLimit,
		HeaderProfileInterval: in.ProfileUpdateInterval,
		HeaderUpdateAlways:    in.UpdateAlways,
		HeaderHappParams:      in.HappParams,
	}
}
