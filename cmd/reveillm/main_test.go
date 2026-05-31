package main_test

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/jamietre/reveillm/internal/adapter"
	"github.com/jamietre/reveillm/internal/config"
	"github.com/jamietre/reveillm/internal/hook"
	"github.com/jamietre/reveillm/internal/proxy"
	"github.com/jamietre/reveillm/internal/runner"
)

func startServer(t *testing.T, configPath string) *httptest.Server {
	t.Helper()
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(cfg.Configs))
	for n := range cfg.Configs {
		names = append(names, n)
	}
	sort.Strings(names)

	a := adapter.NewOpenAIAdapter(&http.Client{})
	hooks := hook.NewManager()
	r := runner.New(cfg, a, hooks)
	h := proxy.NewHandler(r, names)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: h}}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func TestIntegration_proxyRequest(t *testing.T) {
	var receivedModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		json.Unmarshal(body["model"], &receivedModel)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"test","choices":[]}`))
	}))
	defer upstream.Close()

	cfgContent := `
targets:
  local:
    url: ` + upstream.URL + `
    timeout: 5s
configs:
  home:
    targets:
      - target: local
        model: llama3.1:8b
`
	f, _ := os.CreateTemp(t.TempDir(), "*.yaml")
	f.WriteString(cfgContent)
	f.Close()

	srv := startServer(t, f.Name())

	resp, err := http.Post(
		srv.URL+"/home/v1/chat/completions",
		"application/json",
		strings.NewReader(`{"model":"ignored-by-proxy","messages":[]}`),
	)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	if receivedModel != "llama3.1:8b" {
		t.Errorf("want model llama3.1:8b forwarded, got %q", receivedModel)
	}
}

func TestIntegration_unknownConfig_404(t *testing.T) {
	cfgContent := `
targets:
  t:
    url: http://127.0.0.1:1
    timeout: 1s
configs:
  home:
    targets:
      - target: t
        model: m
`
	f, _ := os.CreateTemp(t.TempDir(), "*.yaml")
	f.WriteString(cfgContent)
	f.Close()

	srv := startServer(t, f.Name())

	resp, err := http.Get(srv.URL + "/doesnotexist/v1/chat/completions")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestIntegration_health(t *testing.T) {
	cfgContent := `
targets:
  t:
    url: http://127.0.0.1:1
    timeout: 1s
configs:
  home:
    targets:
      - target: t
        model: m
`
	f, _ := os.CreateTemp(t.TempDir(), "*.yaml")
	f.WriteString(cfgContent)
	f.Close()

	srv := startServer(t, f.Name())

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}
