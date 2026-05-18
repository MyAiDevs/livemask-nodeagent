package singbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSingboxScript creates a shell script that simulates sing-box run behavior.
// It writes a marker file and sleeps until killed.
func fakeSingboxScript(t *testing.T, dir string, markerFile string) string {
	t.Helper()
	if markerFile == "" {
		markerFile = filepath.Join(dir, ".singbox-running")
	}
	scriptPath := filepath.Join(dir, "fake-singbox.sh")
	script := `#!/bin/sh
# fake sing-box: write marker file and sleep until killed
MARKER="` + markerFile + `"
echo "$$" > "$MARKER"
trap "rm -f \"$MARKER\"; exit 0" TERM INT
while true; do sleep 1; done
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake script: %v", err)
	}
	return scriptPath
}

func TestManager_DisabledMode(t *testing.T) {
	dir := t.TempDir()
	cfg := &SingboxConfig{
		Enabled:    false,
		ConfigPath: filepath.Join(dir, "singbox.json"),
		ListenHost: "127.0.0.1",
		ListenPort: 10808,
	}
	mgr := NewManager(cfg)

	s := mgr.Status()
	if s.Enabled {
		t.Fatal("expected enabled=false")
	}
	if s.Status != string(StatusDisabled) {
		t.Fatalf("expected status disabled, got %s", s.Status)
	}

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start should not error in disabled mode: %v", err)
	}
	if mgr.IsRunning() {
		t.Fatal("should not be running in disabled mode")
	}
}

func TestManager_StartWithFakeBinary(t *testing.T) {
	dir := t.TempDir()
	markerFile := filepath.Join(dir, ".singbox-running")
	binPath := fakeSingboxScript(t, dir, markerFile)
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		Enabled:    true,
		BinPath:    binPath,
		ConfigPath: cfgPath,
		WorkDir:    dir,
		LogPath:    filepath.Join(dir, "singbox.log"),
		ListenHost: "127.0.0.1",
		ListenPort: 10808,
		LogLevel:   "info",
	}
	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	mgr := NewManager(cfg)
	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	s := mgr.Status()
	if s.Status != string(StatusRunning) {
		t.Fatalf("expected running, got %s", s.Status)
	}
	if s.PID <= 0 {
		t.Fatal("expected non-zero PID")
	}
	if s.RestartCount != 1 {
		t.Fatalf("expected restart_count=1, got %d", s.RestartCount)
	}
	if s.ConfigPath != cfgPath {
		t.Fatalf("expected config_path %s", cfgPath)
	}
	if !mgr.IsRunning() {
		t.Fatal("IsRunning should be true")
	}

	// Verify log was written (file exists).
	if _, err := os.Stat(cfg.LogPath); err != nil {
		t.Fatalf("log file should exist: %v", err)
	}

	// Stop
	if err := mgr.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	s = mgr.Status()
	if s.Status != string(StatusStopped) {
		t.Fatalf("expected stopped, got %s", s.Status)
	}
	if s.PID != 0 {
		t.Fatal("expected PID=0 after stop")
	}
}

func TestManager_StartMissingBinary(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")
	cfg := &SingboxConfig{
		Enabled:    true,
		BinPath:    "/nonexistent/sing-box",
		ConfigPath: cfgPath,
		WorkDir:    dir,
		ListenHost: "127.0.0.1",
		ListenPort: 10808,
	}
	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	mgr := NewManager(cfg)
	ctx := context.Background()
	err := mgr.Start(ctx)
	if err == nil {
		t.Fatal("expected error for missing binary")
	}

	s := mgr.Status()
	if s.Status != string(StatusFailed) {
		t.Fatalf("expected status failed, got %s", s.Status)
	}
	if s.LastError == "" {
		t.Fatal("expected LastError to be set")
	}
}

func TestManager_StopNoProcess(t *testing.T) {
	dir := t.TempDir()
	cfg := &SingboxConfig{
		Enabled:    true,
		ConfigPath: filepath.Join(dir, "singbox.json"),
		ListenPort: 10808,
	}
	mgr := NewManager(cfg)
	ctx := context.Background()

	if err := mgr.Stop(ctx); err != nil {
		t.Fatalf("Stop without start: %v", err)
	}
	s := mgr.Status()
	if s.Status != string(StatusStopped) {
		t.Fatalf("expected stopped, got %s", s.Status)
	}
}

func TestManager_Restart(t *testing.T) {
	dir := t.TempDir()
	markerFile := filepath.Join(dir, ".singbox-running")
	binPath := fakeSingboxScript(t, dir, markerFile)
	cfgPath := filepath.Join(dir, "singbox.json")
	cfg := &SingboxConfig{
		Enabled:    true,
		BinPath:    binPath,
		ConfigPath: cfgPath,
		WorkDir:    dir,
		ListenHost: "127.0.0.1",
		ListenPort: 10808,
	}
	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	mgr := NewManager(cfg)
	ctx := context.Background()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	firstPID := mgr.Status().PID
	time.Sleep(100 * time.Millisecond)

	if err := mgr.Restart(ctx); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	s := mgr.Status()
	if s.RestartCount < 2 {
		t.Fatalf("expected restart_count >= 2, got %d", s.RestartCount)
	}
	if s.PID == firstPID {
		t.Fatal("PID should have changed after restart")
	}
	if s.Status != string(StatusRunning) {
		t.Fatalf("expected running after restart, got %s", s.Status)
	}
}

func TestManager_HealthCheckProcessDead(t *testing.T) {
	dir := t.TempDir()
	cfg := &SingboxConfig{
		Enabled:    true,
		ConfigPath: filepath.Join(dir, "singbox.json"),
		ListenHost: "127.0.0.1",
		ListenPort: 10808,
	}
	mgr := NewManager(cfg)

	mgr.HealthCheck()
	s := mgr.Status()
	if s.Status != string(StatusStopped) {
		t.Fatalf("expected stopped, got %s", s.Status)
	}
}

func TestManager_ApplyConfig(t *testing.T) {
	dir := t.TempDir()
	markerFile := filepath.Join(dir, ".singbox-running")
	binPath := fakeSingboxScript(t, dir, markerFile)
	cfgPath := filepath.Join(dir, "singbox.json")
	cfg := &SingboxConfig{
		Enabled:               true,
		BinPath:               binPath,
		ConfigPath:            cfgPath,
		WorkDir:               dir,
		LogPath:               filepath.Join(dir, "singbox.log"),
		ListenHost:            "127.0.0.1",
		ListenPort:            10808,
		RestartOnConfigChange: true,
	}

	mgr := NewManager(cfg)
	ctx := context.Background()

	if err := mgr.ApplyConfig(ctx, cfg, "hash-1"); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	s := mgr.Status()
	if s.Status != string(StatusRunning) {
		t.Fatalf("expected running, got %s", s.Status)
	}
	if s.RestartCount != 1 {
		t.Fatalf("expected restart_count=1, got %d", s.RestartCount)
	}

	firstPID := s.PID
	if err := mgr.ApplyConfig(ctx, cfg, "hash-1"); err != nil {
		t.Fatalf("ApplyConfig same hash: %v", err)
	}
	s = mgr.Status()
	if s.PID != firstPID {
		t.Fatal("same hash should not restart the process")
	}

	if err := mgr.ApplyConfig(ctx, cfg, "hash-2"); err != nil {
		t.Fatalf("ApplyConfig new hash: %v", err)
	}
	s = mgr.Status()
	if s.PID == firstPID {
		t.Fatal("new hash should restart the process")
	}
	if s.RestartCount != 2 {
		t.Fatalf("expected restart_count=2, got %d", s.RestartCount)
	}
}

func TestManager_ApplyConfigInvalidPort(t *testing.T) {
	dir := t.TempDir()
	markerFile := filepath.Join(dir, ".singbox-running")
	binPath := fakeSingboxScript(t, dir, markerFile)
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		Enabled:               true,
		BinPath:               binPath,
		ConfigPath:            cfgPath,
		WorkDir:               dir,
		ListenPort:            0,
		RestartOnConfigChange: true,
	}

	mgr := NewManager(cfg)
	ctx := context.Background()

	err := mgr.ApplyConfig(ctx, cfg, "hash-1")
	if err == nil {
		t.Fatal("expected error for invalid port")
	}

	s := mgr.Status()
	if s.Status != string(StatusFailed) {
		t.Fatalf("expected failed status, got %s", s.Status)
	}
}

func TestManager_DisabledDoesNotStart(t *testing.T) {
	dir := t.TempDir()
	binPath := fakeSingboxScript(t, dir, "")
	cfgPath := filepath.Join(dir, "singbox.json")
	cfg := &SingboxConfig{
		Enabled:    false,
		BinPath:    binPath,
		ConfigPath: cfgPath,
	}

	mgr := NewManager(cfg)
	ctx := context.Background()

	if err := mgr.ApplyConfig(ctx, cfg, "hash-1"); err != nil {
		t.Fatalf("ApplyConfig disabled: %v", err)
	}
	if mgr.IsRunning() {
		t.Fatal("disabled should not start")
	}
}

func TestManager_HealthCheckLoop(t *testing.T) {
	dir := t.TempDir()
	markerFile := filepath.Join(dir, ".singbox-running")
	binPath := fakeSingboxScript(t, dir, markerFile)
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		Enabled:    true,
		BinPath:    binPath,
		ConfigPath: cfgPath,
		WorkDir:    dir,
		ListenHost: "127.0.0.1",
		ListenPort: 10808,
	}
	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	mgr := NewManager(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	mgr.StartHealthLoop(ctx)
	time.Sleep(200 * time.Millisecond)
	cancel()
	mgr.WaitForShutdown()

	s := mgr.Status()
	if s.LastHealthCheckAt == nil {
		t.Fatal("expected LastHealthCheckAt to be set")
	}
}

func TestManager_LastErrorNoSecrets(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")
	cfg := &SingboxConfig{
		Enabled:    true,
		BinPath:    "/nonexistent/sing-box",
		ConfigPath: cfgPath,
		WorkDir:    dir,
		ListenHost: "127.0.0.1",
		ListenPort: 10808,
	}
	// Render config first so the config file exists.
	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	mgr := NewManager(cfg)
	_ = mgr.Start(context.Background())

	s := mgr.Status()
	if s.LastError == "" {
		t.Fatal("expected error for missing binary")
	}
	for _, secret := range []string{"node_secret", "password", "key", "token", "private_key"} {
		if strings.Contains(strings.ToLower(s.LastError), secret) {
			t.Fatalf("error should not contain secret field: %s", secret)
		}
	}
}

func TestManager_StopDeadProcess(t *testing.T) {
	dir := t.TempDir()
	cfg := &SingboxConfig{
		Enabled:    true,
		ConfigPath: filepath.Join(dir, "singbox.json"),
		ListenPort: 10808,
	}
	mgr := NewManager(cfg)

	mgr.statusMu.Lock()
	mgr.status.Status = string(StatusFailed)
	mgr.status.PID = 999999
	mgr.statusMu.Unlock()

	ctx := context.Background()
	if err := mgr.Stop(ctx); err != nil {
		t.Fatalf("Stop dead process: %v", err)
	}
	s := mgr.Status()
	if s.Status != string(StatusStopped) {
		t.Fatalf("expected stopped, got %s", s.Status)
	}
}

func TestManager_EndpointReadyDisabled(t *testing.T) {
	cfg := &SingboxConfig{Enabled: false, ListenPort: 10808}
	mgr := NewManager(cfg)
	mgr.HealthCheck()
	s := mgr.Status()
	if s.EndpointReady {
		t.Fatal("expected endpoint_ready=false when disabled")
	}
}

func TestManager_EndpointReadyNoProcess(t *testing.T) {
	dir := t.TempDir()
	cfg := &SingboxConfig{
		Enabled:            true,
		ConfigPath:         filepath.Join(dir, "singbox.json"),
		ListenHost:         "127.0.0.1",
		ListenPort:         10808,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 443,
	}
	mgr := NewManager(cfg)
	mgr.HealthCheck()
	s := mgr.Status()
	if s.EndpointReady {
		t.Fatal("expected endpoint_ready=false when process not started")
	}
}

func TestManager_RuntimeStatusNewFields(t *testing.T) {
	dir := t.TempDir()
	cfg := &SingboxConfig{
		Enabled:            true,
		ConfigPath:         filepath.Join(dir, "singbox.json"),
		ListenHost:         "0.0.0.0",
		ListenPort:         10808,
		Transport:          "mixed",
		ProtocolProfile:    "tcp_udp",
		PublicEndpointHost: "node1.example.com",
		PublicEndpointPort: 8443,
	}
	mgr := NewManager(cfg)
	s := mgr.Status()

	if s.Transport != "mixed" {
		t.Fatalf("expected transport mixed, got %s", s.Transport)
	}
	if s.ProtocolProfile != "tcp_udp" {
		t.Fatalf("expected protocol_profile tcp_udp, got %s", s.ProtocolProfile)
	}
	if s.PublicEndpointHost != "node1.example.com" {
		t.Fatalf("expected public_endpoint_host node1.example.com, got %s", s.PublicEndpointHost)
	}
	if s.PublicEndpointPort != 8443 {
		t.Fatalf("expected public_endpoint_port 8443, got %d", s.PublicEndpointPort)
	}
	if s.EndpointReady {
		t.Fatal("expected endpoint_ready=false initially")
	}
}

func TestManager_HealthCheckPublicEndpointValid(t *testing.T) {
	dir := t.TempDir()
	markerFile := filepath.Join(dir, ".singbox-running")
	binPath := fakeSingboxScript(t, dir, markerFile)
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		Enabled:            true,
		BinPath:            binPath,
		ConfigPath:         cfgPath,
		WorkDir:            dir,
		LogPath:            filepath.Join(dir, "singbox.log"),
		ListenHost:         "127.0.0.1",
		ListenPort:         10808,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 443,
		Transport:          "mixed",
	}
	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	mgr := NewManager(cfg)
	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Health check with fake binary: process alive but port unreachable.
	mgr.HealthCheck()
	s := mgr.Status()
	// Fake binary doesn't open a real port, so status is unhealthy.
	if s.Status != string(StatusUnhealthy) {
		t.Fatalf("expected unhealthy (fake binary no real port), got %s", s.Status)
	}
	if s.Transport != "mixed" {
		t.Fatalf("expected transport mixed, got %s", s.Transport)
	}
	if s.PublicEndpointHost != "node.example.com" {
		t.Fatalf("expected public_endpoint_host, got %s", s.PublicEndpointHost)
	}
	if s.PublicEndpointPort != 443 {
		t.Fatalf("expected public_endpoint_port 443, got %d", s.PublicEndpointPort)
	}
	if s.EndpointReady {
		t.Fatal("expected endpoint_ready=false (port unreachable)")
	}
}

func TestManager_ApplyConfigPropagatesSchema(t *testing.T) {
	dir := t.TempDir()
	markerFile := filepath.Join(dir, ".singbox-running")
	binPath := fakeSingboxScript(t, dir, markerFile)
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		Enabled:               true,
		BinPath:               binPath,
		ConfigPath:            cfgPath,
		WorkDir:               dir,
		ListenHost:            "127.0.0.1",
		ListenPort:            10808,
		Transport:             "mixed",
		DNSEnabled:            true,
		RouteGlobal:           false,
		BypassLAN:             true,
		RestartOnConfigChange: true,
	}

	mgr := NewManager(cfg)
	ctx := context.Background()

	if err := mgr.ApplyConfig(ctx, cfg, "hash-1"); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	s := mgr.Status()
	if s.Status != string(StatusRunning) {
		t.Fatalf("expected running, got %s", s.Status)
	}

	// Update config with new transport.
	cfg.Transport = "tun"
	cfg.TunInterfaceName = "tun0"
	cfg.TunMTU = 9000
	if err := mgr.ApplyConfig(ctx, cfg, "hash-2"); err != nil {
		t.Fatalf("ApplyConfig hash-2: %v", err)
	}

	// Verify new fields in runtime status.
	s = mgr.Status()
	if s.Transport != "tun" {
		t.Fatalf("expected transport tun, got %s", s.Transport)
	}
}
