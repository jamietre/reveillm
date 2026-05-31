package proxy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jamietre/reveillm/internal/proxy"
	"github.com/jamietre/reveillm/internal/runner"
)

type mockRunner struct {
	result *runner.Result
	err    error
}

func (m *mockRunner) Run(_ context.Context, _ string, _ http.Header, _ []byte, configName, _ string) (*runner.Result, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func okResult() *runner.Result {
	rec := httptest.NewRecorder()
	rec.WriteHeader(http.StatusOK)
	rec.Write([]byte(`{"choices":[]}`))
	return &runner.Result{Response: rec.Result()}
}

func failResult() *runner.Result {
	return &runner.Result{
		Failures: []runner.TargetFailure{{Name: "t1", Reason: "connection refused"}},
	}
}

func TestHandler_health(t *testing.T) {
	h := proxy.NewHandler(&mockRunner{result: okResult()}, []string{"home"})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
}

func TestHandler_root_listsConfigs(t *testing.T) {
	h := proxy.NewHandler(&mockRunner{result: okResult()}, []string{"home", "fast"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var body map[string][]string
	json.NewDecoder(w.Body).Decode(&body)
	if len(body["configs"]) != 2 {
		t.Errorf("want 2 configs listed, got %v", body["configs"])
	}
}

func TestHandler_unknownConfig_returns404(t *testing.T) {
	h := proxy.NewHandler(&mockRunner{err: fmt.Errorf("unknown config")}, []string{"home"})
	req := httptest.NewRequest(http.MethodPost, "/unknown/v1/chat/completions",
		strings.NewReader(`{"messages":[]}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestHandler_allTargetsFail_returns503(t *testing.T) {
	h := proxy.NewHandler(&mockRunner{result: failResult()}, []string{"home"})
	req := httptest.NewRequest(http.MethodPost, "/home/v1/chat/completions",
		strings.NewReader(`{"messages":[]}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", w.Code)
	}
	var errBody map[string]interface{}
	json.NewDecoder(w.Body).Decode(&errBody)
	if errBody["error"] == nil {
		t.Error("want error field in 503 body")
	}
}

func TestHandler_successStreamsResponse(t *testing.T) {
	h := proxy.NewHandler(&mockRunner{result: okResult()}, []string{"home"})
	req := httptest.NewRequest(http.MethodPost, "/home/v1/chat/completions",
		strings.NewReader(`{"messages":[]}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	got, _ := io.ReadAll(w.Body)
	if string(got) != `{"choices":[]}` {
		t.Errorf("want response body streamed, got %q", string(got))
	}
}

func TestHandler_badPath_returns404(t *testing.T) {
	h := proxy.NewHandler(&mockRunner{result: okResult()}, []string{"home"})
	req := httptest.NewRequest(http.MethodGet, "/noslash", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}
