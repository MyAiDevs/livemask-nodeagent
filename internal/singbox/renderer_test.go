package singbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRender_MinimalConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		Enabled:    true,
		ConfigPath: cfgPath,
		ListenHost: "127.0.0.1",
		ListenPort: 10808,
		LogLevel:   "info",
	}

	if err := Render(cfg); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	// Verify contents — there should be socks and mixed inbounds.
	content := string(data)
	if !strings.Contains(content, `"socks-in"`) {
		t.Fatal("missing socks inbound tag")
	}
	if !strings.Contains(content, `"mixed-in"`) {
		t.Fatal("missing mixed inbound tag")
	}
	if !strings.Contains(content, `"direct"`) {
		t.Fatal("missing direct outbound")
	}
	if !strings.Contains(content, `"block"`) {
		t.Fatal("missing block outbound")
	}
	if !strings.Contains(content, `10808`) {
		t.Fatal("missing listen port")
	}
	if !strings.Contains(content, `"info"`) {
		t.Fatal("missing log level")
	}

	// Check permissions
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Logf("config has permissions %o (expected 0600)", info.Mode().Perm())
	}
}

func TestRender_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	// Render once.
	cfg := &SingboxConfig{
		ConfigPath: cfgPath,
		ListenPort: 10808,
	}
	if err := Render(cfg); err != nil {
		t.Fatalf("first render: %v", err)
	}

	// Render again (should use atomic write).
	cfg.ListenPort = 20808
	if err := Render(cfg); err != nil {
		t.Fatalf("second render: %v", err)
	}

	// Verify updated port.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), `20808`) {
		t.Fatal("expected updated port in file")
	}

	// No .tmp file should remain.
	if _, err := os.Stat(cfgPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file should have been cleaned up")
	}
}

func TestRender_DefaultValues(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	// Only ConfigPath and ListenPort set; host/logLevel should default.
	cfg := &SingboxConfig{
		ConfigPath: cfgPath,
		ListenPort: 10808,
	}
	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, `"127.0.0.1"`) {
		t.Fatal("expected default listen host 127.0.0.1")
	}
	if !strings.Contains(content, `"info"`) {
		t.Fatal("expected default log level info")
	}
}

func TestRender_InvalidPort(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	tests := []struct {
		port    int
		wantErr string
	}{
		{0, "invalid"},
		{-1, "invalid"},
		{65536, "invalid"},
		{70000, "invalid"},
	}
	for _, tt := range tests {
		cfg := &SingboxConfig{
			ConfigPath: cfgPath,
			ListenPort: tt.port,
		}
		err := Render(cfg)
		if err == nil {
			t.Fatalf("expected error for port %d", tt.port)
		}
		if !strings.Contains(err.Error(), tt.wantErr) {
			t.Fatalf("port %d: expected error containing %q, got %q", tt.port, tt.wantErr, err.Error())
		}
	}
}

func TestRender_NilConfig(t *testing.T) {
	err := Render(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestRender_DoesNotIncludeSecrets(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath: cfgPath,
		ListenPort: 10808,
	}
	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// Ensure no secret-like fields
	for _, secret := range []string{"node_secret", "password", "key", "token", "private_key"} {
		if strings.Contains(strings.ToLower(content), secret) {
			t.Fatalf("config should not contain secret field: %s", secret)
		}
	}
}
