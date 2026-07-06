# Anthropic Proxy (Go)

[![Go 1.24+](https://img.shields.io/badge/go-1.24+-00ADD8.svg)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

高性能代理服务，将 Anthropic API 请求转换为 OpenAI API 格式。让你可以使用现有的 Anthropic 客户端代码调用 OpenAI 兼容模型。

[English](README.md)

## 核心功能

- **无缝兼容** — 使用标准 Anthropic 客户端调用 OpenAI 模型
- **功能完整** — 支持文本、工具调用、流式响应（SSE）、思考/推理内容
- **智能路由** — 根据模型名称将请求路由到不同的 OpenAI 后端
- **热重载** — 配置文件变更无需重启即可生效
- **结构化日志** — 详细的请求/响应日志，带请求 ID 追踪
- **错误映射** — OpenAI 错误码映射为 Anthropic 兼容的错误响应

## 快速开始

### 环境要求

- Go 1.24+
- 一个 OpenAI 兼容的 API 后端

### 构建

```bash
git clone https://github.com/major1201/anthropic-proxy-go.git
cd anthropic-proxy-go
go build -o bin/proxy ./cmd/proxy/
```

### 配置

1. 复制示例配置文件：
```bash
cp config/example.json config/settings.json
```

2. 编辑 `config/settings.json`：
```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 8000
  },
  "api_key": "your-proxy-api-key-here",
  "logging": {
    "level": "INFO"
  },
  "routes": {
    "__default__": {
      "base_url": "https://api.openai.com/v1",
      "api_key": "your-openai-api-key-here",
      "model": "gpt-4o"
    }
  }
}
```

### 运行

```bash
# 源码运行
go run ./cmd/proxy/ --config config/settings.json

# 或使用编译后的二进制文件
./bin/proxy --config config/settings.json
```

### Docker

```bash
docker build -t anthropic-proxy-go .
docker run -p 8000:8000 -v ./config:/app/config anthropic-proxy-go
```

服务启动在 `http://localhost:8000`。

## 使用方法

### Claude Code 集成

编辑 `.claude/settings.json` 配置 Claude Code 使用此代理：

```json
{
    "env": {
        "ANTHROPIC_API_KEY": "your-proxy-api-key-here",
        "ANTHROPIC_BASE_URL": "http://127.0.0.1:8000",
        "DISABLE_TELEMETRY": "1",
        "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"
    },
    "apiKeyHelper": "echo 'your-proxy-api-key-here'"
}
```

### Anthropic Python SDK

```python
from anthropic import Anthropic

client = Anthropic(
    base_url="http://localhost:8000/v1",
    api_key="your-proxy-api-key-here"
)

response = client.messages.create(
    model="claude-sonnet-4-20250514",
    messages=[{"role": "user", "content": "你好！"}],
    max_tokens=1024
)

print(response.content[0].text)
```

### 流式响应

```python
stream = client.messages.create(
    model="claude-sonnet-4-20250514",
    messages=[{"role": "user", "content": "给我讲个故事"}],
    max_tokens=1024,
    stream=True
)

for event in stream:
    if event.type == "content_block_delta":
        print(event.delta.text, end="", flush=True)
```

### 工具调用

```python
tools = [
    {
        "name": "get_weather",
        "description": "获取指定城市的当前天气",
        "input_schema": {
            "type": "object",
            "properties": {
                "city": {"type": "string", "description": "城市名称"}
            },
            "required": ["city"]
        }
    }
]

response = client.messages.create(
    model="claude-sonnet-4-20250514",
    messages=[{"role": "user", "content": "东京现在天气怎么样？"}],
    tools=tools,
    tool_choice={"type": "auto"}
)
```

## 项目结构

```
anthropic-proxy-go/
├── cmd/proxy/main.go              # 入口文件
├── internal/
│   ├── config/
│   │   ├── config.go              # 配置加载、路由查找
│   │   └── watcher.go            # fsnotify 热重载
│   ├── models/
│   │   ├── anthropic.go          # Anthropic API 类型定义
│   │   ├── openai.go             # OpenAI API 类型定义
│   │   └── errors.go             # 错误响应类型定义
│   ├── converter/
│   │   ├── request.go            # Anthropic → OpenAI 请求转换
│   │   ├── response.go           # OpenAI → Anthropic 响应转换
│   │   └── stream.go             # SSE 流式转换
│   ├── client/
│   │   └── openai.go             # OpenAI HTTP 客户端
│   ├── handler/
│   │   └── messages.go           # /v1/messages 处理器
│   ├── middleware/
│   │   ├── auth.go               # API 密钥验证
│   │   └── timing.go             # 请求 ID、计时、日志
│   └── common/
│       ├── logging.go            # slog 配置、请求 ID
│       ├── token_cache.go        # 内存 token 缓存
│       └── token_counter.go      # tiktoken-go 封装
├── config/example.json           # 示例配置
├── tests/integration/            # 集成测试
├── Dockerfile
├── Makefile
└── go.mod
```

## 配置参考

### `server`
| 字段 | 类型 | 默认值 | 说明 |
|-------|------|--------|------|
| `host` | string | `0.0.0.0` | 监听地址 |
| `port` | int | `8000` | 监听端口 |

### `api_key`
客户端访问代理时需要提供的 API 密钥（通过 `x-api-key` 请求头或 `Authorization: Bearer` 请求头）。

### `logging`
| 字段 | 类型 | 默认值 | 说明 |
|-------|------|--------|------|
| `level` | string | `INFO` | 日志级别：`DEBUG`、`INFO`、`WARN`、`ERROR` |

### `routes`
Anthropic 模型名称 → OpenAI 后端配置的映射表。`__default__` 键作为兜底路由。

每个路由配置：
| 字段 | 类型 | 说明 |
|-------|------|------|
| `base_url` | string | OpenAI 兼容 API 的基础 URL |
| `api_key` | string | 访问后端的 API 密钥 |
| `model` | string | 发送给后端的目标模型名称 |

### `parameter_overrides`
强制覆盖客户端请求中的参数值：

| 字段 | 类型 | 说明 |
|-------|------|------|
| `max_tokens` | int/null | 覆盖最大输出 token 数 |
| `temperature` | float/null | 覆盖温度参数（0.0–1.0） |
| `top_p` | float/null | 覆盖 top_p 参数（0.0–1.0） |
| `top_k` | int/null | 覆盖 top_k 参数 |

## API 端点

| 方法 | 路径 | 说明 |
|--------|------|------|
| `POST` | `/v1/messages` | Anthropic Messages API |
| `GET` | `/health` | 健康检查 |
| `GET` | `/` | 欢迎页面 |

## 测试

```bash
make test              # 全部测试（含竞态检测和覆盖率）
make test-integration  # 仅集成测试
```

## 转换规则

| Anthropic | OpenAI |
|-----------|--------|
| `tool_choice: "any"` | `"required"` |
| `tool_choice: "auto"` | `"auto"` |
| `finish_reason: "end_turn"` | `"stop"` |
| `finish_reason: "max_tokens"` | `"length"` |
| `finish_reason: "tool_use"` | `"tool_calls"` |
| 工具 `{name, description, input_schema}` | `{type: "function", function: {name, description, parameters}}` |
| 内容块 `tool_use` | 助手消息带 `tool_calls` |
| 内容块 `tool_result` | `role: "tool"` 消息 |
| `system`（字符串或列表） | 系统角色消息 |

## 致谢

- [claude-code-router](https://github.com/musistudio/claude-code-router) — 原始灵感来源
- [Anthropic](https://www.anthropic.com/) — Claude API 规范
- [OpenAI](https://openai.com/) — OpenAI API 规范

## 许可证

MIT — 详见 [LICENSE](LICENSE)。
