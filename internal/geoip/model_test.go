package geoip

import (
	"encoding/json"
	"testing"
)

func TestIsKnownFormat(t *testing.T) {
	tests := []struct {
		format Format
		known  bool
	}{
		{FormatMaxMindMMDB, true},
		{FormatIP2LocationBIN, true},
		{FormatDBIPMMDB, true},
		{FormatGeoIP2CNMMDB, true},
		{FormatGeoIP2CNDat, true},
		{"unknown-format", false},
		{"csv", false},
		{"", false},
	}
	for _, tc := range tests {
		got := IsKnownFormat(tc.format)
		if got != tc.known {
			t.Errorf("IsKnownFormat(%q) = %v, want %v", tc.format, got, tc.known)
		}
	}
}

func TestValidateSHA256Format(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid hex", "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", false},
		{"valid with prefix", "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", false},
		{"too short", "abc123", true},
		{"invalid char", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", true},
		{"empty", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateSHA256Format(tc.input)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestManifestJSONRoundTrip(t *testing.T) {
	m := &Manifest{
		DatabaseID: "db-123",
		Version:    "2026-05",
		Format:     FormatMaxMindMMDB,
		Profile:    "country",
		Source:     "dbip_lite",
		PackageURL: "https://objects.example/geoip/dbip-country-2026-05.mmdb",
		SHA256:     "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		SizeBytes:  12345678,
		FullPackage: true,
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m2 Manifest
	if err := json.Unmarshal(data, &m2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m2.DatabaseID != "db-123" {
		t.Fatalf("DatabaseID: got %s, want db-123", m2.DatabaseID)
	}
	if m2.Version != "2026-05" {
		t.Fatalf("Version: got %s, want 2026-05", m2.Version)
	}
	if m2.Format != FormatMaxMindMMDB {
		t.Fatalf("Format: got %s, want maxmind-mmdb", m2.Format)
	}
	if m2.Profile != "country" {
		t.Fatalf("Profile: got %s, want country", m2.Profile)
	}
	if m2.Source != "dbip_lite" {
		t.Fatalf("Source: got %s, want dbip_lite", m2.Source)
	}
	if m2.SHA256 != "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789" {
		t.Fatalf("SHA256: got %s", m2.SHA256)
	}
	if m2.SizeBytes != 12345678 {
		t.Fatalf("SizeBytes: got %d", m2.SizeBytes)
	}
	if !m2.FullPackage {
		t.Fatal("FullPackage should be true")
	}
}
