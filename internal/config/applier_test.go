package config

import "testing"

func TestApply_NilCallback(t *testing.T) {
	applier := NewRuntimeApplier(nil)
	old := DefaultRuntimeConfig()
	new := DefaultRuntimeConfig()
	new.Reporting.HeartbeatIntervalSeconds = 120
	err := applier.Apply(&old, &new)
	if err != nil {
		t.Fatalf("unexpected error with nil callback: %v", err)
	}
}

func TestApply_Success(t *testing.T) {
	var applied bool
	applier := NewRuntimeApplier(func(old, new *RuntimeConfig) error {
		applied = true
		if new.Reporting.HeartbeatIntervalSeconds != 120 {
			t.Fatalf("expected 120, got %d", new.Reporting.HeartbeatIntervalSeconds)
		}
		return nil
	})
	old := DefaultRuntimeConfig()
	new := DefaultRuntimeConfig()
	new.Reporting.HeartbeatIntervalSeconds = 120
	err := applier.Apply(&old, &new)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Fatal("callback was not invoked")
	}
}

func TestApply_RejectsLowHeartbeat(t *testing.T) {
	applier := NewRuntimeApplier(func(old, new *RuntimeConfig) error {
		return nil
	})
	old := DefaultRuntimeConfig()
	new := DefaultRuntimeConfig()
	new.Reporting.HeartbeatIntervalSeconds = 1 // < 5
	err := applier.Apply(&old, &new)
	if err == nil {
		t.Fatal("expected error for heartbeat < 5")
	}
}

func TestApply_RejectsLowBatchUpload(t *testing.T) {
	applier := NewRuntimeApplier(nil)
	old := DefaultRuntimeConfig()
	new := DefaultRuntimeConfig()
	new.Reporting.BatchUploadIntervalSeconds = 5 // < 10
	err := applier.Apply(&old, &new)
	if err == nil {
		t.Fatal("expected error for batch_upload < 10")
	}
}

func TestApply_RejectsLowBufferItems(t *testing.T) {
	applier := NewRuntimeApplier(nil)
	old := DefaultRuntimeConfig()
	new := DefaultRuntimeConfig()
	new.Reporting.MaxOfflineBufferItems = 50 // < 100
	err := applier.Apply(&old, &new)
	if err == nil {
		t.Fatal("expected error for max_offline_buffer_items < 100")
	}
}

func TestApply_RejectsLowHealthCheck(t *testing.T) {
	applier := NewRuntimeApplier(nil)
	old := DefaultRuntimeConfig()
	new := DefaultRuntimeConfig()
	new.Singbox.HealthCheckTimeoutSeconds = 0 // < 1
	err := applier.Apply(&old, &new)
	if err == nil {
		t.Fatal("expected error for health_check_timeout < 1")
	}
}

func TestApply_RejectsInvalidTransport(t *testing.T) {
	applier := NewRuntimeApplier(nil)
	old := DefaultRuntimeConfig()
	new := DefaultRuntimeConfig()
	new.Singbox.Transport = "hysteria2"
	err := applier.Apply(&old, &new)
	if err == nil {
		t.Fatal("expected error for invalid transport")
	}
}

func TestApply_CallbackError(t *testing.T) {
	applier := NewRuntimeApplier(func(old, new *RuntimeConfig) error {
		return &ApplyError{Field: "callback", Message: "simulated failure"}
	})
	old := DefaultRuntimeConfig()
	new := DefaultRuntimeConfig()
	new.Reporting.HeartbeatIntervalSeconds = 120
	err := applier.Apply(&old, &new)
	if err == nil {
		t.Fatal("expected error from callback")
	}
}

func TestApply_RejectsInvalidPublicEndpointPort(t *testing.T) {
	applier := NewRuntimeApplier(nil)
	old := DefaultRuntimeConfig()
	new := DefaultRuntimeConfig()
	new.Singbox.PublicEndpointPort = 65536
	err := applier.Apply(&old, &new)
	if err == nil {
		t.Fatal("expected error for public_endpoint_port > 65535")
	}
}

func TestApply_RejectsInvalidPublicProbePort(t *testing.T) {
	applier := NewRuntimeApplier(nil)
	old := DefaultRuntimeConfig()
	new := DefaultRuntimeConfig()
	new.Singbox.PublicProbePort = 70000
	err := applier.Apply(&old, &new)
	if err == nil {
		t.Fatal("expected error for public_probe_port > 65535")
	}
}

func TestApply_RejectsInvalidHealthCheckMode(t *testing.T) {
	applier := NewRuntimeApplier(nil)
	old := DefaultRuntimeConfig()
	new := DefaultRuntimeConfig()
	new.Singbox.HealthCheckMode = "external"
	err := applier.Apply(&old, &new)
	if err == nil {
		t.Fatal("expected error for invalid health_check_mode")
	}
}

func TestApply_RejectsInvalidTunMTU(t *testing.T) {
	applier := NewRuntimeApplier(nil)
	old := DefaultRuntimeConfig()
	new := DefaultRuntimeConfig()
	new.Singbox.TunMTU = 64
	err := applier.Apply(&old, &new)
	if err == nil {
		t.Fatal("expected error for tun_mtu < 128")
	}
}

func TestApply_AcceptsValidPublicEndpointPort(t *testing.T) {
	applier := NewRuntimeApplier(nil)
	old := DefaultRuntimeConfig()
	new := DefaultRuntimeConfig()
	new.Singbox.PublicEndpointPort = 443
	err := applier.Apply(&old, &new)
	if err != nil {
		t.Fatalf("unexpected error for valid public_endpoint_port: %v", err)
	}
}

func TestApply_AcceptsValidTunMTU(t *testing.T) {
	applier := NewRuntimeApplier(nil)
	old := DefaultRuntimeConfig()
	new := DefaultRuntimeConfig()
	new.Singbox.TunMTU = 9000
	err := applier.Apply(&old, &new)
	if err != nil {
		t.Fatalf("unexpected error for valid tun_mtu: %v", err)
	}
}
