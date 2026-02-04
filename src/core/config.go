package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const ConfigFile = "config.json"

// ToolConfig holds configuration for a tool (implementer or reviewer).
type ToolConfig struct {
	Tool  string `json:"tool,omitempty"`
	Model string `json:"model,omitempty"`
}

// Config holds the autom8 configuration.
type Config struct {
	Implementer ToolConfig `json:"implementer,omitempty"`
	Reviewer    ToolConfig `json:"reviewer,omitempty"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Implementer: ToolConfig{Tool: "claude"},
		Reviewer:    ToolConfig{Tool: "claude"},
	}
}

// LoadConfig loads configuration from .autom8/config.json.
// Returns default config if the file doesn't exist.
func LoadConfig() (Config, error) {
	dir, err := GetAutom8Dir()
	if err != nil {
		return DefaultConfig(), nil
	}

	configPath := filepath.Join(dir, ConfigFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	// Apply defaults for missing values
	if cfg.Implementer.Tool == "" {
		cfg.Implementer.Tool = "claude"
	}
	if cfg.Reviewer.Tool == "" {
		cfg.Reviewer.Tool = "claude"
	}

	return cfg, nil
}
