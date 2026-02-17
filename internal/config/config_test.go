package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAgentConfig_ValidFile(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	
	// Create a valid YAML config file
	configContent := `instructions: "Test instructions for agent"
model: "gpt-4"
temperature: 0.7
`
	configPath := filepath.Join(tmpDir, "test-agent.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Set env var to use temp directory
	oldEnv := os.Getenv("AGENT_CONFIG_DIR")
	os.Setenv("AGENT_CONFIG_DIR", tmpDir)
	defer os.Setenv("AGENT_CONFIG_DIR", oldEnv)

	// Load config
	config, err := LoadAgentConfig("test-agent")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify fields
	if config.Instructions != "Test instructions for agent" {
		t.Errorf("Expected instructions 'Test instructions for agent', got %q", config.Instructions)
	}
	if config.Model != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got %q", config.Model)
	}
	if config.Temperature == nil {
		t.Error("Expected temperature to be set")
	} else if *config.Temperature != 0.7 {
		t.Errorf("Expected temperature 0.7, got %f", *config.Temperature)
	}
}

func TestLoadAgentConfig_MissingFile(t *testing.T) {
	// Create temp directory (empty)
	tmpDir := t.TempDir()

	// Set env var to use temp directory
	oldEnv := os.Getenv("AGENT_CONFIG_DIR")
	os.Setenv("AGENT_CONFIG_DIR", tmpDir)
	defer os.Setenv("AGENT_CONFIG_DIR", oldEnv)

	// Try to load non-existent config
	_, err := LoadAgentConfig("nonexistent-agent")
	if err == nil {
		t.Fatal("Expected error for missing file, got nil")
	}

	// Check error message contains expected info
	expectedSubstr := "config file not found"
	if !strings.Contains(err.Error(), expectedSubstr) {
		t.Errorf("Expected error to contain %q, got: %v", expectedSubstr, err)
	}
}

func TestLoadAgentConfig_MalformedYAML(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create a malformed YAML file
	malformedContent := `instructions: "Test
model: [this is not valid yaml
temperature: not a number
`
	configPath := filepath.Join(tmpDir, "malformed-agent.yaml")
	if err := os.WriteFile(configPath, []byte(malformedContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Set env var to use temp directory
	oldEnv := os.Getenv("AGENT_CONFIG_DIR")
	os.Setenv("AGENT_CONFIG_DIR", tmpDir)
	defer os.Setenv("AGENT_CONFIG_DIR", oldEnv)

	// Try to load malformed config
	_, err := LoadAgentConfig("malformed-agent")
	if err == nil {
		t.Fatal("Expected error for malformed YAML, got nil")
	}

	// Check error message contains expected info
	expectedSubstr := "failed to parse config file"
	if !strings.Contains(err.Error(), expectedSubstr) {
		t.Errorf("Expected error to contain %q, got: %v", expectedSubstr, err)
	}
}

func TestLoadAgentConfig_EmptyInstructions(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create a YAML config file with empty instructions
	configContent := `instructions: ""
model: "gpt-4"
`
	configPath := filepath.Join(tmpDir, "empty-instructions.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Set env var to use temp directory
	oldEnv := os.Getenv("AGENT_CONFIG_DIR")
	os.Setenv("AGENT_CONFIG_DIR", tmpDir)
	defer os.Setenv("AGENT_CONFIG_DIR", oldEnv)

	// Try to load config with empty instructions
	_, err := LoadAgentConfig("empty-instructions")
	if err == nil {
		t.Fatal("Expected error for empty instructions, got nil")
	}

	// Check error message contains expected info
	expectedSubstr := "instructions field is empty"
	if !strings.Contains(err.Error(), expectedSubstr) {
		t.Errorf("Expected error to contain %q, got: %v", expectedSubstr, err)
	}
}

func TestLoadAgentConfig_EmptyModel(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create a YAML config file with empty model
	configContent := `instructions: "Test instructions"
model: ""
`
	configPath := filepath.Join(tmpDir, "empty-model.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Set env var to use temp directory
	oldEnv := os.Getenv("AGENT_CONFIG_DIR")
	os.Setenv("AGENT_CONFIG_DIR", tmpDir)
	defer os.Setenv("AGENT_CONFIG_DIR", oldEnv)

	// Try to load config with empty model
	_, err := LoadAgentConfig("empty-model")
	if err == nil {
		t.Fatal("Expected error for empty model, got nil")
	}

	// Check error message contains expected info
	expectedSubstr := "model field is empty"
	if !strings.Contains(err.Error(), expectedSubstr) {
		t.Errorf("Expected error to contain %q, got: %v", expectedSubstr, err)
	}
}

func TestLoadAgentConfig_InvalidAgentName(t *testing.T) {
	// Try to load config with invalid agent name (path traversal attempt)
	_, err := LoadAgentConfig("../etc/passwd")
	if err == nil {
		t.Fatal("Expected error for invalid agent name, got nil")
	}

	// Check error message contains expected info
	expectedSubstr := "invalid agent name"
	if !strings.Contains(err.Error(), expectedSubstr) {
		t.Errorf("Expected error to contain %q, got: %v", expectedSubstr, err)
	}
}

func TestLoadProductionAgentConfigs(t *testing.T) {
	// Point to the production config directory
	oldEnv := os.Getenv("AGENT_CONFIG_DIR")
	os.Setenv("AGENT_CONFIG_DIR", "../../config/agents/")
	defer os.Setenv("AGENT_CONFIG_DIR", oldEnv)

	agentNames := []string{
		"branch_name",
		"builder_resume",
		"builder_cover_letter",
		"pull_request",
		"review_agent",
		"layout_review",
	}
	for _, name := range agentNames {
		t.Run(name, func(t *testing.T) {
			cfg, err := LoadAgentConfig(name)
			if err != nil {
				t.Fatalf("Failed to load config for %q: %v", name, err)
			}
			if cfg.Instructions == "" {
				t.Errorf("Agent %q has empty instructions", name)
			}
			if cfg.Model == "" {
				t.Errorf("Agent %q has empty model", name)
			}
		})
	}
}

