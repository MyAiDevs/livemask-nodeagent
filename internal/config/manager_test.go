package config

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// helperServer starts a test server that responds with a specific config version and hash.
func helperServer(t *testing.T, version int, payload json.RawMessage) *httptest.Server {
	t.Helper()
	hash := ComputeHash(payload)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"schema_version": "1.0",
			"config_key": "nodeagent.runtime_config",
			"config_version": %d,
			"config_hash": %q,
			"payload": %s,
			"fallback_action": "continue",
			"published_at": "2026-05-16T12:00:00Z"
		}`, version, hash, string(payload))
	}))
}

func TestManager_InitialSync(t *testing.T) {
	dir := t.TempDir()
	payload := json.RawMessage(`{"reporting":{"heartbeat_interval_seconds":60,"batch_upload_interval_seconds":300,"max_offline_buffer_items":5000},"degraded_mode":{"enabled":true,"auto_recover":false},"singbox":{"health_check_timeout_seconds":5}}`)
	server := helperServer(t, 1, payload)
	defer server.Close()

	client := NewClient(server.URL+"/internal/agent/config", "test-node", "v1.0")
	store := NewStore(filepath.Join(dir, "cache.json"))
	applier := NewRuntimeApplier(nil)
	mgr := NewManager(client, store, applier)

	changed, err := mgr.SyncOnce(context.Background())
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if !changed {
		t.Fatal("expected config to change")
	}

	status := mgr.Status()
	if status.ConfigVersion != 1 {
		t.Fatalf("expected version 1, got %d", status.ConfigVersion)
	}
	if status.IsDegraded {
		t.Fatal("should not be degraded after success")
	}
	if status.ConfigHash == "" {
		t.Fatal("hash should not be empty")
	}
}

func TestManager_LastKnownGoodFallback(t *testing.T) {
	dir := t.TempDir()

	// Step 1: successful sync to populate cache.
	payload1 := json.RawMessage(`{"reporting":{"heartbeat_interval_seconds":60,"batch_upload_interval_seconds":300,"max_offline_buffer_items":5000},"degraded_mode":{"enabled":true,"auto_recover":false},"singbox":{"health_check_timeout_seconds":5}}`)
	server1 := helperServer(t, 3, payload1)
	client := NewClient(server1.URL+"/internal/agent/config", "test-node", "v1.0")
	store := NewStore(filepath.Join(dir, "cache.json"))
	applier := NewRuntimeApplier(nil)
	mgr := NewManager(client, store, applier)

	_, err := mgr.SyncOnce(context.Background())
	if err != nil {
		t.Fatalf("first sync failed: %v", err)
	}
	server1.Close()

	// Step 2: second manager with same cache, but Backend is down.
	client2 := NewClient("http://localhost:19999/nonexistent", "test-node", "v1.0")
	store2 := NewStore(filepath.Join(dir, "cache.json"))
	mgr2 := NewManager(client2, store2, applier)

	loaded := mgr2.LoadLastKnownGood()
	if !loaded {
		t.Fatal("expected last-known-good to load")
	}
	if mgr2.Status().ConfigVersion != 3 {
		t.Fatalf("expected version 3, got %d", mgr2.Status().ConfigVersion)
	}

	// Non-degraded because we loaded from cache.
	if mgr2.Status().IsDegraded {
		t.Fatal("should not be degraded after loading LKG")
	}
}

func TestManager_InvalidConfigRejected(t *testing.T) {
	dir := t.TempDir()

	// Server returns response with wrong config_key.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"schema_version": "1.0",
			"config_key": "client.remote_config",
			"config_version": 99,
			"config_hash": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			"payload": {"reporting":{"heartbeat_interval_seconds":60}},
			"fallback_action": "continue",
			"published_at": "2026-05-16T12:00:00Z"
		}`)
	}))
	defer server.Close()

	client := NewClient(server.URL+"/internal/agent/config", "test-node", "v1.0")
	store := NewStore(filepath.Join(dir, "cache.json"))
	applier := NewRuntimeApplier(nil)
	mgr := NewManager(client, store, applier)

	_, err := mgr.SyncOnce(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid config_key")
	}
	if mgr.Status().IsDegraded != true {
		t.Fatal("should be in degraded mode after invalid config")
	}
}

func TestManager_InvalidApplyRejected(t *testing.T) {
	// The payload passes validation, but the applier rejects it.
	dir := t.TempDir()
	payload := json.RawMessage(`{"reporting":{"heartbeat_interval_seconds":1,"batch_upload_interval_seconds":300,"max_offline_buffer_items":5000},"degraded_mode":{"enabled":true,"auto_recover":false},"singbox":{"health_check_timeout_seconds":5}}`)
	server := helperServer(t, 5, payload)
	defer server.Close()

	client := NewClient(server.URL+"/internal/agent/config", "test-node", "v1.0")
	store := NewStore(filepath.Join(dir, "cache.json"))
	applier := NewRuntimeApplier(nil) // nil callback still validates fields
	mgr := NewManager(client, store, applier)

	_, err := mgr.SyncOnce(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid heartbeat interval")
	}
	if mgr.Status().IsDegraded != true {
		t.Fatal("should be degraded after rejected apply")
	}
}

func TestManager_SameVersionNoop(t *testing.T) {
	dir := t.TempDir()
	payload := json.RawMessage(`{"reporting":{"heartbeat_interval_seconds":60,"batch_upload_interval_seconds":300,"max_offline_buffer_items":5000},"degraded_mode":{"enabled":true,"auto_recover":false},"singbox":{"health_check_timeout_seconds":5}}`)
	server := helperServer(t, 2, payload)
	defer server.Close()

	client := NewClient(server.URL+"/internal/agent/config", "test-node", "v1.0")
	store := NewStore(filepath.Join(dir, "cache.json"))
	applier := NewRuntimeApplier(nil)
	mgr := NewManager(client, store, applier)

	changed, err := mgr.SyncOnce(context.Background())
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if !changed {
		t.Fatal("expected changed on first sync")
	}

	// Second call, same version.
	changed, err = mgr.SyncOnce(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if changed {
		t.Fatal("expected no change on second sync (same version)")
	}
}

func TestManager_StatusObservable(t *testing.T) {
	dir := t.TempDir()
	payload := json.RawMessage(`{"reporting":{"heartbeat_interval_seconds":60,"batch_upload_interval_seconds":300,"max_offline_buffer_items":5000},"degraded_mode":{"enabled":true,"auto_recover":false},"singbox":{"health_check_timeout_seconds":5}}`)
	server := helperServer(t, 4, payload)
	defer server.Close()

	client := NewClient(server.URL+"/internal/agent/config", "test-node", "v1.0")
	store := NewStore(filepath.Join(dir, "cache.json"))
	applier := NewRuntimeApplier(nil)
	mgr := NewManager(client, store, applier)

	_, _ = mgr.SyncOnce(context.Background())

	status := mgr.Status()
	if status.ConfigVersion != 4 {
		t.Fatalf("expected version 4, got %d", status.ConfigVersion)
	}
	if status.ConfigHash == "" {
		t.Fatal("hash should be set")
	}
	if status.ConfigKey != "nodeagent.runtime_config" {
		t.Fatalf("unexpected key: %s", status.ConfigKey)
	}
	if status.LastFetchAt == nil {
		t.Fatal("LastFetchAt should be set")
	}
	if status.LastError != "" {
		t.Fatalf("unexpected error: %s", status.LastError)
	}
}

func TestManager_StatusHook(t *testing.T) {
	dir := t.TempDir()
	payload := json.RawMessage(`{"reporting":{"heartbeat_interval_seconds":60,"batch_upload_interval_seconds":300,"max_offline_buffer_items":5000},"degraded_mode":{"enabled":true,"auto_recover":false},"singbox":{"health_check_timeout_seconds":5}}`)
	server := helperServer(t, 10, payload)
	defer server.Close()

	client := NewClient(server.URL+"/internal/agent/config", "test-node", "v1.0")
	store := NewStore(filepath.Join(dir, "cache.json"))
	applier := NewRuntimeApplier(nil)
	mgr := NewManager(client, store, applier)

	var hookCalled bool
	mgr.OnStatusChange(func(s ConfigStatus) {
		hookCalled = true
	})

	_, _ = mgr.SyncOnce(context.Background())
	if !hookCalled {
		t.Fatal("status hook was not called")
	}
}

func TestJitteredInterval(t *testing.T) {
	base := 60 * time.Second
	for i := 0; i < 10; i++ {
		got := jitteredInterval(base)
		if got < base {
			t.Fatalf("jittered interval %v < base %v", got, base)
		}
		maxJitter := base + time.Duration(float64(base)*MaxJitterFraction)
		if got > maxJitter {
			t.Fatalf("jittered interval %v > max %v", got, maxJitter)
		}
	}
}

func TestBackoff(t *testing.T) {
	tests := []struct {
		current  time.Duration
		base     time.Duration
		max      time.Duration
		expected time.Duration
	}{
		{30 * time.Second, 30 * time.Second, 10 * time.Minute, 60 * time.Second},
		{5 * time.Minute, 30 * time.Second, 10 * time.Minute, 10 * time.Minute},      // capped
		{15 * time.Minute, 30 * time.Second, 10 * time.Minute, 10 * time.Minute},      // capped and shrunk
		{10 * time.Second, 30 * time.Second, 10 * time.Minute, 30 * time.Second},      // below base
	}
	for _, tt := range tests {
		got := backoff(tt.current, tt.base, BackoffMultiplier, tt.max)
		if got != tt.expected {
			t.Fatalf("backoff(%v,%v,%v,%v) = %v, expected %v", tt.current, tt.base, BackoffMultiplier, tt.max, got, tt.expected)
		}
	}
}

func TestDefaultRuntimeConfig(t *testing.T) {
	cfg := DefaultRuntimeConfig()
	if cfg.Reporting.HeartbeatIntervalSeconds != 60 {
		t.Fatalf("expected heart beat 60, got %d", cfg.Reporting.HeartbeatIntervalSeconds)
	}
	if cfg.DegradedMode.Enabled != true {
		t.Fatal("degraded mode should be enabled by default")
	}
	if cfg.Singbox.HealthCheckTimeoutSeconds != 5 {
		t.Fatalf("expected health check timeout 5, got %d", cfg.Singbox.HealthCheckTimeoutSeconds)
	}
	clone := cfg.Clone()
	clone.Reporting.HeartbeatIntervalSeconds = 999
	if cfg.Reporting.HeartbeatIntervalSeconds == 999 {
		t.Fatal("clone should be a deep copy")
	}
}

func TestManager_CurrentConfigDefaults(t *testing.T) {
	// Manager with no sync should return defaults.
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "cache.json"))
	client := NewClient("http://localhost:1", "n", "v1.0")
	applier := NewRuntimeApplier(nil)
	mgr := NewManager(client, store, applier)

	cfg := mgr.CurrentConfig()
	if cfg.Reporting.HeartbeatIntervalSeconds != 60 {
		t.Fatalf("expected default heartbeat 60, got %d", cfg.Reporting.HeartbeatIntervalSeconds)
	}
}
