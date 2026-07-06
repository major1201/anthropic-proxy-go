# Anthropic Proxy (Go)

[![Go 1.24+](https://img.shields.io/badge/go-1.24+-00ADD8.svg)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

High-performance proxy service that converts Anthropic API requests to OpenAI API format. Call OpenAI-compatible models using existing Anthropic client code.

[中文版本](README_zh.md)

## Core Features

- **Seamless Compatibility** — Use standard Anthropic clients to call OpenAI models
- **Full Functionality** — Supports text, tool calls, streaming (SSE), thinking/reasoning content
- **Intelligent Routing** — Route requests to different OpenAI backends based on model name
- **Hot Reload** — Config file changes take effect without restarting
- **Structured Logging** — Detailed request/response logs with request ID tracking
- **Error Mapping** — OpenAI error codes mapped to Anthropic-compatible error responses

## Quick Start

### Requirements

- Go 1.24+
- An OpenAI-compatible API backend

### Build

```bash
git clone https://github.com/major1201/anthropic-proxy-go.git
cd anthropic-proxy-go
go build -o bin/proxy ./cmd/proxy/
```

### Configuration

1. Copy the example config:
```bash
cp config/example.json config/settings.json
```

2. Edit `config/settings.json`:
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

### Run

```bash
# From source
go run ./cmd/proxy/ --config config/settings.json

# Or with the built binary
./bin/proxy --config config/settings.json
```

### Docker

```bash
docker build -t anthropic-proxy-go .
docker run -p 8000:8000 -v ./config:/app/config anthropic-proxy-go
```

The service starts at `http://localhost:8000`.

## Usage

### Claude Code Integration

Configure Claude Code to use this proxy by editing `.claude/settings.json`:

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
    messages=[{"role": "user", "content": "Hello!"}],
    max_tokens=1024
)

print(response.content[0].text)
```

### Streaming

```python
stream = client.messages.create(
    model="claude-sonnet-4-20250514",
    messages=[{"role": "user", "content": "Tell me a story"}],
    max_tokens=1024,
    stream=True
)

for event in stream:
    if event.type == "content_block_delta":
        print(event.delta.text, end="", flush=True)
```

### Tool Calls

```python
tools = [
    {
        "name": "get_weather",
        "description": "Get current weather for a city",
        "input_schema": {
            "type": "object",
            "properties": {
                "city": {"type": "string", "description": "City name"}
            },
            "required": ["city"]
        }
    }
]

response = client.messages.create(
    model="claude-sonnet-4-20250514",
    messages=[{"role": "user", "content": "What's the weather in Tokyo?"}],
    tools=tools,
    tool_choice={"type": "auto"}
)
```

## Project Structure

```
anthropic-proxy-go/
├── cmd/proxy/main.go              # Entry point
├── internal/
│   ├── config/
│   │   ├── config.go              # Config loading, route lookup
│   │   └── watcher.go            # fsnotify hot reload
│   ├── models/
│   │   ├── anthropic.go          # Anthropic API types
│   │   ├── openai.go             # OpenAI API types
│   │   └── errors.go             # Error response types
│   ├── converter/
│   │   ├── request.go            # Anthropic → OpenAI request
│   │   ├── response.go           # OpenAI → Anthropic response
│   │   └── stream.go             # SSE streaming conversion
│   ├── client/
│   │   └── openai.go             # OpenAI HTTP client
│   ├── handler/
│   │   └── messages.go           # /v1/messages handler
│   ├── middleware/
│   │   ├── auth.go               # API key validation
│   │   └── timing.go             # Request ID, timing, logging
│   └── common/
│       ├── logging.go            # slog setup, request IDs
│       ├── token_cache.go        # In-memory token cache
│       └── token_counter.go      # tiktoken-go wrapper
├── config/example.json           # Example config
├── tests/integration/            # Integration tests
├── Dockerfile
├── Makefile
└── go.mod
```

## Configuration Reference

### `server`
| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `host` | string | `0.0.0.0` | Listen address |
| `port` | int | `8000` | Listen port |

### `api_key`
API key clients must provide to access the proxy (via `x-api-key` header or `Authorization: Bearer`).

### `logging`
| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `level` | string | `INFO` | Log level: `DEBUG`, `INFO`, `WARN`, `ERROR` |

### `routes`
Map of Anthropic model name → OpenAI backend config. The `__default__` key acts as a fallback.

Each route:
| Field | Type | Description |
|-------|------|-------------|
| `base_url` | string | OpenAI-compatible API base URL |
| `api_key` | string | API key for the backend |
| `model` | string | Target model name to send to the backend |

### `parameter_overrides`
Force specific parameter values regardless of client requests:

| Field | Type | Description |
|-------|------|-------------|
| `max_tokens` | int/null | Override max output tokens |
| `temperature` | float/null | Override temperature (0.0–1.0) |
| `top_p` | float/null | Override top_p (0.0–1.0) |
| `top_k` | int/null | Override top_k |

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/messages` | Anthropic Messages API |
| `GET` | `/health` | Health check |
| `GET` | `/` | Welcome page |

## Testing

```bash
make test              # All tests with race detection and coverage
make test-integration  # Integration tests only
```

## Conversion Rules

| Anthropic | OpenAI |
|-----------|--------|
| `tool_choice: "any"` | `"required"` |
| `tool_choice: "auto"` | `"auto"` |
| `finish_reason: "end_turn"` | `"stop"` |
| `finish_reason: "max_tokens"` | `"length"` |
| `finish_reason: "tool_use"` | `"tool_calls"` |
| Tool `{name, description, input_schema}` | `{type: "function", function: {name, description, parameters}}` |
| Content block `tool_use` | Assistant message with `tool_calls` |
| Content block `tool_result` | `role: "tool"` message |
| `system` (string or list) | System role messages |

## Acknowledgements

- [claude-code-router](https://github.com/musistudio/claude-code-router) — Original inspiration
- [Anthropic](https://www.anthropic.com/) — Claude API specification
- [OpenAI](https://openai.com/) — OpenAI API specification

## License

MIT — see [LICENSE](LICENSE) for details.
