package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// AgentConfig holds configuration for an AI agent
type AgentConfig struct {
	Instructions string   `yaml:"instructions" json:"instructions"`
	Model        string   `yaml:"model" json:"model"`
	Temperature  *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`
}

// DefaultConfigDir is the default directory for agent configuration files
const DefaultConfigDir = "config/agents/"

// getConfigDir returns the agent config directory from env var or default
func getConfigDir() string {
	if dir := os.Getenv("AGENT_CONFIG_DIR"); dir != "" {
		return dir
	}
	return DefaultConfigDir
}

// LoadAgentConfig loads agent configuration from a YAML file
func LoadAgentConfig(agentName string) (*AgentConfig, error) {
	// Validate agent name to prevent path traversal
	matched, err := regexp.MatchString(`^[a-z0-9_-]+$`, agentName)
	if err != nil {
		return nil, fmt.Errorf("failed to validate agent name: %w", err)
	}
	if !matched {
		return nil, fmt.Errorf("invalid agent name %q: must match [a-z0-9_-]+", agentName)
	}

	configDir := getConfigDir()
	configPath := filepath.Join(configDir, agentName+".yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found for agent %q: %s", agentName, configPath)
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var config AgentConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}

	// Validate required fields
	if config.Instructions == "" {
		return nil, fmt.Errorf("config file %s: instructions field is empty", configPath)
	}
	if config.Model == "" {
		return nil, fmt.Errorf("config file %s: model field is empty", configPath)
	}

	return &config, nil
}



