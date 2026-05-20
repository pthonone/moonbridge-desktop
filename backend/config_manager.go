package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	appDataDirName = "moonbridge-desktop"
	configFileName = "config.json"
	yamlFileName   = "config.yml"
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

// GenerateMoonBridgeYAML writes the Moon Bridge YAML config file to the data directory.
func (cm *ConfigManager) GenerateMoonBridgeYAML(cfg *DesktopConfig) error {
	if err := os.MkdirAll(cm.dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	var sb strings.Builder

	// Header
	sb.WriteString("mode: \"Transform\"\n\n")

	// Server - MoonBridge listens on internal port, retry proxy listens on external port
	internalPort := cfg.InternalPort
	if internalPort == 0 {
		internalPort = cfg.Port + 1
	}
	sb.WriteString(fmt.Sprintf("server:\n  addr: \"127.0.0.1:%d\"\n\n", internalPort))

	// Persistence
	sb.WriteString("persistence:\n  active_provider: db_sqlite\n\n")

	// Extensions
	if cfg.MetricsEnabled {
		sb.WriteString("extensions:\n  metrics:\n    enabled: true\n  db_sqlite:\n    enabled: true\n    config:\n      path: ./data/moonbridge.db\n      wal: true\n\n")
	} else {
		sb.WriteString("extensions:\n  db_sqlite:\n    enabled: true\n    config:\n      path: ./data/moonbridge.db\n      wal: true\n\n")
	}

	// Models - include both cfg.Models and provider offers
	modelSlugs := make(map[string]bool)
	sb.WriteString("models:\n")
	for _, m := range cfg.Models {
		sb.WriteString(fmt.Sprintf("  %s:\n", m.Slug))
		sb.WriteString(fmt.Sprintf("    context_window: %d\n", m.ContextWindow))
		if m.MaxOutputTokens > 0 {
			sb.WriteString(fmt.Sprintf("    max_output_tokens: %d\n", m.MaxOutputTokens))
		}
		if len(m.Extensions) > 0 {
			sb.WriteString("    extensions:\n")
			for extName, enabled := range m.Extensions {
				sb.WriteString(fmt.Sprintf("      %s:\n        enabled: %t\n", extName, enabled))
			}
		}
		sb.WriteString("\n")
		modelSlugs[m.Slug] = true
	}
	// Add models from provider offers not already in cfg.Models
	for _, p := range cfg.Providers {
		for _, o := range p.Offers {
			if !modelSlugs[o.Model] {
				sb.WriteString(fmt.Sprintf("  %s:\n    context_window: 1000000\n\n", o.Model))
				modelSlugs[o.Model] = true
			}
		}
	}

	// Providers
	sb.WriteString("providers:\n")
	for _, p := range cfg.Providers {
		sb.WriteString(fmt.Sprintf("  %s:\n", p.Key))
		sb.WriteString(fmt.Sprintf("    base_url: \"%s\"\n", p.BaseURL))
		sb.WriteString(fmt.Sprintf("    api_key: \"%s\"\n", p.APIKey))
		sb.WriteString(fmt.Sprintf("    protocol: \"%s\"\n", p.Protocol))
		if p.Version != "" {
			sb.WriteString(fmt.Sprintf("    version: \"%s\"\n", p.Version))
		}
		sb.WriteString("    offers:\n")
		for _, o := range p.Offers {
			sb.WriteString(fmt.Sprintf("      - model: %s\n", o.Model))
			if o.Pricing.Input > 0 || o.Pricing.Output > 0 {
				sb.WriteString("        pricing:\n")
				sb.WriteString(fmt.Sprintf("          input_price: %.1f\n", o.Pricing.Input))
				sb.WriteString(fmt.Sprintf("          output_price: %.1f\n", o.Pricing.Output))
				sb.WriteString(fmt.Sprintf("          cache_write_price: %.2f\n", o.Pricing.CacheWrite))
				sb.WriteString(fmt.Sprintf("          cache_read_price: %.2f\n", o.Pricing.CacheRead))
			}
		}
		sb.WriteString("\n")
	}

	// Routes
	sb.WriteString("routes:\n")
	for _, r := range cfg.Routes {
		sb.WriteString(fmt.Sprintf("  %s:\n    model: %s\n    provider: %s\n\n", r.Alias, r.Model, r.Provider))
	}

	// Defaults
	defaultRoute := cfg.DefaultRoute
	if defaultRoute == "" && len(cfg.Routes) > 0 {
		defaultRoute = cfg.Routes[0].Alias
	}
	sb.WriteString(fmt.Sprintf("defaults:\n  model: %s\n  max_tokens: %d\n", defaultRoute, cfg.MaxTokens))

	path := filepath.Join(cm.dataDir, yamlFileName)
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

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
			{Alias: "moonbridge", Model: "deepseek-v4-pro", Provider: "deepseek"},
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
