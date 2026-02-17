package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// AgentConfig holds configuration for an AI agent
type AgentConfig struct {
	Instructions string   `yaml:"instructions" json:"instructions"`
	Model        string   `yaml:"model" json:"model"`
	Temperature  *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`
}

// getConfigDir returns the agent config directory from env var or default
func getConfigDir() string {
	if dir := os.Getenv("AGENT_CONFIG_DIR"); dir != "" {
		return dir
	}
	return "config/agents/"
}

// loadAgentConfig loads agent configuration from a YAML file
func loadAgentConfig(agentName string) (*AgentConfig, error) {
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

	return &config, nil
}

// GetAgentConfig is a Temporal activity that loads agent configuration
func GetAgentConfig(ctx context.Context, agentName string) (*AgentConfig, error) {
	return loadAgentConfig(agentName)
}

