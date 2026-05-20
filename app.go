package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"moonbridge-desktop/backend"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:resources
var resourcesFS embed.FS

// App is the main Wails application struct.
type App struct {
	ctx          context.Context
	configMgr    *backend.ConfigManager
	codexConfig  *backend.CodexConfig
	mbProcess    *backend.MBProcess
	retryProxy   *backend.RetryProxy
	metricsProxy *backend.MetricsProxy
	usageStore   *backend.UsageStore
	config       *backend.DesktopConfig
	systray      *backend.SystemTray
}

// NewApp creates a new App
func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	var err error
	a.configMgr, err = backend.NewConfigManager()
	if err != nil {
		panic(err)
	}

	codexCfg, codexErr := backend.NewCodexConfig()
	if codexErr != nil {
		fmt.Printf("[Codex] init failed: %v\n", codexErr)
	} else {
		a.codexConfig = codexCfg
	}

	a.config, err = a.configMgr.LoadConfig()
	if err != nil {
		a.config = a.defaultConfig()
		_ = a.configMgr.SaveConfig(a.config)
	}

	// Ensure all provider offers have model entries (fixes existing configs)
	a.ensureModelsFromOffers()

	// Auto-generate routes from providers
	a.syncRoutes()
	_ = a.configMgr.SaveConfig(a.config)

	// Sync Codex config on startup if enabled and installed
	if codexEnabled && a.codexConfig != nil && a.codexConfig.IsInstalled() {
		if err := a.codexConfig.Sync(a.config); err != nil {
			fmt.Printf("[Codex] startup sync failed: %v\n", err)
		} else {
			fmt.Println("[Codex] config synced on startup")
		}
	}

	// Initialize usage store for historical queries (even before bridge starts)
	dataDir := a.configMgr.DataDir()
	usageDir := filepath.Join(dataDir, "data")
	os.MkdirAll(usageDir, 0755)
	a.usageStore, _ = backend.NewUsageStore(usageDir, a.getModelPricing)

	// Start system tray
	a.startSystray()

	// Forward log entries from MBProcess to frontend
	go func() {
		for entry := range backend.LogChan() {
			runtime.EventsEmit(a.ctx, "log-entry", entry)
		}
	}()
}

func (a *App) shutdown(ctx context.Context) {
	if a.mbProcess != nil && a.mbProcess.IsRunning() {
		_ = a.mbProcess.Stop()
	}
	if a.retryProxy != nil {
		a.retryProxy.Stop()
	}
	if a.metricsProxy != nil {
		a.metricsProxy.Stop()
	}
	if a.usageStore != nil {
		_ = a.usageStore.Close()
	}
	if a.systray != nil {
		a.systray.Stop()
	}
	// Clean up any residual moonbridge processes
	backend.KillProcesses("moonbridge.exe")
}

func (a *App) startSystray() {
	a.systray = backend.NewSystemTray()
	a.systray.SetCallbacks(backend.TrayCallbacks{
		OnModelSelect: func(alias string) {
			if a.mbProcess == nil || !a.mbProcess.IsRunning() {
				if err := a.SwitchModel(alias); err != nil {
					println("switch model error:", err.Error())
					return
				}
				if err := a.StartMoonBridge(); err != nil {
					println("start service error:", err.Error())
				}
			} else {
				if err := a.SwitchModel(alias); err != nil {
					println("switch model (running) error:", err.Error())
				}
			}
		},
		OnShowWindow: func() {
			if a.ctx != nil {
				runtime.Show(a.ctx)
			}
		},
		OnExit: func() {
			if a.ctx != nil {
				_ = a.StopMoonBridge()
				runtime.Quit(a.ctx)
			}
		},
		GetModels: func() []backend.RouteConfig {
			if a.config != nil {
				return a.config.Routes
			}
			return nil
		},
		GetCurrent: func() string {
			if a.config != nil {
				return a.config.DefaultRoute
			}
			return ""
		},
	})
	if err := a.systray.Start(); err != nil {
		println("systray start error:", err.Error())
		if a.ctx != nil {
			runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
				Title:   "系统托盘",
				Message: "托盘图标启动失败: " + err.Error(),
				Type:    runtime.WarningDialog,
			})
		}
	}
}

// syncRoutes auto-generates one route per provider model.
func (a *App) syncRoutes() {
	existing := make(map[string]bool)
	routes := make([]backend.RouteConfig, 0)
	for _, p := range a.config.Providers {
		for _, o := range p.Offers {
			if !existing[o.Model] {
				routes = append(routes, backend.RouteConfig{
					Alias:    o.Model,
					Model:    o.Model,
					Provider: p.Key,
				})
				existing[o.Model] = true
			}
		}
	}
	// Add "moonbridge" as a fallback alias for Codex compatibility
	if len(routes) > 0 {
		routes = append([]backend.RouteConfig{{
			Alias:    "moonbridge",
			Model:    routes[0].Model,
			Provider: routes[0].Provider,
		}}, routes...)
	}
	a.config.Routes = routes
	if len(routes) > 0 && a.config.DefaultRoute == "" {
		a.config.DefaultRoute = routes[0].Alias
	}
	// Update system tray menu
	if a.systray != nil {
		a.systray.UpdateMenu()
	}
}

func (a *App) defaultConfig() *backend.DesktopConfig {
	presets := []backend.ProviderPreset{
		backend.PresetProviderDeepseek(),
		backend.PresetProviderQwen(),
		backend.PresetProviderAnthropic(),
		backend.PresetProviderOpenAI(),
		backend.PresetProviderGoogle(),
		backend.PresetProviderOpenRouter(),
		backend.PresetProviderOllama(),
		backend.PresetProviderSiliconFlow(),
	}
	providers := make([]backend.ProviderConfig, 0, len(presets))
	models := make([]backend.ModelConfig, 0)
	for _, p := range presets {
		providers = append(providers, backend.ProviderConfig{
			Key: p.Key, BaseURL: p.BaseURL, APIKey: "", Protocol: p.Protocol,
			Version: p.Version, Offers: p.Models,
		})
		caps, series := inferModelMetadata(p.Models[0].Model, p.Key)
		models = append(models, backend.ModelConfig{
			Slug: p.Models[0].Model, DisplayName: p.Models[0].Model,
			ContextWindow: 1000000, MaxOutputTokens: 65536,
			Capabilities: caps, Series: series, Extensions: map[string]bool{},
		})
	}
	return &backend.DesktopConfig{
		Port:           38440,
		LogLevel:       "info",
		Providers:      providers,
		Models:         models,
		DefaultRoute:   presets[0].Models[0].Model,
		MaxTokens:      65536,
		MetricsEnabled: true,
	}
}

// ----- Status -----

// GetStatus returns whether Moon Bridge is running and current config.
func (a *App) GetStatus() backend.MBStatus {
	if a.mbProcess != nil && a.mbProcess.IsRunning() {
		return backend.MBStatus{
			Running:      true,
			Port:         a.config.Port,
			CurrentRoute: a.config.DefaultRoute,
		}
	}
	return backend.MBStatus{
		Running:      false,
		Port:         a.config.Port,
		CurrentRoute: a.config.DefaultRoute,
	}
}

// ----- Moon Bridge Lifecycle -----

// StartMoonBridge generates config and starts the proxy.
func (a *App) StartMoonBridge() error {
	dataDir := a.configMgr.DataDir()

	// Extract binary if needed
	binaryPath := filepath.Join(dataDir, "moonbridge.exe")
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		if err := backend.ExtractBinary(resourcesFS, binaryPath); err != nil {
			return fmt.Errorf("extract binary: %w", err)
		}
	}

	// Ensure internal port is set
	internalPort := a.config.InternalPort
	if internalPort == 0 {
		internalPort = a.config.Port + 1
	}
	a.config.InternalPort = internalPort

	// Generate YAML (uses internal port for MoonBridge)
	configPath := filepath.Join(dataDir, "config.yml")
	if err := a.configMgr.GenerateMoonBridgeYAML(a.config); err != nil {
		return fmt.Errorf("generate config: %w", err)
	}

	// Create data subdirectory for SQLite
	os.MkdirAll(filepath.Join(dataDir, "data"), 0755)

	// Start MoonBridge on internal port
	a.mbProcess = backend.NewMBProcess(internalPort, configPath, binaryPath)

	if err := a.mbProcess.Start(); err != nil {
		return fmt.Errorf("start moonbridge: %w", err)
	}

	// Start retry proxy on external port -> internal port
	a.retryProxy = backend.NewRetryProxy(a.config.Port, internalPort, 3)
	if err := a.retryProxy.Start(); err != nil {
		a.mbProcess.Stop()
		return fmt.Errorf("start retry proxy: %w", err)
	}

	// Start metrics polling (use internal port since MoonBridge listens there)
	a.metricsProxy = backend.NewMetricsProxy(internalPort)
	a.metricsProxy.Start()

	// Connect retry proxy to usage store (usageStore already initialized in startup)
	a.retryProxy.SetRecordUsage(func(model string, inputTokens, outputTokens, cacheRead, cacheWrite int) {
		if a.usageStore != nil {
			a.usageStore.AddUsage(model, inputTokens, outputTokens, cacheRead, cacheWrite)
		}
	})
	a.retryProxy.SetRecordRequest(func(model string) {
		if a.usageStore != nil {
			a.usageStore.AddUsage(model, 0, 0, 0, 0)
		}
	})

	return nil
}

// StopMoonBridge stops the proxy.
func (a *App) StopMoonBridge() error {
	if a.mbProcess == nil || !a.mbProcess.IsRunning() {
		return fmt.Errorf("not running")
	}
	if a.retryProxy != nil {
		a.retryProxy.Stop()
	}
	if a.metricsProxy != nil {
		a.metricsProxy.Stop()
	}
	err := a.mbProcess.Stop()
	backend.KillProcesses("moonbridge.exe")
	return err
}

// ----- Config -----

// GetConfig returns the current desktop config.
func (a *App) GetConfig() backend.DesktopConfig {
	if a.config == nil {
		cfg, _ := a.configMgr.LoadConfig()
		a.config = cfg
	}
	return *a.config
}

// SaveConfig saves the desktop config and regenerates YAML if running.
func (a *App) SaveConfig(cfg backend.DesktopConfig) error {
	a.config = &cfg
	if err := a.configMgr.SaveConfig(a.config); err != nil {
		return err
	}
	// If running, regenerate YAML and restart
	if a.mbProcess != nil && a.mbProcess.IsRunning() {
		_ = a.StopMoonBridge()
		return a.StartMoonBridge()
	}
	return nil
}

// ----- Models & Providers -----

// ListModels returns available models.
func (a *App) ListModels() []backend.ModelConfig {
	if a.config == nil {
		return nil
	}
	return a.config.Models
}

// ListProviders returns configured providers.
func (a *App) ListProviders() []backend.ProviderConfig {
	if a.config == nil {
		return nil
	}
	return a.config.Providers
}

// AddProvider adds a new provider.
func (a *App) AddProvider(p backend.ProviderConfig) error {
	if a.config == nil {
		cfg, _ := a.configMgr.LoadConfig()
		a.config = cfg
	}
	// Check for duplicate key
	for _, existing := range a.config.Providers {
		if existing.Key == p.Key {
			return fmt.Errorf("服务 '%s' 已存在，请在列表中编辑", p.Key)
		}
	}
	a.config.Providers = append(a.config.Providers, p)
	// Auto-add models from provider offers
	a.ensureModelsFromOffers()
	a.syncRoutes()
	_ = a.configMgr.SaveConfig(a.config)
	a.syncCodexConfig()
	return nil
}

// ensureModelsFromOffers syncs Models to match all provider offers.
// Adds new models and removes models that no longer exist in any offer.
func (a *App) ensureModelsFromOffers() {
	// Collect all model slugs currently offered
	offered := make(map[string]bool)
	for _, p := range a.config.Providers {
		for _, o := range p.Offers {
			offered[o.Model] = true
		}
	}

	// Keep existing models that are still offered, update series/capabilities
	kept := make([]backend.ModelConfig, 0, len(a.config.Models))
	for _, m := range a.config.Models {
		if offered[m.Slug] {
			// Update series/capabilities from the first matching offer
			for _, p := range a.config.Providers {
				for _, o := range p.Offers {
					if o.Model == m.Slug {
						if o.Series != "" {
							m.Series = o.Series
						}
						if len(o.Capabilities) > 0 {
							m.Capabilities = o.Capabilities
						}
						goto nextModel
					}
				}
			}
		nextModel:
			kept = append(kept, m)
			delete(offered, m.Slug)
		}
	}

	// Add remaining offered models that don't have entries yet
	for _, p := range a.config.Providers {
		for _, o := range p.Offers {
			if !offered[o.Model] {
				continue
			}
			caps := o.Capabilities
			series := o.Series
			if len(caps) == 0 {
				caps, series = inferModelMetadata(o.Model, p.Key)
			} else if series == "" {
				_, series = inferModelMetadata(o.Model, p.Key)
			}
			kept = append(kept, backend.ModelConfig{
				Slug:            o.Model,
				DisplayName:     o.Model,
				ContextWindow:   1000000,
				MaxOutputTokens: 65536,
				Capabilities:    caps,
				Series:          series,
				Extensions:      map[string]bool{},
			})
			delete(offered, o.Model)
		}
	}
	a.config.Models = kept
}

// UpdateProvider updates an existing provider by key.
func (a *App) UpdateProvider(key string, p backend.ProviderConfig) error {
	if a.config == nil {
		cfg, _ := a.configMgr.LoadConfig()
		a.config = cfg
	}
	for i, existing := range a.config.Providers {
		if existing.Key == key {
			a.config.Providers[i] = p
			a.ensureModelsFromOffers()
			a.syncRoutes()
			_ = a.configMgr.SaveConfig(a.config)
			a.syncCodexConfig()
			return nil
		}
	}
	return fmt.Errorf("provider not found: %s", key)
}

// DeleteProvider removes a provider by key.
func (a *App) DeleteProvider(key string) error {
	if a.config == nil {
		cfg, _ := a.configMgr.LoadConfig()
		a.config = cfg
	}
	newProviders := make([]backend.ProviderConfig, 0, len(a.config.Providers))
	for _, p := range a.config.Providers {
		if p.Key != key {
			newProviders = append(newProviders, p)
		}
	}
	if len(newProviders) == len(a.config.Providers) {
		return fmt.Errorf("provider not found: %s", key)
	}
	a.config.Providers = newProviders
	a.syncRoutes()
	_ = a.configMgr.SaveConfig(a.config)
	a.syncCodexConfig()
	return nil
}

// SwitchModel changes the active route to use a different model.
func (a *App) SwitchModel(routeAlias string) error {
	if a.config == nil {
		cfg, _ := a.configMgr.LoadConfig()
		a.config = cfg
	}
	a.config.DefaultRoute = routeAlias
	if err := a.configMgr.SaveConfig(a.config); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	a.syncCodexConfig()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "model-changed", routeAlias)
	}
	// If running, restart with new config
	if a.mbProcess != nil && a.mbProcess.IsRunning() {
		_ = a.StopMoonBridge()
		return a.StartMoonBridge()
	}
	return nil
}

// GetUsageStats returns current usage statistics (today).
func (a *App) GetUsageStats() backend.UsageStats {
	if a.usageStore != nil {
		return a.usageStore.GetTodayStats()
	}
	if a.metricsProxy != nil {
		return a.metricsProxy.GetStats()
	}
	return backend.UsageStats{}
}

// AddUsage adds usage data from a completed request (manual call).
func (a *App) AddUsage(model string, inputTokens, outputTokens, cacheRead, cacheWrite int) {
	if a.usageStore != nil {
		a.usageStore.AddUsage(model, inputTokens, outputTokens, cacheRead, cacheWrite)
	}
}

// GetUsageDailyStats returns aggregated usage for the last N days.
func (a *App) GetUsageDailyStats(days int) []backend.DailyUsageRow {
	if a.usageStore == nil {
		return nil
	}
	return a.usageStore.GetDailyStats(days)
}

// GetUsageByModel returns usage grouped by model for a date range.
func (a *App) GetUsageByModel(startStr, endStr string) []backend.ModelUsageRow {
	if a.usageStore == nil {
		return nil
	}
	start, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		return nil
	}
	end, err := time.Parse("2006-01-02", endStr)
	if err != nil {
		return nil
	}
	return a.usageStore.GetByModel(start, end)
}

// GetUsageRecentRecords returns the most recent usage records.
func (a *App) GetUsageRecentRecords(limit int) []backend.UsageRecord {
	if a.usageStore == nil {
		return nil
	}
	return a.usageStore.GetRecentRecords(limit)
}

// ClearUsageToday removes all usage records from today.
func (a *App) ClearUsageToday() (int64, error) {
	if a.usageStore == nil {
		return 0, fmt.Errorf("usage store not initialized")
	}
	return a.usageStore.ClearToday()
}

// ClearUsageDateRange removes usage records in the given date range (inclusive, YYYY-MM-DD).
func (a *App) ClearUsageDateRange(startStr, endStr string) (int64, error) {
	if a.usageStore == nil {
		return 0, fmt.Errorf("usage store not initialized")
	}
	start, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		return 0, fmt.Errorf("invalid start date: %w", err)
	}
	end, err := time.Parse("2006-01-02", endStr)
	if err != nil {
		return 0, fmt.Errorf("invalid end date: %w", err)
	}
	return a.usageStore.ClearDateRange(start, end)
}

// ClearUsageAll removes all usage records.
func (a *App) ClearUsageAll() (int64, error) {
	if a.usageStore == nil {
		return 0, fmt.Errorf("usage store not initialized")
	}
	return a.usageStore.ClearAll()
}

// getModelPricing returns pricing info for a model from the current config.
func (a *App) getModelPricing(model string) *backend.ModelPricing {
	if a.config == nil {
		return nil
	}
	for _, p := range a.config.Providers {
		for _, o := range p.Offers {
			if o.Model == model {
				return &backend.ModelPricing{
					Input:          o.Pricing.Input,
					Output:         o.Pricing.Output,
					CacheRead:      o.Pricing.CacheRead,
					CacheWrite:     o.Pricing.CacheWrite,
					BillingMode:    o.Pricing.BillingMode,
					PerRequestCost: o.Pricing.PerRequestCost,
				}
			}
		}
	}
	return nil
}

// GetProviderPresets returns built-in provider presets.
func (a *App) GetProviderPresets() []backend.ProviderPreset {
	return []backend.ProviderPreset{
		backend.PresetProviderDeepseek(),
		backend.PresetProviderQwen(),
		backend.PresetProviderAnthropic(),
		backend.PresetProviderOpenAI(),
		backend.PresetProviderGoogle(),
		backend.PresetProviderOpenRouter(),
		backend.PresetProviderOllama(),
		backend.PresetProviderSiliconFlow(),
	}
}

// inferModelMetadata infers capabilities and series from model slug and provider key.
func inferModelMetadata(slug, providerKey string) (capabilities []string, series string) {
	slug = strings.ToLower(slug)

	// Determine series
	switch {
	case strings.HasPrefix(slug, "qwen"):
		series = "qwen"
	case strings.HasPrefix(slug, "claude"):
		series = "claude"
	case strings.HasPrefix(slug, "gpt-"), strings.HasPrefix(slug, "o3"), strings.HasPrefix(slug, "o4"):
		series = "gpt"
	case strings.HasPrefix(slug, "gemini"):
		series = "gemini"
	case strings.HasPrefix(slug, "deepseek"):
		series = "deepseek"
	case strings.HasPrefix(slug, "llama"):
		series = "llama"
	case strings.HasPrefix(slug, "codestral"):
		series = "codestral"
	case strings.Contains(slug, "glm"):
		series = "glm"
	default:
		series = ""
	}

	// Helper to add a capability only once
	has := func(cap string) bool {
		for _, c := range capabilities {
			if c == cap {
				return true
			}
		}
		return false
	}
	add := func(cap string) {
		if !has(cap) {
			capabilities = append(capabilities, cap)
		}
	}

	// Keyword-based inference
	if strings.Contains(slug, "vision") || strings.Contains(slug, "pro") || strings.Contains(slug, "sonnet") {
		add("vision")
	}
	if strings.Contains(slug, "reasoning") || strings.Contains(slug, "o3") || strings.Contains(slug, "o4") || strings.Contains(slug, "max") {
		add("reasoning")
	}
	if strings.Contains(slug, "coder") || strings.Contains(slug, "code") || strings.Contains(slug, "codestral") {
		add("coding")
	}
	if strings.Contains(slug, "long") || strings.Contains(slug, "200k") || strings.Contains(slug, "500k") {
		add("long_context")
	}
	if strings.Contains(slug, "search") || strings.Contains(slug, "web") {
		add("web_search")
	}

	// Provider-specific overrides
	if providerKey == "google" && strings.Contains(slug, "pro") {
		add("vision")
		add("reasoning")
		add("long_context")
	}
	if providerKey == "openai" && (strings.HasPrefix(slug, "gpt-4") || strings.HasPrefix(slug, "o")) {
		add("vision")
		add("reasoning")
	}

	if len(capabilities) == 0 {
		capabilities = []string{}
	}
	return
}

// SetPort updates the listen port.
func (a *App) SetPort(port int) error {
	if a.config == nil {
		cfg, _ := a.configMgr.LoadConfig()
		a.config = cfg
	}
	a.config.Port = port
	return a.configMgr.SaveConfig(a.config)
}

// SetLogLevel updates the log level.
func (a *App) SetLogLevel(level string) error {
	if a.config == nil {
		cfg, _ := a.configMgr.LoadConfig()
		a.config = cfg
	}
	a.config.LogLevel = level
	return a.configMgr.SaveConfig(a.config)
}

// ----- Codex Integration -----

// codexEnabled tracks whether Moon Bridge should manage Codex config.
var codexEnabled = true

// SyncCodexConfig syncs MoonBridge config to Codex CLI configuration.
func (a *App) SyncCodexConfig() error {
	if !codexEnabled {
		return fmt.Errorf("Codex 集成已关闭")
	}
	if a.codexConfig == nil {
		var err error
		a.codexConfig, err = backend.NewCodexConfig()
		if err != nil {
			return fmt.Errorf("init codex config: %w", err)
		}
	}
	if !a.codexConfig.IsInstalled() {
		return fmt.Errorf("未检测到 Codex CLI")
	}
	a.syncRoutes()
	if err := a.codexConfig.Sync(a.config); err != nil {
		return err
	}
	return nil
}

// IsCodexInstalled returns true if Codex CLI is detected.
func (a *App) IsCodexInstalled() bool {
	if a.codexConfig == nil {
		a.codexConfig, _ = backend.NewCodexConfig()
	}
	return a.codexConfig != nil && a.codexConfig.IsInstalled()
}

// IsCodexEnabled returns whether Codex integration is currently enabled.
func (a *App) IsCodexEnabled() bool {
	return codexEnabled
}

// SetCodexEnabled enables or disables Codex integration.
// When disabling, it removes Moon Bridge entries from Codex config files.
func (a *App) SetCodexEnabled(enabled bool) error {
	codexEnabled = enabled
	if !enabled {
		if a.codexConfig != nil {
			if err := a.codexConfig.Reset(); err != nil {
				return fmt.Errorf("reset codex config: %w", err)
			}
		}
		return nil
	}
	// Re-enable: sync current config
	return a.SyncCodexConfig()
}

// syncCodexConfig silently syncs Codex config (no error to frontend).
func (a *App) syncCodexConfig() {
	if !codexEnabled {
		return
	}
	if a.codexConfig != nil && a.codexConfig.IsInstalled() {
		_ = a.codexConfig.Sync(a.config)
	}
}
