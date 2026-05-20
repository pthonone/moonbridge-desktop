package backend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// MetricsProxy polls Moon Bridge's stats API and caches the latest usage data.
type MetricsProxy struct {
	mu    sync.RWMutex
	stats UsageStats
	port  int
	stop  chan struct{}
	once  sync.Once
}

var metricsHTTP = &http.Client{Timeout: 2 * time.Second}

func NewMetricsProxy(port int) *MetricsProxy {
	return &MetricsProxy{port: port, stop: make(chan struct{})}
}

// Start begins polling the Moon Bridge stats endpoint.
func (mp *MetricsProxy) Start() {
	go mp.poll()
}

// Stop stops the polling. Safe to call multiple times.
func (mp *MetricsProxy) Stop() {
	mp.once.Do(func() {
		close(mp.stop)
	})
}

// GetStats returns the latest cached usage stats.
func (mp *MetricsProxy) GetStats() UsageStats {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.stats
}

func (mp *MetricsProxy) poll() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-mp.stop:
			return
		case <-ticker.C:
			mp.fetchStats()
		}
	}
}

func (mp *MetricsProxy) fetchStats() {
	// Try management API first
	url := fmt.Sprintf("http://127.0.0.1:%d/api/v1/stats/summary", mp.port)
	resp, err := metricsHTTP.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// Management API not available (Transform mode doesn't mount /api/v1 routes)
		return
	}

	var summary struct {
		Requests     int     `json:"requests"`
		InputTokens  int     `json:"input_tokens"`
		OutputTokens int     `json:"output_tokens"`
		CacheHitRate float64 `json:"cache_hit_rate"`
		TotalCost    float64 `json:"total_cost"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return
	}

	mp.mu.Lock()
	mp.stats.RequestCount = summary.Requests
	mp.stats.InputTokens = summary.InputTokens
	mp.stats.OutputTokens = summary.OutputTokens
	mp.stats.CacheHitRate = summary.CacheHitRate
	mp.stats.TotalCost = summary.TotalCost
	mp.mu.Unlock()
}
