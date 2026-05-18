package singbox

import (
	"encoding/json"
	"fmt"
	"os"
)

// singboxConfigFile is the JSON structure for the sing-box config file.
// This is a minimal MVP skeleton — production config will be richer.
type singboxConfigFile struct {
	Log       *logConfig       `json:"log,omitempty"`
	Inbounds  []inboundConfig  `json:"inbounds"`
	Outbounds []outboundConfig `json:"outbounds"`
}

type logConfig struct {
	Level string `json:"level,omitempty"`
}

type inboundConfig struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Listen     string `json:"listen,omitempty"`
	ListenPort int    `json:"listen_port,omitempty"`
}

type outboundConfig struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`
}

// Render generates a sing-box config file and writes it atomically to
// ConfigPath.  Returns an error if validation fails or the write fails.
func Render(cfg *SingboxConfig) error {
	if cfg == nil {
		return fmt.Errorf("singbox config is nil")
	}
	if cfg.ListenPort <= 0 || cfg.ListenPort > 65535 {
		return fmt.Errorf("singbox listen port %d is invalid (must be 1-65535)", cfg.ListenPort)
	}
	host := cfg.ListenHost
	if host == "" {
		host = "127.0.0.1"
	}
	logLevel := cfg.LogLevel
	if logLevel == "" {
		logLevel = "info"
	}

	sf := singboxConfigFile{
		Log: &logConfig{Level: logLevel},
		Inbounds: []inboundConfig{
			{
				Type:       "socks",
				Tag:        "socks-in",
				Listen:     host,
				ListenPort: cfg.ListenPort,
			},
			{
				Type:       "mixed",
				Tag:        "mixed-in",
				Listen:     host,
				ListenPort: cfg.ListenPort,
			},
		},
		Outbounds: []outboundConfig{
			{Type: "direct", Tag: "direct"},
			{Type: "block", Tag: "block"},
		},
	}

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal singbox config: %w", err)
	}

	// Atomic write: temp file + rename.
	tmpPath := cfg.ConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write temp singbox config: %w", err)
	}
	if err := os.Rename(tmpPath, cfg.ConfigPath); err != nil {
		return fmt.Errorf("rename singbox config: %w", err)
	}
	return nil
}
