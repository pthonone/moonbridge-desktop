package backend

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// UsageStore provides SQLite-backed persistent usage tracking.
type UsageStore struct {
	db        *sql.DB
	dbPath    string
	getPricing func(model string) *ModelPricing
}

// ModelPricing is an alias for Pricing — same fields, no duplication.
type ModelPricing = Pricing

func NewUsageStore(dataDir string, getPricing func(string) *ModelPricing) (*UsageStore, error) {
	os.MkdirAll(dataDir, 0755)
	dbPath := filepath.Join(dataDir, "usage.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open usage db: %w", err)
	}
	// Enable WAL mode for better concurrent read performance
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")

	// Store timestamps as UTC but track local offset for date queries
	_, offset := time.Now().Zone()
	localOffsetHours := float64(offset) / 3600.0

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS usage_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL DEFAULT (datetime('now', '` + fmt.Sprintf("%+g", localOffsetHours) + ` hours')),
		model TEXT NOT NULL,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		cache_read INTEGER NOT NULL DEFAULT 0,
		cache_write INTEGER NOT NULL DEFAULT 0,
		cost REAL NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}

	return &UsageStore{db: db, dbPath: dbPath, getPricing: getPricing}, nil
}

// AddUsage records a single request's usage.
func (us *UsageStore) AddUsage(model string, inputTokens, outputTokens, cacheRead, cacheWrite int) {
	cost := us.calcCost(model, inputTokens, outputTokens, cacheRead, cacheWrite)
	_, _ = us.db.Exec(
		"INSERT INTO usage_records (model, input_tokens, output_tokens, cache_read, cache_write, cost) VALUES (?, ?, ?, ?, ?, ?)",
		model, inputTokens, outputTokens, cacheRead, cacheWrite, cost,
	)
}

func (us *UsageStore) calcCost(model string, input, output, cacheRead, cacheWrite int) float64 {
	var pricing *ModelPricing
	if us.getPricing != nil {
		pricing = us.getPricing(model)
	}
	if pricing == nil {
		return 0
	}
	// Per-request billing (e.g. Coding Plan): fixed cost per API call
	if pricing.BillingMode == "per_request" {
		return math.Round(pricing.PerRequestCost*10000) / 10000
	}
	// Token-based billing: per million tokens
	cost := float64(input)/1e6*pricing.Input +
		float64(output)/1e6*pricing.Output +
		float64(cacheRead)/1e6*pricing.CacheRead +
		float64(cacheWrite)/1e6*pricing.CacheWrite
	return math.Round(cost*10000) / 10000
}

// GetStats returns aggregated usage stats since a given time (or all time if zero).
// Timestamps are stored in local time, so comparisons use local time.
func (us *UsageStore) GetStats(since time.Time) UsageStats {
	var stats UsageStats
	query := "SELECT COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(cache_read),0), COALESCE(SUM(cache_write),0), COALESCE(SUM(cost),0), COUNT(*) FROM usage_records"
	var args []interface{}
	if !since.IsZero() {
		query += " WHERE timestamp >= ?"
		args = append(args, since.Local().Format("2006-01-02 15:04:05"))
	}
	row := us.db.QueryRow(query, args...)
	_ = row.Scan(&stats.InputTokens, &stats.OutputTokens, &stats.CacheRead, &stats.CacheWrite, &stats.TotalCost, &stats.RequestCount)
	return stats
}

// GetTodayStats returns usage stats for today (since midnight local time).
func (us *UsageStore) GetTodayStats() UsageStats {
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return us.GetStats(midnight)
}

// GetDailyStats returns usage stats for the last N days (local time).
func (us *UsageStore) GetDailyStats(days int) []DailyUsageRow {
	if days <= 0 {
		days = 7
	}
	cutoff := time.Now().AddDate(0, 0, -days+1)
	midnight := time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, cutoff.Location())

	rows, err := us.db.Query(
		"SELECT DATE(timestamp), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(cache_read),0), COALESCE(SUM(cache_write),0), COALESCE(SUM(cost),0), COUNT(*) FROM usage_records WHERE timestamp >= ? GROUP BY DATE(timestamp) ORDER BY DATE(timestamp)",
		midnight.Local().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []DailyUsageRow
	for rows.Next() {
		var r DailyUsageRow
		_ = rows.Scan(&r.Date, &r.InputTokens, &r.OutputTokens, &r.CacheRead, &r.CacheWrite, &r.Cost, &r.RequestCount)
		result = append(result, r)
	}
	return result
}

// GetByModel returns usage grouped by model for a date range (local time).
func (us *UsageStore) GetByModel(start, end time.Time) []ModelUsageRow {
	endOfDay := time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 0, end.Location())
	rows, err := us.db.Query(
		"SELECT model, COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(cache_read),0), COALESCE(SUM(cache_write),0), COALESCE(SUM(cost),0), COUNT(*) FROM usage_records WHERE timestamp >= ? AND timestamp <= ? GROUP BY model ORDER BY cost DESC",
		start.Local().Format("2006-01-02 15:04:05"),
		endOfDay.Local().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []ModelUsageRow
	for rows.Next() {
		var r ModelUsageRow
		_ = rows.Scan(&r.Model, &r.InputTokens, &r.OutputTokens, &r.CacheRead, &r.CacheWrite, &r.Cost, &r.RequestCount)
		result = append(result, r)
	}
	return result
}

// GetRecentRecords returns the most recent usage records.
func (us *UsageStore) GetRecentRecords(limit int) []UsageRecord {
	if limit <= 0 {
		limit = 50
	}
	rows, err := us.db.Query(
		"SELECT id, timestamp, model, input_tokens, output_tokens, cache_read, cache_write, cost FROM usage_records ORDER BY timestamp DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []UsageRecord
	for rows.Next() {
		var r UsageRecord
		_ = rows.Scan(&r.ID, &r.Timestamp, &r.Model, &r.InputTokens, &r.OutputTokens, &r.CacheRead, &r.CacheWrite, &r.Cost)
		result = append(result, r)
	}
	return result
}

// Count returns total number of records in the given date range.
func (us *UsageStore) Count(start, end time.Time) int {
	var count int
	query := "SELECT COUNT(*) FROM usage_records"
	var args []interface{}
	if !start.IsZero() && !end.IsZero() {
		query += " WHERE timestamp >= ? AND timestamp <= ?"
		args = append(args, start.Local().Format("2006-01-02 15:04:05"), end.Local().Format("2006-01-02 15:04:05"))
	}
	_ = us.db.QueryRow(query, args...).Scan(&count)
	return count
}

// GetHourlyStats returns usage stats for today grouped by hour (0-23).
func (us *UsageStore) GetHourlyStats() []HourlyUsageRow {
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	rows := make([]HourlyUsageRow, 24)
	for i := 0; i < 24; i++ {
		rows[i].Hour = i
	}
	res, err := us.db.Query(
		"SELECT CAST(strftime('%H', timestamp) AS INTEGER), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(cache_read),0), COALESCE(SUM(cache_write),0), COALESCE(SUM(cost),0), COUNT(*) FROM usage_records WHERE timestamp >= ? GROUP BY 1",
		midnight.Local().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return rows
	}
	defer res.Close()
	for res.Next() {
		var r HourlyUsageRow
		if err := res.Scan(&r.Hour, &r.InputTokens, &r.OutputTokens, &r.CacheRead, &r.CacheWrite, &r.Cost, &r.RequestCount); err == nil && r.Hour >= 0 && r.Hour < 24 {
			rows[r.Hour] = r
		}
	}
	return rows
}

// ClearToday removes all records from today.
func (us *UsageStore) ClearToday() (int64, error) {
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	res, err := us.db.Exec("DELETE FROM usage_records WHERE timestamp >= ?", midnight.Local().Format("2006-01-02 15:04:05"))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ClearDateRange removes records in the given date range (inclusive, local time).
func (us *UsageStore) ClearDateRange(start, end time.Time) (int64, error) {
	endOfDay := time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 0, end.Location())
	res, err := us.db.Exec(
		"DELETE FROM usage_records WHERE timestamp >= ? AND timestamp <= ?",
		start.Local().Format("2006-01-02 15:04:05"),
		endOfDay.Local().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ClearAll removes all usage records.
func (us *UsageStore) ClearAll() (int64, error) {
	res, err := us.db.Exec("DELETE FROM usage_records")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Close closes the database connection.
func (us *UsageStore) Close() error {
	if us.db != nil {
		return us.db.Close()
	}
	return nil
}

// HourlyUsageRow represents one hour's aggregated usage.
type HourlyUsageRow struct {
	Hour         int     `json:"hour"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CacheRead    int     `json:"cache_read"`
	CacheWrite   int     `json:"cache_write"`
	Cost         float64 `json:"cost"`
	RequestCount int     `json:"request_count"`
}

// DailyUsageRow represents one day's aggregated usage.
type DailyUsageRow struct {
	Date         string  `json:"date"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CacheRead    int     `json:"cache_read"`
	CacheWrite   int     `json:"cache_write"`
	Cost         float64 `json:"cost"`
	RequestCount int     `json:"request_count"`
}

// ModelUsageRow represents usage for a specific model in a date range.
type ModelUsageRow struct {
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CacheRead    int     `json:"cache_read"`
	CacheWrite   int     `json:"cache_write"`
	Cost         float64 `json:"cost"`
	RequestCount int     `json:"request_count"`
}

// UsageRecord represents a single request's usage record.
type UsageRecord struct {
	ID           int64   `json:"id"`
	Timestamp    string  `json:"timestamp"`
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CacheRead    int     `json:"cache_read"`
	CacheWrite   int     `json:"cache_write"`
	Cost         float64 `json:"cost"`
}
