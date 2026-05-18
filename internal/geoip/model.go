// Package geoip implements the NodeAgent GeoIP database sync client.
// TASK-NODEAGENT-GEOIP-001.
package geoip

import "time"

// Format represents a supported GeoIP database file format.
type Format string

const (
	// FormatMaxMindMMDB is the MaxMind MMDB format.
	FormatMaxMindMMDB Format = "maxmind-mmdb"
	// FormatIP2LocationBIN is the IP2Location binary format.
	FormatIP2LocationBIN Format = "ip2location-bin"
	// FormatDBIPMMDB is the DB-IP MMDB format.
	FormatDBIPMMDB Format = "dbip-mmdb"
	// FormatGeoIP2CNMMDB is the GeoIP2 CN MMDB format.
	FormatGeoIP2CNMMDB Format = "geoip2-cn-mmdb"
	// FormatGeoIP2CNDat is the GeoIP2 CN DAT format.
	FormatGeoIP2CNDat Format = "geoip2-cn-dat"
)

// KnownFormats is the set of all supported GeoIP database formats.
var KnownFormats = map[Format]bool{
	FormatMaxMindMMDB:    true,
	FormatIP2LocationBIN: true,
	FormatDBIPMMDB:       true,
	FormatGeoIP2CNMMDB:   true,
	FormatGeoIP2CNDat:    true,
}

// IsKnownFormat returns true if the given format is supported.
func IsKnownFormat(f Format) bool {
	return KnownFormats[f]
}

// Manifest describes a GeoIP database artifact issued by Backend.
// TASK-NODEAGENT-GEOIP-001.
type Manifest struct {
	DatabaseID    string        `json:"database_id"`
	Version       string        `json:"version"`
	Format        Format        `json:"format"`
	Profile       string        `json:"profile"`
	Source        string        `json:"source"`
	PackageURL    string        `json:"package_url"`
	SHA256        string        `json:"sha256"`
	SizeBytes     int64         `json:"size_bytes"`
	GeneratedAt   time.Time     `json:"generated_at"`
	ExpiresAt     time.Time     `json:"expires_at"`
	FullPackage   bool          `json:"full_package"`
	DeltaPackages []DeltaPackage `json:"delta_packages,omitempty"`
	Compatibility Compatibility  `json:"compatibility,omitempty"`
	Signature     string        `json:"signature,omitempty"`
}

// DeltaPackage describes an incremental GeoIP update (reserved for future use).
type DeltaPackage struct {
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	PackageURL  string `json:"package_url"`
	SHA256      string `json:"sha256"`
	SizeBytes   int64  `json:"size_bytes"`
}

// Compatibility defines the minimum required versions for NodeAgent and App.
type Compatibility struct {
	NodeAgentMinVersion string `json:"nodeagent_min_version,omitempty"`
	AppMinVersion       string `json:"app_min_version,omitempty"`
}

// CheckResponse is the response from GET /internal/agent/geoip/check.
// TASK-NODEAGENT-GEOIP-001, mapping to GEOIP_DATABASE_SYNC_CONTRACT.md §3.3.
type CheckResponse struct {
	UpdateAvailable bool      `json:"update_available"`
	Database        *Manifest `json:"database,omitempty"`
	RolloutID       string    `json:"rollout_id,omitempty"`
}

// LKGEntry is the on-disk format for last-known-good GeoIP database metadata.
// TASK-NODEAGENT-GEOIP-001.
type LKGEntry struct {
	Source    string    `json:"source"`
	Profile   string    `json:"profile"`
	Format    Format    `json:"format"`
	Version   string    `json:"version"`
	SHA256    string    `json:"sha256"`
	HealthyAt time.Time `json:"healthy_at"`
}

// Status represents the current GeoIP sync status.
type Status string

const (
	StatusDisabled  Status = "disabled"
	StatusReady     Status = "ready"
	StatusSyncing   Status = "syncing"
	StatusStale     Status = "stale"
	StatusFailed    Status = "failed"
)

// GeoIPStatus is an observable snapshot of the GeoIP sync subsystem.
// TASK-NODEAGENT-GEOIP-001.
type GeoIPStatus struct {
	Enabled      bool      `json:"enabled"`
	Status       Status    `json:"status"`
	DatabaseID   string    `json:"database_id,omitempty"`
	Version      string    `json:"version,omitempty"`
	Format       Format    `json:"format,omitempty"`
	Profile      string    `json:"profile,omitempty"`
	Source       string    `json:"source,omitempty"`
	CurrentPath  string    `json:"current_path,omitempty"`
	GeneratedAt  *int64    `json:"generated_at,omitempty"`
	ExpiresAt    *int64    `json:"expires_at,omitempty"`
	LastCheckAt  *int64    `json:"last_check_at,omitempty"`
	LastSyncAt   *int64    `json:"last_sync_at,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	LKGAvailable bool      `json:"lkg_available"`
	SHA256       string    `json:"sha256,omitempty"`
}
