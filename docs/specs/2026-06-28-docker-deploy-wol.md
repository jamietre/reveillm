# reveillm: Docker Deployment + Native WoL

## Context

reveillm has never been deployed. The first deployment target is the docker LXC (`172.16.2.18`). This spec covers two coupled changes: native Wake-on-LAN support in Go (so the image has no external binary dependencies), and the homelab deployment artifacts.

## Native WoL Implementation

### Config change

Add an optional `wol` field to the target config, mutually exclusive with `hook`:

```yaml
targets:
  ollama-home:
    url: http://192.168.1.100:11434
    wol: "AA:BB:CC:DD:EE:FF"
    hook_timeout: 90s
    hook_poll_interval: 5s
    timeout: 120s
```

Validation rejects a target that specifies both `wol` and `hook`.

### WoL packet sender

New package `internal/wol` with a single exported function:

```go
func Wake(mac string) error
```

Parses the MAC address (colon-separated hex), builds the magic packet (6 bytes `0xFF` then the 6-byte MAC repeated 16 times = 102 bytes total), and sends it as a single UDP datagram to `255.255.255.255:9`. Uses `net.Dial("udp", "255.255.255.255:9")` — works correctly under `network_mode: host`.

### Runner integration

The runner currently executes `hook` as `sh -c "<hook>"` before polling the target URL. When a target has `wol` instead, the runner calls `wol.Wake(mac)` directly. The pre-call poll loop (`hook_timeout`, `hook_poll_interval`) is unchanged — it already handles waiting for the machine to respond.

### Dockerfile

No change needed — native Go, no external packages required.

### Config example update

Replace the `hook: "wakeonlan ..."` line in `config.example.yaml` with `wol: "AA:BB:CC:DD:EE:FF"`.

## Deployment

### Image build

`Makefile` at the repo root with a `push` target:

```makefile
REGISTRY ?= 172.16.2.18:5000
IMAGE     := $(REGISTRY)/reveillm

push:
	docker build -t $(IMAGE):latest .
	docker push $(IMAGE):latest
```

Run `make push` from WSL to build and publish. The registry is the local registry already running on docker.lan.

### Homelab repo

New directory `homelab/docker-lxc/docker/stacks/reveillm/` containing:

**`docker-compose.yml`**
```yaml
services:
  reveillm:
    image: 172.16.2.18:5000/reveillm:latest
    network_mode: host
    volumes:
      - ./config.yaml:/etc/reveillm/config.yaml:ro
    environment:
      - ANTHROPIC_API_KEY
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/health"]
      interval: 30s
      timeout: 5s
      retries: 3
```

**`config.yaml`** — the live config with real network values (IPs, MACs, model names). Committed to the homelab repo (no secrets; API keys come from `.env`).

**`.env`** — lives only on docker.lan at `/docker/stacks/reveillm/.env`, never committed to git. Docker Compose loads it automatically:
```
ANTHROPIC_API_KEY=sk-...
```

The docker-compose.yml passes `ANTHROPIC_API_KEY` through via the environment block so it reaches the container from `.env`.

### Deploy.sh integration

Add to `homelab/docker-lxc/deploy.conf`:
```
COPY docker/stacks/reveillm/docker-compose.yml -> /docker/stacks/reveillm/docker-compose.yml
COPY docker/stacks/reveillm/config.yaml -> /docker/stacks/reveillm/config.yaml
RUN cd /docker/stacks/reveillm && docker compose pull && docker compose up -d
```

`.env` is excluded from the COPY — it is created manually on docker.lan once and never touched by deploy.

## Testing

1. `go test ./...` passes with the new `wol` config field and `internal/wol` package
2. `make push` completes and image appears at `172.16.2.18:5000/reveillm:latest`
3. On docker.lan: `docker compose up -d` starts the container; `curl http://localhost:8080/health` returns 200
4. Make a request to the `ollama-home` config while MUSIC3 is sleeping — verify reveillm sends the WoL packet and the machine wakes before the request is forwarded
