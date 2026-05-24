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
	a.syncCodexConfig()

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
		// Preserve existing moonbridge route's Model if user customized it
		mbModel := routes[0].Model
		for _, r := range a.config.Routes {
			if r.Alias == "moonbridge" && r.Model != "" && r.Model != routes[0].Model {
				mbModel = r.Model
				break
			}
		}
		routes = append([]backend.RouteConfig{{
			Alias:    "moonbridge",
			Model:    mbModel,
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
	presets := a.GetProviderPresets()
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
	running := a.mbProcess != nil && a.mbProcess.IsRunning()
	return backend.MBStatus{
		Running:      running,
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

	// Validate SQLite database — if corrupted, backup and let MoonBridge recreate
	dbPath := filepath.Join(dataDir, "data", "moonbridge.db")
	backend.ValidateAndFixSQLiteDB(dbPath)

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
	a.retryProxy.SetResolveModel(func(alias string) string {
		if a.config != nil {
			for _, r := range a.config.Routes {
				if r.Alias == alias {
					resolved := r.Model
					backend.AppLogger.Printf("[ResolveModel] %s -> %s", alias, resolved)
					return resolved
				}
			}
		}
		// No route found — use the alias as-is (it's likely the actual model name)
		backend.AppLogger.Printf("[ResolveModel] %s -> %s (no route, returning alias)", alias, alias)
		return alias
	})
	a.retryProxy.SetGetCurrentModel(func() string {
		if a.config != nil {
			return a.config.DefaultRoute
		}
		return ""
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

// ensureConfigLoaded lazily loads config if nil.
func (a *App) ensureConfigLoaded() error {
	if a.config == nil {
		cfg, err := a.configMgr.LoadConfig()
		if err != nil {
			return err
		}
		a.config = cfg
	}
	return nil
}

// GetConfig returns the current desktop config.
func (a *App) GetConfig() backend.DesktopConfig {
	_ = a.ensureConfigLoaded()
	if a.config == nil {
		return backend.DesktopConfig{}
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

// AddModel adds a new model to the config.
func (a *App) AddModel(m backend.ModelConfig) error {
	if err := a.ensureConfigLoaded(); err != nil {
		return err
	}
	for _, existing := range a.config.Models {
		if existing.Slug == m.Slug {
			return fmt.Errorf("模型 '%s' 已存在", m.Slug)
		}
	}
	a.config.Models = append(a.config.Models, m)
	a.syncRoutes()
	_ = a.configMgr.SaveConfig(a.config)
	a.syncCodexConfig()
	return nil
}

// DeleteModel removes a model by slug.
func (a *App) DeleteModel(slug string) error {
	if err := a.ensureConfigLoaded(); err != nil {
		return err
	}
	before := len(a.config.Models)
	newModels := make([]backend.ModelConfig, 0, before)
	for _, m := range a.config.Models {
		if m.Slug != slug {
			newModels = append(newModels, m)
		}
	}
	if len(newModels) == before {
		return fmt.Errorf("模型 '%s' 不存在", slug)
	}
	a.config.Models = newModels
	// If the deleted model was the default, reset to first available
	if a.config.DefaultRoute == slug && len(a.config.Models) > 0 {
		a.config.DefaultRoute = a.config.Models[0].Slug
	}
	a.syncRoutes()
	_ = a.configMgr.SaveConfig(a.config)
	a.syncCodexConfig()
	return nil
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
	if err := a.ensureConfigLoaded(); err != nil {
		return err
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

// ensureModelsFromOffers rebuilds Models from all provider offers,
// preserving context_window and other user-edited fields for existing models.
func (a *App) ensureModelsFromOffers() {
	// Build offer map: model slug -> first matching offer
	type offerInfo struct {
		offer backend.OfferConfig
		providerKey string
	}
	offerMap := make(map[string]offerInfo)
	for _, p := range a.config.Providers {
		for _, o := range p.Offers {
			if _, exists := offerMap[o.Model]; !exists {
				offerMap[o.Model] = offerInfo{offer: o, providerKey: p.Key}
			}
		}
	}

	backend.AppLogger.Printf("[ensureModelsFromOffers] offers=%d, existing models=%d", len(offerMap), len(a.config.Models))

	// Build new model list from offers, preserving existing model data where possible
	existingMap := make(map[string]backend.ModelConfig)
	for _, m := range a.config.Models {
		existingMap[m.Slug] = m
	}

	var kept []backend.ModelConfig
	// Track order: maintain existing order for models still in offers, append new ones
	seenOrder := make(map[string]bool)
	// First pass: existing models still offered (preserve order)
	for _, m := range a.config.Models {
		if info, ok := offerMap[m.Slug]; ok {
			o := info.offer
			// Update all fields from offer
			m.Series = o.Series
			m.Capabilities = o.Capabilities
			kept = append(kept, m)
			seenOrder[m.Slug] = true
		} else {
			backend.AppLogger.Printf("[ensureModelsFromOffers] removing model %s (no longer in offers)", m.Slug)
		}
	}
	// Second pass: new models not yet in config
	for _, p := range a.config.Providers {
		for _, o := range p.Offers {
			if _, exists := seenOrder[o.Model]; exists {
				continue
			}
			caps := o.Capabilities
			series := o.Series
			if len(caps) == 0 {
				caps, series = inferModelMetadata(o.Model, p.Key)
			} else if series == "" {
				_, series = inferModelMetadata(o.Model, p.Key)
			}
			displayName := o.Model
			if o.Series != "" {
				displayName = o.Series
			}
			kept = append(kept, backend.ModelConfig{
				Slug:            o.Model,
				DisplayName:     displayName,
				ContextWindow:   1000000,
				MaxOutputTokens: 65536,
				Capabilities:    caps,
				Series:          series,
				Extensions:      map[string]bool{},
			})
			seenOrder[o.Model] = true
		}
	}
	a.config.Models = kept
}

// UpdateProvider updates an existing provider by key.
func (a *App) UpdateProvider(key string, p backend.ProviderConfig) error {
	if err := a.ensureConfigLoaded(); err != nil {
		return err
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
	if err := a.ensureConfigLoaded(); err != nil {
		return err
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
	// If the default route was from the deleted provider, reset to first available
	existing := make(map[string]bool)
	for _, p := range a.config.Providers {
		for _, o := range p.Offers {
			existing[o.Model] = true
		}
	}
	if !existing[a.config.DefaultRoute] && len(a.config.Routes) > 0 {
		a.config.DefaultRoute = a.config.Routes[0].Alias
	}
	_ = a.configMgr.SaveConfig(a.config)
	a.syncCodexConfig()
	return nil
}

// SwitchModel changes the active route to use a different model.
func (a *App) SwitchModel(routeAlias string) error {
	backend.AppLogger.Printf("[SwitchModel] switching to: %s", routeAlias)
	if err := a.ensureConfigLoaded(); err != nil {
		return err
	}

	// Rebuild routes from provider offers to remove stale entries
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

	// Verify the requested model exists in provider offers
	if !existing[routeAlias] {
		return fmt.Errorf("model %q not found in any provider", routeAlias)
	}

	// Add "moonbridge" as a fallback alias for Codex compatibility
	if len(routes) > 0 {
		routes = append([]backend.RouteConfig{{
			Alias:    "moonbridge",
			Model:    routeAlias,
			Provider: routes[0].Provider,
		}}, routes...)
	}

	a.config.Routes = routes
	a.config.DefaultRoute = routeAlias
	backend.AppLogger.Printf("[SwitchModel] DefaultRoute=%s, routes=%d (rebuilt)", routeAlias, len(a.config.Routes))

	if err := a.configMgr.SaveConfig(a.config); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	a.syncCodexConfig()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "model-changed", routeAlias)
	}
	// Update system tray menu
	if a.systray != nil {
		a.systray.UpdateMenu()
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

// GetUsageHourlyStats returns today's usage grouped by hour (0-23).
func (a *App) GetUsageHourlyStats() []backend.HourlyUsageRow {
	if a.usageStore == nil {
		return nil
	}
	return a.usageStore.GetHourlyStats()
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
func (a *App) getModelPricing(model string) *backend.Pricing {
	if err := a.ensureConfigLoaded(); err != nil {
		return nil
	}
	for _, p := range a.config.Providers {
		for _, o := range p.Offers {
			if o.Model == model {
				return &o.Pricing
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
	if strings.Contains(slug, "vision") || strings.Contains(slug, "sonnet") {
		add("vision")
	}
	if strings.Contains(slug, "reasoning") || strings.HasPrefix(slug, "o3") || strings.HasPrefix(slug, "o4") {
		add("reasoning")
	}
	if strings.Contains(slug, "coder") || strings.Contains(slug, "codestral") || strings.HasPrefix(slug, "code-") {
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
	if providerKey == "anthropic" && strings.Contains(slug, "pro") {
		add("vision")
	}
	if providerKey == "deepseek" && strings.Contains(slug, "pro") {
		add("vision")
	}

	if len(capabilities) == 0 {
		capabilities = []string{}
	}
	return
}

// SetPort updates the listen port.
func (a *App) SetPort(port int) error {
	if err := a.ensureConfigLoaded(); err != nil {
		return err
	}
	a.config.Port = port
	return a.configMgr.SaveConfig(a.config)
}

// SetLogLevel updates the log level.
func (a *App) SetLogLevel(level string) error {
	if err := a.ensureConfigLoaded(); err != nil {
		return err
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
