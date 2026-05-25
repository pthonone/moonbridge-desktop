package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// debugLogFile is a file-based logger for proxy debugging.
var debugLogFile *log.Logger
var debugLogInit sync.Once

func initDebugLog() {
	debugLogInit.Do(func() {
		logPath := filepath.Join(os.TempDir(), "moonbridge-proxy-debug.log")
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return
		}
		debugLogFile = log.New(f, "", 0)
	})
}

func debugLog(format string, args ...interface{}) {
	initDebugLog()
	if debugLogFile != nil {
		debugLogFile.Printf(format, args...)
	}
}

// RecordUsageFunc is called when usage data is extracted from a response.
type RecordUsageFunc func(model string, inputTokens, outputTokens, cacheRead, cacheWrite int)

// RecordRequestFunc is called for every successful request (for per-request billing).
type RecordRequestFunc func(model string)

// TransparentProxy is an HTTP reverse proxy that resolves model aliases to
// upstream providers, forwards requests directly, and retries on 502 errors.
type TransparentProxy struct {
	mu              sync.Mutex
	listenPort      int
	maxRetries      int
	server          *http.Server
	running         bool
	recordUsage     RecordUsageFunc
	recordRequest   RecordRequestFunc
	resolveRoute    func(alias string) (*ProviderConfig, string, error)
	getCurrentModel func() string
}

// NewTransparentProxy creates a transparent proxy listening on listenPort.
func NewTransparentProxy(listenPort int, maxRetries int) *TransparentProxy {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &TransparentProxy{
		listenPort: listenPort,
		maxRetries: maxRetries,
	}
}

// SetRecordUsage sets the callback for recording usage data.
func (tp *TransparentProxy) SetRecordUsage(fn RecordUsageFunc) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	tp.recordUsage = fn
}

// SetRecordRequest sets the callback for recording per-request billing.
func (tp *TransparentProxy) SetRecordRequest(fn RecordRequestFunc) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	tp.recordRequest = fn
}

// SetResolveRoute sets the function that resolves a model alias to a provider and actual model slug.
func (tp *TransparentProxy) SetResolveRoute(fn func(alias string) (*ProviderConfig, string, error)) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	tp.resolveRoute = fn
}

// SetGetCurrentModel sets the function that returns the user's currently selected model alias.
func (tp *TransparentProxy) SetGetCurrentModel(fn func() string) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	tp.getCurrentModel = fn
}

// Start begins listening for HTTP requests.
func (tp *TransparentProxy) Start() error {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if tp.running {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", tp.handleRequest)

	tp.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", tp.listenPort),
		Handler: mux,
	}

	tp.running = true

	go func() {
		if err := tp.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			AppLogger.Printf("[TransparentProxy] server error: %v", err)
		}
		tp.mu.Lock()
		tp.running = false
		tp.mu.Unlock()
	}()

	return nil
}

// Stop shuts down the transparent proxy.
func (tp *TransparentProxy) Stop() error {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if !tp.running || tp.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return tp.server.Shutdown(ctx)
}

// IsRunning returns whether the proxy is currently listening.
func (tp *TransparentProxy) IsRunning() bool {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	return tp.running
}

// upstreamPathMapping maps incoming request paths to upstream provider paths
// based on the provider's protocol. Returns the path WITHOUT /v1 prefix
// when base_url already contains it (matching cc-switch behavior).
func upstreamPathMapping(incomingPath, protocol string) string {
	switch {
	case incomingPath == "/v1/responses" && protocol == "anthropic":
		return "/messages"
	case incomingPath == "/v1/responses" && protocol == "openai-chat":
		return "/chat/completions"
	case incomingPath == "/v1/responses" && protocol == "openai-response":
		return "/responses"
	case strings.HasPrefix(incomingPath, "/v1/responses/") && protocol == "anthropic":
		return "/messages" + strings.TrimPrefix(incomingPath, "/v1/responses")
	case strings.HasPrefix(incomingPath, "/v1/responses/") && protocol == "openai-chat":
		return "/chat/completions"
	case strings.HasPrefix(incomingPath, "/v1/responses/") && protocol == "openai-response":
		return incomingPath
	default:
		return incomingPath
	}
}

// buildUpstreamURL constructs the upstream URL, deduplicating /v1 prefixes.
// Matching cc-switch's adapter.build_url() logic.
func buildUpstreamURL(baseURL, upstreamPath string) string {
	// Case 1: base_url ends with /v1
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + upstreamPath
	}
	// Case 2: base_url ends with /v1/
	if strings.HasSuffix(baseURL, "/v1/") {
		return baseURL + strings.TrimPrefix(upstreamPath, "/")
	}
	// Case 3: base_url has no path (pure origin)
	if strings.HasSuffix(baseURL, "/") && !strings.Contains(strings.TrimSuffix(baseURL, "/"), "/") {
		return baseURL + "v1" + upstreamPath
	}
	// Case 4: base_url has a custom prefix - direct concatenation
	result := baseURL + upstreamPath
	// Deduplicate /v1/v1 → /v1
	for strings.Contains(result, "/v1/v1") {
		result = strings.Replace(result, "/v1/v1", "/v1", -1)
	}
	return result
}

// setProviderAuthHeaders sets the appropriate auth headers based on provider protocol.
func setProviderAuthHeaders(req *http.Request, provider *ProviderConfig) {
	switch provider.Protocol {
	case "anthropic":
		req.Header.Set("x-api-key", provider.APIKey)
		if provider.Version != "" {
			req.Header.Set("anthropic-version", provider.Version)
		}
		req.Header.Set("content-type", "application/json")
	case "openai-chat", "openai-response", "google-genai":
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	default:
		// Default to OpenAI-style Bearer auth
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
}

// stripClientAuthHeaders removes client-provided auth headers so the proxy
// can replace them with the provider's own credentials.
func stripClientAuthHeaders(headers http.Header) {
	for _, key := range []string{"Authorization", "authorization", "X-Api-Key", "x-api-key", "Anthropic-Version", "anthropic-version", "X-Goog-Api-Key", "x-goog-api-key"} {
		headers.Del(key)
	}
}

// Anthropic-style usage (non-streaming)
type responseUsage struct {
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		InputTokensDetails       struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// OpenAI-style usage (non-streaming, fallback)
type responseUsageOpenAI struct {
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// OpenAI Responses API usage (non-streaming)
type responseUsageResponses struct {
	Model string `json:"model"`
	Usage struct {
		InputTokens        int `json:"input_tokens"`
		OutputTokens       int `json:"output_tokens"`
		TotalTokens        int `json:"total_tokens"`
		InputTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		OutputTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
}

// responseModel extracts the actual model from upstream responses.
type responseModel struct {
	Model string `json:"model"`
}

// SSEUsageExtractor parses SSE stream events to extract usage data.
type SSEUsageExtractor struct {
	inputTokens  int
	outputTokens int
	cacheRead    int
	cacheWrite   int
	model        string
}

func (e *SSEUsageExtractor) recordUsage(tp *TransparentProxy) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	model := e.model
	if model == "" {
		return
	}
	if tp.recordUsage != nil && (e.inputTokens > 0 || e.outputTokens > 0) {
		tp.recordUsage(model, e.inputTokens, e.outputTokens, e.cacheRead, e.cacheWrite)
	}
}

// recordPerRequest records a per-request billing event.
func (e *SSEUsageExtractor) recordPerRequest(tp *TransparentProxy) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	model := e.model
	if model == "" {
		return
	}
	if tp.recordRequest != nil {
		tp.recordRequest(model)
	}
}

// parseAnthropicSSE parses Anthropic-style SSE events for usage data.
func (e *SSEUsageExtractor) parseAnthropicSSE(data string) {
	if strings.Contains(data, `"message_start"`) || strings.Contains(data, `"type":"message_start"`) {
		var msg struct {
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &msg); err == nil && msg.Message.Model != "" {
			e.model = msg.Message.Model
		}
	}

	isDelta := strings.Contains(data, `"message_delta"`) || strings.Contains(data, `"type":"message_delta"`)
	isStop := strings.Contains(data, `"message_stop"`) || strings.Contains(data, `"type":"message_stop"`)
	if !isDelta && !isStop {
		return
	}
	var msg struct {
		Type string `json:"type"`
		Delta struct {
			Usage *struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"delta"`
		Usage *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &msg); err == nil {
		if u := msg.Usage; u != nil {
			e.inputTokens = u.InputTokens
			e.outputTokens = u.OutputTokens
			e.cacheWrite = u.CacheCreationInputTokens
			e.cacheRead = u.CacheReadInputTokens
		} else if u := msg.Delta.Usage; u != nil {
			e.inputTokens = u.InputTokens
			e.outputTokens = u.OutputTokens
			e.cacheWrite = u.CacheCreationInputTokens
			e.cacheRead = u.CacheReadInputTokens
		}
	}
}

// parseOpenAIResponsesSSE parses OpenAI Responses API SSE events for usage data.
func (e *SSEUsageExtractor) parseOpenAIResponsesSSE(data string) {
	if !strings.Contains(data, `"response.completed"`) && !strings.Contains(data, `"type":"response.completed"`) {
		return
	}
	var msg struct {
		Response struct {
			Model string `json:"model"`
			Usage *struct {
				InputTokens           int `json:"input_tokens"`
				OutputTokens          int `json:"output_tokens"`
				TotalTokens           int `json:"total_tokens"`
				InputTokensDetails    struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"input_tokens_details"`
				OutputTokensDetails struct {
					ReasoningTokens int `json:"reasoning_tokens"`
				} `json:"output_tokens_details"`
			} `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(data), &msg); err == nil && msg.Response.Usage != nil {
		if msg.Response.Model != "" {
			e.model = msg.Response.Model
		}
		e.inputTokens = msg.Response.Usage.InputTokens
		e.outputTokens = msg.Response.Usage.OutputTokens
		e.cacheRead = msg.Response.Usage.InputTokensDetails.CachedTokens
	}
}

// parseOpenAISSE parses OpenAI-style SSE events for usage data.
func (e *SSEUsageExtractor) parseOpenAISSE(data string) {
	if data == "[DONE]" {
		return
	}
	var chunk struct {
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &chunk); err == nil && chunk.Usage != nil {
		e.inputTokens = chunk.Usage.PromptTokens
		e.outputTokens = chunk.Usage.CompletionTokens
		e.cacheRead = chunk.Usage.PromptTokensDetails.CachedTokens
	}
}

// sanitizeTools removes tools with empty names from the request body.
func sanitizeTools(originalBody []byte, toolsRaw json.RawMessage) []byte {
	var tools []map[string]any
	if err := json.Unmarshal(toolsRaw, &tools); err != nil {
		return originalBody
	}

	filtered := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		name := getToolName(t)
		if strings.TrimSpace(name) == "" {
			continue
		}
		filtered = append(filtered, t)
	}

	if len(filtered) == len(tools) {
		return originalBody
	}

	var body map[string]any
	if err := json.Unmarshal(originalBody, &body); err != nil {
		return originalBody
	}
	body["tools"] = filtered
	result, err := json.Marshal(body)
	if err != nil {
		return originalBody
	}
	return result
}

// getToolName extracts the name from a tool object.
func getToolName(t map[string]any) string {
	if fn, ok := t["function"].(map[string]any); ok {
		if name, _ := fn["name"].(string); name != "" {
			return name
		}
	}
	if name, ok := t["name"].(string); ok {
		return name
	}
	return ""
}

// convertPromptToMessages transforms a Codex-style request body
// (with "prompt" field) into the standard messages format.
// Returns the rewritten body if conversion happened, original otherwise.
func convertPromptToMessages(bodyBytes []byte) []byte {
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return bodyBytes
	}

	_, hasPrompt := body["prompt"]
	_, hasMessages := body["messages"]
	if !hasPrompt || hasMessages {
		return bodyBytes
	}

	promptVal := body["prompt"]
	if promptVal == nil {
		return bodyBytes
	}

	delete(body, "prompt")
	body["messages"] = []map[string]any{
		{"role": "user", "content": promptVal},
	}

	if rewritten, err := json.Marshal(body); err == nil {
		return rewritten
	}
	return bodyBytes
}

// convertOpenAItoAnthropic is kept for backward compatibility but deprecated.
// All protocol conversion now goes through convertOpenAItoChatCompletions.
func convertOpenAItoAnthropic(bodyBytes []byte) []byte {
	return convertOpenAItoChatCompletions(bodyBytes)
}

// convertOpenAItoChatCompletions adapts an OpenAI Responses API request body to
// OpenAI Chat Completions format for upstream providers that use openai-chat protocol.
func convertOpenAItoChatCompletions(bodyBytes []byte) []byte {
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return bodyBytes
	}

	// instructions → system message
	var messages []any
	if instructions, ok := body["instructions"].(string); ok && instructions != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": instructions,
		})
	}
	delete(body, "instructions")

	// input → messages
	if input, ok := body["input"]; ok {
		delete(body, "input")
		for _, msg := range responsesInputToMessagesForChat(input) {
			messages = append(messages, msg)
		}
	}

	if len(messages) > 0 {
		body["messages"] = messages
	}

	// Always enable streaming when converting to Chat Completions format,
	// since Codex expects SSE and we convert it back to Responses API SSE.
	body["stream"] = true

	// tools: Responses flat format → Chat Completions nested format
	// Responses: {"type": "function", "name": "...", "parameters": ...}
	// Chat Completions: {"type": "function", "function": {"name": "...", "parameters": ...}}
	if tools, ok := body["tools"].([]any); ok {
		converted := make([]any, 0, len(tools))
		for _, t := range tools {
			m, ok := t.(map[string]any)
			if !ok {
				continue
			}
			if m["type"] != "function" {
				continue
			}
			fnDef := make(map[string]any)
			if name, ok := m["name"].(string); ok {
				fnDef["name"] = name
			}
			if desc, ok := m["description"].(string); ok {
				fnDef["description"] = desc
			}
			if params, ok := m["parameters"]; ok {
				fnDef["parameters"] = params
			}
			converted = append(converted, map[string]any{
				"type":     "function",
				"function": fnDef,
			})
		}
		if len(converted) > 0 {
			body["tools"] = converted
		} else {
			delete(body, "tools")
			delete(body, "tool_choice")
		}
	}

	// tool_choice mapping
	if tc, ok := body["tool_choice"]; ok {
		body["tool_choice"] = mapToolChoiceToChatCompletions(tc)
	}

	// Remove Responses API specific fields
	for _, key := range []string{"include", "stream_options", "user", "reasoning", "truncation", "store", "service_tier", "prompt_cache_key", "max_tokens"} {
		delete(body, key)
	}
	delete(body, "parallel_tool_calls")

	// max_output_tokens → max_tokens
	if v, ok := body["max_output_tokens"]; ok {
		delete(body, "max_output_tokens")
		body["max_tokens"] = v
	}

	if rewritten, err := json.Marshal(body); err == nil {
		return rewritten
	}
	return bodyBytes
}

// mapToolChoiceToChatCompletions converts OpenAI Responses tool_choice to Chat Completions format.
func mapToolChoiceToChatCompletions(tc any) any {
	switch v := tc.(type) {
	case string:
		switch v {
		case "required", "auto", "none":
			return v
		default:
			return "auto"
		}
	case map[string]any:
		typ, _ := v["type"].(string)
		switch typ {
		case "function":
			if name, ok := v["name"].(string); ok {
				return map[string]any{"type": "function", "function": map[string]any{"name": name}}
			}
			return "required"
		case "required", "auto", "none":
			return v["type"]
		default:
			return "auto"
		}
	}
	return "auto"
}

// responsesInputToMessagesForChat converts OpenAI Responses API input to
// Chat Completions messages format (simple string content, proper roles).
func responsesInputToMessagesForChat(input any) []map[string]any {
	items, ok := input.([]any)
	if !ok {
		if s, ok := input.(string); ok {
			return []map[string]any{{"role": "user", "content": s}}
		}
		return nil
	}

	var messages []map[string]any
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := m["type"].(string)

		switch itemType {
		case "message", "":
			// Map role: developer → system, other roles as-is
			role, _ := m["role"].(string)
			if role == "" {
				role = "user"
			}
			if role == "developer" {
				role = "system"
			}

			// Flatten content blocks to simple string
			if contentArr, ok := m["content"].([]any); ok {
				var textParts []string
				for _, c := range contentArr {
					cb, ok := c.(map[string]any)
					if !ok {
						continue
					}
					if t, ok := cb["type"].(string); ok {
						if t == "input_text" || t == "output_text" || t == "text" {
							if txt, ok := cb["text"].(string); ok {
								textParts = append(textParts, txt)
							}
						}
					}
				}
				if len(textParts) > 0 {
					messages = append(messages, map[string]any{
						"role":    role,
						"content": strings.Join(textParts, "\n"),
					})
				}
			} else if s, ok := m["content"].(string); ok {
				messages = append(messages, map[string]any{
					"role":    role,
					"content": s,
				})
			}

		case "function_call":
			// Convert to assistant message with tool_calls format
			callID, _ := m["call_id"].(string)
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			args := "{}"
			if argsStr, ok := m["arguments"].(string); ok {
				args = argsStr
			}
			messages = append(messages, map[string]any{
				"role": "assistant",
				"tool_calls": []map[string]any{
					{
						"id":   callID,
						"type": "function",
						"function": map[string]any{
							"name":      name,
							"arguments": args,
						},
					},
				},
			})

		case "function_call_output":
			// Convert to tool message with tool_call_id
			callID, _ := m["call_id"].(string)
			output := ""
			if s, ok := m["output"].(string); ok {
				output = s
			}
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": callID,
				"content":      output,
			})
		}
	}
	return messages
}

// responsesInputToMessages converts OpenAI Responses API input array to Anthropic messages.
func responsesInputToMessages(input any) []map[string]any {
	items, ok := input.([]any)
	if !ok {
		if s, ok := input.(string); ok {
			return []map[string]any{{"role": "user", "content": s}}
		}
		return nil
	}

	var messages []map[string]any
	var currentRole string
	var currentContent []any

	flush := func() {
		if currentRole != "" && len(currentContent) > 0 {
			messages = append(messages, map[string]any{
				"role":    currentRole,
				"content": currentContent,
			})
		}
		currentRole = ""
		currentContent = nil
	}

	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := m["type"].(string)

		switch itemType {
		case "message":
			// message item: {"type": "message", "role": "user"/"assistant", "content": [...]}
			role, _ := m["role"].(string)
			if role == "" {
				role = "user"
			}
			// Flush if role changed
			if currentRole != "" && role != currentRole {
				flush()
			}
			currentRole = role

			// Convert content blocks
			if contentArr, ok := m["content"].([]any); ok {
				for _, c := range contentArr {
					cb := responsesContentToAnthropic(c)
					if cb != nil {
						currentContent = append(currentContent, cb)
					}
				}
			}

		case "function_call":
			// Elevated function call → tool_use block in assistant message
			if currentRole != "" && currentRole != "assistant" {
				flush()
			}
			currentRole = "assistant"

			callID, _ := m["call_id"].(string)
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			toolUse := map[string]any{
				"type": "tool_use",
				"id":   callID,
				"name": name,
			}
			if argsStr, ok := m["arguments"].(string); ok {
				var args any
				if err := json.Unmarshal([]byte(argsStr), &args); err == nil {
					toolUse["input"] = args
				} else {
					toolUse["input"] = map[string]any{}
				}
			} else if args, ok := m["arguments"]; ok {
				toolUse["input"] = args
			} else {
				toolUse["input"] = map[string]any{}
			}
			currentContent = append(currentContent, toolUse)

		case "function_call_output":
			// Function output → tool_result block in user message
			if currentRole != "" && currentRole != "user" {
				flush()
			}
			currentRole = "user"

			callID, _ := m["call_id"].(string)
			output := ""
			if s, ok := m["output"].(string); ok {
				output = s
			}
			currentContent = append(currentContent, map[string]any{
				"type":        "tool_result",
				"tool_use_id": callID,
				"content": []any{
					map[string]any{"type": "text", "text": output},
				},
			})

		default:
			// Unknown type — flush and skip
			flush()
		}
	}

	flush()
	return messages
}

// responsesContentToAnthropic converts a Responses API content block to Anthropic format.
func responsesContentToAnthropic(c any) map[string]any {
	m, ok := c.(map[string]any)
	if !ok {
		return nil
	}
	t, _ := m["type"].(string)
	switch t {
	case "input_text", "output_text", "text":
		if text, ok := m["text"].(string); ok {
			return map[string]any{"type": "text", "text": text}
		}
	case "input_image", "image":
		if url, ok := m["image_url"].(string); ok {
			return map[string]any{
				"type": "image",
				"source": map[string]any{
					"type": "url",
					"url":  url,
				},
			}
		}
	case "refusal":
		if text, ok := m["refusal"].(string); ok {
			return map[string]any{"type": "text", "text": text}
		}
	}
	return nil
}

// mapToolChoiceToAnthropic converts OpenAI Responses tool_choice to Anthropic format.
func mapToolChoiceToAnthropic(tc any) any {
	switch v := tc.(type) {
	case string:
		switch v {
		case "required":
			return map[string]any{"type": "any"}
		case "auto", "none":
			return map[string]any{"type": v}
		default:
			return map[string]any{"type": "auto"}
		}
	case map[string]any:
		typ, _ := v["type"].(string)
		switch typ {
		case "function":
			if name, ok := v["name"].(string); ok {
				return map[string]any{"type": "tool", "name": name}
			}
			return map[string]any{"type": "any"}
		case "required":
			return map[string]any{"type": "any"}
		case "auto", "none":
			return map[string]any{"type": v["type"]}
		default:
			return map[string]any{"type": "auto"}
		}
	}
	return map[string]any{"type": "auto"}
}

// convertChatCompletionsToResponses transforms a Chat Completions response
// to OpenAI Responses API format for Codex compatibility.
func convertChatCompletionsToResponses(bodyBytes []byte) []byte {
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return bodyBytes
	}
	if _, ok := body["output"]; ok {
		return bodyBytes
	}

	result := make(map[string]any)
	if id, ok := body["id"].(string); ok && id != "" {
		result["id"] = id
	}
	if model, ok := body["model"].(string); ok {
		result["model"] = model
	}

	// Always initialize outputItems as empty slice, not nil
	outputItems := make([]any, 0)
	if choices, ok := body["choices"].([]any); ok && len(choices) > 0 {
		for _, c := range choices {
			choice, ok := c.(map[string]any)
			if !ok {
				continue
			}
			message, ok := choice["message"].(map[string]any)
			if !ok {
				continue
			}

			// Content might be empty string (reasoning models put text in reasoning_content)
			contentVal := message["content"]
			content := ""
			if c, ok := contentVal.(string); ok {
				content = c
			}

			// If content is empty, check reasoning_content (DeepSeek-specific)
			if content == "" {
				if rc, ok := message["reasoning_content"].(string); ok && rc != "" {
					content = rc
				}
			}

			if content != "" {
				outputItems = append(outputItems, map[string]any{
					"type":   "message",
					"role":   "assistant",
					"status": "completed",
					"content": []any{
						map[string]any{"type": "output_text", "text": content},
					},
				})
			} else {
				// Truly no content - create empty message
				outputItems = append(outputItems, map[string]any{
					"type":   "message",
					"role":   "assistant",
					"status": "completed",
					"content": []any{},
				})
			}

			if toolCalls, ok := message["tool_calls"].([]any); ok {
				for _, tc := range toolCalls {
					tool, ok := tc.(map[string]any)
					if !ok {
						continue
					}
					fn, ok := tool["function"].(map[string]any)
					if !ok {
						continue
					}
					callID, _ := tool["id"].(string)
					name, _ := fn["name"].(string)
					args, _ := fn["arguments"].(string)
					outputItems = append(outputItems, map[string]any{
						"type":      "function_call",
						"call_id":   callID,
						"name":      name,
						"arguments": args,
						"status":    "completed",
					})
				}
			}
		}
	}
	result["output"] = outputItems

	stopReason, _ := body["stop_reason"].(string)
	if finishReason, ok := body["finish_reason"].(string); ok && stopReason == "" {
		stopReason = finishReason
	}
	switch stopReason {
	case "stop", "tool_calls":
		result["status"] = "completed"
	case "length":
		result["status"] = "incomplete"
	default:
		result["status"] = "completed"
	}

	if usage, ok := body["usage"].(map[string]any); ok {
		responsesUsage := make(map[string]any)
		if v, ok := usage["prompt_tokens"]; ok {
			responsesUsage["input_tokens"] = v
		}
		if v, ok := usage["completion_tokens"]; ok {
			responsesUsage["output_tokens"] = v
		}
		if v, ok := usage["total_tokens"]; ok {
			responsesUsage["total_tokens"] = v
		}
		if details, ok := usage["prompt_tokens_details"].(map[string]any); ok {
			if v, ok := details["cached_tokens"]; ok {
				responsesUsage["input_tokens_details"] = map[string]any{"cached_tokens": v}
			}
		}
		result["usage"] = responsesUsage
	} else {
		result["usage"] = map[string]any{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}
	}

	if rewritten, err := json.Marshal(result); err == nil {
		return rewritten
	}
	return bodyBytes
}

func (tp *TransparentProxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	debugLog("=== REQUEST %s %s ===", r.Method, r.URL.Path)

	// Buffer the request body so we can retry
	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			debugLog("ERROR: failed to read request body: %v", err)
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
	}
	debugLog("Request body (len=%d): %s", len(bodyBytes), string(bodyBytes))

	// Convert Codex-style "prompt" to "messages" format
	if len(bodyBytes) > 0 {
		bodyBytes = convertPromptToMessages(bodyBytes)
	}

	// Extract model from request body and sanitize tools
	var modelName string
	if len(bodyBytes) > 0 {
		var reqBody struct {
			Model string          `json:"model"`
			Tools json.RawMessage `json:"tools"`
		}
		if err := json.Unmarshal(bodyBytes, &reqBody); err == nil {
			if reqBody.Model != "" {
				modelName = reqBody.Model
			}
			if len(reqBody.Tools) > 0 {
				bodyBytes = sanitizeTools(bodyBytes, reqBody.Tools)
			}
		}
	}

	// Rewrite request body model to user's currently selected model.
	// This ensures the upstream provider uses the correct model even if the client has stale config.
	tp.mu.Lock()
	currentModel := tp.getCurrentModel
	tp.mu.Unlock()
	if currentModel != nil {
		selected := currentModel()
		if selected != "" && modelName != selected {
			AppLogger.Printf("[ModelRewrite] request model=%s, rewriting to selected=%s", modelName, selected)
			var body map[string]any
			if err := json.Unmarshal(bodyBytes, &body); err == nil {
				body["model"] = selected
				body["max_tokens"] = 1000000
				if rewritten, err := json.Marshal(body); err == nil {
					bodyBytes = rewritten
					modelName = selected
				}
			}
		}
	}

	// Resolve the model alias to a provider and actual model slug
	tp.mu.Lock()
	resolveRoute := tp.resolveRoute
	tp.mu.Unlock()

	if resolveRoute == nil {
		http.Error(w, "route resolver not configured", http.StatusInternalServerError)
		return
	}

	provider, actualModel, err := resolveRoute(modelName)
	if err != nil {
		http.Error(w, fmt.Sprintf("model not found: %s", modelName), http.StatusNotFound)
		return
	}

	// Map request path to upstream provider path based on protocol
	upstreamPath := upstreamPathMapping(r.URL.Path, provider.Protocol)
	upstreamURL := buildUpstreamURL(provider.BaseURL, upstreamPath)
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	// Update request body model to the actual model slug (not the alias)
	// and set max_tokens to 1M for all requests.
	if len(bodyBytes) > 0 {
		var body map[string]any
		if err := json.Unmarshal(bodyBytes, &body); err == nil {
			if actualModel != modelName {
				body["model"] = actualModel
			}
			body["max_tokens"] = 1000000
			if rewritten, err := json.Marshal(body); err == nil {
				bodyBytes = rewritten
			}
		}
	}

	// Convert OpenAI Responses API format to Chat Completions format for openai-chat providers.
	if len(bodyBytes) > 0 && provider.Protocol == "openai-chat" {
		bodyBytes = convertOpenAItoChatCompletions(bodyBytes)
	}

	AppLogger.Printf("[Proxy] -> %s %s (provider=%s, model=%s, protocol=%s)", r.Method, upstreamURL, provider.Key, actualModel, provider.Protocol)
	debugLog("Upstream: %s %s (provider=%s, model=%s, protocol=%s)", r.Method, upstreamURL, provider.Key, actualModel, provider.Protocol)

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; attempt <= tp.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(500*(1<<uint(attempt-1))) * time.Millisecond
			time.Sleep(backoff)
		}

		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bodyReader)
		if err != nil {
			lastErr = err
			continue
		}

		// Copy client headers but strip auth headers
		for key, values := range r.Header {
			lowerKey := strings.ToLower(key)
			if lowerKey == "authorization" || lowerKey == "x-api-key" || lowerKey == "anthropic-version" || lowerKey == "x-goog-api-key" {
				continue
			}
			for _, val := range values {
				req.Header.Add(key, val)
			}
		}

		// Set provider auth headers
		setProviderAuthHeaders(req, provider)

		client := &http.Client{Timeout: 0}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusBadGateway && attempt < tp.maxRetries {
			resp.Body.Close()
			continue
		}

		lastResp = resp
		lastErr = nil
		break
	}

	if lastErr != nil && lastResp == nil {
		debugLog("Upstream error: %v", lastErr)
		AppLogger.Printf("[Proxy] <- upstream error: %v", lastErr)
		http.Error(w, fmt.Sprintf("upstream error: %v", lastErr), http.StatusBadGateway)
		return
	}

	debugLog("Upstream response: status=%d", lastResp.StatusCode)

	// Forward response headers
	for key, values := range lastResp.Header {
		for _, val := range values {
			w.Header().Add(key, val)
		}
	}

	// Log non-SSE responses for debugging
	ct := lastResp.Header.Get("Content-Type")
	if lastResp.StatusCode != 200 || !strings.Contains(ct, "text/event-stream") {
		respBody, _ := io.ReadAll(lastResp.Body)
		lastResp.Body = io.NopCloser(bytes.NewReader(respBody))
		AppLogger.Printf("[Proxy] <- status=%d contentType=%s bodyLen=%d", lastResp.StatusCode, ct, len(respBody))
		debugLog("Non-SSE response: status=%d, ct=%s, bodyLen=%d, body=%s", lastResp.StatusCode, ct, len(respBody), string(respBody))
		if lastResp.StatusCode != 200 || len(respBody) < 2000 {
			AppLogger.Printf("[Proxy] <- body: %s", string(respBody))
		}
	}

	// Handle SSE streaming responses
	if lastResp.StatusCode == 200 && strings.Contains(ct, "text/event-stream") {
		AppLogger.Printf("[Proxy] <- SSE streaming, status=%d", lastResp.StatusCode)

		// Set explicit SSE headers for Codex compatibility
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(200)

		extractor := &SSEUsageExtractor{model: actualModel}
		reader := bufio.NewReader(lastResp.Body)
		// No event buffering needed - each data: line processed immediately
		var lastCompletedEvent string

		// Determine if we need to convert Chat Completions SSE → OpenAI Responses SSE
		convertSSE := provider.Protocol == "openai-chat"

		// SSE conversion state — matches cc-switch ChatToResponsesState
		var sseMessageID, sseModel string
		var sseCreatedAt int64
		var sseHasEmittedCreated, sseHasCompleted bool
		var sseHasEmittedText, sseHasEmittedReasoning bool
		var sseTextContent, sseReasoningContent strings.Builder
		var sseReasoningOutputIndex, sseTextOutputIndex int // stored output_index for finalize consistency
		// Tool call state - matches cc-switch BTreeMap<usize, ToolCallState>
		type sseToolState struct {
			callID       string
			name         string
			args         strings.Builder
			added        bool
			done         bool
			outputIndex  int
			itemID       string
		}
		sseTools := make(map[int]*sseToolState)
				sseOutputIndex := 0    // increments for each output item
		sseCurrentItemID := "" // item_id for current output item

		writeSSEEvent := func(eventType string, data any) {
			// cc-switch adds "type" field inside the data payload matching the event name
			if m, ok := data.(map[string]any); ok {
				m["type"] = eventType
			}
			jsonBytes, _ := json.Marshal(data)
			line := fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(jsonBytes))
			_, _ = w.Write([]byte(line))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}

		buildResponseObj := func(status string, output []any) map[string]any {
			createdAt := sseCreatedAt
			if createdAt == 0 {
				createdAt = time.Now().Unix()
			}
			return map[string]any{
				"id":         sseMessageID,
				"object":     "response",
				"created_at": createdAt,
				"status":     status,
				"model":      sseModel,
				"output":     output,
				"usage":      nil,
			}
		}

		// emitResponseStarted: cc-switch emits both response.created AND response.in_progress
		emitResponseStarted := func() {
			if sseHasEmittedCreated {
				return
			}
			respObj := buildResponseObj("in_progress", []any{})
			respObj["usage"] = map[string]any{
				"input_tokens":  0,
				"output_tokens": 0,
				"total_tokens":  0,
			}
			writeSSEEvent("response.created", map[string]any{"response": respObj})
			writeSSEEvent("response.in_progress", map[string]any{"response": respObj})
			sseHasEmittedCreated = true
		}

		// sseFinalize: called unconditionally at stream end (cc-switch finalize pattern)
		sseFinalize := func() {
			if sseHasCompleted {
				return
			}
			sseHasCompleted = true

			// 1. Finalize tool calls (cc-switch finalizeTools)
			for tcIdx, ts := range sseTools {
				if ts.added && !ts.done {
					if ts.callID == "" {
						ts.callID = fmt.Sprintf("call_%d", tcIdx)
					}
					if ts.name == "" {
						ts.name = "unknown_tool"
					}
					if ts.itemID == "" {
						ts.itemID = fmt.Sprintf("fc_%s", ts.callID)
					}
					ts.outputIndex = sseOutputIndex
					sseOutputIndex++
					ts.done = true
					sseTools[tcIdx] = ts

					writeSSEEvent("response.function_call_arguments.done", map[string]any{
						"output_index": ts.outputIndex,
						"item_id":      ts.itemID,
						"arguments":    ts.args.String(),
					})
					writeSSEEvent("response.output_item.done", map[string]any{
						"output_index": ts.outputIndex,
						"item": map[string]any{
							"id":        ts.itemID,
							"type":      "function_call",
							"status":    "completed",
							"call_id":   ts.callID,
							"name":      ts.name,
							"arguments": ts.args.String(),
						},
					})
				} else if !ts.added && (ts.callID != "" || ts.name != "") {
					// Tool was never added - create it now
					ts.added = true
					if ts.callID == "" {
						ts.callID = fmt.Sprintf("call_%d", tcIdx)
					}
					if ts.name == "" {
						ts.name = "unknown_tool"
					}
					ts.outputIndex = sseOutputIndex
					ts.itemID = fmt.Sprintf("fc_%s", ts.callID)
					sseOutputIndex++
					ts.done = true
					sseTools[tcIdx] = ts

					writeSSEEvent("response.output_item.added", map[string]any{
						"output_index": ts.outputIndex,
						"item": map[string]any{
							"id":        ts.itemID,
							"type":      "function_call",
							"status":    "in_progress",
							"call_id":   ts.callID,
							"name":      ts.name,
							"arguments": "",
						},
					})
					writeSSEEvent("response.function_call_arguments.done", map[string]any{
						"output_index": ts.outputIndex,
						"item_id":      ts.itemID,
						"arguments":    ts.args.String(),
					})
					writeSSEEvent("response.output_item.done", map[string]any{
						"output_index": ts.outputIndex,
						"item": map[string]any{
							"id":        ts.itemID,
							"type":      "function_call",
							"status":    "completed",
							"call_id":   ts.callID,
							"name":      ts.name,
							"arguments": ts.args.String(),
						},
					})
				}
			}

			// 2. Close reasoning item (cc-switch finalizeReasoning)
			if sseHasEmittedReasoning {
				outputIndex := sseReasoningOutputIndex
				reasoningItemID := fmt.Sprintf("rs_%s", sseMessageID)
				writeSSEEvent("response.reasoning_summary_text.done", map[string]any{
					"output_index":  outputIndex,
					"item_id":       reasoningItemID,
					"summary_index": 0,
					"text":          sseReasoningContent.String(),
				})
				writeSSEEvent("response.reasoning_summary_part.done", map[string]any{
					"output_index":  outputIndex,
					"item_id":       reasoningItemID,
					"summary_index": 0,
					"part": map[string]any{
						"type": "summary_text",
						"text": sseReasoningContent.String(),
					},
				})
				writeSSEEvent("response.output_item.done", map[string]any{
					"output_index": outputIndex,
					"item": map[string]any{
						"id":     reasoningItemID,
						"type":   "reasoning",
						"summary": []any{
							map[string]any{"type": "summary_text", "text": sseReasoningContent.String()},
						},
					},
				})
			}

			// 3. Close text item (cc-switch finalizeText: output_text.done -> content_part.done -> output_item.done)
			if sseHasEmittedText {
				outputIndex := sseTextOutputIndex
				textItemID := fmt.Sprintf("%s_msg", sseMessageID)
				writeSSEEvent("response.output_text.done", map[string]any{
					"output_index":  outputIndex,
					"item_id":       textItemID,
					"content_index": 0,
					"text":          sseTextContent.String(),
				})
				writeSSEEvent("response.content_part.done", map[string]any{
					"output_index":  outputIndex,
					"item_id":       textItemID,
					"content_index": 0,
					"part": map[string]any{
						"type":        "output_text",
						"text":        sseTextContent.String(),
						"annotations": []any{},
					},
				})
				writeSSEEvent("response.output_item.done", map[string]any{
					"output_index": outputIndex,
					"item": map[string]any{
						"id":     textItemID,
						"type":   "message",
						"status": "completed",
						"role":   "assistant",
						"content": []any{
							map[string]any{"type": "output_text", "text": sseTextContent.String(), "annotations": []any{}},
						},
					},
				})
			}

			// 4. Build output array for response.completed (sorted by output_index)
			type outputItem struct {
				index int
				item  map[string]any
			}
			var outputList []outputItem

			// Add reasoning items
			if sseHasEmittedReasoning {
				reasoningItemID := fmt.Sprintf("rs_%s", sseMessageID)
				outputList = append(outputList, outputItem{
					index: sseReasoningOutputIndex,
					item: map[string]any{
						"id":     reasoningItemID,
						"type":   "reasoning",
						"summary": []any{
							map[string]any{"type": "summary_text", "text": sseReasoningContent.String()},
						},
					},
				})
			}
			// Add text items
			if sseHasEmittedText {
				textItemID := fmt.Sprintf("%s_msg", sseMessageID)
				outputList = append(outputList, outputItem{
					index: sseTextOutputIndex,
					item: map[string]any{
						"id":     textItemID,
						"type":   "message",
						"status": "completed",
						"role":   "assistant",
						"content": []any{
							map[string]any{"type": "output_text", "text": sseTextContent.String(), "annotations": []any{}},
						},
					},
				})
			}
			// Add tool call items (from sseTools, matching cc-switch finalizeTools)
			for _, ts := range sseTools {
				if ts.added && ts.done {
					if ts.itemID == "" {
						if ts.callID == "" {
							ts.callID = "call_0"
						}
						ts.itemID = fmt.Sprintf("fc_%s", ts.callID)
					}
					if ts.name == "" {
						ts.name = "unknown_tool"
					}
					outputList = append(outputList, outputItem{
						index: ts.outputIndex,
						item: map[string]any{
							"id":        ts.itemID,
							"type":      "function_call",
							"status":    "completed",
							"call_id":   ts.callID,
							"name":      ts.name,
							"arguments": ts.args.String(),
						},
					})
				}
			}
			// Sort by output_index and build output array
			sort.Slice(outputList, func(i, j int) bool {
				return outputList[i].index < outputList[j].index
			})
			outputItems := make([]any, len(outputList))
			for i, oi := range outputList {
				outputItems[i] = oi.item
			}

			resp := buildResponseObj("completed", outputItems)
			resp["usage"] = map[string]any{
				"input_tokens":          extractor.inputTokens,
				"output_tokens":         extractor.outputTokens,
				"total_tokens":          extractor.inputTokens + extractor.outputTokens,
				"input_tokens_details":  map[string]any{"cached_tokens": extractor.cacheRead},
				"output_tokens_details": map[string]any{"reasoning_tokens": 0},
			}
			writeSSEEvent("response.completed", map[string]any{"response": resp})
		}


		// startReasoningItem: matches cc-switch pushReasoningSummaryPartAdded
		startReasoningItem := func() {
			emitResponseStarted()
			itemID := fmt.Sprintf("rs_%s", sseMessageID)
			sseCurrentItemID = itemID
			sseReasoningOutputIndex = sseOutputIndex
			writeSSEEvent("response.output_item.added", map[string]any{
				"output_index": sseOutputIndex,
				"item": map[string]any{
					"id":      itemID,
					"type":    "reasoning",
					"status":  "in_progress",
					"summary": []any{},
				},
			})
			writeSSEEvent("response.reasoning_summary_part.added", map[string]any{
				"output_index":  sseOutputIndex,
				"item_id":       itemID,
				"summary_index": 0,
				"part": map[string]any{
					"type": "summary_text",
					"text": "",
				},
			})
			sseHasEmittedReasoning = true
			sseOutputIndex++
		}

		// startTextItem: matches cc-switch pushContentPartAdded
		startTextItem := func() {
			emitResponseStarted()
			itemID := fmt.Sprintf("%s_msg", sseMessageID)
			sseCurrentItemID = itemID
			sseTextOutputIndex = sseOutputIndex
			writeSSEEvent("response.output_item.added", map[string]any{
				"output_index": sseOutputIndex,
				"item": map[string]any{
					"id":      itemID,
					"type":    "message",
					"status":  "in_progress",
					"role":    "assistant",
					"content": []any{},
				},
			})
			writeSSEEvent("response.content_part.added", map[string]any{
				"output_index":  sseOutputIndex,
				"item_id":       itemID,
				"content_index": 0,
				"part": map[string]any{
					"type":        "output_text",
					"text":        "",
					"annotations": []any{},
				},
			})
			sseHasEmittedText = true
			sseOutputIndex++
		}

			// SSE block-based parser - matches cc-switch take_sse_block pattern.
			// Accumulates lines into complete SSE events, processes each block as a unit.
			var sseEventName string
			var sseDataLines []string

			processSSEBlock := func() {
				if len(sseDataLines) == 0 {
					return
				}
				dataStr := strings.Join(sseDataLines, "\n")
				sseDataLines = nil

				if dataStr == "" {
					return
				}

				// [DONE] marker
				if strings.TrimSpace(dataStr) == "[DONE]" {
					sseFinalize()
					lastCompletedEvent = dataStr
					return
				}

				// SSE error event (matches cc-switch: event_name == "error")
				if sseEventName == "error" {
					errMsg := "SSE error event received"
					var parsed map[string]any
					if err := json.Unmarshal([]byte(dataStr), &parsed); err == nil {
						if eo, ok := parsed["error"].(map[string]any); ok {
							if m, ok := eo["message"].(string); ok {
								errMsg = m
							}
						} else if m, ok := parsed["message"].(string); ok {
							errMsg = m
						}
					}
					writeSSEEvent("response.failed", map[string]any{
						"response": buildResponseObj("failed", []any{}),
						"error":    map[string]any{"message": errMsg},
					})
					sseHasCompleted = true
					return
				}

				var eventData map[string]any
				if err := json.Unmarshal([]byte(dataStr), &eventData); err != nil {
					debugLog("SSE parse error: %v, data: %s", err, dataStr[:min(len(dataStr), 200)])
					return
				}

				if convertSSE {
					// Extract response metadata
					if id, ok := eventData["id"].(string); ok && sseMessageID == "" {
							// Normalize response ID to resp_ prefix (matches cc-switch response_id_from_chat_id)
							if strings.HasPrefix(id, "resp_") {
								sseMessageID = id
							} else {
								sseMessageID = fmt.Sprintf("resp_%s", id)
							}
					}
					if model, ok := eventData["model"].(string); ok && sseModel == "" {
						sseModel = model
					}
					if sseCreatedAt == 0 {
						if created, ok := eventData["created"].(float64); ok {
							sseCreatedAt = int64(created)
						}
					}

					// Extract usage from final chunk
					if usage, ok := eventData["usage"].(map[string]any); ok {
						if v, ok := usage["prompt_tokens"].(float64); ok {
							extractor.inputTokens = int(v)
						}
						if v, ok := usage["completion_tokens"].(float64); ok {
							extractor.outputTokens = int(v)
						}
					}

					// Check for error in data payload (cc-switch: chunk.get("error"))
					if errorObj, hasError := eventData["error"]; hasError && errorObj != nil {
						errMsg := "upstream error in SSE stream"
						if eo, ok := errorObj.(map[string]any); ok {
							if m, ok := eo["message"].(string); ok {
								errMsg = m
							}
						}
						writeSSEEvent("response.failed", map[string]any{
							"response": buildResponseObj("failed", []any{}),
							"error":    map[string]any{"message": errMsg},
						})
						sseHasCompleted = true
						return
					}

					// Process choices
					if choices, ok := eventData["choices"].([]any); ok {
						for _, c := range choices {
							choice, _ := c.(map[string]any)
							delta, _ := choice["delta"].(map[string]any)

							// Check choice-level error
							if choiceErr, ok := choice["error"]; ok && choiceErr != nil {
								errMsg := "upstream error in stream"
								if em, ok := choiceErr.(string); ok {
									errMsg = em
								}
								writeSSEEvent("response.failed", map[string]any{
									"response": buildResponseObj("failed", []any{}),
									"error":    map[string]any{"message": errMsg},
								})
								sseHasCompleted = true
								continue
							}

							// Track finish_reason for debugging
							if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
								debugLog("SSE finish_reason: %s", fr)
							}

							// Tool calls - matches cc-switch pushToolCallDelta
							if toolCalls, ok := delta["tool_calls"].([]any); ok {
								for _, tc := range toolCalls {
									tool, _ := tc.(map[string]any)
									tcIndex := 0
									if idx, ok := tool["index"].(float64); ok {
										tcIndex = int(idx)
									}
									fn, _ := tool["function"].(map[string]any)
									callID, _ := tool["id"].(string)
									nameVal := ""
									argsVal := ""
									if fn != nil {
										nameVal, _ = fn["name"].(string)
										argsVal, _ = fn["arguments"].(string)
									}

									ts, exists := sseTools[tcIndex]
									if !exists {
										ts = &sseToolState{}
										sseTools[tcIndex] = ts
									}
									if callID != "" {
										ts.callID = callID
									}
									if nameVal != "" {
										ts.name = nameVal
									}
									if argsVal != "" {
										ts.args.WriteString(argsVal)
									}

									// First time we have enough info - emit events
									if !ts.added && (ts.callID != "" || ts.name != "") {
										ts.added = true
										if ts.callID == "" {
											ts.callID = fmt.Sprintf("call_%d", tcIndex)
										}
										if ts.name == "" {
											ts.name = "unknown_tool"
										}
										ts.outputIndex = sseOutputIndex
										ts.itemID = fmt.Sprintf("fc_%s", ts.callID)
										sseOutputIndex++

										writeSSEEvent("response.output_item.added", map[string]any{
											"output_index": ts.outputIndex,
											"item": map[string]any{
												"id":        ts.itemID,
												"type":      "function_call",
												"status":    "in_progress",
												"call_id":   ts.callID,
												"name":      ts.name,
												"arguments": "",
											},
										})
										// Flush buffered arguments
										if ts.args.Len() > 0 {
											writeSSEEvent("response.function_call_arguments.delta", map[string]any{
												"output_index": ts.outputIndex,
												"item_id":      ts.itemID,
												"delta":        ts.args.String(),
											})
										}
									} else if ts.added && argsVal != "" {
										writeSSEEvent("response.function_call_arguments.delta", map[string]any{
											"output_index": ts.outputIndex,
											"item_id":      ts.itemID,
											"delta":        argsVal,
										})
									}
								}
							}

							// Text content delta
							if content, ok := delta["content"].(string); ok && content != "" {
								if !sseHasEmittedText {
									startTextItem()
								}
								writeSSEEvent("response.output_text.delta", map[string]any{
									"output_index":  sseOutputIndex - 1,
									"item_id":       sseCurrentItemID,
									"content_index": 0,
									"delta":         content,
								})
								sseTextContent.WriteString(content)
							}

							// DeepSeek reasoning_content -> separate reasoning item
							if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
								if !sseHasEmittedReasoning {
									startReasoningItem()
								}
								writeSSEEvent("response.reasoning_summary_text.delta", map[string]any{
									"output_index": sseOutputIndex - 1,
									"item_id":      sseCurrentItemID,
									"delta":         rc,
								})
								sseReasoningContent.WriteString(rc)
							}
						}
					}
				} else {
					// Pass through non-converted SSE events
					if strings.Contains(dataStr, "response.completed") {
						lastCompletedEvent = dataStr
					}
					extractor.parseAnthropicSSE(dataStr)
					extractor.parseOpenAISSE(dataStr)
					extractor.parseOpenAIResponsesSSE(dataStr)
					_, _ = w.Write([]byte("data: " + dataStr + "\n\n"))
					if f, ok := w.(http.Flusher); ok {
						f.Flush()
					}
				}
			}

			flushBlock := func() {
				processSSEBlock()
				sseEventName = ""
			}

			for {
				line, err := reader.ReadString('\n')
				if err != nil && err != io.EOF {
					break
				}
				line = strings.TrimRight(line, "\r\n")

				// Empty line = end of SSE block
				if line == "" {
					if len(sseDataLines) > 0 {
						flushBlock()
					}
					if err == io.EOF {
						break
					}
					continue
				}

				// Parse SSE fields
				if strings.HasPrefix(line, "event:") {
					sseEventName = strings.TrimSpace(line[6:])
				} else if strings.HasPrefix(line, "data:") {
					val := line[5:]
					if len(val) > 0 && val[0] == ' ' {
						val = val[1:]
					}
					sseDataLines = append(sseDataLines, val)
				}

				if err == io.EOF {
					if len(sseDataLines) > 0 {
						flushBlock()
					}
					break
				}
			}// Always call finalize at stream end (cc-switch pattern)
		if convertSSE {
			sseFinalize()
		}

		go func() {
			logPath := filepath.Join(os.TempDir(), "moonbridge-sse-v2.log")
			f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				return
			}
			defer f.Close()
			fmt.Fprintf(f, "model=%s input=%d output=%d cacheRead=%d cacheWrite=%d\n",
				extractor.model, extractor.inputTokens, extractor.outputTokens,
				extractor.cacheRead, extractor.cacheWrite)
			fmt.Fprintf(f, "\n--- response.completed event (first 1000 bytes) ---\n")
			if lastCompletedEvent != "" {
				fmt.Fprintf(f, "%s\n", lastCompletedEvent[:min(len(lastCompletedEvent), 1000)])
			} else {
				fmt.Fprintf(f, "(not found)\n")
			}
		}()

		extractor.recordUsage(tp)
		extractor.recordPerRequest(tp)
		lastResp.Body.Close()
		return
	}

	// Non-streaming JSON response
	respBody, err := io.ReadAll(lastResp.Body)
	lastResp.Body.Close()
	if err != nil {
		respBody = []byte{}
	}

	// Extract actual model from upstream response
	upstreamModel := actualModel
	if len(respBody) > 0 {
		var rm responseModel
		if err := json.Unmarshal(respBody, &rm); err == nil && rm.Model != "" {
			upstreamModel = rm.Model
		}
	}

	if lastResp.StatusCode == 200 && strings.Contains(ct, "application/json") && len(respBody) > 0 {
		// Try OpenAI Responses API first
		var r responseUsageResponses
		if err := json.Unmarshal(respBody, &r); err == nil && r.Usage.InputTokens > 0 {
			recModel := upstreamModel
			if r.Model != "" {
				recModel = r.Model
			}
			AppLogger.Printf("[UsageRecord] Responses API: model=%s input=%d output=%d cacheRead=%d cacheWrite=%d",
				recModel, r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.InputTokensDetails.CachedTokens, 0)
			tp.mu.Lock()
			if tp.recordUsage != nil {
				tp.recordUsage(
					recModel,
					r.Usage.InputTokens,
					r.Usage.OutputTokens,
					r.Usage.InputTokensDetails.CachedTokens,
					0,
				)
			}
			if tp.recordRequest != nil {
				tp.recordRequest(recModel)
			}
			tp.mu.Unlock()
		} else {
			// Try Anthropic-style
			var u responseUsage
			if err := json.Unmarshal(respBody, &u); err == nil && u.Usage.InputTokens > 0 {
				AppLogger.Printf("[UsageRecord] Anthropic path: model=%s input=%d output=%d cacheRead=%d cacheWrite=%d",
					upstreamModel, u.Usage.InputTokens, u.Usage.OutputTokens, u.Usage.InputTokensDetails.CachedTokens, u.Usage.CacheCreationInputTokens)
				tp.mu.Lock()
				if tp.recordUsage != nil {
					tp.recordUsage(
						upstreamModel,
						u.Usage.InputTokens,
						u.Usage.OutputTokens,
						u.Usage.InputTokensDetails.CachedTokens,
						u.Usage.CacheCreationInputTokens,
					)
				}
				if tp.recordRequest != nil {
					tp.recordRequest(upstreamModel)
				}
				tp.mu.Unlock()
			} else {
				// Fallback: OpenAI-style
				var o responseUsageOpenAI
				if err := json.Unmarshal(respBody, &o); err == nil && o.Usage.PromptTokens > 0 {
					AppLogger.Printf("[UsageRecord] OpenAI path: model=%s prompt=%d completion=%d",
						upstreamModel, o.Usage.PromptTokens, o.Usage.CompletionTokens)
					tp.mu.Lock()
					if tp.recordUsage != nil {
						tp.recordUsage(
							upstreamModel,
							o.Usage.PromptTokens,
							o.Usage.CompletionTokens,
							o.Usage.PromptTokensDetails.CachedTokens,
							0,
						)
					}
					if tp.recordRequest != nil {
						tp.recordRequest(upstreamModel)
					}
					tp.mu.Unlock()
				} else {
					// Usage parsing failed, but still record per-request
					AppLogger.Printf("[UsageRecord] No usage data extracted, recording per-request: model=%s", upstreamModel)
					tp.mu.Lock()
					if tp.recordRequest != nil {
						tp.recordRequest(upstreamModel)
					}
					tp.mu.Unlock()
				}
			}
		}
	} else {
		AppLogger.Printf("[UsageRecord] Skipped: status=%d ct=%s bodyLen=%d", lastResp.StatusCode, ct, len(respBody))
	}

	// Convert Chat Completions response back to OpenAI Responses API format for Codex compatibility.
	if len(respBody) > 0 && provider.Protocol == "openai-chat" {
		AppLogger.Printf("[NonStreamDebug] UPSTREAM response (len=%d):", len(respBody))
		if len(respBody) > 4000 {
			AppLogger.Printf("[NonStreamDebug] ...truncated: %s...", string(respBody[:4000]))
		} else {
			AppLogger.Printf("[NonStreamDebug] %s", string(respBody))
		}
		respBody = convertChatCompletionsToResponses(respBody)
		AppLogger.Printf("[NonStreamDebug] CONVERTED result (len=%d):", len(respBody))
		if len(respBody) > 4000 {
			AppLogger.Printf("[NonStreamDebug] ...truncated: %s...", string(respBody[:4000]))
		} else {
			AppLogger.Printf("[NonStreamDebug] %s", string(respBody))
		}
	}

	w.WriteHeader(lastResp.StatusCode)
	_, _ = w.Write(respBody)
}
