package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

// SingboxConfigRaw is a helper to parse config as a map
type SingboxConfigRaw map[string]interface{}

var configMu sync.Mutex

// GetSingboxConfig reads the raw config file content
func (c *Config) GetSingboxConfig() (string, error) {
	configMu.Lock()
	defer configMu.Unlock()

	content, err := os.ReadFile(c.SingboxConfigPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// GetSingboxConfigMap reads the raw config file content as a map
func (c *Config) GetSingboxConfigMap() (map[string]interface{}, error) {
	configMu.Lock()
	defer configMu.Unlock()

	content, err := os.ReadFile(c.SingboxConfigPath)
	if err != nil {
		return nil, err
	}

	var rawConfig SingboxConfigRaw
	if err := json.Unmarshal(content, &rawConfig); err != nil {
		return nil, err
	}
	return rawConfig, nil
}

// UpdateSingboxConfig writes raw content to config file and restarts service
func (c *Config) UpdateSingboxConfig(content string) error {
	configMu.Lock()
	defer configMu.Unlock()

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
	if err := os.WriteFile(c.SingboxConfigPath, []byte(content), 0644); err != nil {
		return err
	}

	// 4. Mark pending restart
	c.MarkSingboxPending()
	return nil
}

// ModifySingboxConfig safely modifies the configuration using a callback
func (c *Config) ModifySingboxConfig(modifier func(SingboxConfigRaw) error) error {
	configMu.Lock()
	defer configMu.Unlock()

	// 1. Read
	content, err := os.ReadFile(c.SingboxConfigPath)
	if err != nil {
		return err
	}

	var rawConfig SingboxConfigRaw
	if err := json.Unmarshal(content, &rawConfig); err != nil {
		return err
	}

	// 2. Modify
	if err := modifier(rawConfig); err != nil {
		return err
	}

	// 3. Write
	data, err := json.MarshalIndent(rawConfig, "", "  ")
	if err != nil {
		return err
	}

	// 4. Validate
	if err := c.ValidateConfig(data); err != nil {
		return fmt.Errorf("sing-box validation failed: %v", err)
	}

	// 5. Save
	if err := os.WriteFile(c.SingboxConfigPath, data, 0644); err != nil {
		return err
	}

	// 6. Mark pending restart
	c.MarkSingboxPending()
	return nil
}

// GetSingboxInbounds returns the list of inbounds as map objects
func (c *Config) GetSingboxInbounds() ([]map[string]interface{}, error) {
	configMu.Lock()
	defer configMu.Unlock()

	content, err := os.ReadFile(c.SingboxConfigPath)
	if err != nil {
		return nil, err
	}

	var rawConfig SingboxConfigRaw
	if err := json.Unmarshal(content, &rawConfig); err != nil {
		return nil, err
	}

	inbounds, ok := rawConfig["inbounds"].([]interface{})
	if !ok {
		return []map[string]interface{}{}, nil
	}

	result := make([]map[string]interface{}, 0, len(inbounds))
	for _, inb := range inbounds {
		if inbMap, ok := inb.(map[string]interface{}); ok {
			result = append(result, inbMap)
		}
	}

	return result, nil
}

// AddSingboxInbound appends a new inbound block
func (c *Config) AddSingboxInbound(newInbound map[string]interface{}) error {
	err := c.ModifySingboxConfig(func(rawConfig SingboxConfigRaw) error {
		inbounds, ok := rawConfig["inbounds"].([]interface{})
		if !ok {
			inbounds = []interface{}{}
		}

		// Check for duplicate tag
		newTag, _ := newInbound["tag"].(string)
		if newTag != "" {
			for _, inb := range inbounds {
				if inbMap, ok := inb.(map[string]interface{}); ok {
					if tag, _ := inbMap["tag"].(string); tag == newTag {
						return fmt.Errorf("inbound with tag '%s' already exists", newTag)
					}
				}
			}
		}

		inbounds = append(inbounds, newInbound)
		rawConfig["inbounds"] = inbounds
		return nil
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

	err := c.ModifySingboxConfig(func(rawConfig SingboxConfigRaw) error {
		inbounds, ok := rawConfig["inbounds"].([]interface{})
		if !ok {
			return fmt.Errorf("no inbounds found")
		}

		found := false
		for i, inb := range inbounds {
			if inbMap, ok := inb.(map[string]interface{}); ok {
				if currentTag, _ := inbMap["tag"].(string); currentTag == tag {
					inbounds[i] = updatedInbound
					found = true
					break
				}
			}
		}

		if !found {
			return fmt.Errorf("inbound with tag '%s' not found", tag)
		}

		rawConfig["inbounds"] = inbounds
		return nil
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
	err := c.ModifySingboxConfig(func(rawConfig SingboxConfigRaw) error {
		inbounds, ok := rawConfig["inbounds"].([]interface{})
		if !ok {
			return nil
		}

		newInbounds := []interface{}{}
		found := false
		for _, inb := range inbounds {
			if inbMap, ok := inb.(map[string]interface{}); ok {
				if currentTag, _ := inbMap["tag"].(string); currentTag == tag {
					found = true
					continue
				}
			}
			newInbounds = append(newInbounds, inb)
		}

		if !found {
			return fmt.Errorf("inbound with tag '%s' not found", tag)
		}

		rawConfig["inbounds"] = newInbounds
		return nil
	})

	if err != nil {
		return err
	}

	// Remove from managed lists
	return c.RemoveInboundFromLists(tag)
}

func (c *Config) saveAndReload(rawConfig SingboxConfigRaw) error {
	// Serialize with indentation
	data, err := json.MarshalIndent(rawConfig, "", "  ")
	if err != nil {
		return err
	}

	// Validate before save
	if err := c.ValidateConfig(data); err != nil {
		return fmt.Errorf("sing-box validation failed: %v", err)
	}

	if err := os.WriteFile(c.SingboxConfigPath, data, 0644); err != nil {
		return err
	}

	c.MarkSingboxPending()
	return nil
}

func (c *Config) ValidateConfig(content []byte) error {
	if !c.EnableSingbox {
		return nil
	}

	// Check for port collisions manually since sing-box check might miss them
	if err := c.DetectPortCollision(content); err != nil {
		return err
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

	// Run sing-box check
	// Assuming sing-box is in PATH or use absolute path if needed
	// The command "sing-box check -c <file>"
	cmd := exec.Command("sing-box", "check", "-c", tmpFile.Name())
	output, err := cmd.CombinedOutput()
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
	// Assuming systemd usage
	cmd := exec.Command("systemctl", "restart", "sing-box")
	return cmd.Run()
}
