package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RecordUsageFunc is called when usage data is extracted from a response.
type RecordUsageFunc func(model string, inputTokens, outputTokens, cacheRead, cacheWrite int)

// RecordRequestFunc is called for every successful request (for per-request billing).
type RecordRequestFunc func(model string)

// RetryProxy is an HTTP reverse proxy that automatically retries 502 errors.
type RetryProxy struct {
	mu              sync.Mutex
	listenPort      int
	targetPort      int
	maxRetries      int
	server          *http.Server
	running         bool
	recordUsage     RecordUsageFunc
	recordRequest   RecordRequestFunc
	resolveModel    func(alias string) string    // resolves route alias to actual model name
	getCurrentModel func() string                // returns the user's currently selected model alias
}

// NewRetryProxy creates a retry proxy that listens on listenPort
// and forwards requests to targetPort.
func NewRetryProxy(listenPort, targetPort int, maxRetries int) *RetryProxy {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &RetryProxy{
		listenPort: listenPort,
		targetPort: targetPort,
		maxRetries: maxRetries,
	}
}

// SetRecordUsage sets the callback for recording usage data.
func (rp *RetryProxy) SetRecordUsage(fn RecordUsageFunc) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	rp.recordUsage = fn
}

// SetRecordRequest sets the callback for recording per-request billing.
func (rp *RetryProxy) SetRecordRequest(fn RecordRequestFunc) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	rp.recordRequest = fn
}

// SetResolveModel sets the function that maps route aliases to actual model names.
func (rp *RetryProxy) SetResolveModel(fn func(alias string) string) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	rp.resolveModel = fn
}

// SetGetCurrentModel sets the function that returns the user's currently selected model.
func (rp *RetryProxy) SetGetCurrentModel(fn func() string) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	rp.getCurrentModel = fn
}

// Start begins listening for HTTP requests.
func (rp *RetryProxy) Start() error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	if rp.running {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", rp.handleRequest)

	rp.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", rp.listenPort),
		Handler: mux,
	}

	rp.running = true

	go func() {
		if err := rp.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			AppLogger.Printf("[RetryProxy] server error: %v", err)
		}
		rp.mu.Lock()
		rp.running = false
		rp.mu.Unlock()
	}()

	return nil
}

// Stop shuts down the retry proxy.
func (rp *RetryProxy) Stop() error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	if !rp.running || rp.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return rp.server.Shutdown(ctx)
}

// IsRunning returns whether the proxy is currently listening.
func (rp *RetryProxy) IsRunning() bool {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.running
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

// OpenAI Responses API usage (non-streaming, moonbridge)
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

func (e *SSEUsageExtractor) recordUsage(rp *RetryProxy) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	model := e.model
	if rp.resolveModel != nil {
		model = rp.resolveModel(e.model)
	}
	if model == "" {
		return
	}
	if rp.recordUsage != nil && (e.inputTokens > 0 || e.outputTokens > 0) {
		rp.recordUsage(model, e.inputTokens, e.outputTokens, e.cacheRead, e.cacheWrite)
	}
}

// recordPerRequest records a per-request billing event.
func (e *SSEUsageExtractor) recordPerRequest(rp *RetryProxy) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	model := e.model
	if rp.resolveModel != nil {
		model = rp.resolveModel(e.model)
	}
	if model == "" {
		return
	}
	if rp.recordRequest != nil {
		rp.recordRequest(model)
	}
}

// parseAnthropicSSE parses Anthropic-style SSE events for usage data.
// Extracts model from message_start events, usage from message_delta/message_stop.
func (e *SSEUsageExtractor) parseAnthropicSSE(data string) {
	// Extract model from message_start
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
// Usage appears in "response.completed" event with fields:
// {"type":"response.completed","response":{"usage":{"input_tokens":N,"output_tokens":N,"input_tokens_details":{"cached_tokens":N}}}}
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
// Codex sometimes sends tools with empty function.name, causing upstream 502.
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
// Handles both {"type":"function","function":{"name":"..."}} and {"type":"function","name":"..."}.
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

func (rp *RetryProxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Buffer the request body so we can retry
	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
	}

	target := fmt.Sprintf("http://127.0.0.1:%d%s", rp.targetPort, r.URL.RequestURI())

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
			// Sanitize tools: remove entries with empty names (fixes Codex empty tool bug)
			if len(reqBody.Tools) > 0 {
				bodyBytes = sanitizeTools(bodyBytes, reqBody.Tools)
			}
		}
	}

	// Rewrite request body model to user's currently selected model.
	// This ensures MoonBridge uses the correct model even if Codex has stale config.
	rp.mu.Lock()
	currentModel := rp.getCurrentModel
	rp.mu.Unlock()
	if currentModel != nil {
		selected := currentModel()
		if selected != "" && modelName != selected {
			AppLogger.Printf("[ModelRewrite] request model=%s, rewriting to selected=%s", modelName, selected)
			var body map[string]any
			if err := json.Unmarshal(bodyBytes, &body); err == nil {
				body["model"] = selected
				if rewritten, err := json.Marshal(body); err == nil {
					bodyBytes = rewritten
					modelName = selected
				}
			}
		}
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; attempt <= rp.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(500*(1<<uint(attempt-1))) * time.Millisecond
			time.Sleep(backoff)
		}

		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bodyReader)
		if err != nil {
			lastErr = err
			continue
		}

		for key, values := range r.Header {
			for _, val := range values {
				req.Header.Add(key, val)
			}
		}

		client := &http.Client{Timeout: 0}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusBadGateway && attempt < rp.maxRetries {
			resp.Body.Close()
			continue
		}

		lastResp = resp
		lastErr = nil
		break
	}

	if lastErr != nil && lastResp == nil {
		http.Error(w, fmt.Sprintf("upstream error: %v", lastErr), http.StatusBadGateway)
		return
	}

	// Forward response headers
	for key, values := range lastResp.Header {
		for _, val := range values {
			w.Header().Add(key, val)
		}
	}

	ct := lastResp.Header.Get("Content-Type")

	// Handle SSE streaming responses
	if lastResp.StatusCode == 200 && strings.Contains(ct, "text/event-stream") {
		w.WriteHeader(200)

		extractor := &SSEUsageExtractor{model: modelName}
		scanner := bufio.NewScanner(lastResp.Body)
		var eventBuf strings.Builder
		eventStarted := false
		var lastCompletedEvent string

		for scanner.Scan() {
			line := scanner.Text()

			// Capture data lines for usage extraction
			if strings.HasPrefix(line, "data: ") {
				eventStarted = true
				eventBuf.WriteString(line[6:])
			} else if strings.HasPrefix(line, "data:") {
				eventStarted = true
				eventBuf.WriteString(line[5:])
			}

			// Empty line = end of event
			if line == "" && eventStarted {
				data := eventBuf.String()
				if strings.Contains(data, "response.completed") {
					lastCompletedEvent = data
				}
				extractor.parseAnthropicSSE(data)
				extractor.parseOpenAISSE(data)
				extractor.parseOpenAIResponsesSSE(data)
				eventBuf.Reset()
				eventStarted = false
			}

			// Forward line to client
			_, _ = w.Write([]byte(line + "\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}

		// Process any remaining event data
		if eventStarted {
			data := eventBuf.String()
			if strings.Contains(data, "response.completed") {
				lastCompletedEvent = data
			}
			extractor.parseAnthropicSSE(data)
			extractor.parseOpenAISSE(data)
			extractor.parseOpenAIResponsesSSE(data)
		}

		// Debug: write SSE parsing result to file
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

		extractor.recordUsage(rp)
		extractor.recordPerRequest(rp)
		lastResp.Body.Close()
		return
	}

	// Non-streaming JSON response
	respBody, err := io.ReadAll(lastResp.Body)
	lastResp.Body.Close()
	if err != nil {
		respBody = []byte{}
	}

	// Extract actual model from upstream response (request body model may be stale)
	upstreamModel := modelName
	if len(respBody) > 0 {
		var rm responseModel
		if err := json.Unmarshal(respBody, &rm); err == nil && rm.Model != "" {
			upstreamModel = rm.Model
		}
	}

	resolvedModel := upstreamModel
	rp.mu.Lock()
	if rp.resolveModel != nil {
		resolvedModel = rp.resolveModel(upstreamModel)
	}
	rp.mu.Unlock()

	if lastResp.StatusCode == 200 && strings.Contains(ct, "application/json") && len(respBody) > 0 {
		// Try OpenAI Responses API first (MoonBridge /v1/responses)
		var r responseUsageResponses
		if err := json.Unmarshal(respBody, &r); err == nil && r.Usage.InputTokens > 0 {
			// Use response model if we got one, fall back to resolvedModel
			recModel := resolvedModel
			if r.Model != "" {
				rp.mu.Lock()
				if rp.resolveModel != nil {
					recModel = rp.resolveModel(r.Model)
				} else {
					recModel = r.Model
				}
				rp.mu.Unlock()
			}
			AppLogger.Printf("[UsageRecord] Responses API: model=%s input=%d output=%d cacheRead=%d cacheWrite=%d",
				recModel, r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.InputTokensDetails.CachedTokens, 0)
			rp.mu.Lock()
			if rp.recordUsage != nil {
				rp.recordUsage(
					recModel,
					r.Usage.InputTokens,
					r.Usage.OutputTokens,
					r.Usage.InputTokensDetails.CachedTokens,
					0,
				)
			}
			if rp.recordRequest != nil {
				rp.recordRequest(recModel)
			}
			rp.mu.Unlock()
		} else {
			// Try Anthropic-style
			var u responseUsage
			if err := json.Unmarshal(respBody, &u); err == nil && u.Usage.InputTokens > 0 {
				AppLogger.Printf("[UsageRecord] Anthropic path: model=%s input=%d output=%d cacheRead=%d cacheWrite=%d",
					resolvedModel, u.Usage.InputTokens, u.Usage.OutputTokens, u.Usage.InputTokensDetails.CachedTokens, u.Usage.CacheCreationInputTokens)
				rp.mu.Lock()
				if rp.recordUsage != nil {
					rp.recordUsage(
						resolvedModel,
						u.Usage.InputTokens,
						u.Usage.OutputTokens,
						u.Usage.InputTokensDetails.CachedTokens,
						u.Usage.CacheCreationInputTokens,
					)
				}
				if rp.recordRequest != nil {
					rp.recordRequest(resolvedModel)
				}
				rp.mu.Unlock()
			} else {
				// Fallback: OpenAI-style
				var o responseUsageOpenAI
				if err := json.Unmarshal(respBody, &o); err == nil && o.Usage.PromptTokens > 0 {
					AppLogger.Printf("[UsageRecord] OpenAI path: model=%s prompt=%d completion=%d",
						resolvedModel, o.Usage.PromptTokens, o.Usage.CompletionTokens)
					rp.mu.Lock()
					if rp.recordUsage != nil {
						rp.recordUsage(
							resolvedModel,
							o.Usage.PromptTokens,
							o.Usage.CompletionTokens,
							o.Usage.PromptTokensDetails.CachedTokens,
							0,
						)
					}
					if rp.recordRequest != nil {
						rp.recordRequest(resolvedModel)
					}
					rp.mu.Unlock()
				} else {
					// Usage parsing failed, but still record per-request with resolved model
					AppLogger.Printf("[UsageRecord] No usage data extracted, recording per-request: model=%s", resolvedModel)
					rp.mu.Lock()
					if rp.recordRequest != nil {
						rp.recordRequest(resolvedModel)
					}
					rp.mu.Unlock()
				}
			}
		}
	} else {
		AppLogger.Printf("[UsageRecord] Skipped: status=%d ct=%s bodyLen=%d", lastResp.StatusCode, ct, len(respBody))
	}

	w.WriteHeader(lastResp.StatusCode)
	_, _ = w.Write(respBody)
}
