# Docker Deployment + Native WoL Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add native Wake-on-LAN support to reveillm (no external binary dependencies) and add homelab deployment artifacts for docker.lan.

**Architecture:** A new `internal/wol` package provides `buildMagicPacket` (pure, testable) and `Wake` (sends UDP). The hook manager's `Run` method is refactored to accept `func() error` instead of a shell command string — the runner constructs either a WoL action or a shell exec action based on target config. Deployment uses the existing homelab `deploy.sh` pattern with a local Docker registry.

**Tech Stack:** Go 1.22, `net` stdlib (UDP), Docker, homelab `deploy.sh` rsync pattern, local registry at `172.16.2.18:5000`

---

## File Map

| File | Change |
|------|--------|
| `internal/wol/wol.go` | **Create** — `buildMagicPacket(mac string) ([]byte, error)` and `Wake(mac string) error` |
| `internal/wol/wol_test.go` | **Create** — tests for packet building and MAC validation |
| `internal/config/config.go` | **Modify** — add `WoL string` to `Target`, validation (wol+hook mutually exclusive), default poll interval for wol targets |
| `internal/config/config_test.go` | **Modify** — tests for `wol` field, mutual exclusion |
| `internal/hook/hook.go` | **Modify** — change `Run` and `doWake` to accept `action func() error` instead of `cmd string` |
| `internal/hook/hook_test.go` | **Modify** — update all `m.Run(...)` calls to pass `func() error` |
| `internal/runner/runner.go` | **Modify** — construct wol or shell action based on target config; add `os/exec` import |
| `config.example.yaml` | **Modify** — replace `hook: "wakeonlan ..."` with `wol: "AA:BB:CC:DD:EE:FF"` |
| `Makefile` | **Create** — `push` target to build and push to `172.16.2.18:5000/reveillm:latest` |
| `homelab/docker-lxc/docker/stacks/reveillm/docker-compose.yml` | **Create** (in homelab repo) |
| `homelab/docker-lxc/docker/stacks/reveillm/config.yaml` | **Create** (in homelab repo) |
| `homelab/docker-lxc/deploy.conf` | **Modify** (in homelab repo) — add COPY/RUN entries for reveillm |

---

## Task 1: `internal/wol` — magic packet builder

**Files:**
- Create: `internal/wol/wol.go`
- Create: `internal/wol/wol_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/wol/wol_test.go
package wol_test

import (
	"testing"

	"github.com/jamietre/reveillm/internal/wol"
)

func TestBuildMagicPacket_valid(t *testing.T) {
	pkt, err := wol.BuildMagicPacket("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkt) != 102 {
		t.Fatalf("want 102 bytes, got %d", len(pkt))
	}
	// first 6 bytes must be 0xFF
	for i := 0; i < 6; i++ {
		if pkt[i] != 0xFF {
			t.Errorf("byte %d: want 0xFF, got 0x%02X", i, pkt[i])
		}
	}
	// MAC repeated 16 times starting at byte 6
	mac := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	for rep := 0; rep < 16; rep++ {
		for i, b := range mac {
			off := 6 + rep*6 + i
			if pkt[off] != b {
				t.Errorf("rep %d byte %d: want 0x%02X, got 0x%02X", rep, i, b, pkt[off])
			}
		}
	}
}

func TestBuildMagicPacket_invalidMAC(t *testing.T) {
	cases := []string{"", "ZZ:ZZ:ZZ:ZZ:ZZ:ZZ", "AA:BB:CC:DD:EE", "not-a-mac"}
	for _, tc := range cases {
		_, err := wol.BuildMagicPacket(tc)
		if err == nil {
			t.Errorf("input %q: expected error, got nil", tc)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./internal/wol/...
```
Expected: compile error — package does not exist yet.

- [ ] **Step 3: Implement `internal/wol/wol.go`**

```go
package wol

import (
	"encoding/hex"
	"fmt"
	"net"
	"strings"
)

// BuildMagicPacket constructs a WoL magic packet for the given MAC address.
// mac must be colon-separated hex, e.g. "AA:BB:CC:DD:EE:FF".
func BuildMagicPacket(mac string) ([]byte, error) {
	parts := strings.Split(mac, ":")
	if len(parts) != 6 {
		return nil, fmt.Errorf("invalid MAC address %q: must be 6 colon-separated hex bytes", mac)
	}
	hw := make([]byte, 6)
	for i, p := range parts {
		b, err := hex.DecodeString(p)
		if err != nil || len(b) != 1 {
			return nil, fmt.Errorf("invalid MAC address %q: %w", mac, err)
		}
		hw[i] = b[0]
	}

	pkt := make([]byte, 102)
	for i := 0; i < 6; i++ {
		pkt[i] = 0xFF
	}
	for rep := 0; rep < 16; rep++ {
		copy(pkt[6+rep*6:], hw)
	}
	return pkt, nil
}

// Wake sends a WoL magic packet for mac to the UDP broadcast address on port 9.
func Wake(mac string) error {
	pkt, err := BuildMagicPacket(mac)
	if err != nil {
		return err
	}
	conn, err := net.Dial("udp", "255.255.255.255:9")
	if err != nil {
		return fmt.Errorf("wol: opening UDP socket: %w", err)
	}
	defer conn.Close()
	_, err = conn.Write(pkt)
	return err
}
```

- [ ] **Step 4: Run tests**

```
go test ./internal/wol/...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/wol/
git commit -m "feat: native WoL magic packet sender"
```

---

## Task 2: Config — `wol` field, validation, default poll interval

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/config/config_test.go` (after the existing tests):

```go
func TestLoad_wolField(t *testing.T) {
	yaml := `
targets:
  t:
    url: http://192.168.1.100:11434
    wol: "AA:BB:CC:DD:EE:FF"
    hook_timeout: 90s
    hook_poll_interval: 5s
    timeout: 30s
configs:
  c:
    targets:
      - target: t
        model: llama3.1:70b
`
	cfg, err := config.Load(writeTemp(t, yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Targets["t"].WoL != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("want wol field set, got %q", cfg.Targets["t"].WoL)
	}
}

func TestLoad_wolDefaultPollInterval(t *testing.T) {
	yaml := `
targets:
  t:
    url: http://192.168.1.100:11434
    wol: "AA:BB:CC:DD:EE:FF"
    hook_timeout: 90s
    timeout: 30s
configs:
  c:
    targets:
      - target: t
        model: m
`
	cfg, err := config.Load(writeTemp(t, yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Targets["t"].HookPollInterval != 5*time.Second {
		t.Errorf("want default poll interval 5s, got %v", cfg.Targets["t"].HookPollInterval)
	}
}

func TestLoad_wolAndHookMutuallyExclusive(t *testing.T) {
	yaml := `
targets:
  t:
    url: http://192.168.1.100:11434
    hook: "echo hi"
    wol: "AA:BB:CC:DD:EE:FF"
    hook_timeout: 30s
    timeout: 10s
configs:
  c:
    targets:
      - target: t
        model: m
`
	_, err := config.Load(writeTemp(t, yaml))
	if err == nil {
		t.Fatal("expected error when both hook and wol are set")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./internal/config/...
```
Expected: FAIL — `cfg.Targets["t"].WoL` field does not exist.

- [ ] **Step 3: Update `internal/config/config.go`**

Add `WoL` to `Target`:

```go
type Target struct {
	URL              string        `yaml:"url"`
	APIKey           string        `yaml:"api_key"`
	Timeout          time.Duration `yaml:"timeout"`
	Hook             string        `yaml:"hook"`
	WoL              string        `yaml:"wol"`
	HookTimeout      time.Duration `yaml:"hook_timeout"`
	HookPollInterval time.Duration `yaml:"hook_poll_interval"`
}
```

Update the default poll interval block in `Load` (replace the existing one):

```go
for name, t := range cfg.Targets {
	if (t.Hook != "" || t.WoL != "") && t.HookPollInterval == 0 {
		t.HookPollInterval = 5 * time.Second
	}
	cfg.Targets[name] = t
}
```

Add mutual exclusion check to `validate`:

```go
func validate(cfg *Config) error {
	for name, t := range cfg.Targets {
		if t.URL == "" {
			return fmt.Errorf("target %q: url is required", name)
		}
		if t.Hook != "" && t.WoL != "" {
			return fmt.Errorf("target %q: hook and wol are mutually exclusive", name)
		}
	}
	for cfgName, route := range cfg.Configs {
		for _, entry := range route.Targets {
			if _, ok := cfg.Targets[entry.Target]; !ok {
				return fmt.Errorf("config %q references unknown target %q", cfgName, entry.Target)
			}
			if entry.Model == "" {
				return fmt.Errorf("config %q target %q: model is required", cfgName, entry.Target)
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

```
go test ./internal/config/...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: add wol field to target config"
```

---

## Task 3: Hook manager — accept `func() error` instead of command string

**Files:**
- Modify: `internal/hook/hook.go`
- Modify: `internal/hook/hook_test.go`

- [ ] **Step 1: Update `internal/hook/hook.go`**

Change `Run` and `doWake` to accept `action func() error`:

```go
package hook

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type waker struct {
	mu      sync.Mutex
	active  bool
	waiters []chan error
}

// Manager coalesces concurrent wake attempts for the same target.
type Manager struct {
	mu     sync.Mutex
	wakers map[string]*waker
}

func NewManager() *Manager {
	return &Manager{wakers: make(map[string]*waker)}
}

// Run executes action, then polls pollURL every pollInterval until any HTTP
// response is received. Returns an error if the action fails or hookTimeout is
// exceeded. Concurrent calls for the same targetName coalesce: the second
// caller waits for the first wake attempt rather than firing again.
func (m *Manager) Run(ctx context.Context, targetName string, action func() error, pollURL string, hookTimeout, pollInterval time.Duration) error {
	m.mu.Lock()
	w, ok := m.wakers[targetName]
	if !ok {
		w = &waker{}
		m.wakers[targetName] = w
	}
	m.mu.Unlock()

	w.mu.Lock()
	if w.active {
		ch := make(chan error, 1)
		w.waiters = append(w.waiters, ch)
		w.mu.Unlock()
		select {
		case err := <-ch:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	w.active = true
	w.mu.Unlock()

	err := doWake(ctx, action, pollURL, hookTimeout, pollInterval)

	w.mu.Lock()
	w.active = false
	for _, ch := range w.waiters {
		ch <- err
	}
	w.waiters = nil
	w.mu.Unlock()

	return err
}

func doWake(ctx context.Context, action func() error, pollURL string, hookTimeout, pollInterval time.Duration) error {
	if err := action(); err != nil {
		return fmt.Errorf("wake action failed: %w", err)
	}
	return poll(ctx, pollURL, hookTimeout, pollInterval)
}

func poll(ctx context.Context, url string, timeout, interval time.Duration) error {
	deadline := time.Now().Add(timeout)
	deadlineCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	probe := func() bool {
		reqCtx, reqCancel := context.WithTimeout(deadlineCtx, interval)
		defer reqCancel()
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
		if err != nil {
			return false
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	}

	if probe() {
		return nil
	}

	for {
		select {
		case <-ticker.C:
			if probe() {
				return nil
			}
		case <-deadlineCtx.Done():
			return fmt.Errorf("target not ready after %s", timeout)
		}
	}
}
```

- [ ] **Step 2: Update `internal/hook/hook_test.go`**

Replace the `cmd string` arguments with `func() error` actions. The full updated file:

```go
package hook_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jamietre/reveillm/internal/hook"
)

func shellAction(cmd string) func() error {
	return func() error {
		return exec.Command("sh", "-c", cmd).Run()
	}
}

func TestManager_Run_execsHookCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	m := hook.NewManager()
	err := m.Run(context.Background(), "t1", shellAction("true"), srv.URL+"/", 5*time.Second, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManager_Run_hookCommandFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	m := hook.NewManager()
	err := m.Run(context.Background(), "t1", shellAction("false"), srv.URL+"/", 5*time.Second, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected error when hook command exits non-zero")
	}
}

func TestManager_Run_pollsUntilReady(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callCount.Add(1) < 3 {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := hook.NewManager()
	err := m.Run(context.Background(), "t1", shellAction("true"), srv.URL+"/", 5*time.Second, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount.Load() < 3 {
		t.Errorf("expected at least 3 poll calls, got %d", callCount.Load())
	}
}

func TestManager_Run_timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	m := hook.NewManager()
	err := m.Run(context.Background(), "t1", shellAction("true"), srv.URL+"/", 300*time.Millisecond, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestManager_Run_concurrentWakeCoalesced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := hook.NewManager()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = m.Run(context.Background(), "same-target", shellAction("true"), srv.URL+"/", 5*time.Second, 50*time.Millisecond)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, err)
		}
	}
}
```

- [ ] **Step 3: Run tests**

```
go test ./internal/hook/... ./internal/runner/...
```
Expected: hook tests PASS; runner tests FAIL (runner still passes cmd string).

- [ ] **Step 4: Commit**

```bash
git add internal/hook/
git commit -m "refactor: hook manager accepts action func instead of cmd string"
```

---

## Task 4: Runner — wire WoL and shell actions

**Files:**
- Modify: `internal/runner/runner.go`

- [ ] **Step 1: Update `internal/runner/runner.go`**

Add `os/exec` import and update the hook dispatch block:

```go
package runner

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/jamietre/reveillm/internal/adapter"
	"github.com/jamietre/reveillm/internal/config"
	"github.com/jamietre/reveillm/internal/hook"
	"github.com/jamietre/reveillm/internal/wol"
)

// TargetFailure records why a specific target was skipped.
type TargetFailure struct {
	Name   string
	Reason string
}

// Result holds either a successful upstream response or the list of failures.
// Call Close() when done — it cancels the request context and closes the body.
type Result struct {
	Response *http.Response
	Failures []TargetFailure
	cancel   context.CancelFunc
}

func (r *Result) Close() {
	if r.cancel != nil {
		r.cancel()
	}
	if r.Response != nil {
		r.Response.Body.Close()
	}
}

// Runner iterates the targets in a named config, firing hooks and forwarding.
type Runner struct {
	cfg     *config.Config
	adapter adapter.Adapter
	hooks   *hook.Manager
}

func New(cfg *config.Config, a adapter.Adapter, hooks *hook.Manager) *Runner {
	return &Runner{cfg: cfg, adapter: a, hooks: hooks}
}

// Run attempts each target in order. Returns an error only for unknown config names.
// Target-level failures are recorded in Result.Failures.
func (r *Runner) Run(ctx context.Context, method string, headers http.Header, body []byte, configName, urlPath string) (*Result, error) {
	route, ok := r.cfg.Configs[configName]
	if !ok {
		return nil, fmt.Errorf("unknown config: %q", configName)
	}

	result := &Result{}

	for _, entry := range route.Targets {
		target := r.cfg.Targets[entry.Target]

		var action func() error
		switch {
		case target.WoL != "":
			mac := target.WoL
			action = func() error { return wol.Wake(mac) }
		case target.Hook != "":
			cmd := target.Hook
			action = func() error {
				return exec.CommandContext(ctx, "sh", "-c", cmd).Run()
			}
		}

		if action != nil {
			pollURL := strings.TrimRight(target.URL, "/") + "/"
			err := r.hooks.Run(ctx, entry.Target, action, pollURL, target.HookTimeout, target.HookPollInterval)
			if err != nil {
				result.Failures = append(result.Failures, TargetFailure{Name: entry.Target, Reason: err.Error()})
				continue
			}
		}

		reqCtx, cancel := context.WithTimeout(ctx, target.Timeout)
		resp, err := r.adapter.Forward(reqCtx, method, target.URL+urlPath, headers, body, target.APIKey, entry.Model)
		if err != nil {
			cancel()
			result.Failures = append(result.Failures, TargetFailure{Name: entry.Target, Reason: err.Error()})
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			cancel()
			resp.Body.Close()
			result.Failures = append(result.Failures, TargetFailure{
				Name:   entry.Target,
				Reason: fmt.Sprintf("non-2xx status %d", resp.StatusCode),
			})
			continue
		}

		result.Response = resp
		result.cancel = cancel
		return result, nil
	}

	return result, nil
}
```

- [ ] **Step 2: Run all tests**

```
go test ./...
```
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add internal/runner/runner.go
git commit -m "feat: wire native WoL and shell hook actions in runner"
```

---

## Task 5: Update config.example.yaml

**Files:**
- Modify: `config.example.yaml`

- [ ] **Step 1: Replace hook line with wol**

In `config.example.yaml`, replace:
```yaml
    hook: "wakeonlan AA:BB:CC:DD:EE:FF"
```
with:
```yaml
    wol: "AA:BB:CC:DD:EE:FF"
```

- [ ] **Step 2: Run tests to confirm nothing broke**

```
go test ./...
```
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add config.example.yaml
git commit -m "docs: update example config to use native wol field"
```

---

## Task 6: Makefile for build/push

**Files:**
- Create: `Makefile`

- [ ] **Step 1: Create `Makefile`**

```makefile
REGISTRY ?= 172.16.2.18:5000
IMAGE     := $(REGISTRY)/reveillm

.PHONY: push
push:
	docker build -t $(IMAGE):latest .
	docker push $(IMAGE):latest
```

- [ ] **Step 2: Verify Makefile syntax**

```
make --dry-run push
```
Expected output:
```
docker build -t 172.16.2.18:5000/reveillm:latest .
docker push 172.16.2.18:5000/reveillm:latest
```

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "build: add Makefile with push target for local registry"
```

---

## Task 7: Homelab deployment artifacts

**Files (all in `~/code/homelab`):**
- Create: `docker-lxc/docker/stacks/reveillm/docker-compose.yml`
- Create: `docker-lxc/docker/stacks/reveillm/config.yaml`
- Modify: `docker-lxc/deploy.conf`

- [ ] **Step 1: Create `docker-lxc/docker/stacks/reveillm/docker-compose.yml`**

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

- [ ] **Step 2: Create `docker-lxc/docker/stacks/reveillm/config.yaml`**

Fill in actual values for your network (real IP, MAC, model names):

```yaml
targets:
  ollama-home:
    url: http://172.16.2.10:11434
    wol: "10:7C:61:3D:75:AF"
    hook_timeout: 90s
    hook_poll_interval: 5s
    timeout: 120s

  claude-sonnet:
    url: https://api.anthropic.com
    api_key: "${ANTHROPIC_API_KEY}"
    timeout: 60s

  claude-haiku:
    url: https://api.anthropic.com
    api_key: "${ANTHROPIC_API_KEY}"
    timeout: 30s

configs:
  home:
    targets:
      - target: ollama-home
        model: llama3.1:70b
      - target: claude-sonnet
        model: claude-sonnet-4-6

  fast:
    targets:
      - target: claude-haiku
        model: claude-haiku-4-5-20251001
```

- [ ] **Step 3: Add reveillm entries to `docker-lxc/deploy.conf`**

Append to the existing COPY/RUN list:

```
COPY docker/stacks/reveillm/docker-compose.yml -> /docker/stacks/reveillm/docker-compose.yml
COPY docker/stacks/reveillm/config.yaml -> /docker/stacks/reveillm/config.yaml
RUN cd /docker/stacks/reveillm && docker compose pull && docker compose up -d
```

- [ ] **Step 4: Create `/docker/stacks/reveillm/` on docker.lan and place secrets**

```bash
ssh root@172.16.2.18 mkdir -p /docker/stacks/reveillm
ssh root@172.16.2.18 'cat > /docker/stacks/reveillm/.env' <<'EOF'
ANTHROPIC_API_KEY=sk-...
EOF
```

- [ ] **Step 5: Push the reveillm image**

From `~/code/reveillm`:
```bash
make push
```
Expected: image pushed to `172.16.2.18:5000/reveillm:latest`

- [ ] **Step 6: Deploy via homelab deploy.sh**

From `~/code/homelab`:
```bash
./deploy.sh docker-lxc
```
Expected: files copied, container started, no errors.

- [ ] **Step 7: Smoke test**

```bash
curl http://172.16.2.18:8080/health
```
Expected: `200 OK`

- [ ] **Step 8: Commit homelab changes**

From `~/code/homelab`:
```bash
git add docker-lxc/docker/stacks/reveillm/ docker-lxc/deploy.conf
git commit -m "feat: add reveillm stack to docker-lxc"
```

---

## Task 8: Push reveillm repo

- [ ] **Step 1: Push to GitHub**

From `~/code/reveillm`:
```bash
git push
```
