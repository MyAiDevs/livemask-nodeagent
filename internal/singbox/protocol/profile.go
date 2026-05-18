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
