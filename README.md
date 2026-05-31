# reveillm

Self-hosted OpenAI-compatible LLM proxy with pre-call hooks and sequential fallback.

Point any OpenAI-compatible client at `http://reveillm:8080/{config-name}/v1` to route
requests through a prioritised list of LLM targets. If the first target is sleeping,
a pre-call hook (e.g. Wake-on-LAN) fires before the request is forwarded.

## Features

- **Named configs** — consumers select a routing profile via the URL prefix
- **Pre-call hooks** — shell commands run before forwarding (WoL, custom scripts)
- **Sequential fallback** — tries each target in order; moves on after failure or timeout
- **Streaming** — SSE responses flushed to the client as they arrive
- **Per-target model mapping** — each provider gets the right model name automatically

## Quick start

```bash
cp config.example.yaml config.yaml
# edit config.yaml
ANTHROPIC_API_KEY=sk-... docker compose up -d
```

Configure any OpenAI-compatible client:

```python
from openai import OpenAI
client = OpenAI(base_url="http://localhost:8080/home/v1", api_key="ignored")
```

## Configuration

See [`config.example.yaml`](config.example.yaml) for a fully annotated example.

`model` is **required** on every target entry — it replaces the model field in the
forwarded request. This is necessary because model names differ between providers
(e.g. `llama3.1:70b` is not valid on Claude).

## Development

```bash
go test ./...
go build ./cmd/reveillm
./reveillm --config config.example.yaml
```

## Deployment

```bash
docker compose up -d
```

Runs on port 8080. Uses `network_mode: host` so the container can reach LAN machines
(including sending WoL packets) without extra Docker network config.
