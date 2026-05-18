package config

import (
	"fmt"
	"log"
)

// ApplyError records a failure to apply a config value.
type ApplyError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ApplyError) Error() string {
	return fmt.Sprintf("apply %s: %s", e.Field, e.Message)
}

// RuntimeApplier applies config changes to the runtime scheduler, reporter,
// and degraded mode controller. The implementation is pluggable so the main
// binary can wire its own callbacks.
type RuntimeApplier struct {
	onConfigChange func(old, new *RuntimeConfig) error
}

// NewRuntimeApplier creates a RuntimeApplier.
// The onConfigChange callback is invoked when a validated config is ready to
// be applied. If nil, changes are logged but not applied (dry-run mode).
func NewRuntimeApplier(onConfigChange func(old, new *RuntimeConfig) error) *RuntimeApplier {
	return &RuntimeApplier{onConfigChange: onConfigChange}
}

// Apply validates that runtime-relevant fields are sane, then calls the
// registered callback. It returns an ApplyError if field-level validation
// fails, allowing the caller to reject the config and keep the old one.
func (a *RuntimeApplier) Apply(old, new *RuntimeConfig) error {
	// Field-level sanity checks.
	if new.Reporting.HeartbeatIntervalSeconds < 5 {
		return &ApplyError{
			Field:   "reporting.heartbeat_interval_seconds",
			Message: fmt.Sprintf("must be >= 5, got %d", new.Reporting.HeartbeatIntervalSeconds),
		}
	}
	if new.Reporting.BatchUploadIntervalSeconds < 10 {
		return &ApplyError{
			Field:   "reporting.batch_upload_interval_seconds",
			Message: fmt.Sprintf("must be >= 10, got %d", new.Reporting.BatchUploadIntervalSeconds),
		}
	}
	if new.Reporting.MaxOfflineBufferItems < 100 {
		return &ApplyError{
			Field:   "reporting.max_offline_buffer_items",
			Message: fmt.Sprintf("must be >= 100, got %d", new.Reporting.MaxOfflineBufferItems),
		}
	}
	if new.Singbox.HealthCheckTimeoutSeconds < 1 {
		return &ApplyError{
			Field:   "singbox.health_check_timeout_seconds",
			Message: fmt.Sprintf("must be >= 1, got %d", new.Singbox.HealthCheckTimeoutSeconds),
		}
	}
	if new.Singbox.Transport != "" &&
		new.Singbox.Transport != "mixed" &&
		new.Singbox.Transport != "socks" &&
		new.Singbox.Transport != "tun" {
		return &ApplyError{
			Field:   "singbox.transport",
			Message: "must be mixed, socks, or tun",
		}
	}
	if new.Singbox.ListenPort != 0 {
		if new.Singbox.ListenPort < 1 || new.Singbox.ListenPort > 65535 {
			return &ApplyError{
				Field:   "singbox.listen_port",
				Message: fmt.Sprintf("must be 1-65535, got %d", new.Singbox.ListenPort),
			}
		}
	}
	if new.Singbox.PublicEndpointPort != 0 {
		if new.Singbox.PublicEndpointPort < 1 || new.Singbox.PublicEndpointPort > 65535 {
			return &ApplyError{
				Field:   "singbox.public_endpoint_port",
				Message: fmt.Sprintf("must be 1-65535, got %d", new.Singbox.PublicEndpointPort),
			}
		}
	}
	if new.Singbox.PublicProbePort != 0 {
		if new.Singbox.PublicProbePort < 1 || new.Singbox.PublicProbePort > 65535 {
			return &ApplyError{
				Field:   "singbox.public_probe_port",
				Message: fmt.Sprintf("must be 1-65535, got %d", new.Singbox.PublicProbePort),
			}
		}
	}
	if new.Singbox.HealthCheckMode != "" &&
		new.Singbox.HealthCheckMode != "local" &&
		new.Singbox.HealthCheckMode != "public" &&
		new.Singbox.HealthCheckMode != "both" {
		return &ApplyError{
			Field:   "singbox.health_check_mode",
			Message: "must be local, public, or both",
		}
	}
	if new.Singbox.TunMTU != 0 && new.Singbox.TunMTU < 128 {
		return &ApplyError{
			Field:   "singbox.tun_mtu",
			Message: fmt.Sprintf("must be >= 128, got %d", new.Singbox.TunMTU),
		}
	}

	if a.onConfigChange == nil {
		log.Printf("[config] dry-run: would apply config version (no callback registered)")
		return nil
	}

	if err := a.onConfigChange(old, new); err != nil {
		return &ApplyError{
			Field:   "callback",
			Message: err.Error(),
		}
	}
	return nil
}
