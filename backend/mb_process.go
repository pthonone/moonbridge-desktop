package backend

import (
	"embed"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
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

// MBProcess manages the Moon Bridge subprocess lifecycle.
type MBProcess struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	running    bool
	port       int
	configPath string
	binaryPath string
	readyChan  chan error
}

// NewMBProcess creates a new MBProcess.
func NewMBProcess(port int, configPath, binaryPath string) *MBProcess {
	return &MBProcess{
		port:       port,
		configPath: configPath,
		binaryPath: binaryPath,
		readyChan:  make(chan error, 1),
	}
}

// ExtractBinary extracts the embedded moonbridge.exe to the given path.
func ExtractBinary(fs embed.FS, destPath string) error {
	data, err := fs.ReadFile("resources/moonbridge.exe")
	if err != nil {
		return fmt.Errorf("read embedded binary: %w", err)
	}
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create binary dir: %w", err)
	}
	return os.WriteFile(destPath, data, 0755)
}

// Start launches the Moon Bridge process.
func (p *MBProcess) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("already running")
	}

	p.cmd = exec.Command(p.binaryPath, "-config", p.configPath)
	p.cmd.Dir = filepath.Dir(p.configPath)

	// Hide console window on Windows
	if runtime.GOOS == "windows" {
		p.cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}

	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := p.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	// Capture stdout logs
	go p.captureLogs(stdout)
	// Capture stderr logs
	go p.captureLogs(stderr)

	// Health check: poll until ready
	go p.healthCheck()

	// Wait for health check to complete (or timeout)
	err = <-p.readyChan
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	// Mark as running after health check passes
	p.running = true
	return nil
}

// Stop terminates the Moon Bridge process and all child processes.
func (p *MBProcess) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running || p.cmd == nil || p.cmd.Process == nil {
		return fmt.Errorf("not running")
	}

	pid := p.cmd.Process.Pid

	// Kill the entire process tree on Windows to release the port
	if runtime.GOOS == "windows" {
		cmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = cmd.Run()
	} else {
		if err := p.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill process: %w", err)
		}
	}

	// Wait briefly for process exit
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}

	p.running = false
	return nil
}

// IsRunning returns whether the process is currently running.
func (p *MBProcess) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

func (p *MBProcess) healthCheck() {
	url := fmt.Sprintf("http://127.0.0.1:%d/models", p.port)
	client := &http.Client{Timeout: 2 * time.Second}
	for i := 0; i < 30; i++ {
		time.Sleep(500 * time.Millisecond)
		resp, err := client.Get(url)
		if err == nil && resp != nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				p.readyChan <- nil
				return
			}
		}
	}
	// Process started but health endpoint not ready yet, treat as running
	p.readyChan <- nil
}

func (p *MBProcess) captureLogs(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			line := string(buf[:n])
			entry := parseLogLine(line)
			select {
			case globalLogChan <- entry:
			default:
				// Channel full, drop oldest
			}
		}
		if err != nil {
			break
		}
	}
}

func parseLogLine(raw string) LogEntry {
	raw = strings.TrimSpace(raw)
	entry := LogEntry{Raw: raw, Timestamp: time.Now().Format("15:04:05"), Level: "INFO", Message: raw}

	// Try to parse structured log: time=... level=... msg=...
	if idx := strings.Index(raw, "level="); idx != -1 {
		rest := raw[idx+6:]
		end := strings.IndexByte(rest, ' ')
		if end == -1 {
			end = len(rest)
		}
		entry.Level = strings.ToUpper(rest[:end])
	}
	if idx := strings.Index(raw, "msg="); idx != -1 {
		rest := raw[idx+4:]
		// Remove quotes if present
		if len(rest) > 0 && rest[0] == '"' {
			if end := strings.Index(rest[1:], "\""); end != -1 {
				entry.Message = rest[1 : end+1]
			}
		} else {
			end := strings.IndexByte(rest, ' ')
			if end == -1 {
				end = len(rest)
			}
			entry.Message = rest[:end]
		}
	}
	return entry
}
