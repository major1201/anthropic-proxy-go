// Package config provides configuration loading, singleton access, and hot reload.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

// ServerConfig is the server configuration.
type ServerConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// LoggingConfig is the logging configuration.
type LoggingConfig struct {
	Level string `json:"level"`
}

// OpenAIModelConfig is the configuration for a specific model route.
type OpenAIModelConfig struct {
	BaseURL string  `json:"base_url"`
	APIKey  string  `json:"api_key"`
	Model   *string `json:"model,omitempty"`
}

// ParameterOverridesConfig allows overriding request parameters from config.
type ParameterOverridesConfig struct {
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`
}

// Config is the root application configuration.
type Config struct {
	Server             ServerConfig             `json:"server"`
	APIKey             string                   `json:"api_key"`
	Logging            LoggingConfig            `json:"logging"`
	ParameterOverrides ParameterOverridesConfig `json:"parameter_overrides"`
	Routes             map[string]OpenAIModelConfig `json:"routes"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8000,
		},
		APIKey: "your-proxy-api-key-here",
		Logging: LoggingConfig{
			Level: "INFO",
		},
		Routes: map[string]OpenAIModelConfig{},
	}
}

// globalConfig holds the current config via atomic pointer for lock-free reads.
var globalConfig atomic.Pointer[Config]

func init() {
	globalConfig.Store(DefaultConfig())
}

// GetConfig returns the current global config.
func GetConfig() *Config {
	return globalConfig.Load()
}

// SetConfig atomically replaces the global config.
func SetConfig(cfg *Config) {
	globalConfig.Store(cfg)
}

// GetConfigFilePath returns the config file path from env or default.
func GetConfigFilePath() string {
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		return p
	}
	return "config/settings.json"
}

// LoadConfig loads configuration from a JSON file.
// Falls back to config/example.json if the specified file doesn't exist.
func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = GetConfigFilePath()
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Try example config as fallback
			examplePath := filepath.Join(filepath.Dir(configPath), "example.json")
			exampleData, exampleErr := os.ReadFile(examplePath)
			if exampleErr != nil {
				cfg := DefaultConfig()
				return cfg, nil
			}

			cfg := DefaultConfig()
			if err := json.Unmarshal(exampleData, cfg); err != nil {
				return nil, fmt.Errorf("failed to parse example config: %w", err)
			}

			// Write example as settings.json
			settingsData, _ := json.MarshalIndent(cfg, "", "  ")
			_ = os.WriteFile(configPath, settingsData, 0644)

			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}

	return cfg, nil
}

// GetRoute looks up the route configuration for a model name.
// Falls back to "__default__" route if the specific model is not found.
func (c *Config) GetRoute(model string) (*OpenAIModelConfig, bool) {
	if route, ok := c.Routes[model]; ok {
		return &route, true
	}
	if route, ok := c.Routes["__default__"]; ok {
		return &route, true
	}
	return nil, false
}

// GetServerAddr returns the server address as host:port.
func (c *Config) GetServerAddr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}
