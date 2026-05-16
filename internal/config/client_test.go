package config

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Fetch_Success(t *testing.T) {
	payload := json.RawMessage(`{"reporting":{"heartbeat_interval_seconds":60}}`)
	hash := ComputeHash(payload)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Query().Get("node_id") != "test-node" {
			t.Fatal("unexpected node_id")
		}
		if r.URL.Query().Get("agent_version") != "v1.0" {
			t.Fatal("unexpected agent_version")
		}
		if r.URL.Query().Get("config_version") != "5" {
			t.Fatal("unexpected config_version query param")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{
			"schema_version": "1.0",
			"config_key": "nodeagent.runtime_config",
			"config_version": 7,
			"config_hash": %q,
			"payload": {"reporting":{"heartbeat_interval_seconds":60}},
			"fallback_action": "continue",
			"published_at": "2026-05-16T12:00:00Z"
		}`, hash)
	}))
	defer server.Close()

	client := NewClient(server.URL+"/internal/agent/config", "test-node", "v1.0")
	resp, err := client.Fetch(context.Background(), 5)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if resp.ConfigVersion != 7 {
		t.Fatalf("expected version 7, got %d", resp.ConfigVersion)
	}
	if resp.ConfigKey != "nodeagent.runtime_config" {
		t.Fatalf("unexpected config_key: %s", resp.ConfigKey)
	}
}

func TestClient_Fetch_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"error":{"code":"CONFIG_KEY_NOT_FOUND","message":"unknown config key"}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL+"/internal/agent/config", "test-node", "v1.0")
	_, err := client.Fetch(context.Background(), 0)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestClient_Fetch_NoVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.URL.Query().Get("config_version"); v != "" {
			t.Fatalf("expected no config_version, got %s", v)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"schema_version": "1.0",
			"config_key": "nodeagent.runtime_config",
			"config_version": 1,
			"config_hash": "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			"payload": {},
			"fallback_action": "continue",
			"published_at": "2026-05-16T12:00:00Z"
		}`)
	}))
	defer server.Close()

	client := NewClient(server.URL+"/internal/agent/config", "test-node", "v1.0")
	resp, err := client.Fetch(context.Background(), 0)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if resp.ConfigVersion != 1 {
		t.Fatalf("expected version 1, got %d", resp.ConfigVersion)
	}
}

func TestClient_Fetch_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't respond — trigger context timeout.
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(server.URL+"/internal/agent/config", "test-node", "v1.0",
		WithHTTPClient(&http.Client{Timeout: 1}),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()
	_, err := client.Fetch(ctx, 0)
	if err == nil {
		t.Fatal("expected error for timeout")
	}
}
