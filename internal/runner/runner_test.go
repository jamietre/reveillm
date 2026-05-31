package runner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jamietre/reveillm/internal/adapter"
	"github.com/jamietre/reveillm/internal/config"
	"github.com/jamietre/reveillm/internal/hook"
	"github.com/jamietre/reveillm/internal/runner"
)

func makeConfig(targets map[string]config.Target, configs map[string]config.RouteConfig) *config.Config {
	return &config.Config{Targets: targets, Configs: configs}
}

func okServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func failServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunner_firstTargetSuccess(t *testing.T) {
	srv := okServer(t)

	cfg := makeConfig(
		map[string]config.Target{"t1": {URL: srv.URL, Timeout: 5 * time.Second}},
		map[string]config.RouteConfig{"c": {Targets: []config.ConfigEntry{{Target: "t1", Model: "m"}}}},
	)

	r := runner.New(cfg, adapter.NewOpenAIAdapter(http.DefaultClient), hook.NewManager())
	result, err := r.Run(context.Background(), http.MethodPost, http.Header{}, []byte(`{"messages":[]}`), "c", "/v1/chat/completions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.Close()
	if result.Response == nil {
		t.Fatal("expected a response")
	}
	if result.Response.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", result.Response.StatusCode)
	}
}

func TestRunner_fallbackOnNonSuccess(t *testing.T) {
	bad := failServer(t)
	good := okServer(t)

	cfg := makeConfig(
		map[string]config.Target{
			"bad":  {URL: bad.URL, Timeout: 5 * time.Second},
			"good": {URL: good.URL, Timeout: 5 * time.Second},
		},
		map[string]config.RouteConfig{"c": {Targets: []config.ConfigEntry{
			{Target: "bad", Model: "m"},
			{Target: "good", Model: "m"},
		}}},
	)

	r := runner.New(cfg, adapter.NewOpenAIAdapter(http.DefaultClient), hook.NewManager())
	result, err := r.Run(context.Background(), http.MethodPost, http.Header{}, []byte(`{"messages":[]}`), "c", "/v1/chat/completions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.Close()
	if result.Response == nil {
		t.Fatalf("expected fallback response; failures: %v", result.Failures)
	}
	if result.Response.StatusCode != http.StatusOK {
		t.Errorf("want 200 from fallback, got %d", result.Response.StatusCode)
	}
	if len(result.Failures) != 1 {
		t.Errorf("want 1 failure recorded, got %d", len(result.Failures))
	}
}

func TestRunner_allFail(t *testing.T) {
	bad := failServer(t)

	cfg := makeConfig(
		map[string]config.Target{"t1": {URL: bad.URL, Timeout: 5 * time.Second}},
		map[string]config.RouteConfig{"c": {Targets: []config.ConfigEntry{{Target: "t1", Model: "m"}}}},
	)

	r := runner.New(cfg, adapter.NewOpenAIAdapter(http.DefaultClient), hook.NewManager())
	result, err := r.Run(context.Background(), http.MethodPost, http.Header{}, []byte(`{"messages":[]}`), "c", "/v1/chat/completions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != nil {
		result.Close()
		t.Fatal("expected no response when all targets fail")
	}
	if len(result.Failures) != 1 {
		t.Errorf("want 1 failure, got %d", len(result.Failures))
	}
}

func TestRunner_unknownConfig(t *testing.T) {
	cfg := makeConfig(map[string]config.Target{}, map[string]config.RouteConfig{})
	r := runner.New(cfg, adapter.NewOpenAIAdapter(http.DefaultClient), hook.NewManager())
	_, err := r.Run(context.Background(), http.MethodPost, http.Header{}, []byte(`{}`), "nonexistent", "/v1/chat/completions")
	if err == nil {
		t.Fatal("expected error for unknown config")
	}
}

// mockAdapter lets us test hook interaction without a real HTTP server as target.
type mockAdapter struct{}

func (m *mockAdapter) Forward(_ context.Context, _, _ string, _ http.Header, _ []byte, _, _ string) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rec.WriteHeader(http.StatusOK)
	return rec.Result(), nil
}

func TestRunner_hookCalledForTarget(t *testing.T) {
	readySrv := okServer(t) // answers readiness polls

	cfg := makeConfig(
		map[string]config.Target{"t1": {
			URL:              readySrv.URL,
			Timeout:          5 * time.Second,
			Hook:             "true",
			HookTimeout:      2 * time.Second,
			HookPollInterval: 100 * time.Millisecond,
		}},
		map[string]config.RouteConfig{"c": {Targets: []config.ConfigEntry{{Target: "t1", Model: "m"}}}},
	)

	r := runner.New(cfg, &mockAdapter{}, hook.NewManager())
	result, err := r.Run(context.Background(), http.MethodPost, http.Header{}, []byte(`{"messages":[]}`), "c", "/v1/chat/completions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.Close()
	if result.Response == nil {
		t.Fatalf("expected response; failures: %v", result.Failures)
	}
}

func TestRunner_hookFailureFallsToNextTarget(t *testing.T) {
	good := okServer(t)

	cfg := makeConfig(
		map[string]config.Target{
			"sleeping": {
				URL:              "http://127.0.0.1:1",
				Timeout:          5 * time.Second,
				Hook:             "false",
				HookTimeout:      500 * time.Millisecond,
				HookPollInterval: 50 * time.Millisecond,
			},
			"fallback": {URL: good.URL, Timeout: 5 * time.Second},
		},
		map[string]config.RouteConfig{"c": {Targets: []config.ConfigEntry{
			{Target: "sleeping", Model: "m"},
			{Target: "fallback", Model: "m"},
		}}},
	)

	r := runner.New(cfg, adapter.NewOpenAIAdapter(http.DefaultClient), hook.NewManager())
	result, err := r.Run(context.Background(), http.MethodPost, http.Header{}, []byte(`{"messages":[]}`), "c", "/v1/chat/completions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.Close()
	if result.Response == nil {
		t.Fatalf("expected fallback response; failures: %v", result.Failures)
	}
}
