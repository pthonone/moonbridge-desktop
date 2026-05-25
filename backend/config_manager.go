package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	appDataDirName = "moonbridge-desktop"
	configFileName = "config.json"
)

// ConfigManager handles reading/writing desktop config and generating Moon Bridge YAML.
type ConfigManager struct {
	dataDir string
}

func NewConfigManager() (*ConfigManager, error) {
	dataDir, err := getAppDataDir()
	if err != nil {
		return nil, err
	}
	return &ConfigManager{dataDir: dataDir}, nil
}

func (cm *ConfigManager) DataDir() string { return cm.dataDir }

// LoadConfig loads the desktop JSON config.
func (cm *ConfigManager) LoadConfig() (*DesktopConfig, error) {
	path := filepath.Join(cm.dataDir, configFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cm.defaultConfig(), nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg DesktopConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// SaveConfig saves the desktop JSON config.
func (cm *ConfigManager) SaveConfig(cfg *DesktopConfig) error {
	if err := os.MkdirAll(cm.dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(cm.dataDir, configFileName)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// ---------- defaults ----------

func (cm *ConfigManager) defaultConfig() *DesktopConfig {
	ds := PresetProviderDeepseek()
	return &DesktopConfig{
		Port:           38440,
		InternalPort:   38441,
		LogLevel:       "info",
		Providers: []ProviderConfig{
			{
				Key:      ds.Key,
				BaseURL:  ds.BaseURL,
				APIKey:   "",
				Protocol: ds.Protocol,
				Version:  ds.Version,
				Offers:   ds.Models,
			},
		},
		Models: []ModelConfig{
			{
				Slug:            "deepseek-v4-pro",
				DisplayName:     "DeepSeek V4 Pro",
				ContextWindow:   1000000,
				MaxOutputTokens: 384000,
				ReasoningLevel:  "high",
				Extensions:      map[string]bool{"deepseek_v4": true},
			},
			{
				Slug:            "deepseek-v4-flash",
				DisplayName:     "DeepSeek V4 Flash",
				ContextWindow:   1000000,
				MaxOutputTokens: 384000,
				ReasoningLevel:  "high",
				Extensions:      map[string]bool{"deepseek_v4": true},
			},
		},
		Routes: []RouteConfig{
			{Alias: "moonbridge", Model: "deepseek-v4-flash", Provider: "deepseek"},
		},
		DefaultRoute:   "moonbridge",
		MaxTokens:      65536,
		MetricsEnabled: true,
	}
}

func getAppDataDir() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appData, appDataDirName), nil
}
