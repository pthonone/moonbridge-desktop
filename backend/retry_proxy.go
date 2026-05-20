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
	mu             sync.Mutex
	listenPort     int
	targetPort     int
	maxRetries     int
	server         *http.Server
	running        bool
	recordUsage    RecordUsageFunc
	recordRequest  RecordRequestFunc
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
			log.Printf("[RetryProxy] server error: %v", err)
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
	if rp.recordUsage != nil && (e.inputTokens > 0 || e.outputTokens > 0) {
		rp.recordUsage(e.model, e.inputTokens, e.outputTokens, e.cacheRead, e.cacheWrite)
	}
}

// recordPerRequest records a per-request billing event.
func (e *SSEUsageExtractor) recordPerRequest(rp *RetryProxy) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if rp.recordRequest != nil {
		rp.recordRequest(e.model)
	}
}

// parseAnthropicSSE parses Anthropic-style SSE events for usage data.
func (e *SSEUsageExtractor) parseAnthropicSSE(data string) {
	if !strings.Contains(data, "message_delta") {
		return
	}
	var msg struct {
		Delta struct {
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"delta"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &msg); err == nil {
		if u := msg.Usage; u != nil {
			e.inputTokens = u.InputTokens
			e.outputTokens = u.OutputTokens
		} else if u := msg.Delta.Usage; u != nil {
			e.inputTokens = u.InputTokens
			e.outputTokens = u.OutputTokens
		}
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

	// Extract model from request body
	var modelName string
	if len(bodyBytes) > 0 {
		var reqBody struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(bodyBytes, &reqBody); err == nil && reqBody.Model != "" {
			modelName = reqBody.Model
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
				extractor.parseAnthropicSSE(data)
				extractor.parseOpenAISSE(data)
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
			extractor.parseAnthropicSSE(data)
			extractor.parseOpenAISSE(data)
		}

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

	if lastResp.StatusCode == 200 && strings.Contains(ct, "application/json") && len(respBody) > 0 {
		// Try Anthropic-style first
		var u responseUsage
		if err := json.Unmarshal(respBody, &u); err == nil && u.Usage.InputTokens > 0 {
			rp.mu.Lock()
			if rp.recordUsage != nil {
				rp.recordUsage(
					modelName,
					u.Usage.InputTokens,
					u.Usage.OutputTokens,
					u.Usage.InputTokensDetails.CachedTokens,
					u.Usage.CacheCreationInputTokens,
				)
			}
			if rp.recordRequest != nil {
				rp.recordRequest(modelName)
			}
			rp.mu.Unlock()
		} else {
			// Fallback: OpenAI-style
			var o responseUsageOpenAI
			if err := json.Unmarshal(respBody, &o); err == nil && o.Usage.PromptTokens > 0 {
				rp.mu.Lock()
				if rp.recordUsage != nil {
					rp.recordUsage(
						modelName,
						o.Usage.PromptTokens,
						o.Usage.CompletionTokens,
						o.Usage.PromptTokensDetails.CachedTokens,
						0,
					)
				}
				if rp.recordRequest != nil {
					rp.recordRequest(modelName)
				}
				rp.mu.Unlock()
			} else {
				// No usage data, still record per-request
				rp.mu.Lock()
				if rp.recordRequest != nil {
					rp.recordRequest(modelName)
				}
				rp.mu.Unlock()
			}
		}
	} else {
		// Non-200 or non-JSON: still record per-request
		rp.mu.Lock()
		if rp.recordRequest != nil && lastResp.StatusCode == 200 {
			rp.recordRequest(modelName)
		}
		rp.mu.Unlock()
	}

	w.WriteHeader(lastResp.StatusCode)
	_, _ = w.Write(respBody)
}
