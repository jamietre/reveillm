package proxy

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jamietre/reveillm/internal/runner"
)

// RunnerIface is the subset of runner.Runner used by Handler (enables mock in tests).
type RunnerIface interface {
	Run(ctx context.Context, method string, headers http.Header, body []byte, configName, urlPath string) (*runner.Result, error)
}

// Handler is the root http.Handler for reveillm.
type Handler struct {
	runner     RunnerIface
	configList []string
	configSet  map[string]struct{}
}

func NewHandler(r RunnerIface, configNames []string) *Handler {
	set := make(map[string]struct{}, len(configNames))
	for _, n := range configNames {
		set[n] = struct{}{}
	}
	return &Handler{runner: r, configList: configNames, configSet: set}
}

// statusWriter captures the response status code written by handlers.
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.code = code
	sw.ResponseWriter.WriteHeader(code)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/health":
		w.WriteHeader(http.StatusOK)
		return
	case "/":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string][]string{"configs": h.configList})
		return
	}

	sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
	start := time.Now()
	defer func() {
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.code,
			"ms", time.Since(start).Milliseconds(),
		)
	}()
	w = sw

	trimmed := strings.TrimPrefix(r.URL.Path, "/")
	idx := strings.IndexByte(trimmed, '/')
	if idx < 0 {
		http.NotFound(w, r)
		return
	}
	configName := trimmed[:idx]
	urlPath := trimmed[idx:]

	if _, ok := h.configSet[configName]; !ok {
		http.NotFound(w, r)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	result, err := h.runner.Run(r.Context(), r.Method, r.Header, body, configName, urlPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer result.Close()

	if result.Response == nil {
		h.writeAllFailed(w, result.Failures)
		return
	}

	for k, vs := range result.Response.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(result.Response.StatusCode)
	io.Copy(newFlushWriter(w), result.Response.Body)
}

func (h *Handler) writeAllFailed(w http.ResponseWriter, failures []runner.TargetFailure) {
	type targetErr struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	type errDetail struct {
		Message string      `json:"message"`
		Type    string      `json:"type"`
		Targets []targetErr `json:"targets"`
	}
	ts := make([]targetErr, len(failures))
	for i, f := range failures {
		ts[i] = targetErr{Name: f.Name, Reason: f.Reason}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(map[string]errDetail{
		"error": {Message: "all targets failed", Type: "upstream_error", Targets: ts},
	})
}

// flushWriter wraps ResponseWriter and flushes after each write for SSE streaming.
type flushWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newFlushWriter(w http.ResponseWriter) io.Writer {
	fw := &flushWriter{w: w}
	fw.flusher, _ = w.(http.Flusher)
	return fw
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if fw.flusher != nil {
		fw.flusher.Flush()
	}
	return n, err
}
