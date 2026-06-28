package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/jamietre/reveillm/internal/config"
)

const validYAML = `
targets:
  ollama-home:
    url: http://192.168.1.100:11434
    hook: "wakeonlan AA:BB:CC:DD:EE:FF"
    hook_timeout: 90s
    hook_poll_interval: 5s
    timeout: 30s
  claude:
    url: https://api.anthropic.com
    api_key: "sk-test"
    timeout: 30s
configs:
  home:
    targets:
      - target: ollama-home
        model: llama3.1:70b
      - target: claude
        model: claude-3-5-sonnet-20241022
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func TestLoad_valid(t *testing.T) {
	cfg, err := config.Load(writeTemp(t, validYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 2 {
		t.Errorf("want 2 targets, got %d", len(cfg.Targets))
	}
	if len(cfg.Configs) != 1 {
		t.Errorf("want 1 config, got %d", len(cfg.Configs))
	}
	home := cfg.Configs["home"]
	if len(home.Targets) != 2 {
		t.Errorf("want 2 config targets, got %d", len(home.Targets))
	}
	if home.Targets[0].Model != "llama3.1:70b" {
		t.Errorf("want model llama3.1:70b, got %s", home.Targets[0].Model)
	}
	ollama := cfg.Targets["ollama-home"]
	if ollama.Timeout != 30*time.Second {
		t.Errorf("want timeout 30s, got %v", ollama.Timeout)
	}
	if ollama.HookPollInterval != 5*time.Second {
		t.Errorf("want poll interval 5s, got %v", ollama.HookPollInterval)
	}
}

func TestLoad_defaultPollInterval(t *testing.T) {
	yaml := `
targets:
  t:
    url: http://example.com
    hook: "echo hi"
    hook_timeout: 30s
    timeout: 10s
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

func TestLoad_envInterpolation(t *testing.T) {
	t.Setenv("TEST_KEY", "sk-abc123")
	yaml := `
targets:
  t:
    url: http://example.com
    api_key: "${TEST_KEY}"
    timeout: 10s
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
	if cfg.Targets["t"].APIKey != "sk-abc123" {
		t.Errorf("want api_key sk-abc123, got %q", cfg.Targets["t"].APIKey)
	}
}

func TestLoad_unknownTarget(t *testing.T) {
	yaml := `
targets:
  real:
    url: http://example.com
    timeout: 10s
configs:
  c:
    targets:
      - target: missing
        model: m
`
	_, err := config.Load(writeTemp(t, yaml))
	if err == nil {
		t.Fatal("expected error for unknown target reference")
	}
}

func TestLoad_noModel_passthrough(t *testing.T) {
	yaml := `
targets:
  t:
    url: http://example.com
    timeout: 10s
configs:
  c:
    targets:
      - target: t
`
	cfg, err := config.Load(writeTemp(t, yaml))
	if err != nil {
		t.Fatalf("expected no error for missing model (passthrough): %v", err)
	}
	if cfg.Configs["c"].Targets[0].Model != "" {
		t.Fatal("expected empty model for passthrough")
	}
}

func TestLoad_fileNotFound(t *testing.T) {
	_, err := config.Load("/nonexistent/path.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_missingURL(t *testing.T) {
	yaml := `
targets:
  t:
    url: ""
    timeout: 10s
configs:
  c:
    targets:
      - target: t
        model: m
`
	_, err := config.Load(writeTemp(t, yaml))
	if err == nil {
		t.Fatal("expected error for empty url")
	}
}

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
