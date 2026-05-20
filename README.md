# Moon Bridge Desktop

A desktop manager for **Moon Bridge** — a unified AI model proxy that routes requests across multiple providers. Built with [Wails](https://wails.io) (Go + Vue 3).

## Features

- **Multi-Provider Support**: Configure DeepSeek, Qwen (通义千问), Anthropic, OpenAI, Google Gemini, OpenRouter, Ollama, and SiliconFlow (硅基流动)
- **System Tray**: Quick model switching from the system tray without opening the app window
- **Usage Tracking**: SQLite-backed token and cost statistics with daily aggregation
- **Codex CLI Integration**: Auto-sync Moon Bridge config to Codex CLI
- **Real-time Metrics**: Live token usage, cache hit rate, and cost monitoring
- **Model Capabilities**: Auto-detect vision, reasoning, coding, long context, and web search capabilities

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.25, Wails v2 |
| Frontend | Vue 3 + TypeScript + Vite |
| Database | SQLite (modernc.org/sqlite) |
| Charts | Apache ECharts |
| State | Pinia |

## Architecture

```
┌──────────────┐     ┌──────────────────────────────────────┐
│   Vue 3 UI   │◄───►│        Wails Runtime (Go)            │
└──────────────┘     ├──────────────────────────────────────┤
                     │  App (app.go)                        │
                     │  ├─ ConfigManager (JSON + YAML)      │
                     │  ├─ SystemTray (Win32 API)           │
                     │  ├─ MBProcess (moonbridge.exe child) │
                     │  ├─ RetryProxy (HTTP, auto-retry)     │
                     │  ├─ MetricsProxy (usage polling)      │
                     │  ├─ UsageStore (SQLite)              │
                     │  └─ CodexConfig (sync to Codex CLI)   │
                     ├──────────────────────────────────────┤
                     │  moonbridge.exe (embedded binary)    │
                     └──────────────────────────────────────┘
```

## Quick Start

### Prerequisites

- [Go](https://go.dev/dl/) 1.25+
- [Node.js](https://nodejs.org/) 18+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation): `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Windows (system tray uses Win32 API)

### Development

```bash
# Install frontend dependencies
cd frontend && npm install

# Run in live development mode (hot reload)
wails dev
```

### Building

```bash
wails build
```

The binary will be generated at `build/bin/moonbridge-desktop.exe`.

### Configuration

On first launch, the app creates a config file at `%APPDATA%\moonbridge-desktop\config.json`. API keys are stored there — they are **never** committed to this repo.

## License

MIT
