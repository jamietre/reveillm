# reveillm — Design Spec

**Date:** 2026-05-30  
**Status:** Approved

## Overview

reveillm is a self-hosted OpenAI-compatible HTTP proxy written in Go. It routes LLM API requests to a prioritised sequence of targets, firing optional pre-call hooks (e.g. Wake-on-LAN) before forwarding when a target machine may be sleeping. If a target fails or times out, it falls back to the next in sequence. Named configurations are selected by the consumer via URL prefix.

Primary use case: prefer a free local GPU (which may be sleeping) and fall back to a paid cloud provider only when the local machine is unavailable.

---

## URL Convention

```
POST http://reveillm:8080/{config-name}/v1/chat/completions
```

The `{config-name}` prefix is stripped before forwarding. A request to `/home/v1/chat/completions` against a target at `http://172.16.2.10:11434` becomes `http://172.16.2.10:11434/v1/chat/completions`.

Additional endpoints:
- `GET /health` — service liveness check (for Docker healthcheck)
- `GET /` — returns list of configured config names (debugging aid)

---

## Config Format

Config is a YAML file at `/etc/reveillm/config.yaml` (overridable via `--config` flag). Environment variable interpolation is supported in string values using `${VAR}` syntax.

```yaml
targets:
  ollama-home:
    url: http://172.16.2.10:11434
    hook: "wakeonlan 10:7C:61:3D:75:AF"
    hook_timeout: 90s
    hook_poll_interval: 5s
    timeout: 30s

  claude:
    url: https://api.anthropic.com
    api_key: "${ANTHROPIC_API_KEY}"
    timeout: 30s

configs:
  home:
    targets:
      - target: ollama-home
        model: llama3.1:70b
      - target: claude
        model: claude-3-5-sonnet-20241022

  fast:
    targets:
      - target: claude
        model: claude-3-5-haiku-20241022
```

### Target fields

| Field | Required | Description |
|---|---|---|
| `url` | yes | Base URL of the target (no trailing slash) |
| `api_key` | no | Sent as `Authorization: Bearer <key>` |
| `timeout` | yes | Request timeout after service is ready |
| `hook` | no | Shell command to run before the first request attempt |
| `hook_timeout` | if hook set | Max time to wait for service to become ready after hook |
| `hook_poll_interval` | if hook set | How often to poll for readiness (default: 5s) |

### Config entry fields

| Field | Required | Description |
|---|---|---|
| `target` | yes | Name of a target defined in `targets:` |
| `model` | yes | Model name to send to this target — consumer's model field is ignored |

### Model handling

The consumer's `model` field in the request body is ignored. Each target entry in a config **must** declare the model to use. This is required because model names are provider-specific (e.g. `llama3.1:70b` is not valid on Claude). The proxy substitutes the `model` field in the JSON body before forwarding.

---

## Request Flow

```
request arrives
  ↓
extract {config-name} from URL → 404 if unknown
  ↓
parse request body (extract fields for rewriting)
  ↓
for each target in config (in order):
  ↓
  has hook?
    → run shell command
    → poll GET {target_url}/ every hook_poll_interval; ready when any HTTP response is received (even 4xx/5xx — a TCP connection error means not yet ready); give up after hook_timeout
    → on timeout: log, try next target
  ↓
  rewrite request:
    - strip config-name prefix from path
    - substitute model field in body
    - set Authorization header from target api_key (if set)
    - set target base URL
  ↓
  forward request, stream response back via io.Copy
    - success (2xx): done
    - failure (timeout / conn error / non-2xx): log, try next target
  ↓
all targets exhausted → 503 with JSON error listing each target and failure reason
```

### Concurrent wake handling

If two requests arrive simultaneously for the same sleeping target, the second request waits on the same wake attempt rather than firing another WoL packet. Implemented via a per-target mutex and shared "waking" state.

### Streaming

Response bodies are piped through via `io.Copy` and never buffered. This is required for SSE streaming completions. If the client disconnects mid-stream, the forwarded request is cancelled via context cancellation.

---

## Adapter Interface

A small `Adapter` interface is defined to allow future format-translating adapters (e.g. for providers that don't speak OpenAI-compatible APIs). The default implementation is a passthrough that only substitutes the model field and rewrites auth/URL.

```go
type Adapter interface {
    Forward(ctx context.Context, req *http.Request, target Target, model string) (*http.Response, error)
}
```

For non-OpenAI-compatible providers in the future, the recommended approach is to point a target at a LiteLLM instance that handles format translation, rather than building translation logic into reveillm.

---

## Project Structure

```
reveillm/
├── cmd/reveillm/
│   └── main.go                 # entry point, flag parsing, wiring
├── internal/
│   ├── config/
│   │   └── config.go           # YAML load/parse/validate, env var interpolation
│   ├── proxy/
│   │   ├── handler.go          # HTTP handler, URL routing, request parsing
│   │   ├── forwarder.go        # streaming HTTP proxy (rewrite + io.Copy)
│   │   └── adapter.go          # Adapter interface + OpenAI passthrough impl
│   ├── hook/
│   │   └── hook.go             # shell exec, readiness poll, per-target mutex
│   └── runner/
│       └── runner.go           # iterates targets, orchestrates hook + forward
├── config.example.yaml
├── Dockerfile
├── docker-compose.yml
└── go.mod
```

---

## Deployment

Docker on the Docker LXC (`172.16.2.18`). Multi-stage Dockerfile: `golang:alpine` build stage → `alpine:3.19` runtime (~20MB). Alpine is used rather than distroless because hook commands require a shell (`sh -c`). `ca-certificates` and `bash` are installed via `apk add`.

```yaml
services:
  reveillm:
    image: reveillm:latest
    # no ports mapping — network_mode: host exposes on host port 8080 directly
    volumes:
      - ./config.yaml:/etc/reveillm/config.yaml:ro
    environment:
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
    restart: unless-stopped
    network_mode: host
```

`network_mode: host` gives the container direct LAN access to reach `172.16.2.10` (MUSIC3) and send WoL packets without Docker network routing complications.

---

## Error Responses

All errors return JSON:

```json
{
  "error": {
    "message": "all targets failed",
    "type": "upstream_error",
    "targets": [
      { "name": "ollama-home", "reason": "hook timeout after 90s" },
      { "name": "claude", "reason": "connection refused" }
    ]
  }
}
```

---

## Out of Scope

- Config hot-reload (restart the container to pick up config changes)
- Authentication on the reveillm endpoint itself (assumed private LAN)
- Request logging / metrics (can be added later)
- HTTP hook type (shell only for now)
