package protocol

type ProtocolConfig struct {
	Profile            string
	Transport          string
	ListenHost         string
	ListenPort         int
	PublicEndpointHost string
	PublicEndpointPort int
	SNI                string
	ALPN               string
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
	CertRef string
	KeyRef  string
}

type DNSConfig struct {
	Enabled  bool
	Strategy string
	Servers  []string
}

type RouteConfig struct {
	Global           bool
	BypassLAN        bool
	FinalOutbound    string
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
	Type      string // tcp|udp|http|custom
	Host      string
	Port      int
	TimeoutMS int
	Optional  bool

	// Backward-compatible fields for older internal tests and callers.
	Target    string
	Required  bool
	TimeoutMs int
}

type SecretRef struct {
	Name         string
	Source       string // env|file|backend|local_generated
	Required     bool
	RotatePolicy string
	RedactionKey string
}
