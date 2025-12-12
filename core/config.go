package core

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

type Config struct {
	SingboxConfigPath     string   `json:"singbox_config_path"`
	SingboxAPIAddr        string   `json:"singbox_api_addr"`
	ManagedInbounds       []string `json:"managed_inbounds"`
	StatsInbounds         []string `json:"stats_inbounds"`
	StatsOutbounds        []string `json:"stats_outbounds"`
	AccessLogPath         string   `json:"access_log_path"`
	LogSource             string   `json:"log_source"` // "journal" or "file"
	DatabasePath          string   `json:"database_path"`
	ListenAddr            string   `json:"listen_addr"`
	WireGuardConfigPath   string   `json:"wireguard_config_path"`
	EnableWireGuard       bool     `json:"enable_wireguard"`
	EnableSingbox         bool     `json:"enable_singbox"`
	UseStatsSampler       bool     `json:"use_stats_sampler"`
	SamplerIntervalSec    int      `json:"sampler_interval_sec"`
	ActiveThresholdBytes  int64    `json:"active_threshold_bytes"`
	RetentionEnabled      bool     `json:"retention_enabled"`
	RetentionDays         int      `json:"retention_days"`
	WGSamplerIntervalSec  int      `json:"wg_sampler_interval_sec"`
	WGRetentionDays       int      `json:"wg_retention_days"`
	AggregationEnabled    bool     `json:"aggregation_enabled"`
	AggregationDays       int      `json:"aggregation_days"`
	PublicIP              string   `json:"public_ip"`
	SingboxPendingChanges bool     `json:"-"` // Not persisted, runtime flag
	ConfigPath            string   `json:"-"`
	APIKey                string   `json:"api_key"`

	JWTSecret string `json:"jwt_secret"`
	mu        sync.Mutex
}

type UserAccount struct {
	Name        string   `json:"name"`
	UUID        string   `json:"uuid"`
	Flow        string   `json:"flow"`
	InboundTags []string `json:"inbound_tags"`
}

func LoadConfig(path ...string) *Config {
	cfg := &Config{
		SingboxConfigPath:    "/etc/sing-box/config.json",
		SingboxAPIAddr:       "127.0.0.1:8080",
		ManagedInbounds:      []string{"in-reality"},
		StatsInbounds:        []string{"in-reality"},
		StatsOutbounds:       []string{"direct"},
		AccessLogPath:        "/var/log/singbox.log",
		LogSource:            "journal",
		DatabasePath:         "/var/lib/ogs-swg/stats.db",
		ListenAddr:           ":8080",
		WireGuardConfigPath:  "/etc/wireguard/wg0.conf",
		EnableWireGuard:      true,
		EnableSingbox:        true,
		APIKey:               "",
		UseStatsSampler:      true,
		SamplerIntervalSec:   120,
		ActiveThresholdBytes: 1024,
		RetentionEnabled:     false,
		RetentionDays:        90,
		AggregationEnabled:   false,
		AggregationDays:      7,
		WGSamplerIntervalSec: 60,
		WGRetentionDays:      30,

		JWTSecret: "replace-me-with-a-secure-secret-please",
	}

	configPath := "config.json"
	if len(path) > 0 && path[0] != "" {
		configPath = path[0]
	}
	cfg.ConfigPath = configPath

	f, err := os.Open(configPath)
	if err == nil {
		defer f.Close()
		json.NewDecoder(f).Decode(cfg)
	}

	return cfg
}

func (c *Config) GetActiveUsers() ([]UserAccount, error) {
	inbounds, err := c.GetSingboxInbounds()
	if err != nil {
		return nil, err
	}

	userMap := make(map[string]*UserAccount)
	tagFilter := make(map[string]bool)
	for _, t := range c.ManagedInbounds {
		if t != "" {
			tagFilter[t] = true
		}
	}

	for _, inbound := range inbounds {
		tag, _ := inbound["tag"].(string)

		// Filter by managed tags
		if len(tagFilter) > 0 && !tagFilter[tag] {
			continue
		}

		inbType, _ := inbound["type"].(string)
		if inbType != "vless" {
			continue
		}

		usersList, ok := inbound["users"].([]interface{})
		if !ok {
			continue
		}

		for _, u := range usersList {
			userMapData, ok := u.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := userMapData["name"].(string)
			uuid, _ := userMapData["uuid"].(string)
			flow, _ := userMapData["flow"].(string)

			if name != "" {
				if existing, exists := userMap[name]; exists {
					// Add tag if not exists
					found := false
					for _, t := range existing.InboundTags {
						if t == tag {
							found = true
							break
						}
					}
					if !found {
						existing.InboundTags = append(existing.InboundTags, tag)
					}
				} else {
					userMap[name] = &UserAccount{
						Name:        name,
						UUID:        uuid,
						Flow:        flow,
						InboundTags: []string{tag},
					}
				}
			}
		}
	}

	users := make([]UserAccount, 0, len(userMap))
	for _, u := range userMap {
		users = append(users, *u)
	}
	return users, nil
}

func (c *Config) AddUser(name, uuid, flow, inboundTag string) error {
	if inboundTag == "" {
		return fmt.Errorf("inbound tag is required")
	}
	flow = normalizeFlow(flow)

	return c.ModifySingboxConfig(func(cfgMap SingboxConfigRaw) error {
		inbounds := c.findManagedInbounds(cfgMap)
		if len(inbounds) == 0 {
			return os.ErrInvalid
		}

		// Find the specific inbound
		var targetInbound map[string]interface{}
		for _, inbound := range inbounds {
			if tag, ok := inbound["tag"].(string); ok && tag == inboundTag {
				targetInbound = inbound
				break
			}
		}

		if targetInbound == nil {
			return fmt.Errorf("inbound '%s' not found or not managed", inboundTag)
		}

		users := ensureUsers(targetInbound)
		for _, u := range users {
			if um, ok := u.(map[string]interface{}); ok {
				if um["name"] == name {
					return fmt.Errorf("user %s already exists in inbound %s", name, inboundTag)
				}
			}
		}

		user := map[string]interface{}{
			"name": name,
			"uuid": uuid,
		}
		if flow != "" {
			user["flow"] = flow
		}
		users = append(users, user)
		targetInbound["users"] = users

		c.syncStatsUsers(cfgMap)
		return nil
	})
}

func (c *Config) RemoveUser(name string) error {
	return c.ModifySingboxConfig(func(cfgMap SingboxConfigRaw) error {
		inbounds := c.findManagedInbounds(cfgMap)
		if len(inbounds) == 0 {
			return os.ErrInvalid
		}
		for _, inbound := range inbounds {
			users := ensureUsers(inbound)
			newUsers := []interface{}{}
			for _, u := range users {
				if um, ok := u.(map[string]interface{}); ok {
					if um["name"] == name {
						continue
					}
				}
				newUsers = append(newUsers, u)
			}
			inbound["users"] = newUsers
		}
		c.syncStatsUsers(cfgMap)
		return nil
	})
}

func (c *Config) UpdateUser(name, uuid, flow string) error {
	flow = normalizeFlow(flow)

	return c.ModifySingboxConfig(func(cfgMap SingboxConfigRaw) error {
		inbounds := c.findManagedInbounds(cfgMap)
		if len(inbounds) == 0 {
			return os.ErrInvalid
		}

		found := false
		for _, inbound := range inbounds {
			users := ensureUsers(inbound)
			for _, u := range users {
				if um, ok := u.(map[string]interface{}); ok {
					if um["name"] == name {
						um["uuid"] = uuid
						if flow != "" {
							um["flow"] = flow
						}
						found = true
					}
				}
			}
		}

		if !found {
			return fmt.Errorf("user %s not found", name)
		}

		return nil
	})
}

func (c *Config) findManagedInbounds(cfgMap map[string]interface{}) []map[string]interface{} {
	inbounds, ok := cfgMap["inbounds"].([]interface{})
	if !ok || len(inbounds) == 0 {
		return nil
	}

	managed := c.ManagedInbounds
	tagFilter := make(map[string]bool)
	for _, t := range managed {
		if t != "" {
			tagFilter[t] = true
		}
	}

	var result []map[string]interface{}
	for _, inbound := range inbounds {
		if inboundMap, ok := inbound.(map[string]interface{}); ok {
			if inboundMap["type"] != "vless" {
				continue
			}
			if len(tagFilter) > 0 {
				if tag, ok := inboundMap["tag"].(string); ok && tagFilter[tag] {
					result = append(result, inboundMap)
				}
			} else {
				result = append(result, inboundMap)
			}
		}
	}
	return result
}

func ensureUsers(inbound map[string]interface{}) []interface{} {
	clients, ok := inbound["users"].([]interface{})
	if !ok {
		clients = []interface{}{}
	}
	return clients
}

func (c *Config) syncStatsUsers(cfgMap map[string]interface{}) {
	names := []string{}
	seen := make(map[string]bool)
	tagFilter := make(map[string]bool)
	for _, t := range c.ManagedInbounds {
		if t != "" {
			tagFilter[t] = true
		}
	}
	if inbounds, ok := cfgMap["inbounds"].([]interface{}); ok {
		for _, inb := range inbounds {
			inbMap, ok := inb.(map[string]interface{})
			if !ok {
				continue
			}
			if inbMap["type"] != "vless" {
				continue
			}
			if len(tagFilter) > 0 {
				if tag, ok := inbMap["tag"].(string); ok && !tagFilter[tag] {
					continue
				}
			}
			users := ensureUsers(inbMap)
			for _, u := range users {
				if um, ok := u.(map[string]interface{}); ok {
					if name, ok := um["name"].(string); ok && name != "" && !seen[name] {
						names = append(names, name)
						seen[name] = true
					}
				}
			}
		}
	}

	exp, ok := cfgMap["experimental"].(map[string]interface{})
	if !ok {
		exp = map[string]interface{}{}
		cfgMap["experimental"] = exp
	}
	v2, ok := exp["v2ray_api"].(map[string]interface{})
	if !ok {
		v2 = map[string]interface{}{}
		exp["v2ray_api"] = v2
	}
	if _, ok := v2["listen"]; !ok || v2["listen"] == "" {
		v2["listen"] = c.SingboxAPIAddr
	}
	stats, ok := v2["stats"].(map[string]interface{})
	if !ok {
		stats = map[string]interface{}{}
		v2["stats"] = stats
	}
	stats["enabled"] = true
	if len(c.StatsInbounds) > 0 {
		stats["inbounds"] = toInterfaceSlice(c.StatsInbounds)
	}
	if len(c.StatsOutbounds) > 0 {
		stats["outbounds"] = toInterfaceSlice(c.StatsOutbounds)
	}
	stats["users"] = toInterfaceSlice(names)
}

func toInterfaceSlice(list []string) []interface{} {
	out := make([]interface{}, 0, len(list))
	for _, v := range list {
		out = append(out, v)
	}
	return out
}

func normalizeFlow(flow string) string {
	flow = strings.TrimSpace(flow)
	if strings.EqualFold(flow, "none") {
		return ""
	}
	return flow
}
func (c *Config) SaveAppConfig() error {
	path := c.ConfigPath
	if path == "" {
		path = "config.json"
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(c)
}

// MarkSingboxPending marks that Sing-box configuration has pending changes
func (c *Config) MarkSingboxPending() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.SingboxPendingChanges = true
}

// ApplySingboxChanges applies pending Sing-box configuration changes by reloading the service
func (c *Config) ApplySingboxChanges() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ReloadSingbox(); err != nil {
		return err
	}

	c.SingboxPendingChanges = false
	return nil
}

func (c *Config) SyncInboundsFromSingbox() error {
	inbounds, err := c.GetSingboxInbounds()
	if err != nil {
		return err
	}

	saveNeeded := false
	managedSet := make(map[string]bool)
	statsSet := make(map[string]bool)

	for _, t := range c.ManagedInbounds {
		managedSet[t] = true
	}
	for _, t := range c.StatsInbounds {
		statsSet[t] = true
	}

	for _, inb := range inbounds {
		tag, ok := inb["tag"].(string)
		if !ok || tag == "" {
			continue
		}
		inbType, _ := inb["type"].(string)

		// Auto-discover VLESS, VMess, Trojan
		if inbType == "vless" || inbType == "vmess" || inbType == "trojan" {
			if !managedSet[tag] {
				c.ManagedInbounds = append(c.ManagedInbounds, tag)
				managedSet[tag] = true
				saveNeeded = true
			}
			if !statsSet[tag] {
				c.StatsInbounds = append(c.StatsInbounds, tag)
				statsSet[tag] = true
				saveNeeded = true
			}
		}
	}

	if saveNeeded {
		return c.SaveAppConfig()
	}
	return nil
}

// RemoveInboundFromLists removes an inbound tag from managed_inbounds and stats_inbounds
func (c *Config) RemoveInboundFromLists(tag string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	changed := false

	// Remove from ManagedInbounds
	newManaged := []string{}
	for _, t := range c.ManagedInbounds {
		if t != tag {
			newManaged = append(newManaged, t)
		} else {
			changed = true
		}
	}
	c.ManagedInbounds = newManaged

	// Remove from StatsInbounds
	newStats := []string{}
	for _, t := range c.StatsInbounds {
		if t != tag {
			newStats = append(newStats, t)
		} else {
			changed = true
		}
	}
	c.StatsInbounds = newStats

	if changed {
		return c.SaveAppConfig()
	}
	return nil
}

// RenameInboundInLists updates an inbound tag in managed_inbounds and stats_inbounds
func (c *Config) RenameInboundInLists(oldTag, newTag string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	changed := false

	// Update in ManagedInbounds
	for i, t := range c.ManagedInbounds {
		if t == oldTag {
			c.ManagedInbounds[i] = newTag
			changed = true
			break
		}
	}

	// Update in StatsInbounds
	for i, t := range c.StatsInbounds {
		if t == oldTag {
			c.StatsInbounds[i] = newTag
			changed = true
			break
		}
	}

	if changed {
		return c.SaveAppConfig()
	}
	return nil
}
