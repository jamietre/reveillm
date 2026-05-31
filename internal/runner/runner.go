package runner

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jamietre/reveillm/internal/adapter"
	"github.com/jamietre/reveillm/internal/config"
	"github.com/jamietre/reveillm/internal/hook"
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

		if target.Hook != "" {
			err := r.hooks.Run(ctx, entry.Target, target.Hook, target.URL+"/", target.HookTimeout, target.HookPollInterval)
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
