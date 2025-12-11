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

// UpdateSingboxConfig writes raw content to config file and restarts service
func (c *Config) UpdateSingboxConfig(content string) error {
	configMu.Lock()
	defer configMu.Unlock()

	// 1. Validate JSON structure
	var validCheck map[string]interface{}
	if err := json.Unmarshal([]byte(content), &validCheck); err != nil {
		return fmt.Errorf("invalid json: %v", err)
	}

	// 2. Write to file
	if err := os.WriteFile(c.SingboxConfigPath, []byte(content), 0644); err != nil {
		return err
	}

	// 3. Restart Service
	return c.ReloadSingbox()
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
	configMu.Lock()
	defer configMu.Unlock()

	content, err := os.ReadFile(c.SingboxConfigPath)
	if err != nil {
		return err
	}

	var rawConfig SingboxConfigRaw
	if err := json.Unmarshal(content, &rawConfig); err != nil {
		return err
	}

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

	return c.saveAndReload(rawConfig)
}

// UpdateSingboxInbound updates an existing inbound by tag
func (c *Config) UpdateSingboxInbound(tag string, updatedInbound map[string]interface{}) error {
	configMu.Lock()
	defer configMu.Unlock()

	content, err := os.ReadFile(c.SingboxConfigPath)
	if err != nil {
		return err
	}

	var rawConfig SingboxConfigRaw
	if err := json.Unmarshal(content, &rawConfig); err != nil {
		return err
	}

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
	return c.saveAndReload(rawConfig)
}

// DeleteSingboxInbound removes an inbound by tag
func (c *Config) DeleteSingboxInbound(tag string) error {
	configMu.Lock()
	defer configMu.Unlock()

	content, err := os.ReadFile(c.SingboxConfigPath)
	if err != nil {
		return err
	}

	var rawConfig SingboxConfigRaw
	if err := json.Unmarshal(content, &rawConfig); err != nil {
		return err
	}

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
	return c.saveAndReload(rawConfig)
}

func (c *Config) saveAndReload(rawConfig SingboxConfigRaw) error {
	// Serialize with indentation
	data, err := json.MarshalIndent(rawConfig, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(c.SingboxConfigPath, data, 0644); err != nil {
		return err
	}

	return c.ReloadSingbox()
}

func (c *Config) ReloadSingbox() error {
	if !c.EnableSingbox {
		return nil
	}
	// Assuming systemd usage
	cmd := exec.Command("systemctl", "restart", "sing-box")
	return cmd.Run()
}
