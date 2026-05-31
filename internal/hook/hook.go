package hook

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
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

// Run executes cmd via sh -c, then polls pollURL every pollInterval until any
// HTTP response is received. Returns an error if the hook command fails or
// hookTimeout is exceeded. Concurrent calls for the same targetName coalesce:
// the second caller waits for the first wake attempt rather than firing again.
func (m *Manager) Run(ctx context.Context, targetName, cmd, pollURL string, hookTimeout, pollInterval time.Duration) error {
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

	err := doWake(ctx, cmd, pollURL, hookTimeout, pollInterval)

	w.mu.Lock()
	w.active = false
	for _, ch := range w.waiters {
		ch <- err
	}
	w.waiters = nil
	w.mu.Unlock()

	return err
}

func doWake(ctx context.Context, cmd, pollURL string, hookTimeout, pollInterval time.Duration) error {
	if err := exec.CommandContext(ctx, "sh", "-c", cmd).Run(); err != nil {
		return fmt.Errorf("hook command failed: %w", err)
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

	// probe immediately before waiting for the first tick
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
