# Moon Bridge Desktop

[Moon Bridge](https://github.com/moonbridge-ai/moonbridge) 桌面管理器，用于统一代理多个 AI 模型提供商的请求。基于 [Wails](https://wails.io)（Go + Vue 3）构建。

## 功能特性

- **多提供商支持**：支持配置 DeepSeek、Qwen（通义千问）、Anthropic、OpenAI、Google Gemini、OpenRouter、Ollama（本地）和硅基流动
- **系统托盘**：无需打开主窗口即可快速切换模型
- **用量追踪**：基于 SQLite 的 token 和费用统计，支持按日聚合
- **Codex CLI 集成**：自动同步 Moon Bridge 配置到 Codex CLI
- **实时指标**：实时显示 token 用量、缓存命中率和费用
- **模型能力检测**：自动识别视觉、推理、编码、长上下文和联网搜索能力

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.25, Wails v2 |
| 前端 | Vue 3 + TypeScript + Vite |
| 数据库 | SQLite (modernc.org/sqlite) |
| 图表 | Apache ECharts |
| 状态管理 | Pinia |

## 架构

```
──────────────┐     ┌──────────────────────────────────────┐
│   Vue 3 UI   │◄───►│        Wails Runtime (Go)            │
└──────────────┘     ├──────────────────────────────────────┤
                     │  App (app.go)                        │
                     │  ├─ ConfigManager (JSON + YAML)      │
                     │  ├─ SystemTray (Win32 API)           │
                     │  ├─ MBProcess (moonbridge.exe 子进程) │
                     │  ├─ RetryProxy (HTTP 自动重试)        │
                     │  ├─ MetricsProxy (用量轮询)           │
                     │  ├─ UsageStore (SQLite)              │
                     │  └─ CodexConfig (同步到 Codex CLI)    │
                     ├──────────────────────────────────────┤
                     │  moonbridge.exe (嵌入的二进制)        │
                     ──────────────────────────────────────┘
```

## 快速开始

### 前置条件

- [Go](https://go.dev/dl/) 1.25+
- [Node.js](https://nodejs.org/) 18+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation)：`go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Windows 系统（系统托盘基于 Win32 API）

### 开发模式

```bash
# 安装前端依赖
cd frontend && npm install

# 热更新开发模式
wails dev
```

### 构建

```bash
wails build
```

可执行文件将生成在 `build/bin/moonbridge-desktop.exe`。

### 配置

首次启动时，应用会在 `%APPDATA%\moonbridge-desktop\config.json` 创建配置文件。API 密钥存储在该文件中，**不会**提交到本仓库。

## 许可证

MIT
