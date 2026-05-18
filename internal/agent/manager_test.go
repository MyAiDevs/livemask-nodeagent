package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/MyAiDevs/livemask-nodeagent/internal/singbox"
)

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

type mockConfigProvider struct {
	configVersion int
	configHash    string
	degraded      bool
}

func (m *mockConfigProvider) ConfigVersion() int   { return m.configVersion }
func (m *mockConfigProvider) ConfigHash() string    { return m.configHash }
func (m *mockConfigProvider) IsDegraded() bool      { return m.degraded }

type mockSingboxProvider struct {
	status singbox.RuntimeStatus
}

func (m *mockSingboxProvider) Status() SingboxRuntimeStatus {
	return m.status
}

func TestManager_LoadIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	identityStore := NewIdentityStore(path)

	_ = identityStore.Save(&Identity{NodeID: "saved-uuid", NodeSecret: "saved-secret"})

	client := NewClient("http://localhost:1", "v1.0")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{}, identityStore, nil)

	loaded := mgr.LoadIdentity()
	if !loaded {
		t.Fatal("expected identity to load")
	}
	if mgr.Status().NodeID != "saved-uuid" {
		t.Fatalf("expected NodeID saved-uuid, got %s", mgr.Status().NodeID)
	}
}

func TestManager_LoadIdentityNoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	identityStore := NewIdentityStore(path)

	client := NewClient("http://localhost:1", "v1.0")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{}, identityStore, nil)

	loaded := mgr.LoadIdentity()
	if loaded {
		t.Fatal("expected false when no identity file exists")
	}
}

func TestManager_RegisterNewNode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	identityStore := NewIdentityStore(path)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/agent/register" {
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprint(w, `{"node_id":"new-uuid","node_secret":"new-secret","status":"pending_review"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{}, identityStore, nil)

	err := mgr.Register(context.Background(), "test-server")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	status := mgr.Status()
	if !status.Registered {
		t.Fatal("expected registered=true")
	}
	if status.NodeID != "new-uuid" {
		t.Fatalf("expected NodeID new-uuid, got %s", status.NodeID)
	}

	loaded, _ := identityStore.Load()
	if loaded == nil {
		t.Fatal("identity should be persisted after registration")
	}
	if loaded.NodeID != "new-uuid" {
		t.Fatalf("expected persisted NodeID new-uuid, got %s", loaded.NodeID)
	}
	if loaded.NodeSecret != "new-secret" {
		t.Fatalf("expected persisted NodeSecret new-secret, got %s", loaded.NodeSecret)
	}
}

func TestManager_RegisterFailure_NoExit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	identityStore := NewIdentityStore(path)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{}, identityStore, nil)

	err := mgr.Register(context.Background(), "test-server")
	if err == nil {
		t.Fatal("expected register to fail")
	}

	status := mgr.Status()
	if status.IsDeployed != true {
		t.Fatal("agent should still be deployed despite register failure")
	}
}

func TestManager_HeartbeatWithHMAC(t *testing.T) {
	dir := t.TempDir()
	identityStore := NewIdentityStore(filepath.Join(dir, "identity.json"))
	_ = identityStore.Save(&Identity{NodeID: "hb-node", NodeSecret: "hb-secret"})

	var capturedSig, capturedTimestamp string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/agent/heartbeat" {
			capturedSig = r.Header.Get("X-Signature")
			capturedTimestamp = r.Header.Get("X-Timestamp")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"ok":true,"server_config_version":5}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	client.SetNodeIdentity("hb-node", "hb-secret")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{configVersion: 3, configHash: "sha256:abc"}, identityStore, nil)

	mgr.LoadIdentity()
	err := mgr.sendHeartbeat()
	if err != nil {
		t.Fatalf("heartbeat failed: %v", err)
	}

	expectedSig := ComputeSignature("hb-node", capturedTimestamp, "hb-secret")
	if capturedSig != expectedSig {
		t.Fatalf("HMAC signature mismatch: got %s, expected %s", capturedSig, expectedSig)
	}

	status := mgr.Status()
	if status.HeartbeatsSent != 1 {
		t.Fatalf("expected 1 heartbeat, got %d", status.HeartbeatsSent)
	}
}

func TestManager_HeartbeatDegraded(t *testing.T) {
	dir := t.TempDir()
	identityStore := NewIdentityStore(filepath.Join(dir, "identity.json"))
	_ = identityStore.Save(&Identity{NodeID: "d-node", NodeSecret: "d-secret"})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/agent/heartbeat" {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"ok":true,"server_config_version":2,"degraded":true}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	client.SetNodeIdentity("d-node", "d-secret")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{configVersion: 5, degraded: true}, identityStore, nil)
	mgr.LoadIdentity()

	err := mgr.sendHeartbeat()
	if err != nil {
		t.Fatalf("heartbeat failed: %v", err)
	}

	status := mgr.Status()
	if !status.Degraded {
		t.Fatal("expected degraded=true")
	}
}

func TestManager_HeartbeatBackendFailure_DegradedRetry(t *testing.T) {
	dir := t.TempDir()
	identityStore := NewIdentityStore(filepath.Join(dir, "identity.json"))
	_ = identityStore.Save(&Identity{NodeID: "f-node", NodeSecret: "f-secret"})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	client.SetNodeIdentity("f-node", "f-secret")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{configVersion: 1}, identityStore, nil)
	mgr.LoadIdentity()

	err := mgr.sendHeartbeat()
	if err == nil {
		t.Fatal("expected heartbeat error")
	}

	status := mgr.Status()
	if status.HeartbeatsSent != 1 {
		t.Fatalf("expected 1 heartbeat, got %d", status.HeartbeatsSent)
	}
	if status.LastHeartbeatOK {
		t.Fatal("expected LastHeartbeatOK=false")
	}
	if status.LastHeartbeatErr == "" {
		t.Fatal("expected LastHeartbeatErr to be set")
	}
}

func TestManager_HeartbeatLoopStartStop(t *testing.T) {
	dir := t.TempDir()
	identityStore := NewIdentityStore(filepath.Join(dir, "identity.json"))
	_ = identityStore.Save(&Identity{NodeID: "l-node", NodeSecret: "l-secret"})

	var hbCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/agent/heartbeat" {
			hbCount++
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"ok":true,"server_config_version":1}`)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	client.SetNodeIdentity("l-node", "l-secret")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{configVersion: 1}, identityStore, nil)
	mgr.LoadIdentity()

	mgr.StartHeartbeatLoop(50 * time.Millisecond)
	time.Sleep(120 * time.Millisecond)
	mgr.StopHeartbeatLoop()

	if hbCount < 1 {
		t.Fatalf("expected at least 1 heartbeat, got %d", hbCount)
	}
}

func TestManager_StatusHook(t *testing.T) {
	dir := t.TempDir()
	identityStore := NewIdentityStore(filepath.Join(dir, "identity.json"))
	_ = identityStore.Save(&Identity{NodeID: "h-node", NodeSecret: "h-secret"})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/agent/heartbeat" {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"ok":true,"server_config_version":1}`)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	client.SetNodeIdentity("h-node", "h-secret")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{configVersion: 1}, identityStore, nil)
	mgr.LoadIdentity()

	var hookCalled bool
	mgr.OnStatusChange(func(s AgentStatus) {
		hookCalled = true
	})

	_ = mgr.sendHeartbeat()
	if !hookCalled {
		t.Fatal("status hook was not called after heartbeat")
	}
}

func TestManager_StatusSnapshot(t *testing.T) {
	dir := t.TempDir()
	identityStore := NewIdentityStore(filepath.Join(dir, "identity.json"))
	client := NewClient("http://localhost:1", "v1.0")
	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{configVersion: 7, configHash: "sha256:xyz"}, identityStore, nil)

	status := mgr.Status()
	if !status.IsDeployed {
		t.Fatal("expected IsDeployed=true")
	}
	if status.Registered {
		t.Fatal("expected Registered=false")
	}
	if status.HeartbeatsSent != 0 {
		t.Fatal("expected 0 heartbeats")
	}
}

func TestManager_SingboxStatusRunning(t *testing.T) {
	dir := t.TempDir()
	identityStore := NewIdentityStore(filepath.Join(dir, "identity.json"))
	client := NewClient("http://localhost:1", "v1.0")

	sbProvider := &mockSingboxProvider{
		status: singbox.RuntimeStatus{
			Enabled: true,
			Status:  "running",
			PID:     1234,
			ConfigPath: "/tmp/singbox.json",
			ListenHost: "127.0.0.1",
			ListenPort: 10808,
		},
	}

	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{}, identityStore, sbProvider)

	status := mgr.Status()
	if status.SingboxStatus != SingboxStatusRunning {
		t.Fatalf("expected singbox_status running, got %s", status.SingboxStatus)
	}
	if status.Singbox == nil {
		t.Fatal("expected singbox nested object")
	}
	if status.Singbox.PID != 1234 {
		t.Fatalf("expected PID 1234, got %d", status.Singbox.PID)
	}
	if status.Degraded {
		t.Fatal("expected not degraded when singbox running")
	}
}

func TestManager_SingboxStatusFailedDegrades(t *testing.T) {
	dir := t.TempDir()
	identityStore := NewIdentityStore(filepath.Join(dir, "identity.json"))
	client := NewClient("http://localhost:1", "v1.0")

	sbProvider := &mockSingboxProvider{
		status: singbox.RuntimeStatus{
			Enabled:   true,
			Status:    "failed",
			LastError: "singbox_failed: port check failed",
		},
	}

	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{}, identityStore, sbProvider)

	status := mgr.Status()
	if status.SingboxStatus != SingboxStatusFailed {
		t.Fatalf("expected singbox_status failed, got %s", status.SingboxStatus)
	}
	if !status.Degraded {
		t.Fatal("expected degraded when singbox failed")
	}
	if status.DegradedReason != "singbox_failed" {
		t.Fatalf("expected degraded_reason 'singbox_failed', got %q", status.DegradedReason)
	}
}

func TestManager_SingboxStatusUnhealthyDegrades(t *testing.T) {
	dir := t.TempDir()
	identityStore := NewIdentityStore(filepath.Join(dir, "identity.json"))
	client := NewClient("http://localhost:1", "v1.0")

	sbProvider := &mockSingboxProvider{
		status: singbox.RuntimeStatus{
			Enabled:   true,
			Status:    "unhealthy",
			LastError: "process running but port unreachable",
		},
	}

	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{}, identityStore, sbProvider)

	status := mgr.Status()
	if status.SingboxStatus != SingboxStatusUnhealthy {
		t.Fatalf("expected singbox_status unhealthy, got %s", status.SingboxStatus)
	}
	if !status.Degraded {
		t.Fatal("expected degraded when singbox unhealthy")
	}
	if status.DegradedReason != "singbox_unhealthy" {
		t.Fatalf("expected degraded_reason 'singbox_unhealthy', got %q", status.DegradedReason)
	}
}

func TestManager_SingboxStatusDisabled(t *testing.T) {
	dir := t.TempDir()
	identityStore := NewIdentityStore(filepath.Join(dir, "identity.json"))
	client := NewClient("http://localhost:1", "v1.0")

	sbProvider := &mockSingboxProvider{
		status: singbox.RuntimeStatus{
			Enabled: false,
			Status:  "disabled",
		},
	}

	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{}, identityStore, sbProvider)

	status := mgr.Status()
	if status.SingboxStatus != SingboxStatusDisabled {
		t.Fatalf("expected singbox_status disabled, got %s", status.SingboxStatus)
	}
	if status.Degraded {
		t.Fatal("expected not degraded when singbox disabled")
	}
}

func TestManager_NoSingboxProvider(t *testing.T) {
	dir := t.TempDir()
	identityStore := NewIdentityStore(filepath.Join(dir, "identity.json"))
	client := NewClient("http://localhost:1", "v1.0")

	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{}, identityStore, nil)

	status := mgr.Status()
	if status.SingboxStatus != SingboxStatusUnknown {
		t.Fatalf("expected singbox_status unknown, got %s", status.SingboxStatus)
	}
	if status.Singbox != nil {
		t.Fatal("expected nil singbox object when no provider")
	}
}

func TestManager_HeartbeatSingboxFailedDegrades(t *testing.T) {
	dir := t.TempDir()
	identityStore := NewIdentityStore(filepath.Join(dir, "identity.json"))
	_ = identityStore.Save(&Identity{NodeID: "sb-node", NodeSecret: "sb-secret"})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/agent/heartbeat" {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"ok":true,"server_config_version":1}`)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "v1.0")
	client.SetNodeIdentity("sb-node", "sb-secret")

	sbProvider := &mockSingboxProvider{
		status: singbox.RuntimeStatus{
			Enabled:   true,
			Status:    "failed",
			LastError: "exited unexpectedly",
		},
	}

	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{configVersion: 1}, identityStore, sbProvider)
	mgr.LoadIdentity()

	err := mgr.sendHeartbeat()
	if err != nil {
		t.Fatalf("heartbeat failed: %v", err)
	}

	status := mgr.Status()
	if status.SingboxStatus != SingboxStatusFailed {
		t.Fatalf("expected singbox_status failed, got %s", status.SingboxStatus)
	}
	if !status.Degraded {
		t.Fatal("expected degraded=true")
	}
	if status.DegradedReason != "singbox_failed" {
		t.Fatalf("expected degraded_reason 'singbox_failed', got %q", status.DegradedReason)
	}
}

func TestManager_SingboxStatusNewFields(t *testing.T) {
	dir := t.TempDir()
	identityStore := NewIdentityStore(filepath.Join(dir, "identity.json"))
	_ = identityStore.Save(&Identity{NodeID: "ep-node", NodeSecret: "ep-secret"})

	sbProvider := &mockSingboxProvider{
		status: singbox.RuntimeStatus{
			Enabled:            true,
			Status:             "running",
			ListenHost:         "0.0.0.0",
			ListenPort:         10808,
			Transport:          "mixed",
			ProtocolProfile:    "tcp_udp",
			PublicEndpointHost: "node1.example.com",
			PublicEndpointPort: 8443,
			EndpointReady:      true,
		},
	}

	client := NewClient("http://localhost:1", "v1.0")
	client.SetNodeIdentity("ep-node", "ep-secret")

	mgr := NewManager(client, &mockCollector{}, &mockConfigProvider{configVersion: 1}, identityStore, sbProvider)
	mgr.LoadIdentity()

	status := mgr.Status()
	if status.Singbox == nil {
		t.Fatal("expected singbox status to be non-nil")
	}
	if status.Singbox.Transport != "mixed" {
		t.Fatalf("expected transport mixed, got %s", status.Singbox.Transport)
	}
	if status.Singbox.ProtocolProfile != "tcp_udp" {
		t.Fatalf("expected protocol_profile tcp_udp, got %s", status.Singbox.ProtocolProfile)
	}
	if status.Singbox.PublicEndpointHost != "node1.example.com" {
		t.Fatalf("expected public_endpoint_host node1.example.com, got %s", status.Singbox.PublicEndpointHost)
	}
	if status.Singbox.PublicEndpointPort != 8443 {
		t.Fatalf("expected public_endpoint_port 8443, got %d", status.Singbox.PublicEndpointPort)
	}
	if !status.Singbox.EndpointReady {
		t.Fatal("expected endpoint_ready=true")
	}
}
