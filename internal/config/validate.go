package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ExpectedConfigKey      = "nodeagent.runtime_config"
	ExpectedSchemaVersion  = "1.0"
	AllowedSchemaVersions  = "1.0"
)

// ValidationError records a single validation failure.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidateResponse checks the ConfigResponse for well-formedness.
// Returns a list of non-fatal warnings and an error if the response is
// fundamentally unusable.
func ValidateResponse(resp *ConfigResponse) (warnings []string, err error) {
	if resp == nil {
		return nil, &ValidationError{Field: "response", Message: "nil response"}
	}

	// 1. config_key must match the expected key for NodeAgent.
	if resp.ConfigKey != ExpectedConfigKey {
		err = &ValidationError{Field: "config_key",
			Message: fmt.Sprintf("expected %q, got %q", ExpectedConfigKey, resp.ConfigKey)}
		return
	}

	// 2. schema_version must be in the allowed set.
	if resp.SchemaVersion == "" {
		warnings = append(warnings, "schema_version is empty")
	} else if resp.SchemaVersion != AllowedSchemaVersions {
		warnings = append(warnings,
			fmt.Sprintf("schema_version %q is unknown; allowed: %s", resp.SchemaVersion, AllowedSchemaVersions))
	}

	// 3. config_version must be positive.
	if resp.ConfigVersion <= 0 {
		err = &ValidationError{Field: "config_version",
			Message: fmt.Sprintf("must be positive, got %d", resp.ConfigVersion)}
		return
	}

	// 4. config_hash must be present and well-formed.
	if resp.ConfigHash == "" {
		err = &ValidationError{Field: "config_hash", Message: "is empty"}
		return
	}
	if !strings.HasPrefix(resp.ConfigHash, "sha256:") {
		err = &ValidationError{Field: "config_hash",
			Message: fmt.Sprintf("must start with sha256:, got %q", resp.ConfigHash)}
		return
	}
	hexPart := strings.TrimPrefix(resp.ConfigHash, "sha256:")
	if len(hexPart) != 64 {
		warnings = append(warnings,
			fmt.Sprintf("config_hash hex part length is %d, expected 64", len(hexPart)))
	}
	if _, hexErr := hex.DecodeString(hexPart); hexErr != nil {
		warnings = append(warnings, fmt.Sprintf("config_hash is not valid hex: %v", hexErr))
	}

	// 5. payload must be non-empty valid JSON.
	if len(resp.Payload) == 0 {
		err = &ValidationError{Field: "payload", Message: "is empty"}
		return
	}
	if !json.Valid(resp.Payload) {
		err = &ValidationError{Field: "payload", Message: "is not valid JSON"}
		return
	}

	return
}

// ComputeHash returns the sha256:... hash of the canonical JSON payload.
func ComputeHash(payload json.RawMessage) string {
	h := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(h[:])
}

// VerifyHash returns true if the payload's computed hash matches the expected hash.
func VerifyHash(payload json.RawMessage, expectedHash string) (bool, error) {
	computed := ComputeHash(payload)
	if computed != expectedHash {
		return false, &ValidationError{Field: "config_hash",
			Message: fmt.Sprintf("computed %q, expected %q", computed, expectedHash)}
	}
	return true, nil
}
