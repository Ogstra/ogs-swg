package core

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/joho/godotenv"
)

type Config struct {
	SingboxConfigPath     string   `json:"singbox_config_path" env:"OGS_SINGBOX_CONFIG_PATH" env-default:"/etc/sing-box/config.json"`
	SingboxAPIAddr        string   `json:"singbox_api_addr" env:"OGS_SINGBOX_API_ADDR" env-default:"127.0.0.1:8080"`
	ManagedInbounds       []string `json:"managed_inbounds" env:"OGS_MANAGED_INBOUNDS" env-default:"in-reality"`
	StatsInbounds         []string `json:"stats_inbounds" env:"OGS_STATS_INBOUNDS" env-default:"in-reality"`
	StatsOutbounds        []string `json:"stats_outbounds" env:"OGS_STATS_OUTBOUNDS" env-default:"direct"`
	AccessLogPath         string   `json:"access_log_path" env:"OGS_ACCESS_LOG_PATH" env-default:"data/access.log"`
	LogSource             string   `json:"log_source" env:"OGS_LOG_SOURCE" env-default:"journal"` // "journal" or "file"
	DatabasePath          string   `json:"database_path" env:"OGS_DB_PATH" env-default:"data/stats.db"`
	ListenAddr            string   `json:"listen_addr" env:"OGS_LISTEN_ADDR" env-default:":8080"`
	WireGuardConfigPath   string   `json:"wireguard_config_path" env:"OGS_WIREGUARD_CONFIG_PATH" env-default:"/etc/wireguard/wg0.conf"`
	EnableWireGuard       bool     `json:"enable_wireguard" env:"OGS_ENABLE_WIREGUARD" env-default:"true"`
	EnableSingbox         bool     `json:"enable_singbox" env:"OGS_ENABLE_SINGBOX" env-default:"true"`
	UseStatsSampler       bool     `json:"use_stats_sampler" env:"OGS_USE_STATS_SAMPLER" env-default:"true"`
	SamplerIntervalSec    int      `json:"sampler_interval_sec" env:"OGS_SAMPLER_INTERVAL_SEC" env-default:"120"`
	ActiveThresholdBytes  int64    `json:"active_threshold_bytes" env:"OGS_ACTIVE_THRESHOLD_BYTES" env-default:"1024"`
	RetentionEnabled      bool     `json:"retention_enabled" env:"OGS_RETENTION_ENABLED" env-default:"false"`
	RetentionDays         int      `json:"retention_days" env:"OGS_RETENTION_DAYS" env-default:"90"`
	WGSamplerIntervalSec  int      `json:"wg_sampler_interval_sec" env:"OGS_WG_SAMPLER_INTERVAL_SEC" env-default:"60"`
	WGRetentionDays       int      `json:"wg_retention_days" env:"OGS_WG_RETENTION_DAYS" env-default:"30"`
	AggregationEnabled    bool     `json:"aggregation_enabled" env:"OGS_AGGREGATION_ENABLED" env-default:"false"`
	AggregationDays       int      `json:"aggregation_days" env:"OGS_AGGREGATION_DAYS" env-default:"7"`
	PublicIP              string   `json:"public_ip" env:"OGS_PUBLIC_IP"`
	SingboxPendingChanges bool     `json:"-"` // Not persisted, runtime flag
	ConfigPath            string   `json:"-"`
	APIKey                string   `json:"api_key" env:"OGS_API_KEY"`

	// Execution mode: "local" (default/bare metal), "docker_local" (Docker on same host), "ssh" (remote via SSH).
	// docker_local takes priority over ssh_host. SSH mode is used only when ssh_host is set and execution_mode is not "docker_local".
	ExecutionMode string `json:"execution_mode" env:"OGS_EXECUTION_MODE"`

	// SSH Configuration
	SSHHost                  string `json:"ssh_host" env:"OGS_SSH_HOST"`
	SSHPort                  int    `json:"ssh_port" env:"OGS_SSH_PORT" env-default:"22"`
	SSHUser                  string `json:"ssh_user" env:"OGS_SSH_USER" env-default:"ogs_agent"`
	SSHKeyPath               string `json:"ssh_key_path" env:"OGS_SSH_KEY_PATH"`
	SSHKeyPassphrase         string `json:"ssh_key_passphrase" env:"OGS_SSH_KEY_PASS"`
	SSHKnownHostsPath        string `json:"ssh_known_hosts_path" env:"OGS_SSH_KNOWN_HOSTS"`
	SSHInsecureIgnoreHostKey bool   `json:"ssh_insecure_ignore_host_key" env:"OGS_SSH_INSECURE_IGNORE_HOST_KEY"`

	// Sysctl Whitelist (Optional override)
	SysctlWhitelist []string `json:"sysctl_whitelist" env:"OGS_SYSCTL_WHITELIST"`

	JWTSecret string `json:"jwt_secret" env:"OGS_JWT_SECRET"`

	executor SystemExecutor
	mu       sync.Mutex
}

type UserAccount struct {
	Name          string   `json:"name"`
	UUID          string   `json:"uuid"`
	Flow          string   `json:"flow"`
	VmessSecurity string   `json:"vmess_security,omitempty"`
	VmessAlterID  int      `json:"vmess_alter_id,omitempty"`
	InboundTags   []string `json:"inbound_tags"`
}

func isUserInboundType(inbType string) bool {
	switch strings.ToLower(strings.TrimSpace(inbType)) {
	case "vless", "vmess", "trojan":
		return true
	default:
		return false
	}
}

func inboundTypeFromMap(inbound map[string]interface{}) string {
	if inbType, ok := inbound["type"].(string); ok {
		return strings.ToLower(strings.TrimSpace(inbType))
	}
	return ""
}

func LoadConfig(path ...string) *Config {
	cfg := &Config{}

	configPath := "config.json"
	if len(path) > 0 && path[0] != "" {
		configPath = path[0]
	}
	cfg.ConfigPath = configPath

	// Preload .env if it exists
	_ = godotenv.Load(".env")

	// cleanenv.ReadConfig automatically reads the JSON file, extracts defaults from tags, and overrides with OS Env vars.
	err := cleanenv.ReadConfig(configPath, cfg)
	if err != nil {
		log.Printf("cleanenv: reading from %s failed, falling back to ENV variables (%v)", configPath, err)
		_ = cleanenv.ReadEnv(cfg)
	}

	// Legacy bind fallback
	if cfg.ListenAddr == "" || cfg.ListenAddr == ":8080" {
		if v := os.Getenv("OGS_BIND"); v != "" {
			cfg.ListenAddr = v
		}
	}

	// Generate secure JWT secret if not set or using default insecure value
	if cfg.JWTSecret == "" || cfg.JWTSecret == "replace-me-with-a-secure-secret-please" {
		secretBytes := make([]byte, 32)
		if _, err := rand.Read(secretBytes); err == nil {
			cfg.JWTSecret = base64.URLEncoding.EncodeToString(secretBytes)
		} else {
			// Fallback: use a timestamp-based secret (less secure but better than default)
			cfg.JWTSecret = fmt.Sprintf("auto-generated-%d", os.Getpid())
		}
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

		inbType := inboundTypeFromMap(inbound)
		if !isUserInboundType(inbType) {
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
			vmessSecurity, _ := userMapData["security"].(string)
			vmessAlterID := 0
			if alterRaw, ok := userMapData["alter_id"]; ok {
				switch v := alterRaw.(type) {
				case float64:
					vmessAlterID = int(v)
				case int:
					vmessAlterID = v
				case int64:
					vmessAlterID = int(v)
				case string:
					if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
						vmessAlterID = parsed
					}
				}
			}
			if inbType == "trojan" {
				uuid, _ = userMapData["password"].(string)
				flow = ""
			}
			if inbType == "vmess" {
				if uuid == "" {
					if id, ok := userMapData["id"].(string); ok {
						uuid = id
					}
				}
				flow = ""
			}

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
					if existing.UUID == "" && uuid != "" {
						existing.UUID = uuid
					}
					if existing.Flow == "" && flow != "" {
						existing.Flow = flow
					}
					if existing.VmessSecurity == "" && vmessSecurity != "" {
						existing.VmessSecurity = vmessSecurity
					}
					if existing.VmessAlterID == 0 && vmessAlterID != 0 {
						existing.VmessAlterID = vmessAlterID
					}
				} else {
					userMap[name] = &UserAccount{
						Name:          name,
						UUID:          uuid,
						Flow:          flow,
						VmessSecurity: vmessSecurity,
						VmessAlterID:  vmessAlterID,
						InboundTags:   []string{tag},
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

func (c *Config) AddUser(name, uuid, flow, inboundTag, vmessSecurity string, vmessAlterID int) error {
	if inboundTag == "" {
		return fmt.Errorf("inbound tag is required")
	}

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

		inbType := inboundTypeFromMap(targetInbound)
		if !isUserInboundType(inbType) {
			return fmt.Errorf("unsupported inbound type: %s", inbType)
		}

		users := ensureUsers(targetInbound)
		if inbType == "vmess" {
			sanitizeVmessUsers(users)
		}
		for _, u := range users {
			if um, ok := u.(map[string]interface{}); ok {
				if um["name"] == name {
					return fmt.Errorf("user %s already exists in inbound %s", name, inboundTag)
				}
			}
		}

		user := map[string]interface{}{
			"name": name,
		}
		switch inbType {
		case "vless":
			user["uuid"] = uuid
			flow = normalizeFlow(flow)
			if flow != "" {
				user["flow"] = flow
			}
		case "vmess":
			user["uuid"] = uuid
			if vmessAlterID != 0 {
				user["alter_id"] = vmessAlterID
			}
		case "trojan":
			user["password"] = uuid
		default:
			return fmt.Errorf("unsupported inbound type: %s", inbType)
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

// RemoveUserFromInbound removes a user from a specific inbound only
func (c *Config) RemoveUserFromInbound(name, inboundTag string) error {
	return c.ModifySingboxConfig(func(cfgMap SingboxConfigRaw) error {
		inbounds := c.findManagedInbounds(cfgMap)
		if len(inbounds) == 0 {
			return os.ErrInvalid
		}

		found := false
		for _, inbound := range inbounds {
			// Only process the specified inbound
			tag, _ := inbound["tag"].(string)
			if tag != inboundTag {
				continue
			}

			users := ensureUsers(inbound)
			newUsers := []interface{}{}
			for _, u := range users {
				if um, ok := u.(map[string]interface{}); ok {
					if um["name"] == name {
						found = true
						continue
					}
				}
				newUsers = append(newUsers, u)
			}
			inbound["users"] = newUsers
		}

		if !found {
			return fmt.Errorf("user %s not found in inbound %s", name, inboundTag)
		}

		c.syncStatsUsers(cfgMap)
		return nil
	})
}

// UpdateUserInInbound updates uuid/flow for a user in a specific inbound.
func (c *Config) UpdateUserInInbound(name, uuid, flow, inboundTag, vmessSecurity string, vmessAlterID int) error {
	return c.ModifySingboxConfig(func(cfgMap SingboxConfigRaw) error {
		inbounds := c.findManagedInbounds(cfgMap)
		if len(inbounds) == 0 {
			return os.ErrInvalid
		}

		found := false
		for _, inbound := range inbounds {
			tag, _ := inbound["tag"].(string)
			if tag != inboundTag {
				continue
			}

			inbType := inboundTypeFromMap(inbound)
			if !isUserInboundType(inbType) {
				continue
			}

			users := ensureUsers(inbound)
			if inbType == "vmess" {
				sanitizeVmessUsers(users)
			}
			for _, u := range users {
				if um, ok := u.(map[string]interface{}); ok {
					if um["name"] == name {
						switch inbType {
						case "vless":
							um["uuid"] = uuid
							flow = normalizeFlow(flow)
							if flow != "" {
								um["flow"] = flow
							} else {
								delete(um, "flow")
							}
						case "vmess":
							um["uuid"] = uuid
							delete(um, "flow")
							delete(um, "security")
							um["alter_id"] = vmessAlterID
						case "trojan":
							um["password"] = uuid
							delete(um, "flow")
						}
						found = true
						break
					}
				}
			}
		}

		if !found {
			return fmt.Errorf("user %s not found in inbound %s", name, inboundTag)
		}

		c.syncStatsUsers(cfgMap)
		return nil
	})
}

func (c *Config) UpdateUser(name, uuid, flow, inboundTag, vmessSecurity string, vmessAlterID int) error {
	return c.ModifySingboxConfig(func(cfgMap SingboxConfigRaw) error {
		inbounds := c.findManagedInbounds(cfgMap)
		if len(inbounds) == 0 {
			return os.ErrInvalid
		}

		targetType := ""
		if inboundTag != "" {
			for _, inbound := range inbounds {
				if tag, ok := inbound["tag"].(string); ok && tag == inboundTag {
					targetType = inboundTypeFromMap(inbound)
					break
				}
			}
		}

		found := false
		for _, inbound := range inbounds {
			inbType := inboundTypeFromMap(inbound)
			if !isUserInboundType(inbType) {
				continue
			}
			if targetType != "" && inbType != targetType {
				continue
			}

			users := ensureUsers(inbound)
			if inbType == "vmess" {
				sanitizeVmessUsers(users)
			}
			for _, u := range users {
				if um, ok := u.(map[string]interface{}); ok {
					if um["name"] == name {
						switch inbType {
						case "vless":
							um["uuid"] = uuid
							flow = normalizeFlow(flow)
							if flow != "" {
								um["flow"] = flow
							} else {
								delete(um, "flow")
							}
						case "vmess":
							um["uuid"] = uuid
							delete(um, "flow")
							delete(um, "security")
							um["alter_id"] = vmessAlterID
						case "trojan":
							um["password"] = uuid
							delete(um, "flow")
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

// RenameUser renames a user across all managed inbounds where it exists.
func (c *Config) RenameUser(originalName, newName, uuid, flow, vmessSecurity string, vmessAlterID int) error {
	if originalName == "" || newName == "" {
		return fmt.Errorf("user name is required")
	}
	if originalName == newName {
		return nil
	}

	return c.ModifySingboxConfig(func(cfgMap SingboxConfigRaw) error {
		inbounds := c.findManagedInbounds(cfgMap)
		if len(inbounds) == 0 {
			return os.ErrInvalid
		}

		found := false
		for _, inbound := range inbounds {
			inbType := inboundTypeFromMap(inbound)
			if !isUserInboundType(inbType) {
				continue
			}

			tag, _ := inbound["tag"].(string)
			users := ensureUsers(inbound)
			if inbType == "vmess" {
				sanitizeVmessUsers(users)
			}

			for _, u := range users {
				if um, ok := u.(map[string]interface{}); ok {
					if um["name"] == newName {
						return fmt.Errorf("user %s already exists in inbound %s", newName, tag)
					}
				}
			}

			for _, u := range users {
				if um, ok := u.(map[string]interface{}); ok {
					if um["name"] == originalName {
						um["name"] = newName
						switch inbType {
						case "vless":
							um["uuid"] = uuid
							flow = normalizeFlow(flow)
							if flow != "" {
								um["flow"] = flow
							} else {
								delete(um, "flow")
							}
						case "vmess":
							um["uuid"] = uuid
							delete(um, "flow")
							delete(um, "security")
							um["alter_id"] = vmessAlterID
						case "trojan":
							um["password"] = uuid
							delete(um, "flow")
						}
						found = true
					}
				}
			}
		}

		if !found {
			return fmt.Errorf("user %s not found", originalName)
		}

		c.syncStatsUsers(cfgMap)
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
			if !isUserInboundType(inboundTypeFromMap(inboundMap)) {
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

func sanitizeVmessUsers(users []interface{}) {
	for _, u := range users {
		if um, ok := u.(map[string]interface{}); ok {
			delete(um, "security")
		}
	}
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
			if !isUserInboundType(inboundTypeFromMap(inbMap)) {
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
	listenAddr := strings.TrimSpace(c.SingboxAPIAddr)
	if v, ok := v2["listen"].(string); ok && strings.TrimSpace(v) != "" {
		listenAddr = strings.TrimSpace(v)
	}
	if c.ExecutionMode == "docker_local" {
		if host, port, err := net.SplitHostPort(listenAddr); err == nil && port != "" {
			h := strings.TrimSpace(strings.Trim(host, "[]"))
			if h == "" || strings.EqualFold(h, "localhost") {
				listenAddr = net.JoinHostPort("0.0.0.0", port)
			} else if ip := net.ParseIP(h); ip != nil && ip.IsLoopback() {
				listenAddr = net.JoinHostPort("0.0.0.0", port)
			}
		}
	}
	if listenAddr == "" {
		listenAddr = c.SingboxAPIAddr
	}
	v2["listen"] = listenAddr
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
