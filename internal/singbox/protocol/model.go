package protocol

// ProtocolProfile renders and describes one sing-box protocol profile.
type ProtocolProfile interface {
	Name() string
	Validate(cfg ProtocolConfig) error
	Render(cfg ProtocolConfig) (*RenderResult, error)
	Endpoint(cfg ProtocolConfig) EndpointMetadata
	HealthChecks(cfg ProtocolConfig) []HealthCheckSpec
	SecretRefs(cfg ProtocolConfig) []SecretRef
	Redact(cfg ProtocolConfig) ProtocolConfig
	SupportsClientConfig() bool
}

type ProtocolConfig struct {
	Profile            string
	Transport          string
	ListenHost         string
	ListenPort         int
	PublicEndpointHost string
	PublicEndpointPort int
	TLS                TLSConfig
	DNS                DNSConfig
	Route              RouteConfig
	Secrets            []SecretRef
	Raw                map[string]any
}

type TLSConfig struct {
	Enabled bool
	SNI     string
	ALPN    string
}

type DNSConfig struct {
	Enabled  bool
	Strategy string
	Servers  []string
}

type RouteConfig struct {
	Global           bool
	BypassLAN        bool
	ProxyOutboundTag string
}

type RenderResult struct {
	Inbounds  []map[string]any
	Outbounds []map[string]any
	Route     map[string]any
	DNS       map[string]any
}

type EndpointMetadata struct {
	Host            string
	Port            int
	Transport       string
	ProtocolProfile string
	SNI             string
	ALPN            string
	Ready           bool
}

type HealthCheckSpec struct {
	Name      string
	Type      string
	Target    string
	Required  bool
	TimeoutMs int
}

type SecretRef struct {
	Name         string
	Source       string
	Required     bool
	RotatePolicy string
	RedactionKey string
}
