package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"time"
)

// GetSingboxConfig reads the raw config file content
func (c *Config) GetSingboxConfig() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.executor != nil {
		content, err := c.executor.ReadConfig(context.Background(), c.SingboxConfigPath)
		if err != nil {
			return "", err
		}
		return string(content), nil
	}

	content, err := os.ReadFile(c.SingboxConfigPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func (c *Config) readSingboxConfigLocked() ([]byte, error) {
	if c.executor != nil {
		return c.executor.ReadConfig(context.Background(), c.SingboxConfigPath)
	}
	return os.ReadFile(c.SingboxConfigPath)
}

func (c *Config) readSingboxConfigMapLocked() (map[string]interface{}, error) {
	content, err := c.readSingboxConfigLocked()
	if err != nil {
		return nil, err
	}

	raw := make(map[string]interface{})
	if err := json.Unmarshal(content, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *Config) writeSingboxConfigLocked(data []byte) error {
	if c.executor != nil {
		return c.executor.WriteConfig(context.Background(), c.SingboxConfigPath, data, 0644)
	}
	return os.WriteFile(c.SingboxConfigPath, data, 0644)
}

func (c *Config) writeValidatedSingboxConfigMapLocked(raw map[string]interface{}) error {
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}

	if err := c.ValidateConfig(data); err != nil {
		return fmt.Errorf("sing-box validation failed: %v", err)
	}
	if err := c.writeSingboxConfigLocked(data); err != nil {
		return err
	}

	return c.afterSingboxConfigWriteLocked(nil)
}

func decodeInboundRawList(raw json.RawMessage) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return []json.RawMessage{}, nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(trimmed, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func decodeRawList(raw json.RawMessage) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return []json.RawMessage{}, nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(trimmed, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func encodeInboundRawList(list []json.RawMessage) (json.RawMessage, error) {
	return encodeRawList(list)
}

func encodeRawList(list []json.RawMessage) (json.RawMessage, error) {
	if list == nil {
		list = []json.RawMessage{}
	}
	data, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func compactJSONBytes(data []byte) []byte {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	var out bytes.Buffer
	if err := json.Compact(&out, trimmed); err != nil {
		return trimmed
	}
	return out.Bytes()
}

func normalizedRawMessage(raw json.RawMessage) []byte {
	return compactJSONBytes(raw)
}

func rawMessageEqual(left, right json.RawMessage) bool {
	return bytes.Equal(normalizedRawMessage(left), normalizedRawMessage(right))
}

func jsonSemanticallyEqual(left, right []byte) bool {
	var leftAny interface{}
	var rightAny interface{}
	if err := json.Unmarshal(left, &leftAny); err != nil {
		return bytes.Equal(compactJSONBytes(left), compactJSONBytes(right))
	}
	if err := json.Unmarshal(right, &rightAny); err != nil {
		return false
	}
	return reflect.DeepEqual(leftAny, rightAny)
}

// SanitiseManagedInboundFields removes protocol/transport-specific fields that
// must not survive a managed inbound write.
func SanitiseManagedInboundFields(inbound map[string]interface{}) {
	if inbound == nil {
		return
	}

	inbType, _ := inbound["type"].(string)
	inbType = strings.ToLower(strings.TrimSpace(inbType))

	transportType := "tcp"
	if transport, ok := inbound["transport"].(map[string]interface{}); ok && transport != nil {
		if rawType, ok := transport["type"].(string); ok && strings.TrimSpace(rawType) != "" {
			transportType = strings.ToLower(strings.TrimSpace(rawType))
		}
	}

	if tls, ok := inbound["tls"].(map[string]interface{}); ok && tls != nil {
		if transportType == "ws" {
			delete(tls, "alpn")
		}
		if inbType != "vless" || transportType == "ws" {
			delete(tls, "reality")
		}
		if len(tls) == 0 {
			delete(inbound, "tls")
		}
	}

	users, ok := inbound["users"].([]interface{})
	if !ok {
		return
	}
	stripFlow := inbType != "vless" || transportType == "ws"
	if !stripFlow {
		return
	}
	for _, rawUser := range users {
		user, ok := rawUser.(map[string]interface{})
		if !ok || user == nil {
			continue
		}
		delete(user, "flow")
	}
}

func renameExperimentalStatsInbound(cfg *SingboxConfig, oldTag, newTag string) {
	if cfg == nil || cfg.Experimental == nil || cfg.Experimental.V2RayAPI == nil || cfg.Experimental.V2RayAPI.Stats == nil {
		return
	}
	for i, tag := range cfg.Experimental.V2RayAPI.Stats.Inbounds {
		if tag == oldTag {
			cfg.Experimental.V2RayAPI.Stats.Inbounds[i] = newTag
		}
	}
}

func statsInboundsForWrite(existing []string, oldTag, newTag string) []string {
	if len(existing) == 0 {
		return nil
	}
	out := make([]string, len(existing))
	copy(out, existing)
	if oldTag == "" || newTag == "" || oldTag == newTag {
		return out
	}
	for i, tag := range out {
		if tag == oldTag {
			out[i] = newTag
		}
	}
	return out
}

func ensureExperimentalStatsInbounds(cfg *SingboxConfig, statsInbounds []string) {
	if cfg == nil || len(statsInbounds) == 0 {
		return
	}
	if cfg.Experimental == nil {
		cfg.Experimental = &Experimental{}
	}
	if cfg.Experimental.V2RayAPI == nil {
		cfg.Experimental.V2RayAPI = &V2RayAPI{}
	}
	if cfg.Experimental.V2RayAPI.Stats == nil {
		cfg.Experimental.V2RayAPI.Stats = &V2RayStats{}
	}
	cfg.Experimental.V2RayAPI.Stats.Enabled = true
	cfg.Experimental.V2RayAPI.Stats.Inbounds = statsInbounds
}

// mergeInboundWithOriginal overlays the typed fields from typedBytes onto the
// original raw JSON object, preserving any unknown fields present in original
// that are not modeled by the typed struct (e.g. "x-meta").
func mergeInboundWithOriginal(original, typedBytes json.RawMessage) (json.RawMessage, error) {
	origMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(original, &origMap); err != nil {
		return typedBytes, nil // fall back to typed-only output
	}
	typedMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(typedBytes, &typedMap); err != nil {
		return typedBytes, nil
	}
	// Overwrite original keys with typed values; unknown original keys are kept.
	for k, v := range typedMap {
		origMap[k] = v
	}
	merged, err := json.Marshal(origMap)
	if err != nil {
		return typedBytes, nil
	}
	return merged, nil
}

func parseRawObject(data json.RawMessage) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return map[string]json.RawMessage{}, nil
	}
	out := map[string]json.RawMessage{}
	if err := json.Unmarshal(trimmed, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func assertExperimentalAllowedChanges(before, after json.RawMessage) error {
	beforeMap, err := parseRawObject(before)
	if err != nil {
		return fmt.Errorf("invalid original experimental section: %w", err)
	}
	afterMap, err := parseRawObject(after)
	if err != nil {
		return fmt.Errorf("invalid updated experimental section: %w", err)
	}

	seen := make(map[string]struct{}, len(beforeMap)+len(afterMap))
	for key := range beforeMap {
		seen[key] = struct{}{}
	}
	for key := range afterMap {
		seen[key] = struct{}{}
	}

	for key := range seen {
		if key == "v2ray_api" {
			continue
		}
		if !jsonSemanticallyEqual(beforeMap[key], afterMap[key]) {
			return fmt.Errorf("subsection %q changed outside allowed scope", key)
		}
	}
	return nil
}

func assertAllowedScopeChanges(before, after []byte) error {
	beforeTop := map[string]json.RawMessage{}
	afterTop := map[string]json.RawMessage{}
	if err := json.Unmarshal(before, &beforeTop); err != nil {
		return fmt.Errorf("invalid original config json: %w", err)
	}
	if err := json.Unmarshal(after, &afterTop); err != nil {
		return fmt.Errorf("invalid updated config json: %w", err)
	}

	seen := make(map[string]struct{}, len(beforeTop)+len(afterTop))
	for key := range beforeTop {
		seen[key] = struct{}{}
	}
	for key := range afterTop {
		seen[key] = struct{}{}
	}

	for key := range seen {
		switch key {
		case "inbounds":
			continue
		case "experimental":
			if err := assertExperimentalAllowedChanges(beforeTop[key], afterTop[key]); err != nil {
				return fmt.Errorf("experimental changed outside allowed scope: %w", err)
			}
		default:
			if !rawMessageEqual(beforeTop[key], afterTop[key]) {
				return fmt.Errorf("top-level section %q changed outside allowed scope", key)
			}
		}
	}
	return nil
}

// GetSingboxConfigMap reads the raw config file content as a map
func (c *Config) GetSingboxConfigMap() (map[string]interface{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readSingboxConfigMapLocked()
}

func (c *Config) GetSingboxDNS() (map[string]interface{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	rawConfig, err := c.readSingboxConfigMapLocked()
	if err != nil {
		return nil, err
	}
	dnsSection, ok := rawConfig["dns"].(map[string]interface{})
	if !ok || dnsSection == nil {
		return map[string]interface{}{}, nil
	}
	return dnsSection, nil
}

func (c *Config) UpdateSingboxDNS(dns map[string]interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	rawConfig, err := c.readSingboxConfigMapLocked()
	if err != nil {
		return err
	}

	if len(dns) == 0 {
		delete(rawConfig, "dns")
	} else {
		rawConfig["dns"] = dns
	}
	return c.writeValidatedSingboxConfigMapLocked(rawConfig)
}

// UpdateSingboxConfig writes raw content to config file and restarts service
func (c *Config) UpdateSingboxConfig(content string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 1. Validate JSON structure
	var validCheck map[string]interface{}
	if err := json.Unmarshal([]byte(content), &validCheck); err != nil {
		return fmt.Errorf("invalid json: %v", err)
	}

	// 2. Validate with Sing-box
	if err := c.ValidateConfig([]byte(content)); err != nil {
		return fmt.Errorf("sing-box validation failed: %v", err)
	}

	// 3. Write to file
	if c.executor != nil {
		if err := c.executor.WriteConfig(context.Background(), c.SingboxConfigPath, []byte(content), 0644); err != nil {
			return err
		}
	} else {
		if err := os.WriteFile(c.SingboxConfigPath, []byte(content), 0644); err != nil {
			return err
		}
	}

	// 4. Mark pending restart (lock already held)
	c.SingboxPendingChanges = true
	return nil
}

// ModifySingboxConfig safely modifies the configuration using a callback
func (c *Config) ModifySingboxConfig(modifier func(*SingboxConfig) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.modifySingboxConfig(modifier)
}

func (c *Config) modifySingboxConfig(modifier func(*SingboxConfig) error) error {
	content, err := c.readSingboxConfigLocked()
	if err != nil {
		return err
	}

	var cfg SingboxConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return err
	}

	beforeTyped, err := json.Marshal(&cfg)
	if err != nil {
		return err
	}

	if err := modifier(&cfg); err != nil {
		return err
	}

	afterTyped, err := json.Marshal(&cfg)
	if err != nil {
		return err
	}

	if jsonSemanticallyEqual(beforeTyped, afterTyped) {
		return nil
	}

	data, err := json.MarshalIndent(&cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := assertAllowedScopeChanges(content, data); err != nil {
		return err
	}

	if err := c.ValidateConfig(data); err != nil {
		return fmt.Errorf("sing-box validation failed: %v", err)
	}

	if err := c.writeSingboxConfigLocked(data); err != nil {
		return err
	}

	return c.afterSingboxConfigWriteLocked(&cfg)
}

func decodeSingboxInboundMeta(rawInbound json.RawMessage) (SingboxInboundMeta, error) {
	inboundMap := map[string]interface{}{}
	if err := json.Unmarshal(rawInbound, &inboundMap); err != nil {
		return SingboxInboundMeta{}, err
	}
	return parseSingboxInboundMetaFromMap(inboundMap), nil
}

func parseSingboxInboundMetaFromMap(inboundMap map[string]interface{}) SingboxInboundMeta {
	tag, _ := inboundMap["tag"].(string)
	inboundType, _ := inboundMap["type"].(string)
	return SingboxInboundMeta{
		Tag:        strings.TrimSpace(tag),
		Type:       strings.ToLower(strings.TrimSpace(inboundType)),
		ListenPort: parseAlterIDValue(inboundMap["listen_port"]),
	}
}

func decodeSingboxInboundUserViews(rawUsers interface{}) []SingboxInboundUserView {
	userInterfaces, ok := rawUsers.([]interface{})
	if !ok || len(userInterfaces) == 0 {
		return nil
	}

	users := make([]SingboxInboundUserView, 0, len(userInterfaces))
	for _, rawUser := range userInterfaces {
		userMap, ok := rawUser.(map[string]interface{})
		if !ok || len(userMap) == 0 {
			continue
		}

		user := SingboxInboundUserView{}
		user.Name, _ = userMap["name"].(string)
		if user.Name == "" {
			user.Name, _ = userMap["username"].(string)
		}
		user.UUID, _ = userMap["uuid"].(string)
		user.ID, _ = userMap["id"].(string)
		user.Password, _ = userMap["password"].(string)
		user.Flow, _ = userMap["flow"].(string)
		user.Security, _ = userMap["security"].(string)
		if alterRaw, ok := userMap["alterId"]; ok {
			user.AlterID = parseAlterIDValue(alterRaw)
		} else if alterRaw, ok := userMap["alter_id"]; ok {
			vmessLegacyAlterIDWarnOnce.Do(func() {
				log.Printf("singbox vmess legacy key detected: normalizing alter_id to alterId")
			})
			user.AlterID = parseAlterIDValue(alterRaw)
		}

		users = append(users, user)
	}
	return users
}

func decodeSingboxInboundView(rawInbound json.RawMessage) (SingboxInboundView, error) {
	inboundMap := map[string]interface{}{}
	if err := json.Unmarshal(rawInbound, &inboundMap); err != nil {
		return SingboxInboundView{}, err
	}
	meta := parseSingboxInboundMetaFromMap(inboundMap)

	var tlsCfg *TLSConfig
	rawKeys := map[string]json.RawMessage{}
	if err := json.Unmarshal(rawInbound, &rawKeys); err == nil {
		if tlsRaw, ok := rawKeys["tls"]; ok && len(tlsRaw) > 0 && string(tlsRaw) != "null" {
			var t TLSConfig
			if err := json.Unmarshal(tlsRaw, &t); err == nil {
				tlsCfg = &t
			}
		}
	}

	return SingboxInboundView{
		Tag:        meta.Tag,
		Type:       meta.Type,
		ListenPort: meta.ListenPort,
		Users:      decodeSingboxInboundUserViews(inboundMap["users"]),
		TLS:        tlsCfg,
		Raw:        inboundMap,
	}, nil
}

func decodeSingboxOutboundView(rawOutbound json.RawMessage) (SingboxOutboundView, error) {
	outboundMap := map[string]interface{}{}
	if err := json.Unmarshal(rawOutbound, &outboundMap); err != nil {
		return SingboxOutboundView{}, err
	}

	tag, _ := outboundMap["tag"].(string)
	outboundType, _ := outboundMap["type"].(string)
	server, _ := outboundMap["server"].(string)
	domainStrategy, _ := outboundMap["domain_strategy"].(string)
	domainResolver, _ := outboundMap["domain_resolver"].(string)

	return SingboxOutboundView{
		Tag:            strings.TrimSpace(tag),
		Type:           strings.ToLower(strings.TrimSpace(outboundType)),
		Server:         strings.TrimSpace(server),
		ServerPort:     parseAlterIDValue(outboundMap["server_port"]),
		DomainStrategy: strings.TrimSpace(domainStrategy),
		DomainResolver: strings.TrimSpace(domainResolver),
	}, nil
}

func (c *Config) getSingboxInboundViewsLocked() ([]SingboxInboundView, error) {
	content, err := c.readSingboxConfigLocked()
	if err != nil {
		return nil, err
	}

	var cfg SingboxConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, err
	}

	rawInbounds, err := decodeInboundRawList(cfg.Inbounds)
	if err != nil {
		return nil, err
	}
	views := make([]SingboxInboundView, 0, len(rawInbounds))
	for _, rawInbound := range rawInbounds {
		view, err := decodeSingboxInboundView(rawInbound)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (c *Config) GetSingboxInboundViews() ([]SingboxInboundView, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getSingboxInboundViewsLocked()
}

func (c *Config) GetSingboxInboundView(tag string) (*SingboxInboundView, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	needle := strings.TrimSpace(tag)
	if needle == "" {
		return nil, fmt.Errorf("inbound tag is required")
	}

	views, err := c.getSingboxInboundViewsLocked()
	if err != nil {
		return nil, err
	}
	for i := range views {
		if views[i].Tag == needle {
			view := views[i]
			return &view, nil
		}
	}

	return nil, fmt.Errorf("inbound with tag '%s' not found", needle)
}

// GetSingboxInbounds returns the list of inbounds as map objects.
func (c *Config) GetSingboxInbounds() ([]map[string]interface{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	views, err := c.getSingboxInboundViewsLocked()
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(views))
	for _, view := range views {
		result = append(result, view.Raw)
	}
	return result, nil
}

func (c *Config) GetSingboxInboundMetas() ([]SingboxInboundMeta, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	views, err := c.getSingboxInboundViewsLocked()
	if err != nil {
		return nil, err
	}
	metas := make([]SingboxInboundMeta, 0, len(views))
	for _, view := range views {
		metas = append(metas, SingboxInboundMeta{
			Tag:        view.Tag,
			Type:       view.Type,
			ListenPort: view.ListenPort,
		})
	}
	return metas, nil
}

func (c *Config) GetSingboxInboundMeta(tag string) (*SingboxInboundMeta, error) {
	view, err := c.GetSingboxInboundView(tag)
	if err != nil {
		return nil, err
	}
	return &SingboxInboundMeta{
		Tag:        view.Tag,
		Type:       view.Type,
		ListenPort: view.ListenPort,
	}, nil
}

func (c *Config) GetSingboxInboundByTag(tag string) (map[string]interface{}, error) {
	view, err := c.GetSingboxInboundView(tag)
	if err != nil {
		return nil, err
	}
	return view.Raw, nil
}

func (c *Config) GetSingboxOutboundViews() ([]SingboxOutboundView, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	content, err := c.readSingboxConfigLocked()
	if err != nil {
		return nil, err
	}

	var cfg SingboxConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, err
	}

	rawOutbounds, err := decodeRawList(cfg.Outbounds)
	if err != nil {
		return nil, err
	}

	views := make([]SingboxOutboundView, 0, len(rawOutbounds))
	for _, rawOutbound := range rawOutbounds {
		view, err := decodeSingboxOutboundView(rawOutbound)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (c *Config) UpdateSingboxOutboundDomainStrategies(updates []SingboxOutboundDomainStrategyUpdate) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	content, err := c.readSingboxConfigLocked()
	if err != nil {
		return err
	}

	var cfg SingboxConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return err
	}

	rawOutbounds, err := decodeRawList(cfg.Outbounds)
	if err != nil {
		return err
	}

	updatesByTag := make(map[string]string, len(updates))
	for _, update := range updates {
		tag := strings.TrimSpace(update.Tag)
		if tag == "" {
			return fmt.Errorf("outbound tag is required")
		}
		updatesByTag[tag] = strings.TrimSpace(update.DomainStrategy)
	}

	foundTags := make(map[string]bool, len(updatesByTag))
	for i, rawOutbound := range rawOutbounds {
		outboundMap := map[string]interface{}{}
		if err := json.Unmarshal(rawOutbound, &outboundMap); err != nil {
			return err
		}

		tag, _ := outboundMap["tag"].(string)
		tag = strings.TrimSpace(tag)
		nextStrategy, ok := updatesByTag[tag]
		if !ok {
			continue
		}
		foundTags[tag] = true

		if nextStrategy == "" {
			delete(outboundMap, "domain_strategy")
		} else {
			outboundMap["domain_strategy"] = nextStrategy
		}

		updatedRaw, err := json.Marshal(outboundMap)
		if err != nil {
			return err
		}
		rawOutbounds[i] = updatedRaw
	}

	for tag := range updatesByTag {
		if !foundTags[tag] {
			return fmt.Errorf("outbound with tag '%s' not found", tag)
		}
	}

	cfg.Outbounds, err = encodeRawList(rawOutbounds)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(&cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := c.ValidateConfig(data); err != nil {
		return fmt.Errorf("sing-box validation failed: %v", err)
	}
	if err := c.writeSingboxConfigLocked(data); err != nil {
		return err
	}

	return c.afterSingboxConfigWriteLocked(&cfg)
}

type UserInboundInfo struct {
	Tag           string `json:"tag"`
	UUID          string `json:"uuid"`
	Password      string `json:"password,omitempty"`
	Flow          string `json:"flow,omitempty"`
	VmessSecurity string `json:"vmess_security,omitempty"`
	VmessAlterID  int    `json:"vmess_alter_id,omitempty"`
}

// GetUserInbounds returns inbound tags with per-inbound flow/uuid for a user.
func (c *Config) GetUserInbounds(name string) ([]UserInboundInfo, error) {
	inbounds, err := c.GetSingboxInboundViews()
	if err != nil {
		return nil, err
	}

	result := []UserInboundInfo{}
	for _, inbound := range inbounds {
		if inbound.Tag == "" {
			continue
		}
		if len(inbound.Users) == 0 {
			continue
		}
		for _, user := range inbound.Users {
			if user.Name != name {
				continue
			}
			uuid := user.UUID
			flow := user.Flow
			switch inbound.Type {
			case "hysteria2":
				result = append(result, UserInboundInfo{
					Tag:      inbound.Tag,
					Password: user.Password,
				})
				continue
			case "anytls":
				result = append(result, UserInboundInfo{
					Tag:      inbound.Tag,
					Password: user.Password,
				})
				continue
			case "naive":
				result = append(result, UserInboundInfo{
					Tag:      inbound.Tag,
					Password: user.Password,
				})
				continue
			case "shadowsocks":
				result = append(result, UserInboundInfo{
					Tag:      inbound.Tag,
					Password: user.Password,
				})
				continue
			case "trojan":
				uuid = user.Password
				flow = ""
			case "vmess":
				if uuid == "" {
					uuid = user.ID
				}
				flow = ""
			}
			result = append(result, UserInboundInfo{
				Tag:           inbound.Tag,
				UUID:          uuid,
				Flow:          flow,
				VmessSecurity: user.Security,
				VmessAlterID:  user.AlterID,
			})
		}
	}
	return result, nil
}

// AddSingboxInbound appends a new inbound block
func (c *Config) AddSingboxInbound(newInbound map[string]interface{}) error {
	err := c.ModifySingboxConfig(func(cfg *SingboxConfig) error {
		inbounds, err := decodeInboundRawList(cfg.Inbounds)
		if err != nil {
			return err
		}

		newTag, _ := newInbound["tag"].(string)
		if newTag != "" {
			for _, rawInbound := range inbounds {
				inboundMap := map[string]interface{}{}
				if err := json.Unmarshal(rawInbound, &inboundMap); err != nil {
					return err
				}
				if tag, _ := inboundMap["tag"].(string); tag == newTag {
					return fmt.Errorf("inbound with tag '%s' already exists", newTag)
				}
			}
		}

		newInboundRaw, err := json.Marshal(newInbound)
		if err != nil {
			return err
		}
		inbounds = append(inbounds, newInboundRaw)
		cfg.Inbounds, err = encodeInboundRawList(inbounds)
		return err
	})

	if err != nil {
		return err
	}

	// Sync to managed_inbounds
	return c.SyncInboundsFromSingbox()
}

// UpdateSingboxInbound updates an existing inbound by tag
func (c *Config) UpdateSingboxInbound(tag string, updatedInbound map[string]interface{}) error {
	// Check if tag is being renamed
	newTag, _ := updatedInbound["tag"].(string)
	tagChanged := newTag != "" && newTag != tag
	SanitiseManagedInboundFields(updatedInbound)
	writeStatsInbounds := statsInboundsForWrite(c.StatsInbounds, tag, newTag)

	err := c.ModifySingboxConfig(func(cfg *SingboxConfig) error {
		inbounds, err := decodeInboundRawList(cfg.Inbounds)
		if err != nil {
			return err
		}

		updatedRaw, err := json.Marshal(updatedInbound)
		if err != nil {
			return err
		}
		updatedTyped, err := decodeTypedInbound(updatedRaw)
		if err != nil {
			return err
		}

		found := false
		for i, rawInbound := range inbounds {
			inboundMap := map[string]interface{}{}
			if err := json.Unmarshal(rawInbound, &inboundMap); err != nil {
				return err
			}
			if tagChanged {
				if existingTag, _ := inboundMap["tag"].(string); existingTag == newTag {
					return fmt.Errorf("inbound with tag '%s' already exists", newTag)
				}
			}
			if currentTag, _ := inboundMap["tag"].(string); currentTag == tag {
				data, err := json.Marshal(updatedTyped)
				if err != nil {
					return err
				}
				if merged, mergeErr := mergeInboundWithOriginal(rawInbound, data); mergeErr == nil {
					data = merged
				}
				inbounds[i] = data
				if tagChanged {
					renameExperimentalStatsInbound(cfg, tag, newTag)
				}
				ensureExperimentalStatsInbounds(cfg, writeStatsInbounds)
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("inbound with tag '%s' not found", tag)
		}

		cfg.Inbounds, err = encodeInboundRawList(inbounds)
		return err
	})

	if err != nil {
		return err
	}

	// If tag was renamed, update in managed lists
	if tagChanged {
		if err := c.RenameInboundInLists(tag, newTag); err != nil {
			return err
		}
	}

	// Sync to managed_inbounds
	return c.SyncInboundsFromSingbox()
}

// DeleteSingboxInbound removes an inbound by tag
func (c *Config) DeleteSingboxInbound(tag string) error {
	err := c.ModifySingboxConfig(func(cfg *SingboxConfig) error {
		inbounds, err := decodeInboundRawList(cfg.Inbounds)
		if err != nil {
			return err
		}

		newInbounds := make([]json.RawMessage, 0, len(inbounds))
		found := false
		for _, rawInbound := range inbounds {
			inboundMap := map[string]interface{}{}
			if err := json.Unmarshal(rawInbound, &inboundMap); err != nil {
				return err
			}
			if currentTag, _ := inboundMap["tag"].(string); currentTag == tag {
				found = true
				continue
			}
			newInbounds = append(newInbounds, rawInbound)
		}

		if !found {
			return fmt.Errorf("inbound with tag '%s' not found", tag)
		}

		cfg.Inbounds, err = encodeInboundRawList(newInbounds)
		return err
	})

	if err != nil {
		return err
	}

	// Remove from managed lists
	return c.RemoveInboundFromLists(tag)
}

func (c *Config) saveAndReload(rawConfig *SingboxConfig) error {
	data, err := json.MarshalIndent(rawConfig, "", "  ")
	if err != nil {
		return err
	}

	// Validate before save
	if err := c.ValidateConfig(data); err != nil {
		return fmt.Errorf("sing-box validation failed: %v", err)
	}

	if c.executor != nil {
		if err := c.executor.WriteConfig(context.Background(), c.SingboxConfigPath, data, 0644); err != nil {
			return err
		}
	} else {
		if err := os.WriteFile(c.SingboxConfigPath, data, 0644); err != nil {
			return err
		}
	}

	return c.afterSingboxConfigWriteLocked(rawConfig)
}

func (c *Config) ValidateConfig(content []byte) error {
	if !c.EnableSingbox {
		return nil
	}

	// Check for port collisions manually since sing-box check might miss them
	if err := c.DetectPortCollision(content); err != nil {
		return err
	}

	if c.executor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return c.executor.ValidateSingboxConfig(ctx, content)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "singbox_check_*.json")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(content); err != nil {
		return fmt.Errorf("failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %v", err)
	}

	// Run sing-box check with timeout to avoid hanging the API request.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sing-box", "check", "-c", tmpFile.Name())
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("sing-box check timeout")
	}
	if err != nil {
		return fmt.Errorf("invalid config: %s", string(output))
	}

	return nil
}

// DetectPortCollision parses the config and checks for overlapping ports in inbounds
func (c *Config) DetectPortCollision(content []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(content, &raw); err != nil {
		return fmt.Errorf("invalid json structure: %v", err)
	}

	inbounds, ok := raw["inbounds"].([]interface{})
	if !ok {
		return nil
	}

	// Map of Port -> Tag
	usedPorts := make(map[int]string)

	for _, inb := range inbounds {
		inbMap, ok := inb.(map[string]interface{})
		if !ok {
			continue
		}

		tag, _ := inbMap["tag"].(string)

		// check "listen_port" (int)
		if portVal, ok := inbMap["listen_port"]; ok {
			if port, ok := portVal.(float64); ok { // json unmarshals numbers as float64
				p := int(port)
				if existingTag, exists := usedPorts[p]; exists {
					return fmt.Errorf("port %d is already in use by inbound '%s'", p, existingTag)
				}
				usedPorts[p] = tag
			}
		}

		// check "listen" (string) if it contains :port ?
		// sing-box "listen" usually is IP. "listen_port" is port.
		// However, for some types it might differ.
		// We focus on "listen_port" field which is standard for vless/vmess/mixed/etc.
	}

	return nil
}

func (c *Config) ReloadSingbox() error {
	if !c.EnableSingbox {
		return nil
	}

	raw, err := c.readSingboxConfigLocked()
	if err == nil {
		var cfg SingboxConfig
		if json.Unmarshal(raw, &cfg) == nil &&
			cfg.Experimental != nil &&
			cfg.Experimental.ClashAPI != nil &&
			cfg.Experimental.ClashAPI.ExternalController != "" {
			return c.reloadViaClashAPI(cfg.Experimental.ClashAPI)
		}
	}

	if c.executor != nil {
		return c.executor.RestartService(context.Background(), "sing-box")
	}

	// Assuming systemd usage
	cmd := exec.Command("systemctl", "restart", "sing-box")
	return cmd.Run()
}

func (c *Config) reloadViaClashAPI(api *ClashAPI) error {
	body, err := json.Marshal(map[string]string{"path": c.SingboxConfigPath})
	if err != nil {
		return fmt.Errorf("clash API reload: marshal request body: %w", err)
	}

	req, err := http.NewRequest(
		http.MethodPut,
		fmt.Sprintf("http://%s/configs?force=false", api.ExternalController),
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("clash API reload: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if api.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+api.Secret)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("clash API reload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("clash API reload: unexpected status %d", resp.StatusCode)
	}

	return nil
}

// GetSingboxRouteRules reads the current route.rules array from the config.
func (c *Config) GetSingboxRouteRules() ([]map[string]interface{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	raw, err := c.readSingboxConfigMapLocked()
	if err != nil {
		return nil, err
	}

	route, _ := raw["route"].(map[string]interface{})
	if route == nil {
		return []map[string]interface{}{}, nil
	}

	rulesRaw, _ := route["rules"].([]interface{})
	rules := make([]map[string]interface{}, 0, len(rulesRaw))
	for _, r := range rulesRaw {
		if m, ok := r.(map[string]interface{}); ok {
			rules = append(rules, m)
		}
	}
	return rules, nil
}

// UpsertSingboxRouteRules merges newRules into route.rules, skipping duplicates.
// Two rules are considered identical when their inbound+protocol+action+outbound fields match.
func (c *Config) UpsertSingboxRouteRules(newRules []map[string]interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	raw, err := c.readSingboxConfigMapLocked()
	if err != nil {
		return err
	}

	// Ensure route map exists.
	route, _ := raw["route"].(map[string]interface{})
	if route == nil {
		route = map[string]interface{}{}
	}

	// Read existing rules.
	existingRaw, _ := route["rules"].([]interface{})
	existing := make([]map[string]interface{}, 0, len(existingRaw))
	for _, r := range existingRaw {
		if m, ok := r.(map[string]interface{}); ok {
			existing = append(existing, m)
		}
	}

	ruleKey := func(m map[string]interface{}) string {
		data, _ := json.Marshal([]interface{}{m["inbound"], m["protocol"], m["action"], m["outbound"]})
		return string(data)
	}

	seen := make(map[string]struct{}, len(existing))
	for _, r := range existing {
		seen[ruleKey(r)] = struct{}{}
	}

	added := 0
	for _, nr := range newRules {
		k := ruleKey(nr)
		if _, dup := seen[k]; dup {
			continue
		}
		existing = append(existing, nr)
		seen[k] = struct{}{}
		added++
	}

	if added == 0 {
		return nil // nothing changed
	}

	// Convert []map back to []interface{} for JSON compatibility.
	merged := make([]interface{}, len(existing))
	for i, m := range existing {
		merged[i] = m
	}
	route["rules"] = merged
	raw["route"] = route

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := c.ValidateConfig(data); err != nil {
		return fmt.Errorf("sing-box validation failed: %v", err)
	}
	if err := c.writeSingboxConfigLocked(data); err != nil {
		return err
	}
	return c.afterSingboxConfigWriteLocked(nil)
}

type RouteTagRuleResolution struct {
	Index        int
	Rule         map[string]interface{}
	AuthUsers    []string
	Broken       bool
	BrokenReason string
}

func copyStringInterfaceMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	data, err := json.Marshal(in)
	if err != nil {
		out := make(map[string]interface{}, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		out = make(map[string]interface{}, len(in))
		for k, v := range in {
			out[k] = v
		}
	}
	return out
}

func normalizeAuthUsers(value interface{}) ([]string, error) {
	rawValues := []interface{}{}
	switch v := value.(type) {
	case string:
		rawValues = append(rawValues, v)
	case []interface{}:
		rawValues = v
	case []string:
		for _, item := range v {
			rawValues = append(rawValues, item)
		}
	default:
		return nil, fmt.Errorf("auth_user must be a string or string array")
	}

	seen := make(map[string]struct{}, len(rawValues))
	out := make([]string, 0, len(rawValues))
	for _, raw := range rawValues {
		user, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("auth_user must contain only strings")
		}
		user = strings.TrimSpace(user)
		if user == "" {
			continue
		}
		if _, exists := seen[user]; exists {
			continue
		}
		seen[user] = struct{}{}
		out = append(out, user)
	}
	return out, nil
}

func containsRouteTagString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func CanonicalRouteTagRuleMatch(rule map[string]interface{}) (string, error) {
	if rule == nil {
		return "", fmt.Errorf("route rule is required")
	}
	action, hasAction := rule["action"]
	if hasAction {
		actionString, ok := action.(string)
		if !ok || strings.TrimSpace(actionString) != "route" {
			return "", fmt.Errorf("route tag rule incompatible: action must be route")
		}
	}
	outbound, ok := rule["outbound"].(string)
	if !ok || strings.TrimSpace(outbound) == "" {
		return "", fmt.Errorf("route tag rule incompatible: outbound is required")
	}
	authUser, ok := rule["auth_user"]
	if !ok {
		return "", fmt.Errorf("route tag rule incompatible: auth_user is required")
	}
	if _, err := normalizeAuthUsers(authUser); err != nil {
		return "", fmt.Errorf("route tag rule incompatible: %w", err)
	}

	copyRule := copyStringInterfaceMap(rule)
	delete(copyRule, "auth_user")
	data, err := json.Marshal(copyRule)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func routeRulesFromRawConfig(raw map[string]interface{}) ([]map[string]interface{}, error) {
	route, _ := raw["route"].(map[string]interface{})
	if route == nil {
		return []map[string]interface{}{}, nil
	}
	rulesRaw, _ := route["rules"].([]interface{})
	rules := make([]map[string]interface{}, 0, len(rulesRaw))
	for _, rawRule := range rulesRaw {
		rule, ok := rawRule.(map[string]interface{})
		if !ok {
			continue
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func compactJSONString(input string) (string, error) {
	var out bytes.Buffer
	if err := json.Compact(&out, []byte(strings.TrimSpace(input))); err != nil {
		return "", err
	}
	return out.String(), nil
}

func resolveRouteTagRuleFromRules(ruleMatchJSON string, rules []map[string]interface{}) (RouteTagRuleResolution, error) {
	needle, err := compactJSONString(ruleMatchJSON)
	if err != nil {
		return RouteTagRuleResolution{}, err
	}

	matches := []RouteTagRuleResolution{}
	for i, rule := range rules {
		canonical, err := CanonicalRouteTagRuleMatch(rule)
		if err != nil {
			continue
		}
		if canonical != needle {
			continue
		}
		authUsers, err := normalizeAuthUsers(rule["auth_user"])
		if err != nil {
			continue
		}
		matches = append(matches, RouteTagRuleResolution{
			Index:     i,
			Rule:      copyStringInterfaceMap(rule),
			AuthUsers: authUsers,
		})
	}

	switch len(matches) {
	case 0:
		return RouteTagRuleResolution{Index: -1, Broken: true, BrokenReason: "route_rule_not_found"}, nil
	case 1:
		return matches[0], nil
	default:
		return RouteTagRuleResolution{Index: -1, Broken: true, BrokenReason: "route_rule_ambiguous"}, nil
	}
}

func (c *Config) ResolveRouteTagRule(ruleMatchJSON string) (RouteTagRuleResolution, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	raw, err := c.readSingboxConfigMapLocked()
	if err != nil {
		return RouteTagRuleResolution{}, err
	}
	rules, err := routeRulesFromRawConfig(raw)
	if err != nil {
		return RouteTagRuleResolution{}, err
	}
	return resolveRouteTagRuleFromRules(ruleMatchJSON, rules)
}

func resolveUserRouteTagsFromRules(userName string, tags []UserRouteTag, rules []map[string]interface{}) ([]UserRouteTag, error) {
	userName = strings.TrimSpace(userName)
	if userName == "" {
		return []UserRouteTag{}, nil
	}
	assigned := make([]UserRouteTag, 0, len(tags))
	for _, tag := range tags {
		resolution, err := resolveRouteTagRuleFromRules(tag.RuleMatchJSON, rules)
		if err != nil {
			return nil, err
		}
		if resolution.Broken {
			continue
		}
		if containsRouteTagString(resolution.AuthUsers, userName) {
			assigned = append(assigned, tag)
		}
	}
	return assigned, nil
}

func (c *Config) ResolveUserRouteTags(userName string, tags []UserRouteTag) ([]UserRouteTag, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	raw, err := c.readSingboxConfigMapLocked()
	if err != nil {
		return nil, err
	}
	rules, err := routeRulesFromRawConfig(raw)
	if err != nil {
		return nil, err
	}
	return resolveUserRouteTagsFromRules(userName, tags, rules)
}

func dedupeRouteTagIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func routeTagByID(tags []UserRouteTag) map[int64]UserRouteTag {
	out := make(map[int64]UserRouteTag, len(tags))
	for _, tag := range tags {
		out[tag.ID] = tag
	}
	return out
}

func addAuthUser(users []string, userName string) []string {
	if containsRouteTagString(users, userName) {
		return users
	}
	return append(users, userName)
}

func removeAuthUser(users []string, userName string) []string {
	out := make([]string, 0, len(users))
	for _, user := range users {
		if user != userName {
			out = append(out, user)
		}
	}
	return out
}

func (c *Config) UpdateUserRouteTagMembership(userName string, targetTagIDs []int64, tags []UserRouteTag) ([]UserRouteTag, error) {
	userName = strings.TrimSpace(userName)
	if userName == "" {
		return nil, fmt.Errorf("user name is required")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	raw, err := c.readSingboxConfigMapLocked()
	if err != nil {
		return nil, err
	}
	rules, err := routeRulesFromRawConfig(raw)
	if err != nil {
		return nil, err
	}

	tagsByID := routeTagByID(tags)
	currentTags, err := resolveUserRouteTagsFromRules(userName, tags, rules)
	if err != nil {
		return nil, err
	}

	currentSet := make(map[int64]struct{}, len(currentTags))
	for _, tag := range currentTags {
		currentSet[tag.ID] = struct{}{}
	}
	targetSet := make(map[int64]struct{}, len(targetTagIDs))
	for _, id := range dedupeRouteTagIDs(targetTagIDs) {
		targetSet[id] = struct{}{}
	}

	changedSet := map[int64]struct{}{}
	for id := range currentSet {
		if _, ok := targetSet[id]; !ok {
			changedSet[id] = struct{}{}
		}
	}
	for id := range targetSet {
		if _, ok := currentSet[id]; !ok {
			changedSet[id] = struct{}{}
		}
	}
	if len(changedSet) == 0 {
		return currentTags, nil
	}

	for id := range changedSet {
		tag, ok := tagsByID[id]
		if !ok {
			return nil, fmt.Errorf("route tag needs relink: tag %d missing", id)
		}
		resolution, err := resolveRouteTagRuleFromRules(tag.RuleMatchJSON, rules)
		if err != nil {
			return nil, err
		}
		if resolution.Broken {
			return nil, fmt.Errorf("route tag needs relink: tag %d %s", id, resolution.BrokenReason)
		}
		nextUsers := resolution.AuthUsers
		if _, shouldHave := targetSet[id]; shouldHave {
			nextUsers = addAuthUser(nextUsers, userName)
		} else {
			nextUsers = removeAuthUser(nextUsers, userName)
		}
		rules[resolution.Index]["auth_user"] = nextUsers
	}

	route, _ := raw["route"].(map[string]interface{})
	if route == nil {
		route = map[string]interface{}{}
	}
	mergedRules := make([]interface{}, len(rules))
	for i, rule := range rules {
		mergedRules[i] = rule
	}
	route["rules"] = mergedRules
	raw["route"] = route

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := c.ValidateConfig(data); err != nil {
		return nil, fmt.Errorf("sing-box validation failed: %v", err)
	}
	if err := c.writeSingboxConfigLocked(data); err != nil {
		return nil, err
	}
	if err := c.afterSingboxConfigWriteLocked(nil); err != nil {
		return nil, err
	}

	updatedRules, err := routeRulesFromRawConfig(raw)
	if err != nil {
		return nil, err
	}
	return resolveUserRouteTagsFromRules(userName, tags, updatedRules)
}

func (c *Config) afterSingboxConfigWriteLocked(_ *SingboxConfig) error {
	c.SingboxPendingChanges = true
	return nil
}
