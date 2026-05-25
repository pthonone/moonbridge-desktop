package backend

// Pricing holds per-model pricing in RMB per M tokens.
type Pricing struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheWrite float64 `json:"cache_write"`
	CacheRead  float64 `json:"cache_read"`
	// BillingMode: "token" (per-token) or "per_request" (per-call, e.g. Coding Plan)
	BillingMode string `json:"billing_mode"`
	// PerRequestCost: cost per API call when BillingMode is "per_request"
	PerRequestCost float64 `json:"per_request_cost"`
}

// OfferConfig defines a model offering from a provider.
type OfferConfig struct {
	Model        string   `json:"model"`
	Pricing      Pricing  `json:"pricing"`
	PricingTier  string   `json:"pricing_tier"` // economy / standard / premium / ultra
	Series       string   `json:"series,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// ProviderConfig defines a model provider.
type ProviderConfig struct {
	Key       string        `json:"key"`
	BaseURL   string        `json:"base_url"`
	APIKey    string        `json:"api_key"`
	Protocol  string        `json:"protocol"` // anthropic | openai-response | google-genai | openai-chat
	Version   string        `json:"version"`
	Offers    []OfferConfig `json:"offers"`
}

// ModelConfig defines a model's capabilities.
type ModelConfig struct {
	Slug            string   `json:"slug"`
	DisplayName     string   `json:"display_name"`
	Description     string   `json:"description"`
	ContextWindow   int      `json:"context_window"`
	MaxOutputTokens int      `json:"max_output_tokens"`
	ReasoningLevel  string   `json:"reasoning_level"`
	Capabilities    []string `json:"capabilities"` // vision, reasoning, coding, long_context, web_search
	Series          string   `json:"series"`       // model series grouping (e.g. "qwen", "claude", "gpt")
	Extensions      map[string]bool `json:"extensions"` // extension name -> enabled
}

// RouteConfig maps an alias to a provider + model.
type RouteConfig struct {
	Alias    string `json:"alias"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
}

// DesktopConfig is the top-level desktop application config.
type DesktopConfig struct {
	Port           int              `json:"port"`
	InternalPort   int              `json:"internal_port"`
	LogLevel       string           `json:"log_level"`
	Providers      []ProviderConfig `json:"providers"`
	Models         []ModelConfig    `json:"models"`
	Routes         []RouteConfig    `json:"routes"`
	DefaultRoute   string           `json:"default_route"` // route alias
	MaxTokens      int              `json:"max_tokens"`
	MetricsEnabled bool             `json:"metrics_enabled"`
}

// UsageStats represents token usage statistics.
type UsageStats struct {
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	CacheRead       int     `json:"cache_read"`
	CacheWrite      int     `json:"cache_write"`
	TotalCost       float64 `json:"total_cost"`
	RequestCount    int     `json:"request_count"`
	CacheHitRate    float64 `json:"cache_hit_rate"`
}

// MBStatus represents Moon Bridge process status.
type MBStatus struct {
	Running      bool   `json:"running"`
	Port         int    `json:"port"`
	CurrentRoute string `json:"current_route"`
	Error        string `json:"error"`
}

// LogEntry represents a single log line from Moon Bridge.
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Raw       string `json:"raw"`
}

// ProviderPreset defines a ready-to-use provider template.
type ProviderPreset struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
	Protocol string `json:"protocol"`
	Version  string `json:"version"`
	Models   []OfferConfig `json:"models"`
}

// PresetProviderDeepseek is the DeepSeek preset.
func PresetProviderDeepseek() ProviderPreset {
	return ProviderPreset{
		Key:      "deepseek",
		Name:     "DeepSeek",
		BaseURL:  "https://api.deepseek.com/anthropic",
		Protocol: "anthropic",
		Version:  "2023-06-01",
		Models:   []OfferConfig{},
	}
}

// PresetProviderQwen is the Alibaba Qwen preset.
func PresetProviderQwen() ProviderPreset {
	return ProviderPreset{
		Key:      "qwen",
		Name:     "Qwen (阿里通义)",
		BaseURL:  "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Protocol: "openai-chat",
		Version:  "",
		Models:   []OfferConfig{},
	}
}

// PresetProviderAnthropic is the Anthropic preset.
func PresetProviderAnthropic() ProviderPreset {
	return ProviderPreset{
		Key:      "anthropic",
		Name:     "Anthropic",
		BaseURL:  "https://api.anthropic.com",
		Protocol: "anthropic",
		Version:  "2023-06-01",
		Models:   []OfferConfig{},
	}
}

// PresetProviderOpenAI is the OpenAI preset.
func PresetProviderOpenAI() ProviderPreset {
	return ProviderPreset{
		Key:      "openai",
		Name:     "OpenAI",
		BaseURL:  "https://api.openai.com/v1",
		Protocol: "openai-chat",
		Version:  "",
		Models:   []OfferConfig{},
	}
}

// PresetProviderGoogle is the Google preset.
func PresetProviderGoogle() ProviderPreset {
	return ProviderPreset{
		Key:      "google",
		Name:     "Google",
		BaseURL:  "https://generativelanguage.googleapis.com/v1beta/openai",
		Protocol: "openai-chat",
		Version:  "",
		Models:   []OfferConfig{},
	}
}

// PresetProviderOpenRouter is the OpenRouter preset.
func PresetProviderOpenRouter() ProviderPreset {
	return ProviderPreset{
		Key:      "openrouter",
		Name:     "OpenRouter",
		BaseURL:  "https://openrouter.ai/api/v1",
		Protocol: "openai-chat",
		Version:  "",
		Models:   []OfferConfig{},
	}
}

// PresetProviderOllama is the Ollama preset (local, free).
func PresetProviderOllama() ProviderPreset {
	return ProviderPreset{
		Key:      "ollama",
		Name:     "Ollama (本地)",
		BaseURL:  "http://localhost:11434/v1",
		Protocol: "openai-chat",
		Version:  "",
		Models:   []OfferConfig{},
	}
}

// PresetProviderSiliconFlow is the SiliconFlow (硅基流动) preset.
func PresetProviderSiliconFlow() ProviderPreset {
	return ProviderPreset{
		Key:      "siliconflow",
		Name:     "硅基流动",
		BaseURL:  "https://api.siliconflow.cn/v1",
		Protocol: "openai-chat",
		Version:  "",
		Models:   []OfferConfig{},
	}
}
