package backend

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// KillProcesses kills all processes with the given image name (Windows only).
func KillProcesses(name string) {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("taskkill", "/F", "/IM", name)
		_ = cmd.Run()
	}
}

// globalLogChan is the shared channel for log entries emitted to the frontend.
var globalLogChan = make(chan LogEntry, 1000)

// LogChan returns the global log entry channel.
func LogChan() <-chan LogEntry {
	return globalLogChan
}

// init sets up a custom logger that writes to the frontend via globalLogChan.
var AppLogger *log.Logger

func init() {
	AppLogger = log.New(&logWriter{}, "", 0)
}

// logWriter implements io.Writer and pushes log entries to the frontend channel.
type logWriter struct{}

func (w *logWriter) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))
	if line == "" {
		return len(p), nil
	}
	entry := LogEntry{
		Raw:       line,
		Timestamp: time.Now().Format("15:04:05"),
		Level:     "INFO",
		Message:   line,
	}
	select {
	case globalLogChan <- entry:
	default:
	}
	return len(p), nil
}

// ValidateAndFixSQLiteDB checks if a SQLite database file is valid.
// SQLite files start with "SQLite format 3\000" and are at least 100 bytes.
// If corrupted, removes the file so it can be recreated.
func ValidateAndFixSQLiteDB(dbPath string) bool {
	data, err := os.ReadFile(dbPath)
	if err != nil {
		// File doesn't exist — not corrupted
		return true
	}

	// SQLite database files start with "SQLite format 3\000" and are >= 100 bytes
	if len(data) < 100 || !bytes.Equal(data[:16], []byte("SQLite format 3\x00")) {
		backupPath := dbPath + ".corrupted." + time.Now().Format("20060102150405")
		removed := false
		if err := os.Rename(dbPath, backupPath); err == nil {
			removed = true
		} else if err := os.Remove(dbPath); err == nil {
			removed = true
		} else {
			// File may be locked — truncate to force recreation
			if f, err := os.OpenFile(dbPath, os.O_WRONLY|os.O_TRUNC, 0644); err == nil {
				f.Close()
				_ = os.Remove(dbPath)
				removed = true
			}
		}
		if removed {
			AppLogger.Printf("[SQLite] corrupted DB at %s, backed up to %s", dbPath, backupPath)
		} else {
			AppLogger.Printf("[SQLite] WARNING: failed to handle corrupted DB at %s", dbPath)
		}
	}

	// Remove WAL and SHM journal files
	for _, suffix := range []string{"-wal", "-shm"} {
		journalPath := dbPath + suffix
		if err := os.Remove(journalPath); err == nil {
			AppLogger.Printf("[SQLite] removed journal file %s", journalPath)
		}
	}

	return true
}
