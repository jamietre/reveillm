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
