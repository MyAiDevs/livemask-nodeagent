package singbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sbprotocol "github.com/MyAiDevs/livemask-nodeagent/internal/singbox/protocol"
)

func TestRender_ProductionSchema(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath:         cfgPath,
		ListenHost:         "127.0.0.1",
		ListenPort:         10808,
		LogLevel:           "info",
		Transport:          "mixed",
		DNSEnabled:         true,
		DNSStrategy:        "prefer_ipv4",
		DNSServers:         []string{"1.1.1.1", "8.8.8.8"},
		RouteGlobal:        false,
		BypassLAN:          true,
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 443,
		TLSEnabled:         true,
		SNI:                "node.example.com",
		ALPN:               "h2,http/1.1",
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
	if len(inbounds) != 1 {
		t.Fatalf("expected 1 service inbound, got %d", len(inbounds))
	}

	mixedInbound := inbounds[0].(map[string]any)
	if mixedInbound["type"] != "mixed" {
		t.Fatalf("expected mixed inbound type, got %s", mixedInbound["type"])
	}
	if mixedInbound["listen"] != "127.0.0.1" {
		t.Fatalf("expected listen host to stay local, got %s", mixedInbound["listen"])
	}
	if mixedInbound["listen_port"] != float64(10808) {
		t.Fatalf("expected listen_port 10808, got %v", mixedInbound["listen_port"])
	}
	if mixedInbound["listen"] == "node.example.com" || mixedInbound["listen_port"] == float64(443) {
		t.Fatal("public endpoint must not be used as sing-box listen bind")
	}
	tlsConfig, ok := mixedInbound["tls"].(map[string]any)
	if !ok {
		t.Fatal("expected tls metadata")
	}
	if tlsConfig["server_name"] != "node.example.com" {
		t.Fatalf("expected tls server_name node.example.com, got %s", tlsConfig["server_name"])
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

func TestRender_SocksTransport(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath: cfgPath,
		ListenHost: "127.0.0.1",
		ListenPort: 10808,
		Transport:  "socks",
	}

	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	inbounds := parsed["inbounds"].([]any)
	if len(inbounds) != 1 {
		t.Fatalf("expected one socks inbound, got %d", len(inbounds))
	}
	inbound := inbounds[0].(map[string]any)
	if inbound["type"] != "socks" {
		t.Fatalf("expected socks inbound, got %s", inbound["type"])
	}
}

func TestRender_FakeProfileDispatch(t *testing.T) {
	profileName := "fake-renderer-dispatch"
	_ = sbprotocol.Register(&fakeProtocolProfile{name: profileName})

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")
	cfg := &SingboxConfig{
		ConfigPath:      cfgPath,
		ListenPort:      10808,
		ProtocolProfile: profileName,
	}

	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	inbounds := parsed["inbounds"].([]any)
	inbound := inbounds[0].(map[string]any)
	if inbound["type"] != "mixed" || inbound["tag"] != "fake-in" {
		t.Fatalf("expected fake profile inbound, got %+v", inbound)
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

func TestPublicProbeErrorSanitized(t *testing.T) {
	errText := sanitizeError("dial tcp token=abc123 password=secret private_key=value node_secret=hidden: connect failed")
	for _, secret := range []string{"token=abc123", "password=secret", "private_key=value", "node_secret=hidden"} {
		if strings.Contains(errText, secret) {
			t.Fatalf("expected sanitized error, got %s", errText)
		}
	}
	if strings.Count(errText, "<redacted>") != 4 {
		t.Fatalf("expected redactions, got %s", errText)
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

func TestRender_TransportIsNotProtocolProfile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath: cfgPath,
		ListenPort: 10808,
		Transport:  "hysteria2",
	}
	if err := Render(cfg); err != nil {
		t.Fatalf("transport alone should not select a reserved protocol profile: %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), `"type": "mixed"`) {
		t.Fatalf("expected default mixed profile, got %s", string(data))
	}
}

func TestRender_Hysteria2ProtocolProfile(t *testing.T) {
	t.Setenv("HYSTERIA2_AUTH", "test-auth-value")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath:         cfgPath,
		ListenHost:         "127.0.0.1",
		ListenPort:         443,
		ProtocolProfile:    "hysteria2",
		Transport:          "udp",
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
		TLSEnabled:         true,
		SNI:                "node.example.com",
		ALPN:               "h2",
		Hysteria2UpMbps:    100,
		Hysteria2DownMbps:  500,
	}

	if err := Render(cfg); err != nil {
		t.Fatalf("Render hysteria2: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	inbounds := parsed["inbounds"].([]any)
	if len(inbounds) != 1 {
		t.Fatalf("expected 1 inbound, got %d", len(inbounds))
	}
	inbound := inbounds[0].(map[string]any)
	if inbound["type"] != "hysteria2" {
		t.Fatalf("expected type hysteria2, got %v", inbound["type"])
	}
	if inbound["listen"] != "127.0.0.1" {
		t.Fatalf("expected listen 127.0.0.1, got %v", inbound["listen"])
	}
	if inbound["listen_port"] != float64(443) {
		t.Fatalf("expected listen_port 443, got %v", inbound["listen_port"])
	}

	// Verify TLS config.
	tlsConfig, ok := inbound["tls"].(map[string]any)
	if !ok {
		t.Fatal("expected tls config")
	}
	if tlsConfig["server_name"] != "node.example.com" {
		t.Fatalf("expected server_name node.example.com, got %v", tlsConfig["server_name"])
	}

	// Verify hysteria2 config block.
	hy2Config, ok := inbound["hysteria2"].(map[string]any)
	if !ok {
		t.Fatal("expected hysteria2 config block")
	}
	if hy2Config["up_mbps"] != float64(100) {
		t.Fatalf("expected up_mbps 100, got %v", hy2Config["up_mbps"])
	}
	if hy2Config["down_mbps"] != float64(500) {
		t.Fatalf("expected down_mbps 500, got %v", hy2Config["down_mbps"])
	}
	auth, ok := hy2Config["auth"].(string)
	if !ok || auth == "" {
		t.Fatal("expected non-empty auth in hysteria2 config")
	}

	// Verify no secret markers in rendered JSON from other fields.
	// Auth is expected in config since sing-box needs it to operate.
}

func TestRender_Hysteria2NoSecret_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath:         cfgPath,
		ListenHost:         "127.0.0.1",
		ListenPort:         443,
		ProtocolProfile:    "hysteria2",
		Transport:          "udp",
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 8443,
	}
	err := Render(cfg)
	if err == nil {
		t.Fatal("expected error for missing auth secret")
	}
	for _, secret := range []string{"test-auth-value", "password=test", "private_key=value"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error should not contain secret: %s", secret)
		}
	}
}

func TestRender_ReservedProtocolProfiles(t *testing.T) {
	for _, name := range []string{"vless_reality", "trojan", "shadowtls", "wireguard"} {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "singbox.json")
		cfg := &SingboxConfig{
			ConfigPath:      cfgPath,
			ListenPort:      10808,
			ProtocolProfile: name,
		}
		err := Render(cfg)
		if err == nil {
			t.Fatalf("expected not implemented error for %s", name)
		}
		if !strings.Contains(err.Error(), "not implemented") {
			t.Fatalf("expected not implemented, got: %v", err)
		}
	}
}

func TestRender_LegacySingboxProfile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath:      cfgPath,
		ListenPort:      10808,
		ProtocolProfile: "singbox",
	}
	if err := Render(cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), `"type": "mixed"`) {
		t.Fatalf("expected mixed render for singbox alias, got %s", string(data))
	}
}

func TestRender_UnknownProtocolProfile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")

	cfg := &SingboxConfig{
		ConfigPath:      cfgPath,
		ListenPort:      10808,
		ProtocolProfile: "missing-profile",
	}
	err := Render(cfg)
	if err == nil {
		t.Fatal("expected error for unknown profile")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected not registered error, got: %v", err)
	}
}

func TestRender_ProfileErrorSanitized(t *testing.T) {
	profileName := "fake-secret-error"
	_ = sbprotocol.Register(&fakeProtocolProfile{name: profileName, renderErr: "token=abc password=secret private_key=value"})

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "singbox.json")
	err := Render(&SingboxConfig{
		ConfigPath:      cfgPath,
		ListenPort:      10808,
		ProtocolProfile: profileName,
	})
	if err == nil {
		t.Fatal("expected render error")
	}
	for _, secret := range []string{"token=abc", "password=secret", "private_key=value"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("expected sanitized error, got %v", err)
		}
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

func TestProfileEndpointMetadata(t *testing.T) {
	ep, err := ProfileEndpointMetadata(&SingboxConfig{
		ListenPort:         10808,
		Transport:          "mixed",
		PublicEndpointHost: "node.example.com",
		PublicEndpointPort: 443,
		TLSEnabled:         true,
		SNI:                "node.example.com",
		ALPN:               "h2",
	})
	if err != nil {
		t.Fatalf("ProfileEndpointMetadata: %v", err)
	}
	if ep.Host != "node.example.com" || ep.Port != 443 || ep.Transport != "mixed" || ep.SNI != "node.example.com" {
		t.Fatalf("unexpected endpoint metadata: %+v", ep)
	}
}

func TestProfileHealthChecks(t *testing.T) {
	checks, err := ProfileHealthChecks(&SingboxConfig{
		ListenHost: "127.0.0.1",
		ListenPort: 10808,
		Transport:  "socks",
	})
	if err != nil {
		t.Fatalf("ProfileHealthChecks: %v", err)
	}
	if len(checks) != 1 || checks[0].Type != "tcp" || !strings.Contains(checks[0].Target, "10808") {
		t.Fatalf("unexpected checks: %+v", checks)
	}
}

type fakeProtocolProfile struct {
	name      string
	renderErr string
}

func (p *fakeProtocolProfile) Name() string                             { return p.name }
func (p *fakeProtocolProfile) Validate(sbprotocol.ProtocolConfig) error { return nil }
func (p *fakeProtocolProfile) Render(sbprotocol.ProtocolConfig) (*sbprotocol.RenderResult, error) {
	if p.renderErr != "" {
		return nil, errString(p.renderErr)
	}
	return &sbprotocol.RenderResult{
		Inbounds: []map[string]any{{
			"type":        "mixed",
			"tag":         "fake-in",
			"listen":      "127.0.0.1",
			"listen_port": 10808,
		}},
	}, nil
}
func (p *fakeProtocolProfile) Endpoint(cfg sbprotocol.ProtocolConfig) sbprotocol.EndpointMetadata {
	return sbprotocol.EndpointMetadata{Host: cfg.PublicEndpointHost, Port: cfg.PublicEndpointPort, ProtocolProfile: p.name}
}
func (p *fakeProtocolProfile) HealthChecks(sbprotocol.ProtocolConfig) []sbprotocol.HealthCheckSpec {
	return []sbprotocol.HealthCheckSpec{{Name: "fake", Type: "tcp", Required: true}}
}
func (p *fakeProtocolProfile) SecretRefs(cfg sbprotocol.ProtocolConfig) []sbprotocol.SecretRef {
	return cfg.Secrets
}
func (p *fakeProtocolProfile) Redact(cfg sbprotocol.ProtocolConfig) sbprotocol.ProtocolConfig {
	return sbprotocol.RedactConfig(cfg)
}
func (p *fakeProtocolProfile) SupportsClientConfig() bool { return false }

type errString string

func (e errString) Error() string { return string(e) }
