package config

import (
	"context"
	"os"
	"path/filepath"
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
	config, err := loadAgentConfig("test-agent")
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
	_, err := loadAgentConfig("nonexistent-agent")
	if err == nil {
		t.Fatal("Expected error for missing file, got nil")
	}

	// Check error message contains expected info
	expectedSubstr := "config file not found"
	if !contains(err.Error(), expectedSubstr) {
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
	_, err := loadAgentConfig("malformed-agent")
	if err == nil {
		t.Fatal("Expected error for malformed YAML, got nil")
	}

	// Check error message contains expected info
	expectedSubstr := "failed to parse config file"
	if !contains(err.Error(), expectedSubstr) {
		t.Errorf("Expected error to contain %q, got: %v", expectedSubstr, err)
	}
}

func TestGetAgentConfig_Activity(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create a valid YAML config file
	configContent := `instructions: "Activity test instructions"
model: "gpt-3.5-turbo"
`
	configPath := filepath.Join(tmpDir, "activity-test.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Set env var to use temp directory
	oldEnv := os.Getenv("AGENT_CONFIG_DIR")
	os.Setenv("AGENT_CONFIG_DIR", tmpDir)
	defer os.Setenv("AGENT_CONFIG_DIR", oldEnv)

	// Call activity function
	ctx := context.Background()
	config, err := GetAgentConfig(ctx, "activity-test")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify fields
	if config.Instructions != "Activity test instructions" {
		t.Errorf("Expected instructions 'Activity test instructions', got %q", config.Instructions)
	}
	if config.Model != "gpt-3.5-turbo" {
		t.Errorf("Expected model 'gpt-3.5-turbo', got %q", config.Model)
	}
	if config.Temperature != nil {
		t.Errorf("Expected temperature to be nil, got %v", config.Temperature)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

