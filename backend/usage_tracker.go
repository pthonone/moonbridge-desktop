package backend

import (
	"sync"
)

// UsageTracker tracks usage stats locally since Transform mode
// doesn't expose the /api/v1/stats management endpoints.
type UsageTracker struct {
	mu    sync.RWMutex
	stats UsageStats
}

func NewUsageTracker() *UsageTracker {
	return &UsageTracker{}
}

// AddUsage adds a single request's usage data.
func (ut *UsageTracker) AddUsage(inputTokens, outputTokens, cacheRead, cacheWrite int) {
	ut.mu.Lock()
	ut.stats.InputTokens += inputTokens
	ut.stats.OutputTokens += outputTokens
	ut.stats.CacheRead += cacheRead
	ut.stats.CacheWrite += cacheWrite
	ut.stats.RequestCount++
	ut.mu.Unlock()
}

// GetStats returns current usage stats.
func (ut *UsageTracker) GetStats() UsageStats {
	ut.mu.RLock()
	defer ut.mu.RUnlock()
	return ut.stats
}

// Reset clears all stats.
func (ut *UsageTracker) Reset() {
	ut.mu.Lock()
	ut.stats = UsageStats{}
	ut.mu.Unlock()
}
