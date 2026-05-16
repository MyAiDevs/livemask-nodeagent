package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockCollector implements MetricsCollector for testing.
type mockCollector struct{}

func (m *mockCollector) Collect() (*SystemMetrics, error) {
	return &SystemMetrics{
		CPUPercent:    15.0,
		MemoryPercent: 40.0,
		MemoryUsedMB:  1024,
		Load1:         0.5,
		Load5:         0.3,
		Load15:        0.2,
	}, nil
}

// mockConfigProvider implements ConfigProvider for testing.
type mockConfigProvider struct {
	configVersion int
	configHash    string
	degraded      bool
}

func (m *mockConfigProvider) ConfigVersion() int    { return m.configVersion }
func (m *mockConfigProvider) ConfigHash() string      { return m.configHash }
func (m *mockConfigProvider) IsDegraded() bool         { return m.degraded }

func TestManager_RegisterSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/agent/register" {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"node_id":"test-node","status":"active"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node", "v1.0")
	collector := &mockCollector{}
	cfgProvider := &mockConfigProvider{configVersion: 3, configHash: "sha256:abc", degraded: false}
	mgr := NewManager(client, collector, cfgProvider)

	err := mgr.Register(context.Background())
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	status := mgr.Status()
	if !status.Registered {
		t.Fatal("expected registered=true")
	}
	if status.NodeStatus != "active" {
		t.Fatalf("expected status active, got %s", status.NodeStatus)
	}
	if status.LastRegisterAt == nil {
		t.Fatal("expected LastRegisterAt to be set")
	}
}

func TestManager_RegisterFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node", "v1.0")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{})

	err := mgr.Register(context.Background())
	if err == nil {
		t.Fatal("expected register to fail")
	}

	status := mgr.Status()
	if status.Registered {
		t.Fatal("expected registered=false after failure")
	}
	if status.LastRegisterErr == "" {
		t.Fatal("expected LastRegisterErr to be set")
	}
}

func TestManager_RegisterNotExiting(t *testing.T) {
	// Register failure must NOT exit the process — it should enter degraded
	// mode but allow the agent to continue.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":"node not found"}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node", "v1.0")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{})

	err := mgr.Register(context.Background())
	if err == nil {
		t.Fatal("expected register error")
	}
	// Agent must still be functional.
	status := mgr.Status()
	if !status.IsDeployed {
		t.Fatal("agent should still be deployed despite register failure")
	}
}

func TestManager_SendHeartbeat(t *testing.T) {
	var heartbeatCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/agent/heartbeat" {
			heartbeatCount++
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"accepted":true,"server_time":1712345678}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node", "v1.0")
	cfgProvider := &mockConfigProvider{configVersion: 3, configHash: "sha256:abc", degraded: false}
	mgr := NewManager(client, &mockCollector{}, cfgProvider)

	// Send a single heartbeat manually.
	err := mgr.sendHeartbeat()
	if err != nil {
		t.Fatalf("heartbeat failed: %v", err)
	}
	if heartbeatCount != 1 {
		t.Fatalf("expected 1 heartbeat, got %d", heartbeatCount)
	}

	status := mgr.Status()
	if status.HeartbeatsSent != 1 {
		t.Fatalf("expected 1 heartbeat sent, got %d", status.HeartbeatsSent)
	}
	if !status.LastHeartbeatOK {
		t.Fatal("expected heartbeat OK")
	}
	if !status.IsDeployed {
		t.Fatal("expected IsDeployed")
	}
}

func TestManager_HeartbeatDegraded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"accepted":true}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node", "v1.0")
	cfgProvider := &mockConfigProvider{configVersion: 5, degraded: true}
	mgr := NewManager(client, &mockCollector{}, cfgProvider)

	err := mgr.sendHeartbeat()
	if err != nil {
		t.Fatalf("heartbeat failed: %v", err)
	}

	// When config is degraded, heartbeat should report degraded=true.
	// We can verify this by checking the Status which reads from configProvider.
	status := mgr.Status()
	if !status.Degraded {
		t.Fatal("expected degraded=true when config provider is degraded")
	}
}

func TestManager_HeartbeatBackendFailure(t *testing.T) {
	// Simulate Backend being down — heartbeat must fail gracefully.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node", "v1.0")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{configVersion: 1})

	err := mgr.sendHeartbeat()
	if err == nil {
		t.Fatal("expected heartbeat error")
	}

	status := mgr.Status()
	if status.LastHeartbeatOK {
		t.Fatal("expected LastHeartbeatOK=false after failure")
	}
	if status.LastHeartbeatErr == "" {
		t.Fatal("expected LastHeartbeatErr to be set")
	}

	// Process must still be functional.
	if !status.IsDeployed {
		t.Fatal("agent should still be deployed despite heartbeat failure")
	}
}

func TestManager_HeartbeatLoopStartStop(t *testing.T) {
	var hbCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/agent/heartbeat" {
			hbCount++
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"accepted":true}`)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node", "v1.0")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{configVersion: 1})

	mgr.StartHeartbeatLoop(50 * time.Millisecond)
	time.Sleep(120 * time.Millisecond)
	mgr.StopHeartbeatLoop()

	if hbCount < 1 {
		t.Fatalf("expected at least 1 heartbeat, got %d", hbCount)
	}
}

func TestManager_StatusSnapshot(t *testing.T) {
	client := NewClient("http://localhost:1", "test-node", "v1.0")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{configVersion: 7, configHash: "sha256:xyz"})

	status := mgr.Status()
	if !status.IsDeployed {
		t.Fatal("expected IsDeployed=true")
	}
	if status.Registered {
		t.Fatal("expected Registered=false before any register call")
	}
	if status.HeartbeatsSent != 0 {
		t.Fatal("expected 0 heartbeats initially")
	}
}

func TestManager_StatusHook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/agent/heartbeat" {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"accepted":true}`)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node", "v1.0")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{configVersion: 1})

	var hookCalled bool
	mgr.OnStatusChange(func(s AgentStatus) {
		hookCalled = true
	})

	_ = mgr.sendHeartbeat()
	if !hookCalled {
		t.Fatal("status hook was not called after heartbeat")
	}
}

func TestManager_SetSingboxStatus(t *testing.T) {
	client := NewClient("http://localhost:1", "test-node", "v1.0")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{})

	mgr.SetSingboxStatus("unhealthy")
	status := mgr.Status()
	if status.SingboxStatus != "unhealthy" {
		t.Fatalf("expected singbox unhealthy, got %s", status.SingboxStatus)
	}
}
