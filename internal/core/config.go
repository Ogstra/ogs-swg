package core

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
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
	WireGuardConfigDir    string   `json:"wireguard_config_dir" env:"OGS_WIREGUARD_CONFIG_DIR"`
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
	SubscriptionDomain    string   `json:"subscription_domain" env:"OGS_SUBSCRIPTION_DOMAIN"`
	SingboxPendingChanges bool     `json:"-"` // Not persisted, runtime flag
	ConfigPath            string   `json:"-"`
	APIKey                string   `json:"api_key" env:"OGS_API_KEY"`
	APIKeyReadOnly        bool     `json:"api_key_read_only" env:"OGS_API_KEY_READ_ONLY" env-default:"false"`
	DemoMode              bool     `json:"demo_mode" env:"OGS_DEMO_MODE" env-default:"false"`
	DisablePasswordLogin  bool     `json:"disable_password_login" env:"OGS_DISABLE_PASSWORD_LOGIN" env-default:"false"`

	// Execution mode: "local" (default/bare metal), "docker_local" (Docker on same host).
	ExecutionMode string `json:"execution_mode" env:"OGS_EXECUTION_MODE"`

	// WireGuard test mode: when true, WireGuard service/wg calls are simulated so UI flows can be tested without wg/systemd installed.
	WireGuardTestMode bool `json:"wireguard_test_mode" env:"OGS_WIREGUARD_TEST_MODE" env-default:"false"`

	// Sysctl Whitelist (Optional override)
	SysctlWhitelist []string `json:"sysctl_whitelist" env:"OGS_SYSCTL_WHITELIST"`

	JWTSecret string `json:"jwt_secret" env:"OGS_JWT_SECRET"`

	SubscriptionProtection SubscriptionProtectionConfig `json:"subscription_protection"`

	executor SystemExecutor
	mu       sync.Mutex
}

type SubscriptionProtectionConfig struct {
	MaxRequests     int  `json:"max_requests"`
	WindowSeconds   int  `json:"window_seconds"`
	UAFilterEnabled bool `json:"ua_filter_enabled"`
}

type UserAccount struct {
	Name          string   `json:"name"`
	UUID          string   `json:"uuid"`
	Flow          string   `json:"flow"`
	VmessSecurity string   `json:"vmess_security,omitempty"`
	VmessAlterID  int      `json:"vmess_alter_id,omitempty"`
	InboundTags   []string `json:"inbound_tags"`
}

var ErrUserAssignedToAnotherInbound = errors.New("user is already assigned to another inbound")

func isUserInboundType(inbType string) bool {
	switch strings.ToLower(strings.TrimSpace(inbType)) {
	case "vless", "vmess", "trojan", "hysteria2":
		return true
	default:
		return false
	}
}

var vmessLegacyAlterIDWarnOnce sync.Once

func parseAlterIDValue(raw interface{}) int {
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return parsed
		}
	}
	return 0
}

func (u *VmessUser) UnmarshalJSON(data []byte) error {
	type alias VmessUser
	var tmp struct {
		alias
		LegacyAlterID interface{} `json:"alter_id"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*u = VmessUser(tmp.alias)
	if u.AlterID == 0 && tmp.LegacyAlterID != nil {
		vmessLegacyAlterIDWarnOnce.Do(func() {
			log.Printf("singbox vmess legacy key detected: normalizing alter_id to alterId")
		})
		u.AlterID = parseAlterIDValue(tmp.LegacyAlterID)
	}
	return nil
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

	derivePath := strings.TrimSpace(cfg.WireGuardConfigPath)
	if derivePath == "" {
		derivePath = "/etc/wireguard/wg0.conf"
	}
	if strings.TrimSpace(cfg.WireGuardConfigDir) == "" {
		cfg.WireGuardConfigDir = filepath.Dir(derivePath)
	}
	cfg.WireGuardConfigDir = filepath.Clean(cfg.WireGuardConfigDir)
	cfg.normalizeSubscriptionProtection()

	return cfg
}

func (c *Config) normalizeSubscriptionProtection() {
	if c.SubscriptionProtection.MaxRequests <= 0 {
		c.SubscriptionProtection.MaxRequests = 60
	}
	if c.SubscriptionProtection.WindowSeconds <= 0 {
		c.SubscriptionProtection.WindowSeconds = 60
	}
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
		inbType = strings.ToLower(strings.TrimSpace(inbType))
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
			if alterRaw, ok := userMapData["alterId"]; ok {
				vmessAlterID = parseAlterIDValue(alterRaw)
			} else if alterRaw, ok := userMapData["alter_id"]; ok {
				vmessLegacyAlterIDWarnOnce.Do(func() {
					log.Printf("singbox vmess legacy key detected: normalizing alter_id to alterId")
				})
				vmessAlterID = parseAlterIDValue(alterRaw)
			}
			if inbType == "trojan" || inbType == "hysteria2" {
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

	return c.ModifySingboxConfig(func(cfg *SingboxConfig) error {
		inboundsResult, err := c.findManagedInbounds(cfg)
		if err != nil {
			return err
		}
		if len(inboundsResult.inbounds) == 0 {
			return os.ErrInvalid
		}

		for _, inbound := range inboundsResult.inbounds {
			for _, existingName := range inbound.UserNames() {
				if existingName != name {
					continue
				}
				if inbound.Base().Tag == inboundTag {
					return fmt.Errorf("user %s already exists in inbound %s", name, inboundTag)
				}
				return fmt.Errorf("%w: user %s already exists in inbound %s; multiple inbounds per user are deprecated", ErrUserAssignedToAnotherInbound, name, inbound.Base().Tag)
			}
		}

		for _, inbound := range inboundsResult.inbounds {
			if inbound.Base().Tag != inboundTag {
				continue
			}
			switch inb := inbound.(type) {
			case *VlessInbound:
				inb.Users = append(inb.Users, VlessUser{
					Name: name,
					UUID: uuid,
					Flow: normalizeFlow(flow),
				})
			case *VmessInbound:
				inb.Users = append(inb.Users, VmessUser{
					Name:    name,
					UUID:    uuid,
					AlterID: vmessAlterID,
				})
			case *TrojanInbound:
				inb.Users = append(inb.Users, TrojanUser{
					Name:     name,
					Password: uuid,
				})
			case *Hysteria2Inbound:
				inb.Users = append(inb.Users, Hysteria2User{
					Name:     name,
					Password: uuid, // caller passes password in the uuid parameter by convention
				})
				// flow is silently ignored — Hysteria2 has no flow field
			default:
				return fmt.Errorf("unsupported inbound type: %s", inbound.Base().Type)
			}

			if err := inboundsResult.commit(cfg); err != nil {
				return err
			}
			c.syncStatsUsers(cfg)
			return nil
		}

		return fmt.Errorf("inbound '%s' not found or not managed", inboundTag)
	})
}

func (c *Config) RemoveUser(name string) error {
	return c.ModifySingboxConfig(func(cfg *SingboxConfig) error {
		inboundsResult, err := c.findManagedInbounds(cfg)
		if err != nil {
			return err
		}
		if len(inboundsResult.inbounds) == 0 {
			return os.ErrInvalid
		}
		for _, inbound := range inboundsResult.inbounds {
			switch inb := inbound.(type) {
			case *VlessInbound:
				filtered := inb.Users[:0]
				for _, u := range inb.Users {
					if u.Name != name {
						filtered = append(filtered, u)
					}
				}
				inb.Users = filtered
			case *VmessInbound:
				filtered := inb.Users[:0]
				for _, u := range inb.Users {
					if u.Name != name {
						filtered = append(filtered, u)
					}
				}
				inb.Users = filtered
			case *TrojanInbound:
				filtered := inb.Users[:0]
				for _, u := range inb.Users {
					if u.Name != name {
						filtered = append(filtered, u)
					}
				}
				inb.Users = filtered
			case *Hysteria2Inbound:
				filtered := inb.Users[:0]
				for _, u := range inb.Users {
					if u.Name != name {
						filtered = append(filtered, u)
					}
				}
				inb.Users = filtered
			}
		}
		if err := inboundsResult.commit(cfg); err != nil {
			return err
		}
		c.syncStatsUsers(cfg)
		return nil
	})
}

func (c *Config) RemoveUserFromInbound(name, inboundTag string) error {
	return c.ModifySingboxConfig(func(cfg *SingboxConfig) error {
		inboundsResult, err := c.findManagedInbounds(cfg)
		if err != nil {
			return err
		}
		if len(inboundsResult.inbounds) == 0 {
			return os.ErrInvalid
		}

		found := false
		for _, inbound := range inboundsResult.inbounds {
			if inbound.Base().Tag != inboundTag {
				continue
			}
			switch inb := inbound.(type) {
			case *VlessInbound:
				filtered := inb.Users[:0]
				for _, u := range inb.Users {
					if u.Name == name {
						found = true
						continue
					}
					filtered = append(filtered, u)
				}
				inb.Users = filtered
			case *VmessInbound:
				filtered := inb.Users[:0]
				for _, u := range inb.Users {
					if u.Name == name {
						found = true
						continue
					}
					filtered = append(filtered, u)
				}
				inb.Users = filtered
			case *TrojanInbound:
				filtered := inb.Users[:0]
				for _, u := range inb.Users {
					if u.Name == name {
						found = true
						continue
					}
					filtered = append(filtered, u)
				}
				inb.Users = filtered
			case *Hysteria2Inbound:
				filtered := inb.Users[:0]
				for _, u := range inb.Users {
					if u.Name == name {
						found = true
						continue
					}
					filtered = append(filtered, u)
				}
				inb.Users = filtered
			}
		}

		if !found {
			return fmt.Errorf("user %s not found in inbound %s", name, inboundTag)
		}
		if err := inboundsResult.commit(cfg); err != nil {
			return err
		}
		c.syncStatsUsers(cfg)
		return nil
	})
}

func (c *Config) UpdateUserInInbound(name, uuid, flow, inboundTag, vmessSecurity string, vmessAlterID int) error {
	return c.ModifySingboxConfig(func(cfg *SingboxConfig) error {
		inboundsResult, err := c.findManagedInbounds(cfg)
		if err != nil {
			return err
		}
		if len(inboundsResult.inbounds) == 0 {
			return os.ErrInvalid
		}

		found := false
		for _, inbound := range inboundsResult.inbounds {
			if inbound.Base().Tag != inboundTag {
				continue
			}
			switch inb := inbound.(type) {
			case *VlessInbound:
				for i := range inb.Users {
					if inb.Users[i].Name == name {
						inb.Users[i].UUID = uuid
						inb.Users[i].Flow = normalizeFlow(flow)
						found = true
						break
					}
				}
			case *VmessInbound:
				for i := range inb.Users {
					if inb.Users[i].Name == name {
						inb.Users[i].UUID = uuid
						inb.Users[i].AlterID = vmessAlterID
						found = true
						break
					}
				}
			case *TrojanInbound:
				for i := range inb.Users {
					if inb.Users[i].Name == name {
						inb.Users[i].Password = uuid
						found = true
						break
					}
				}
			case *Hysteria2Inbound:
				for i := range inb.Users {
					if inb.Users[i].Name == name {
						inb.Users[i].Password = uuid // uuid param carries password
						found = true
						break
					}
				}
				// flow is silently ignored
			}
		}

		if !found {
			return fmt.Errorf("user %s not found in inbound %s", name, inboundTag)
		}
		if err := inboundsResult.commit(cfg); err != nil {
			return err
		}
		c.syncStatsUsers(cfg)
		return nil
	})
}

func (c *Config) UpdateUser(name, uuid, flow, inboundTag, vmessSecurity string, vmessAlterID int) error {
	return c.ModifySingboxConfig(func(cfg *SingboxConfig) error {
		inboundsResult, err := c.findManagedInbounds(cfg)
		if err != nil {
			return err
		}
		if len(inboundsResult.inbounds) == 0 {
			return os.ErrInvalid
		}

		targetType := ""
		if inboundTag != "" {
			for _, inbound := range inboundsResult.inbounds {
				if inbound.Base().Tag == inboundTag {
					targetType = strings.ToLower(inbound.Base().Type)
					break
				}
			}
		}

		found := false
		for _, inbound := range inboundsResult.inbounds {
			inbType := strings.ToLower(inbound.Base().Type)
			if targetType != "" && inbType != targetType {
				continue
			}
			switch inb := inbound.(type) {
			case *VlessInbound:
				for i := range inb.Users {
					if inb.Users[i].Name == name {
						inb.Users[i].UUID = uuid
						inb.Users[i].Flow = normalizeFlow(flow)
						found = true
					}
				}
			case *VmessInbound:
				for i := range inb.Users {
					if inb.Users[i].Name == name {
						inb.Users[i].UUID = uuid
						inb.Users[i].AlterID = vmessAlterID
						found = true
					}
				}
			case *TrojanInbound:
				for i := range inb.Users {
					if inb.Users[i].Name == name {
						inb.Users[i].Password = uuid
						found = true
					}
				}
			case *Hysteria2Inbound:
				for i := range inb.Users {
					if inb.Users[i].Name == name {
						inb.Users[i].Password = uuid
						found = true
					}
				}
			}
		}

		if !found {
			return fmt.Errorf("user %s not found", name)
		}
		if err := inboundsResult.commit(cfg); err != nil {
			return err
		}
		return nil
	})
}

func (c *Config) RenameUser(originalName, newName, uuid, flow, vmessSecurity string, vmessAlterID int) error {
	if originalName == "" || newName == "" {
		return fmt.Errorf("user name is required")
	}
	if originalName == newName {
		return nil
	}

	return c.ModifySingboxConfig(func(cfg *SingboxConfig) error {
		inboundsResult, err := c.findManagedInbounds(cfg)
		if err != nil {
			return err
		}
		if len(inboundsResult.inbounds) == 0 {
			return os.ErrInvalid
		}

		for _, inbound := range inboundsResult.inbounds {
			for _, n := range inbound.UserNames() {
				if n == newName {
					return fmt.Errorf("user %s already exists in inbound %s", newName, inbound.Base().Tag)
				}
			}
		}

		found := false
		for _, inbound := range inboundsResult.inbounds {
			switch inb := inbound.(type) {
			case *VlessInbound:
				for i := range inb.Users {
					if inb.Users[i].Name == originalName {
						inb.Users[i].Name = newName
						inb.Users[i].UUID = uuid
						inb.Users[i].Flow = normalizeFlow(flow)
						found = true
					}
				}
			case *VmessInbound:
				for i := range inb.Users {
					if inb.Users[i].Name == originalName {
						inb.Users[i].Name = newName
						inb.Users[i].UUID = uuid
						inb.Users[i].AlterID = vmessAlterID
						found = true
					}
				}
			case *TrojanInbound:
				for i := range inb.Users {
					if inb.Users[i].Name == originalName {
						inb.Users[i].Name = newName
						inb.Users[i].Password = uuid
						found = true
					}
				}
			case *Hysteria2Inbound:
				for i := range inb.Users {
					if inb.Users[i].Name == originalName {
						inb.Users[i].Name = newName
						inb.Users[i].Password = uuid
						found = true
					}
				}
			}
		}

		if !found {
			return fmt.Errorf("user %s not found", originalName)
		}
		if err := inboundsResult.commit(cfg); err != nil {
			return err
		}
		c.syncStatsUsers(cfg)
		return nil
	})
}

type managedInbounds struct {
	rawList   []json.RawMessage
	indices   []int
	inbounds  []ManagedInbound
	originals []json.RawMessage // original raw bytes for each managed inbound (parallel to indices/inbounds)
}

func (m *managedInbounds) commit(cfg *SingboxConfig) error {
	if m == nil {
		return nil
	}
	for i, idx := range m.indices {
		data, err := json.Marshal(m.inbounds[i])
		if err != nil {
			return err
		}
		// Merge typed output back onto the original raw object so that unknown
		// fields (e.g. "x-meta") that the typed struct does not model are preserved.
		if i < len(m.originals) && len(m.originals[i]) > 0 {
			merged, mergeErr := mergeInboundWithOriginal(m.originals[i], data)
			if mergeErr == nil {
				data = merged
			}
		}
		m.rawList[idx] = data
	}
	encoded, err := encodeInboundRawList(m.rawList)
	if err != nil {
		return err
	}
	cfg.Inbounds = encoded
	return nil
}

func decodeTypedInbound(raw json.RawMessage) (ManagedInbound, error) {
	var base InboundBase
	if err := json.Unmarshal(raw, &base); err != nil {
		return nil, fmt.Errorf("decode inbound base: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(base.Type)) {
	case "vless":
		var inbound VlessInbound
		if err := json.Unmarshal(raw, &inbound); err != nil {
			return nil, fmt.Errorf("decode vless inbound: %w", err)
		}
		return &inbound, nil
	case "vmess":
		var inbound VmessInbound
		if err := json.Unmarshal(raw, &inbound); err != nil {
			return nil, fmt.Errorf("decode vmess inbound: %w", err)
		}
		return &inbound, nil
	case "trojan":
		var inbound TrojanInbound
		if err := json.Unmarshal(raw, &inbound); err != nil {
			return nil, fmt.Errorf("decode trojan inbound: %w", err)
		}
		return &inbound, nil
	case "hysteria2":
		var inbound Hysteria2Inbound
		if err := json.Unmarshal(raw, &inbound); err != nil {
			return nil, fmt.Errorf("decode hysteria2 inbound: %w", err)
		}
		return &inbound, nil
	default:
		return nil, fmt.Errorf("unsupported inbound type: %q", base.Type)
	}
}

func (c *Config) findManagedInbounds(cfg *SingboxConfig) (*managedInbounds, error) {
	rawList, err := decodeInboundRawList(cfg.Inbounds)
	if err != nil {
		return nil, err
	}
	if len(rawList) == 0 {
		return &managedInbounds{rawList: rawList}, nil
	}

	managed := c.ManagedInbounds
	tagFilter := make(map[string]bool)
	for _, t := range managed {
		if t != "" {
			tagFilter[t] = true
		}
	}

	result := &managedInbounds{
		rawList:   rawList,
		indices:   make([]int, 0, len(rawList)),
		inbounds:  make([]ManagedInbound, 0, len(rawList)),
		originals: make([]json.RawMessage, 0, len(rawList)),
	}
	for i, rawInbound := range rawList {
		var base InboundBase
		if err := json.Unmarshal(rawInbound, &base); err != nil {
			return nil, err
		}
		inbType := strings.ToLower(strings.TrimSpace(base.Type))
		if !isUserInboundType(inbType) {
			continue
		}
		if len(tagFilter) > 0 && !tagFilter[base.Tag] {
			continue
		}
		inbound, err := decodeTypedInbound(rawInbound)
		if err != nil {
			return nil, err
		}
		result.indices = append(result.indices, i)
		result.inbounds = append(result.inbounds, inbound)
		result.originals = append(result.originals, append(json.RawMessage(nil), rawInbound...))
	}

	return result, nil
}

func (c *Config) syncStatsUsers(cfg *SingboxConfig) {
	names := []string{}
	seen := make(map[string]bool)
	tagFilter := make(map[string]bool)
	for _, t := range c.ManagedInbounds {
		if t != "" {
			tagFilter[t] = true
		}
	}
	rawInbounds, err := decodeInboundRawList(cfg.Inbounds)
	if err == nil {
		for _, rawInbound := range rawInbounds {
			var base InboundBase
			if err := json.Unmarshal(rawInbound, &base); err != nil {
				continue
			}
			inbType := strings.ToLower(strings.TrimSpace(base.Type))
			if !isUserInboundType(inbType) {
				continue
			}
			if len(tagFilter) > 0 && !tagFilter[base.Tag] {
				continue
			}
			inbound, err := decodeTypedInbound(rawInbound)
			if err != nil {
				continue
			}
			for _, name := range inbound.UserNames() {
				if name != "" && !seen[name] {
					names = append(names, name)
					seen[name] = true
				}
			}
		}
	}

	if cfg.Experimental == nil {
		cfg.Experimental = &Experimental{}
	}
	if cfg.Experimental.V2RayAPI == nil {
		cfg.Experimental.V2RayAPI = &V2RayAPI{}
	}
	v2 := cfg.Experimental.V2RayAPI

	listenAddr := strings.TrimSpace(c.SingboxAPIAddr)
	if strings.TrimSpace(v2.Listen) != "" {
		listenAddr = strings.TrimSpace(v2.Listen)
	}
	if c.ExecutionMode == "docker_local" && !dockerLocalHostNetworkEnabled() {
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
	v2.Listen = listenAddr

	if v2.Stats == nil {
		v2.Stats = &V2RayStats{}
	}
	stats := v2.Stats
	stats.Enabled = true
	if len(c.StatsInbounds) > 0 {
		stats.Inbounds = append([]string(nil), c.StatsInbounds...)
	}
	if len(c.StatsOutbounds) > 0 {
		stats.Outbounds = append([]string(nil), c.StatsOutbounds...)
	}
	stats.Users = names
}

func dockerLocalHostNetworkEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OGS_DOCKER_LOCAL_HOST_NETWORK"))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
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

func (c *Config) GetSingboxPendingChanges() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.SingboxPendingChanges
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
		if inbType == "vless" || inbType == "vmess" || inbType == "trojan" || inbType == "hysteria2" {
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
