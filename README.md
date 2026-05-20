# Moon Bridge Desktop

[Moon Bridge](https://github.com/moonbridge-ai/moonbridge) 桌面管理器，用于统一代理多个 AI 模型提供商的请求。基于 [Wails](https://wails.io)（Go + Vue 3）构建。

## 功能特性

- **多提供商支持**：支持配置 DeepSeek、Qwen（通义千问）、Anthropic、OpenAI、Google Gemini、OpenRouter、Ollama（本地）和硅基流动
- **系统托盘**：无需打开主窗口即可快速切换模型
- **用量追踪**：基于 SQLite 的 token 和费用统计，支持按日聚合
- **Codex CLI 集成**：自动同步 Moon Bridge 配置到 Codex CLI
- **实时指标**：实时显示 token 用量、缓存命中率和费用
- **模型能力检测**：自动识别视觉、推理、编码、长上下文和联网搜索能力

## 使用指南

### 1. 快速入门

#### 安装

1. 克隆仓库：`git clone https://github.com/pthonone/moonbridge-desktop.git`
2. 安装依赖并构建：
   ```bash
   cd moonbridge-desktop
   cd frontend && npm install && cd ..
   wails build
   ```
3. 运行 `build\bin\moonbridge-desktop.exe`

#### 首次使用

1. 启动程序后，点击「**设置**」标签页
2. 在提供商列表中选择你想添加的服务商（如 DeepSeek、Qwen 等），点击「**添加**」
3. 填写你的 API Key
4. 点击「**启动服务**」按钮，代理服务将在 `localhost:38440` 启动
5. 在右侧模型列表中选择你想要的模型
6. 将你的 AI 客户端（如 Codex CLI、Cursor 等）的 base URL 指向 `http://127.0.0.1:38440/v1`

### 2. 配置说明

#### 配置文件位置

配置文件保存在 `%APPDATA%\moonbridge-desktop\config.json`

#### 配置参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `port` | 代理服务对外端口 | `38440` |
| `internal_port` | Moon Bridge 内部监听端口 | `port + 1` |
| `log_level` | 日志级别 | `info` |
| `default_route` | 默认路由（模型别名） | 第一个模型的 alias |
| `max_tokens` | 最大输出 token 数 | `65536` |
| `metrics_enabled` | 是否启用指标采集 | `true` |

#### 提供商配置示例

每个提供商包含以下字段：

```json
{
  "key": "deepseek",
  "base_url": "https://api.deepseek.com/anthropic",
  "api_key": "你的API密钥",
  "protocol": "anthropic",
  "version": "2023-06-01",
  "offers": [
    {
      "model": "deepseek-v4-pro",
      "pricing": {
        "input": 2,
        "output": 8,
        "cache_write": 1,
        "cache_read": 0.2,
        "billing_mode": "token"
      }
    }
  ]
}
```

#### 支持的协议类型

| 协议值 | 说明 |
|--------|------|
| `anthropic` | Anthropic Claude API 格式 |
| `openai-chat` | OpenAI Chat Completions API 格式 |
| `openai-response` | OpenAI Responses API 格式 |
| `google-genai` | Google Generative AI API 格式 |

### 3. 日常操作

#### 启动/停止服务

- **启动**：点击「启动服务」按钮
- **停止**：点击「停止服务」按钮
- **托盘操作**：右键点击托盘图标，可选择模型或退出

#### 切换模型

**方式一**：在应用主界面「模型」标签页中点击对应模型卡片

**方式二**：右键点击系统托盘图标，从菜单中选择模型

**方式三**：修改配置文件中的 `default_route` 字段后重启服务

#### 查看用量

- **用量统计**：首页显示今日 token 用量和费用
- **历史数据**：点击「用量」标签页查看按日聚合的用量趋势图
- **清除数据**：可清除当天、指定日期范围或全部用量记录

### 4. Codex CLI 集成

如果你的系统已安装 [Codex CLI](https://github.com/openai/codex-cli)，Moon Bridge Desktop 可自动同步配置：

1. 确保已安装 Codex CLI 且存在 `~/.codex/config.toml`
2. 在 Moon Bridge Desktop 中点击「同步 Codex 配置」按钮
3. 之后在 Codex CLI 中使用 `moonbridge` 作为 model provider 即可

## 环境要求

### 运行环境

- **操作系统**：Windows 10/11（系统托盘依赖 Win32 API）
- **无需安装 Go/Node.js**：可直接使用编译后的 `.exe` 文件

### 开发环境

| 工具 | 版本要求 | 说明 |
|------|---------|------|
| Go | 1.25+ | 后端编译 |
| Node.js | 18+ | 前端编译 |
| npm | 9+ | 前端包管理 |
| Wails CLI | 2.12+ | 桌面应用构建 |
| WebView2 Runtime | 系统自带 | Windows 11 默认已安装 |

### 安装开发工具

```bash
# 安装 Go
# 访问 https://go.dev/dl/ 下载安装

# 安装 Node.js
# 访问 https://nodejs.org/ 下载安装

# 安装 Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# 安装 Go 依赖
go mod download

# 安装前端依赖
cd frontend && npm install
```

## 项目结构

```
moonbridge-desktop/
├── main.go                  # Wails 入口
├── app.go                   # 应用主逻辑（服务生命周期、配置管理）
── go.mod                   # Go 模块定义
├── wails.json               # Wails 项目配置
├── .gitignore               # Git 忽略规则
├── build/                   # 构建资源
│   ├── windows/             # Windows 图标和清单
│   ── bin/                 # 编译输出（已忽略）
├── backend/                 # Go 后端包
│   ├── types.go             # 数据类型定义
│   ├── config_manager.go    # 配置文件管理
│   ├── codex_config.go      # Codex CLI 配置同步
│   ├── mb_process.go        # Moon Bridge 进程管理
│   ├── retry_proxy.go       # HTTP 重试代理
│   ├── metrics_proxy.go     # 指标轮询代理
│   ├── usage_store.go       # SQLite 用量存储
│   ├── usage_tracker.go     # 用量跟踪
│   └── systray.go           # 系统托盘（Win32 API）
├── frontend/                # Vue 3 前端
│   ├── src/
│   │   ├── App.vue          # 主应用组件
│   │   ├── components/      # 子组件
│   │   │   ├── Sidebar.vue          # 侧边栏导航
│   │   │   ├── Dashboard.vue        # 仪表盘
│   │   │   ├── ModelCard.vue        # 模型卡片
│   │   │   ├── ProviderEditor.vue   # 提供商编辑器
│   │   │   └── LogViewer.vue        # 日志查看器
│   │   ├── wails.ts         # Wails 绑定封装
│   │   └── style.css        # 全局样式
│   ├── package.json         # 前端依赖
│   ── vite.config.ts       # Vite 配置
└── resources/               # 嵌入资源
    └── moonbridge.exe       # Moon Bridge 代理二进制
```

## 依赖项目

### 核心依赖

| 项目 | 版本 | 说明 |
|------|------|------|
| [Wails](https://github.com/wailsapp/wails) | v2.12.0 | Go + Web 桌面应用框架 |
| [Vue](https://github.com/vuejs/core) | 3.x | 前端响应式框架 |
| [Vite](https://github.com/vitejs/vite) | 3.x | 前端构建工具 |
| [Pinia](https://github.com/vuejs/pinia) | 3.x | Vue 状态管理 |
| [ECharts](https://github.com/apache/echarts) | 6.x | 数据可视化图表库 |
| [SQLite](https://github.com/ncruces/go-sqlite3) | 1.50.1 | 嵌入式数据库 |

### 间接依赖

| 项目 | 用途 |
|------|------|
| [Echo v4](https://github.com/labstack/echo) | HTTP 代理服务器（Wails 内置） |
| [websocket](https://github.com/gorilla/websocket) | 前后端通信（Wails 内置） |
| [go-reflector](https://github.com/tkrajina/go-reflector) | 反射工具 |
| [lo](https://github.com/samber/lo) | Lodash for Go |
| [go-toast](https://git.sr.ht/~jackmordaunt/go-toast) | 桌面通知 |

## 许可证

MIT
