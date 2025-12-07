package core

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	SingboxConfigPath    string   `json:"singbox_config_path"`
	SingboxAPIAddr       string   `json:"singbox_api_addr"`
	ManagedInbounds      []string `json:"managed_inbounds"`
	StatsInbounds        []string `json:"stats_inbounds"`
	StatsOutbounds       []string `json:"stats_outbounds"`
	AccessLogPath        string   `json:"access_log_path"`
	LogSource            string   `json:"log_source"` // "journal" or "file"
	DatabasePath         string   `json:"database_path"`
	ListenAddr           string   `json:"listen_addr"`
	WireGuardConfigPath  string   `json:"wireguard_config_path"`
	EnableWireGuard      bool     `json:"enable_wireguard"`
	EnableSingbox        bool     `json:"enable_singbox"`
	UseStatsSampler      bool     `json:"use_stats_sampler"`
	SamplerIntervalSec   int      `json:"sampler_interval_sec"`
	ActiveThresholdBytes int64    `json:"active_threshold_bytes"`
	RetentionEnabled     bool     `json:"retention_enabled"`
	RetentionDays        int      `json:"retention_days"`
	ConfigPath           string   `json:"-"`
	APIKey               string   `json:"api_key"`
}

type UserAccount struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
	Flow string `json:"flow"`
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

type SingboxConfig struct {
	Inbounds []struct {
		Type  string `json:"type"`
		Tag   string `json:"tag"`
		Users []struct {
			Name string `json:"name"`
			UUID string `json:"uuid"`
			Flow string `json:"flow"`
		} `json:"users"`
	} `json:"inbounds"`
}

func LoadUsersFromSingboxConfig(path string, managed []string) ([]UserAccount, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg SingboxConfig
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}

	uniqueUsers := make(map[string]bool)
	var users []UserAccount
	tagFilter := make(map[string]bool)
	for _, t := range managed {
		if t != "" {
			tagFilter[t] = true
		}
	}

	for _, inbound := range cfg.Inbounds {
		if inbound.Type != "vless" {
			continue
		}
		if len(tagFilter) > 0 && !tagFilter[inbound.Tag] {
			continue
		}
		for _, client := range inbound.Users {
			if client.Name != "" && !uniqueUsers[client.Name] {
				uniqueUsers[client.Name] = true
				users = append(users, UserAccount{
					Name: client.Name,
					UUID: client.UUID,
					Flow: client.Flow,
				})
			}
		}
	}
	return users, nil
}

func (c *Config) AddUser(name, uuid, flow string) error {
	cfgMap, err := c.loadConfigMap()
	if err != nil {
		return err
	}

	inbounds := c.findManagedInbounds(cfgMap)
	if len(inbounds) == 0 {
		return os.ErrInvalid
	}

	for _, inbound := range inbounds {
		users := ensureUsers(inbound)
		exists := false
		for _, u := range users {
			if um, ok := u.(map[string]interface{}); ok {
				if um["name"] == name {
					return fmt.Errorf("user %s already exists", name)
				}
			}
		}
		if !exists {
			users = append(users, map[string]interface{}{
				"name": name,
				"uuid": uuid,
				"flow": flow,
			})
		}
		inbound["users"] = users
	}

	c.syncStatsUsers(cfgMap)
	return c.saveConfigMap(cfgMap)
}

func (c *Config) RemoveUser(name string) error {
	cfgMap, err := c.loadConfigMap()
	if err != nil {
		return err
	}

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
	return c.saveConfigMap(cfgMap)
}

func (c *Config) UpdateUser(name, uuid, flow string) error {
	cfgMap, err := c.loadConfigMap()
	if err != nil {
		return err
	}

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
					found = true
					if uuid != "" {
						um["uuid"] = uuid
					}
					if flow != "" {
						um["flow"] = flow
					}
				}
			}
		}
		inbound["users"] = users
	}

	if !found {
		return fmt.Errorf("user not found")
	}

	c.syncStatsUsers(cfgMap)
	return c.saveConfigMap(cfgMap)
}

func (c *Config) saveConfigMap(cfgMap map[string]interface{}) error {
	f, err := os.Create(c.SingboxConfigPath)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(cfgMap)
}

func (c *Config) loadConfigMap() (map[string]interface{}, error) {
	content, err := os.ReadFile(c.SingboxConfigPath)
	if err != nil {
		return nil, err
	}

	var cfgMap map[string]interface{}
	if err := json.Unmarshal(content, &cfgMap); err != nil {
		return nil, err
	}
	return cfgMap, nil
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
