package config

import (
	"encoding/json"
	"testing"
)

func fullConfigHash(t *testing.T, payload json.RawMessage) string {
	t.Helper()
	return ComputeHash(payload)
}

func TestValidateResponse_Nil(t *testing.T) {
	_, err := ValidateResponse(nil)
	if err == nil {
		t.Fatal("expected error for nil response")
	}
}

func TestValidateResponse_WrongKey(t *testing.T) {
	resp := &ConfigResponse{
		ConfigKey:      "client.remote_config",
		SchemaVersion:  "1.0",
		ConfigVersion:  1,
		ConfigHash:     "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Payload:        json.RawMessage(`{}`),
	}
	_, err := ValidateResponse(resp)
	if err == nil {
		t.Fatal("expected error for wrong config_key")
	}
}

func TestValidateResponse_EmptyHash(t *testing.T) {
	resp := &ConfigResponse{
		ConfigKey:     "nodeagent.runtime_config",
		SchemaVersion: "1.0",
		ConfigVersion: 1,
		ConfigHash:    "",
		Payload:       json.RawMessage(`{}`),
	}
	_, err := ValidateResponse(resp)
	if err == nil {
		t.Fatal("expected error for empty hash")
	}
}

func TestValidateResponse_InvalidHashPrefix(t *testing.T) {
	resp := &ConfigResponse{
		ConfigKey:     "nodeagent.runtime_config",
		SchemaVersion: "1.0",
		ConfigVersion: 1,
		ConfigHash:    "md5:abc123",
		Payload:       json.RawMessage(`{}`),
	}
	_, err := ValidateResponse(resp)
	if err == nil {
		t.Fatal("expected error for invalid hash prefix")
	}
}

func TestValidateResponse_ZeroVersion(t *testing.T) {
	resp := &ConfigResponse{
		ConfigKey:     "nodeagent.runtime_config",
		SchemaVersion: "1.0",
		ConfigVersion: 0,
		ConfigHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Payload:       json.RawMessage(`{}`),
	}
	_, err := ValidateResponse(resp)
	if err == nil {
		t.Fatal("expected error for zero version")
	}
}

func TestValidateResponse_EmptyPayload(t *testing.T) {
	resp := &ConfigResponse{
		ConfigKey:     "nodeagent.runtime_config",
		SchemaVersion: "1.0",
		ConfigVersion: 1,
		ConfigHash:    "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Payload:       json.RawMessage(``),
	}
	_, err := ValidateResponse(resp)
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
}

func TestValidateResponse_InvalidJSONPayload(t *testing.T) {
	resp := &ConfigResponse{
		ConfigKey:     "nodeagent.runtime_config",
		SchemaVersion: "1.0",
		ConfigVersion: 1,
		ConfigHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Payload:       json.RawMessage(`{invalid`),
	}
	_, err := ValidateResponse(resp)
	if err == nil {
		t.Fatal("expected error for invalid JSON payload")
	}
}

func TestValidateResponse_Valid(t *testing.T) {
	payload := json.RawMessage(`{"reporting":{"heartbeat_interval_seconds":30}}`)
	hash := ComputeHash(payload)
	resp := &ConfigResponse{
		ConfigKey:     "nodeagent.runtime_config",
		SchemaVersion: "1.0",
		ConfigVersion: 3,
		ConfigHash:    hash,
		Payload:       payload,
	}
	warnings, err := ValidateResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Unknown schema version should produce a warning.
	if len(warnings) == 0 {
		// Actually "1.0" is known, so no warning here.
	}
}

func TestComputeHashAndVerify(t *testing.T) {
	payload := json.RawMessage(`{"key":"value"}`)
	hash := ComputeHash(payload)
	if len(hash) != 64+7 { // sha256: + 64 hex chars
		t.Fatalf("unexpected hash length: %d", len(hash))
	}

	ok, err := VerifyHash(payload, hash)
	if err != nil || !ok {
		t.Fatalf("verify failed: ok=%v err=%v", ok, err)
	}

	_, err = VerifyHash(payload, "sha256:0000")
	if err == nil {
		t.Fatal("expected error for wrong hash")
	}
}
