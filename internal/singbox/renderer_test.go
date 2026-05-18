package singbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRender_ProductionSchema(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath:  cfgPath,
		ListenHost:  "127.0.0.1",
		ListenPort:  10808,
		LogLevel:    "info",
		Transport:   "mixed",
		DNSEnabled:  true,
		DNSStrategy: "prefer_ipv4",
		DNSServers:  []string{"1.1.1.1", "8.8.8.8"},
		RouteGlobal: false,
		BypassLAN:   true,
	}

	if err := Render(cfg); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	// Unmarshal and verify structure.
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify inbounds.
	inbounds, ok := parsed["inbounds"].([]any)
	if !ok {
		t.Fatal("inbounds not found or not an array")
	}
	if len(inbounds) < 2 {
		t.Fatalf("expected at least 2 inbounds, got %d", len(inbounds))
	}

	// Verify first inbound is "mixed"
	mixedInbound := inbounds[1].(map[string]any)
	if mixedInbound["type"] != "mixed" {
		t.Fatalf("expected mixed inbound type, got %s", mixedInbound["type"])
	}
	if mixedInbound["listen_port"] != float64(10808) {
		t.Fatalf("expected listen_port 10808, got %v", mixedInbound["listen_port"])
	}

	// Verify outbounds.
	outbounds, ok := parsed["outbounds"].([]any)
	if !ok {
		t.Fatal("outbounds not found")
	}
	if len(outbounds) < 2 {
		t.Fatalf("expected at least 2 outbounds, got %d", len(outbounds))
	}

	// Verify route.
	route, ok := parsed["route"].(map[string]any)
	if !ok {
		t.Fatal("route not found")
	}
	if route["final"] != "direct" {
		t.Fatalf("expected route final=direct, got %s", route["final"])
	}

	// Verify rules include bypass-LAN and DNS.
	rules, ok := route["rules"].([]any)
	if !ok {
		t.Fatal("route.rules not found")
	}
	if len(rules) < 2 {
		t.Fatalf("expected at least 2 rules, got %d", len(rules))
	}

	// Verify DNS section.
	dns, ok := parsed["dns"].(map[string]any)
	if !ok {
		t.Fatal("dns not found")
	}
	if dns["strategy"] != "prefer_ipv4" {
		t.Fatalf("expected dns strategy prefer_ipv4, got %s", dns["strategy"])
	}
	dnsServers, ok := dns["servers"].([]any)
	if !ok || len(dnsServers) == 0 {
		t.Fatal("dns servers not found")
	}
	firstServer := dnsServers[0].(map[string]any)
	if firstServer["address"] != "1.1.1.1" {
		t.Fatalf("expected dns server 1.1.1.1, got %s", firstServer["address"])
	}

	// Verify permissions.
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Logf("perms %o (expected 0600)", info.Mode().Perm())
	}
}

func TestRender_TunTransport(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath:       cfgPath,
		ListenHost:       "127.0.0.1",
		ListenPort:       10808,
		Transport:        "tun",
		TunInterfaceName: "tun0",
		TunMTU:           9000,
	}

	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	var parsed map[string]any
	json.Unmarshal(data, &parsed)

	inbounds := parsed["inbounds"].([]any)
	tunFound := false
	for _, ib := range inbounds {
		ibm := ib.(map[string]any)
		if ibm["type"] == "tun" {
			tunFound = true
			if ibm["interface_name"] != "tun0" {
				t.Fatalf("expected interface_name tun0, got %s", ibm["interface_name"])
			}
			if ibm["mtu"] != float64(9000) {
				t.Fatalf("expected mtu 9000, got %v", ibm["mtu"])
			}
		}
	}
	if !tunFound {
		t.Fatal("tun inbound not found")
	}
}

func TestRender_RouteGlobal(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath:       cfgPath,
		ListenPort:       10808,
		RouteGlobal:      true,
		ProxyOutboundTag: "proxy",
	}

	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	var parsed map[string]any
	json.Unmarshal(data, &parsed)

	route := parsed["route"].(map[string]any)
	if route["final"] != "proxy" {
		t.Fatalf("expected route final=proxy, got %s", route["final"])
	}

	outbounds := parsed["outbounds"].([]any)
	proxyFound := false
	for _, ob := range outbounds {
		obm := ob.(map[string]any)
		if obm["tag"] == "proxy" {
			proxyFound = true
		}
	}
	if !proxyFound {
		t.Fatal("proxy outbound not found")
	}
}

func TestRender_NoSecrets(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath: cfgPath,
		ListenPort: 10808,
	}
	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	for _, secret := range []string{"node_secret", "password", "key", "token", "private_key"} {
		if strings.Contains(strings.ToLower(content), secret) {
			t.Fatalf("config should not contain secret field: %s", secret)
		}
	}
}

func TestRender_InvalidPublicEndpointPort(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath:         cfgPath,
		ListenPort:         10808,
		PublicEndpointPort: 65536,
	}
	err := Render(cfg)
	if err == nil {
		t.Fatal("expected error for invalid public endpoint port")
	}
	if !strings.Contains(err.Error(), "public endpoint port") {
		t.Fatalf("expected error about public endpoint port, got: %v", err)
	}
}

func TestRender_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{ConfigPath: cfgPath, ListenPort: 10808}
	if err := Render(cfg); err != nil {
		t.Fatalf("first render: %v", err)
	}
	cfg.ListenPort = 20808
	if err := Render(cfg); err != nil {
		t.Fatalf("second render: %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "20808") {
		t.Fatal("expected updated port")
	}
	if _, err := os.Stat(cfgPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file should be cleaned up")
	}
}

func TestIsEndpointReady_NotEnabled(t *testing.T) {
	ready, reason := IsEndpointReady(&SingboxConfig{Enabled: false, ListenPort: 10808})
	if ready {
		t.Fatal("expected not ready for disabled")
	}
	if reason != "singbox not enabled" {
		t.Fatalf("expected 'singbox not enabled', got %s", reason)
	}
}

func TestIsEndpointReady_InvalidPort(t *testing.T) {
	ready, reason := IsEndpointReady(&SingboxConfig{Enabled: true, ListenPort: 0})
	if ready {
		t.Fatal("expected not ready for invalid port")
	}
	if !strings.Contains(reason, "listen port invalid") {
		t.Fatalf("expected 'listen port invalid', got %s", reason)
	}
}

func TestIsEndpointReady_PublicEndpointMissingHost(t *testing.T) {
	ready, reason := IsEndpointReady(&SingboxConfig{
		Enabled:            true,
		ListenPort:         10808,
		PublicEndpointPort: 443,
	})
	if ready {
		t.Fatal("expected not ready when public port set but host empty")
	}
	if !strings.Contains(reason, "public endpoint host is empty") {
		t.Fatalf("expected 'public endpoint host is empty', got %s", reason)
	}
}

func TestIsEndpointReady_Ready(t *testing.T) {
	ready, reason := IsEndpointReady(&SingboxConfig{
		Enabled:            true,
		ListenPort:         10808,
		PublicEndpointHost: "node1.example.com",
		PublicEndpointPort: 443,
	})
	if !ready {
		t.Fatalf("expected ready, got: %s", reason)
	}
}

func TestProxyHostPort(t *testing.T) {
	hostPort := ProxyHostPort(&SingboxConfig{ListenHost: "127.0.0.1", ListenPort: 10808})
	if hostPort != "127.0.0.1:10808" {
		t.Fatalf("expected 127.0.0.1:10808, got %s", hostPort)
	}
}

func TestProxyHostPort_EmptyHost(t *testing.T) {
	hostPort := ProxyHostPort(&SingboxConfig{ListenPort: 10808})
	if hostPort != "127.0.0.1:10808" {
		t.Fatalf("expected 127.0.0.1:10808, got %s", hostPort)
	}
}

func TestRender_DNSDefaultServers(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath: cfgPath,
		ListenPort: 10808,
		DNSEnabled: true,
	}
	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	var parsed map[string]any
	json.Unmarshal(data, &parsed)

	dns := parsed["dns"].(map[string]any)
	servers := dns["servers"].([]any)
	if len(servers) < 1 {
		t.Fatal("expected at least 1 default DNS server")
	}
	defServer := servers[0].(map[string]any)
	if defServer["address"] != "https://1.1.1.1/dns-query" {
		t.Fatalf("expected default dns, got %s", defServer["address"])
	}
}

func TestRender_NoDNSWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath: cfgPath,
		ListenPort: 10808,
		DNSEnabled: false,
	}
	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(data), `"dns"`) {
		t.Fatal("dns section should not be present when disabled")
	}
}
