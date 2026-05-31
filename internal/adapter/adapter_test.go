package adapter_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jamietre/reveillm/internal/adapter"
)

func TestSubstituteModel(t *testing.T) {
	body := []byte(`{"model":"old-model","messages":[{"role":"user","content":"hi"}]}`)
	result, err := adapter.SubstituteModel(body, "new-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var obj map[string]json.RawMessage
	json.Unmarshal(result, &obj)
	var got string
	json.Unmarshal(obj["model"], &got)
	if got != "new-model" {
		t.Errorf("want model new-model, got %s", got)
	}
	if _, ok := obj["messages"]; !ok {
		t.Error("messages field should be preserved")
	}
}

func TestSubstituteModel_nonJSON_passthrough(t *testing.T) {
	body := []byte(`not json`)
	result, err := adapter.SubstituteModel(body, "m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != "not json" {
		t.Errorf("want passthrough for non-JSON, got %q", result)
	}
}

func TestOpenAIAdapter_setsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a := adapter.NewOpenAIAdapter(http.DefaultClient)
	body := []byte(`{"model":"x","messages":[]}`)
	resp, err := a.Forward(context.Background(), http.MethodPost, srv.URL+"/v1/chat/completions", http.Header{}, body, "sk-test", "new-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
	if gotAuth != "Bearer sk-test" {
		t.Errorf("want Bearer sk-test, got %q", gotAuth)
	}
}

func TestOpenAIAdapter_noAuthWhenEmpty(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a := adapter.NewOpenAIAdapter(http.DefaultClient)
	body := []byte(`{"model":"x","messages":[]}`)
	resp, err := a.Forward(context.Background(), http.MethodPost, srv.URL+"/v1/chat/completions", http.Header{}, body, "", "m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
	if gotAuth != "" {
		t.Errorf("want empty auth, got %q", gotAuth)
	}
}

func TestOpenAIAdapter_substitutesModel(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a := adapter.NewOpenAIAdapter(http.DefaultClient)
	body := []byte(`{"model":"old","messages":[]}`)
	resp, err := a.Forward(context.Background(), http.MethodPost, srv.URL+"/v1/chat/completions", http.Header{}, body, "", "new-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	var obj map[string]json.RawMessage
	json.Unmarshal(gotBody, &obj)
	var model string
	json.Unmarshal(obj["model"], &model)
	if model != "new-model" {
		t.Errorf("want model new-model forwarded to server, got %s", model)
	}
}

func TestOpenAIAdapter_streamsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("chunk1"))
		w.Write([]byte("chunk2"))
	}))
	defer srv.Close()

	a := adapter.NewOpenAIAdapter(http.DefaultClient)
	body := []byte(`{"model":"x","messages":[]}`)
	resp, err := a.Forward(context.Background(), http.MethodPost, srv.URL+"/", http.Header{}, body, "", "m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "chunk1chunk2" {
		t.Errorf("want chunk1chunk2, got %q", string(got))
	}
}
