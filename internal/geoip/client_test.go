package geoip

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientCheck_UpdateAvailable(t *testing.T) {
	var capturedNodeID, capturedTimestamp, capturedSignature string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/internal/agent/geoip/check" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		capturedNodeID = r.Header.Get("X-Node-ID")
		capturedTimestamp = r.Header.Get("X-Timestamp")
		capturedSignature = r.Header.Get("X-Signature")

		if capturedNodeID == "" {
			t.Fatal("missing X-Node-ID header")
		}
		if capturedTimestamp == "" {
			t.Fatal("missing X-Timestamp header")
		}
		if capturedSignature == "" {
			t.Fatal("missing X-Signature header")
		}

		// Verify format query.
		if r.URL.Query().Get("format") != "maxmind-mmdb" {
			t.Fatalf("unexpected format: %s", r.URL.Query().Get("format"))
		}
		if r.URL.Query().Get("profile") != "country" {
			t.Fatalf("unexpected profile: %s", r.URL.Query().Get("profile"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"update_available": true,
			"database": {
				"database_id": "db-2026-05",
				"version": "2026-05",
				"format": "maxmind-mmdb",
				"profile": "country",
				"source": "dbip_lite",
				"package_url": "https://objects.example/geoip/dbip-country-2026-05.mmdb",
				"sha256": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
				"size_bytes": 12345678,
				"generated_at": "2026-05-18T00:00:00Z",
				"expires_at": "2026-06-30T00:00:00Z",
				"full_package": true
			},
			"rollout_id": "geoip-2026-05-country"
		}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node-id", "test-node-secret", "v1.0")
	resp, err := client.Check(context.Background(), "2026-04", FormatMaxMindMMDB, "country")
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if !resp.UpdateAvailable {
		t.Fatal("expected update_available true")
	}
	if resp.RolloutID != "geoip-2026-05-country" {
		t.Fatalf("expected rollout_id geoip-2026-05-country, got %s", resp.RolloutID)
	}
	if resp.Database == nil {
		t.Fatal("expected database in response")
	}
	if resp.Database.Version != "2026-05" {
		t.Fatalf("expected version 2026-05, got %s", resp.Database.Version)
	}
	if resp.Database.Format != FormatMaxMindMMDB {
		t.Fatalf("expected format maxmind-mmdb, got %s", resp.Database.Format)
	}

	// Verify HMAC headers.
	if capturedNodeID != "test-node-id" {
		t.Fatalf("node_id mismatch: %s", capturedNodeID)
	}
	if capturedSignature == "" {
		t.Fatal("signature should not be empty")
	}
}

func TestClientCheck_NoUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"update_available": false,
			"database": {
				"source": "dbip_lite",
				"edition": "country",
				"format": "maxmind-mmdb",
				"version": "2026-05",
				"sha256": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
			}
		}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node-id", "test-node-secret", "v1.0")
	resp, err := client.Check(context.Background(), "2026-05", FormatMaxMindMMDB, "country")
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if resp.UpdateAvailable {
		t.Fatal("expected update_available false")
	}
}

func TestClientCheck_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"NODE_SECRET_MISMATCH"}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-node-id", "wrong-secret", "v1.0")
	_, err := client.Check(context.Background(), "2026-05", FormatMaxMindMMDB, "country")
	if err == nil {
		t.Fatal("expected error for non-200 check")
	}
}

func TestClientDownload_Success(t *testing.T) {
	dbContent := []byte("mock geoip database binary content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(dbContent)
	}))
	defer server.Close()

	client := NewClient("http://dummy", "test-node-id", "test-node-secret", "v1.0")
	data, err := client.DownloadPackage(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if len(data) != len(dbContent) {
		t.Fatalf("expected %d bytes, got %d", len(dbContent), len(data))
	}
}

func TestClientDownload_ExceedsLimit(t *testing.T) {
	bigData := make([]byte, 1024) // small but we test with a low limit
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bigData)
	}))
	defer server.Close()

	client := NewClient("http://dummy", "test-node-id", "test-node-secret", "v1.0",
		WithDownloadLimit(500))
	_, err := client.DownloadPackage(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected error for download exceeding limit")
	}
}

func TestClientDownload_RejectsNonHTTPS(t *testing.T) {
	client := NewClient("http://dummy", "test-node-id", "test-node-secret", "v1.0")
	_, err := client.DownloadPackage(context.Background(), "ftp://evil.example.com/geoip.db")
	if err == nil {
		t.Fatal("expected error for non-http(s) scheme")
	}
}

func TestClientReportEvent(t *testing.T) {
	var capturedMethod, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path

		if r.Header.Get("X-Node-ID") == "" {
			t.Fatal("missing X-Node-ID")
		}
		if r.Header.Get("X-Signature") == "" {
			t.Fatal("missing X-Signature")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "node-uuid", "node-secret", "v1.0")
	event := &SyncEvent{
		RolloutID:   "geoip-2026-05-country",
		FromVersion: "2026-04",
		ToVersion:   "2026-05",
		Status:      "installed",
		CurrentSHA256: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	}
	err := client.ReportEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("ReportEvent failed: %v", err)
	}
	if capturedMethod != "POST" {
		t.Fatalf("expected POST, got %s", capturedMethod)
	}
	if capturedPath != "/internal/agent/geoip/events" {
		t.Fatalf("expected /internal/agent/geoip/events, got %s", capturedPath)
	}
}
