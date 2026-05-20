package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CodexConfig manages Codex CLI configuration files.
type CodexConfig struct {
	configPath string
}

// NewCodexConfig creates a new Codex config manager.
func NewCodexConfig() (*CodexConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}
	return &CodexConfig{configPath: filepath.Join(home, ".codex")}, nil
}

// IsInstalled returns true if Codex CLI is detected.
func (cc *CodexConfig) IsInstalled() bool {
	_, err := os.Stat(filepath.Join(cc.configPath, "config.toml"))
	return err == nil
}

// Sync updates Codex config to match the given desktop config.
// It writes config.toml and models_catalog.json.
func (cc *CodexConfig) Sync(cfg *DesktopConfig) error {
	if !cc.IsInstalled() {
		return fmt.Errorf("Codex 未安装，未找到 %s", cc.configPath)
	}

	if err := cc.writeConfigTOML(cfg); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	if err := cc.writeModelsCatalog(cfg); err != nil {
		return fmt.Errorf("write models_catalog.json: %w", err)
	}
	return nil
}

// CodexConfigTOML represents the TOML structure we write.
// We write it manually since we don't have a TOML library.
func (cc *CodexConfig) writeConfigTOML(cfg *DesktopConfig) error {
	defaultModel := cfg.DefaultRoute
	if defaultModel == "moonbridge" && len(cfg.Routes) > 0 {
		// Use the first non-moonbridge route as the model name
		for _, r := range cfg.Routes {
			if r.Alias != "moonbridge" {
				defaultModel = r.Alias
				break
			}
		}
	}

	// Read existing config.toml and preserve non-model settings
	path := filepath.Join(cc.configPath, "config.toml")
	existing := cc.readExistingConfigTOML(path)

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("model = \"%s\"\n", defaultModel))
	sb.WriteString("model_provider = \"moonbridge\"\n")
	sb.WriteString("model_reasoning_effort = \"high\"\n")
	sb.WriteString("\n")
	sb.WriteString("[model_providers.moonbridge]\n")
	sb.WriteString("name = \"Moon Bridge\"\n")
	sb.WriteString(fmt.Sprintf("base_url = \"http://127.0.0.1:%d/v1\"\n", cfg.Port))
	sb.WriteString("wire_api = \"responses\"\n")

	// Append non-model sections from existing config
	if existing != "" {
		sb.WriteString("\n")
		sb.WriteString(existing)
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func (cc *CodexConfig) readExistingConfigTOML(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(data), "\n")
	var result []string
	inModelSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip model-related lines
		if strings.HasPrefix(trimmed, "model = ") ||
			strings.HasPrefix(trimmed, "model_provider = ") ||
			strings.HasPrefix(trimmed, "model_reasoning_effort = ") ||
			trimmed == "[model_providers.moonbridge]" ||
			strings.HasPrefix(trimmed, "name = ") && inModelSection ||
			strings.HasPrefix(trimmed, "base_url = ") ||
			strings.HasPrefix(trimmed, "wire_api = ") {
			inModelSection = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[model_providers.moonbridge]") {
			inModelSection = false
		}
		result = append(result, line)
	}

	// Clean up leading/trailing empty lines
	out := strings.Join(result, "\n")
	out = strings.Trim(out, "\n")
	return out
}

// CodexModelCatalog represents the models_catalog.json structure.
type codexModelCatalog struct {
	Models []codexModelEntry `json:"models"`
}

type codexModelEntry struct {
	Slug                   string   `json:"slug"`
	DisplayName            string   `json:"display_name"`
	Description            string   `json:"description"`
	DefaultReasoningLevel  string   `json:"default_reasoning_level"`
	SupportedReasoningLevels []reasoningLevel `json:"supported_reasoning_levels"`
	ShellType              string   `json:"shell_type"`
	Visibility             string   `json:"visibility"`
	SupportedInAPI         bool     `json:"supported_in_api"`
	Priority               int      `json:"priority"`
	AdditionalSpeedTiers   []any    `json:"additional_speed_tiers"`
	AvailabilityNux        any      `json:"availability_nux"`
	Upgrade                any      `json:"upgrade"`
	BaseInstructions       string   `json:"base_instructions"`
	SupportsReasoningSummaries bool  `json:"supports_reasoning_summaries"`
	DefaultReasoningSummary   string `json:"default_reasoning_summary"`
	SupportVerbosity          bool   `json:"support_verbosity"`
	DefaultVerbosity          any    `json:"default_verbosity"`
	ApplyPatchToolType        string `json:"apply_patch_tool_type"`
	WebSearchToolType         string `json:"web_search_tool_type"`
	TruncationPolicy          struct {
		Mode  string `json:"mode"`
		Limit int    `json:"limit"`
	} `json:"truncation_policy"`
	SupportsParallelToolCalls  bool `json:"supports_parallel_tool_calls"`
	SupportsImageDetailOriginal bool `json:"supports_image_detail_original"`
	ContextWindow              int  `json:"context_window"`
	MaxContextWindow           int  `json:"max_context_window"`
	EffectiveContextWindowPercent int `json:"effective_context_window_percent"`
	ExperimentalSupportedTools []any `json:"experimental_supported_tools"`
	InputModalities            []string `json:"input_modalities"`
	SupportsSearchTool         bool `json:"supports_search_tool"`
}

type reasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

func (cc *CodexConfig) writeModelsCatalog(cfg *DesktopConfig) error {
	path := filepath.Join(cc.configPath, "models_catalog.json")

	// Try to read existing catalog for base_instructions and other settings
	existingCatalog := cc.readExistingCatalog(path)

	var catalog codexModelCatalog

	for _, r := range cfg.Routes {
		// Skip the "moonbridge" alias - only include real model names
		if r.Alias == "moonbridge" {
			continue
		}

		model := cc.findModelBySlug(cfg.Models, r.Model)
		displayName := r.Alias
		contextWindow := 1000000
		if model != nil {
			if model.DisplayName != "" {
				displayName = model.DisplayName
			}
			if model.ContextWindow > 0 {
				contextWindow = model.ContextWindow
			}
		}

		// Use existing entry's base_instructions if available
		baseInstr := cc.defaultBaseInstructions(r.Alias)
		if existing, ok := existingCatalog[r.Alias]; ok {
			baseInstr = existing.BaseInstructions
		}

		entry := codexModelEntry{
			Slug:          r.Alias,
			DisplayName:   displayName,
			Description:   fmt.Sprintf("%s model via Moon Bridge", r.Alias),
			DefaultReasoningLevel: "high",
			SupportedReasoningLevels: []reasoningLevel{
				{Effort: "high", Description: "High reasoning effort"},
				{Effort: "xhigh", Description: "Extra high reasoning effort"},
			},
			ShellType:        "unified_exec",
			Visibility:       "list",
			SupportedInAPI:   true,
			Priority:         0,
			BaseInstructions: baseInstr,
			SupportsReasoningSummaries: true,
			DefaultReasoningSummary:    "auto",
			ApplyPatchToolType:         "freeform",
			WebSearchToolType:          "text",
			TruncationPolicy: struct {
				Mode  string `json:"mode"`
				Limit int    `json:"limit"`
			}{Mode: "tokens", Limit: 10000},
			SupportsParallelToolCalls:  true,
			SupportsImageDetailOriginal: false,
			ContextWindow:              contextWindow,
			MaxContextWindow:           contextWindow,
			EffectiveContextWindowPercent: 95,
			InputModalities:            []string{"text"},
			SupportsSearchTool:         false,
		}
		catalog.Models = append(catalog.Models, entry)
	}

	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal catalog: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func (cc *CodexConfig) readExistingCatalog(path string) map[string]codexModelEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var catalog codexModelCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil
	}
	result := make(map[string]codexModelEntry)
	for _, m := range catalog.Models {
		result[m.Slug] = m
	}
	return result
}

func (cc *CodexConfig) findModelBySlug(models []ModelConfig, slug string) *ModelConfig {
	for _, m := range models {
		if m.Slug == slug {
			return &m
		}
	}
	return nil
}

func (cc *CodexConfig) defaultBaseInstructions(slug string) string {
	return fmt.Sprintf("You are %s, a coding agent. You and the user share one workspace, and your job is to collaborate with them until their goal is genuinely handled.", slug)
}
