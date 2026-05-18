package geoip

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestManagerInitialStateDisabled(t *testing.T) {
	mgr := NewGeoIPManager(false, "country", FormatMaxMindMMDB, nil, nil)
	s := mgr.Status()
	if s.Enabled {
		t.Fatal("expected disabled")
	}
	if s.Status != StatusDisabled {
		t.Fatalf("expected status disabled, got %s", s.Status)
	}
}

func TestManagerInitialStateEnabled(t *testing.T) {
	dir := t.TempDir()
	storage := NewStorage(dir, "country")
	client := NewClient("http://dummy", "node", "secret", "v1.0")

	mgr := NewGeoIPManager(true, "country", FormatMaxMindMMDB, client, storage)
	s := mgr.Status()
	if !s.Enabled {
		t.Fatal("expected enabled")
	}
	if s.Status != StatusReady {
		t.Fatalf("expected status ready, got %s", s.Status)
	}
}

func TestManagerValidateManifest_RejectsUnknownFormat(t *testing.T) {
	dir := t.TempDir()
	storage := NewStorage(dir, "country")
	client := NewClient("http://dummy", "node", "secret", "v1.0")

	mgr := NewGeoIPManager(true, "country", FormatMaxMindMMDB, client, storage)

	// Test private method via exported paths — we test via the validateManifest error.
	// We'll test validateManifest directly using the manager's method.
	m := &Manifest{
		DatabaseID: "db-1",
		Version:    "2026-05",
		Format:     "unknown-format",
		Profile:    "country",
		Source:     "test",
		PackageURL: "https://example.com/test.mmdb",
		SHA256:     "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	}
	err := mgr.validateManifest(m)
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManagerValidateManifest_RejectsUnknownProfile(t *testing.T) {
	dir := t.TempDir()
	storage := NewStorage(dir, "country")
	client := NewClient("http://dummy", "node", "secret", "v1.0")

	mgr := NewGeoIPManager(true, "city", FormatMaxMindMMDB, client, storage)

	m := &Manifest{
		DatabaseID: "db-1",
		Version:    "2026-05",
		Format:     FormatMaxMindMMDB,
		Profile:    "country", // manager expects "city"
		Source:     "test",
		PackageURL: "https://example.com/test.mmdb",
		SHA256:     "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	}
	err := mgr.validateManifest(m)
	if err == nil {
		t.Fatal("expected error for profile mismatch")
	}
	if !strings.Contains(err.Error(), "profile mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManagerValidateManifest_AcceptsValid(t *testing.T) {
	dir := t.TempDir()
	storage := NewStorage(dir, "country")
	client := NewClient("http://dummy", "node", "secret", "v1.0")

	mgr := NewGeoIPManager(true, "country", FormatMaxMindMMDB, client, storage)

	m := &Manifest{
		DatabaseID: "db-1",
		Version:    "2026-05",
		Format:     FormatMaxMindMMDB,
		Profile:    "country",
		Source:     "test",
		PackageURL: "https://example.com/test.mmdb",
		SHA256:     "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		FullPackage: true,
	}
	err := mgr.validateManifest(m)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestManagerValidateManifest_EmptyPackageURL(t *testing.T) {
	dir := t.TempDir()
	storage := NewStorage(dir, "country")
	client := NewClient("http://dummy", "node", "secret", "v1.0")

	mgr := NewGeoIPManager(true, "country", FormatMaxMindMMDB, client, storage)

	m := &Manifest{
		Format: FormatMaxMindMMDB,
		Profile: "country",
		SHA256:  "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	}
	err := mgr.validateManifest(m)
	if err == nil {
		t.Fatal("expected error for empty package_url")
	}
}

func TestManagerVerifyBytesSHA256_Pass(t *testing.T) {
	data := []byte("test database content")
	expected := sha256Hex(data) // internal helper

	err := verifyBytesSHA256(data, expected)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
}

func TestManagerVerifyBytesSHA256_Fail(t *testing.T) {
	data := []byte("test database content")
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"

	err := verifyBytesSHA256(data, wrongHash)
	if err == nil {
		t.Fatal("expected verification failure")
	}
}

func TestManagerRedactError(t *testing.T) {
	tests := []struct {
		input    string
		redacted bool
	}{
		{"normal error message", false},
		{"contains node_secret in the error", true},
		{"nodeSecret found here", true},
		{"X-Signature mismatch", true},
		{"connection refused", false},
		{"file not found", false},
	}
	for _, tc := range tests {
		result := redactError(fmt.Errorf("%s", tc.input))
		if tc.redacted && !strings.Contains(result, "redacted") {
			t.Errorf("expected redacted for %q, got %q", tc.input, result)
		}
		if !tc.redacted && strings.Contains(result, "redacted") {
			t.Errorf("expected not redacted for %q, got %q", tc.input, result)
		}
	}
}

func TestManagerRedactError_Nil(t *testing.T) {
	if redactError(nil) != "" {
		t.Fatal("expected empty string for nil error")
	}
}

func TestManagerLoadCached(t *testing.T) {
	dir := t.TempDir()
	storage := NewStorage(dir, "country")
	client := NewClient("http://dummy", "node", "secret", "v1.0")

	if err := storage.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	// Write manifest.
	m := &Manifest{
		DatabaseID: "db-cached",
		Version:    "2026-04",
		Format:     FormatMaxMindMMDB,
		Profile:    "country",
		Source:     "dbip_lite",
		PackageURL: "https://example.com/test.mmdb",
		SHA256:     "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		FullPackage: true,
	}
	if err := storage.WriteManifest(m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Write LKG.
	lkg := &LKGEntry{
		Source:    "dbip_lite",
		Profile:   "country",
		Format:    FormatMaxMindMMDB,
		Version:   "2026-04",
		SHA256:    "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		HealthyAt: time.Now(),
	}
	if err := storage.WriteLKG(lkg); err != nil {
		t.Fatalf("WriteLKG: %v", err)
	}

	mgr := NewGeoIPManager(true, "country", FormatMaxMindMMDB, client, storage)
	loaded := mgr.LoadCached()
	if !loaded {
		t.Fatal("expected to load cached data")
	}

	s := mgr.Status()
	if s.Version != "2026-04" {
		t.Fatalf("expected version 2026-04, got %s", s.Version)
	}
	if !s.LKGAvailable {
		t.Fatal("expected LKG available")
	}
}

func TestManagerLoadCached_NoData(t *testing.T) {
	dir := t.TempDir()
	storage := NewStorage(dir, "country")
	client := NewClient("http://dummy", "node", "secret", "v1.0")

	mgr := NewGeoIPManager(true, "country", FormatMaxMindMMDB, client, storage)
	loaded := mgr.LoadCached()
	if loaded {
		t.Fatal("expected no cached data")
	}
}

func TestManagerStatusTransitions(t *testing.T) {
	dir := t.TempDir()
	storage := NewStorage(dir, "country")
	client := NewClient("http://dummy", "node", "secret", "v1.0")

	mgr := NewGeoIPManager(true, "country", FormatMaxMindMMDB, client, storage)

	// Initial status should be ready.
	s := mgr.Status()
	if s.Status != StatusReady {
		t.Fatalf("expected ready, got %s", s.Status)
	}

	// Load cache returns false but status should still be ready.
	mgr.LoadCached()
	s = mgr.Status()
	if s.Status != StatusReady {
		t.Fatalf("expected ready after cache miss, got %s", s.Status)
	}

	// Check with a server that returns an error should set failed status.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"error":"server error"}`)
	}))
	defer server.Close()

	checkClient := NewClient(server.URL, "node", "secret", "v1.0")
	mgr.client = checkClient

	_, err := mgr.Check(context.Background())
	if err == nil {
		t.Fatal("expected check error")
	}
	s = mgr.Status()
	if s.Status != StatusFailed {
		t.Fatalf("expected failed after check error, got %s", s.Status)
	}
	if s.LastError == "" {
		t.Fatal("expected last_error to be set")
	}
}

func TestManagerIsFreshWithExpiry(t *testing.T) {
	dir := t.TempDir()
	storage := NewStorage(dir, "country")
	client := NewClient("http://dummy", "node", "secret", "v1.0")

	mgr := NewGeoIPManager(true, "country", FormatMaxMindMMDB, client, storage)

	// No manifest yet — not fresh.
	if mgr.IsFresh() {
		t.Fatal("should not be fresh with no manifest")
	}

	// Set manifest with future expiry.
	mgr.mu.Lock()
	mgr.manifest = &Manifest{
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	mgr.mu.Unlock()

	if !mgr.IsFresh() {
		t.Fatal("should be fresh with future expiry")
	}

	// Set manifest with past expiry.
	mgr.mu.Lock()
	mgr.manifest = &Manifest{
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	mgr.mu.Unlock()

	if mgr.IsFresh() {
		t.Fatal("should not be fresh with past expiry")
	}
}

func TestManagerSyncDisabled(t *testing.T) {
	client := NewClient("http://dummy", "node", "secret", "v1.0")
	storage := NewStorage(t.TempDir(), "country")
	mgr := NewGeoIPManager(false, "country", FormatMaxMindMMDB, client, storage)

	err := mgr.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error for disabled sync")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManagerFullSyncFlow(t *testing.T) {
	dir := t.TempDir()
	dbContent := []byte("geoip database content test data")

	// Create a server that provides check + download.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/agent/geoip/check":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{
				"update_available": true,
				"database": {
					"database_id": "db-sync-test",
					"version": "2026-06",
					"format": "maxmind-mmdb",
					"profile": "country",
					"source": "dbip_lite",
					"package_url": "`+helperServerURL(r)+`/download",
					"sha256": "`+sha256Hex(dbContent)+`",
					"size_bytes": `+fmt.Sprintf("%d", len(dbContent))+`,
					"generated_at": "2026-06-01T00:00:00Z",
					"expires_at": "2026-07-01T00:00:00Z",
					"full_package": true
				},
				"rollout_id": "geoip-2026-06-country"
			}`)
		case "/download":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(dbContent)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	storage := NewStorage(dir, "country")
	client := NewClient(server.URL, "node-uuid", "node-secret", "v1.0")

	mgr := NewGeoIPManager(true, "country", FormatMaxMindMMDB, client, storage)

	if err := mgr.Sync(context.Background()); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	s := mgr.Status()
	if s.Status != StatusReady {
		t.Fatalf("expected ready after sync, got %s", s.Status)
	}
	if s.Version != "2026-06" {
		t.Fatalf("expected version 2026-06, got %s", s.Version)
	}
	if !s.LKGAvailable {
		t.Fatal("expected LKG after successful sync")
	}
}


func TestManagerStatusHook(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"error":"fail"}`)
	}))
	defer server.Close()

	storage := NewStorage(dir, "country")
	client := NewClient(server.URL, "node", "secret", "v1.0")
	mgr := NewGeoIPManager(true, "country", FormatMaxMindMMDB, client, storage)

	var hookCalled bool
	mgr.OnStatusChange(func(s GeoIPStatus) {
		hookCalled = true
	})

	_, _ = mgr.Check(context.Background())
	if !hookCalled {
		t.Fatal("status hook was not called")
	}
}

func TestManagerIsFresh_NoExpiryWithRecentSync(t *testing.T) {
	dir := t.TempDir()
	storage := NewStorage(dir, "country")
	client := NewClient("http://dummy", "node", "secret", "v1.0")

	mgr := NewGeoIPManager(true, "country", FormatMaxMindMMDB, client, storage,
		WithCheckInterval(24*time.Hour))

	now := time.Now()
	mgr.mu.Lock()
	mgr.manifest = &Manifest{Version: "2026-05"}
	mgr.lastSyncAt = &now
	mgr.mu.Unlock()

	if !mgr.IsFresh() {
		t.Fatal("should be fresh with recent sync and no expiry")
	}
}

func TestManagerNoSecretInErrorLog(t *testing.T) {
	dir := t.TempDir()
	storage := NewStorage(dir, "country")
	client := NewClient("http://dummy", "node", "my-secret-value-abc123", "v1.0")
	mgr := NewGeoIPManager(true, "country", FormatMaxMindMMDB, client, storage)

	s := mgr.Status()
	_ = s

	// Verify the client doesn't leak secret.
	if client.nodeSecret != "my-secret-value-abc123" {
		t.Fatal("client should store the secret")
	}

	// Check that error messages don't contain secrets.
	// We test via redactError directly since manager calls it internally.
	redacted := redactError(fmt.Errorf("failed to verify: node_secret=abc"))
	if strings.Contains(redacted, "node_secret") {
		t.Fatal("secret leaked in redacted error")
	}
	if !strings.Contains(redacted, "redacted") {
		t.Fatal("expected redacted indicator")
	}
}

func TestManagerCorruptedPackageKeepsCurrent(t *testing.T) {
	dir := t.TempDir()
	dbContent := []byte("original good database")
	wrongContent := []byte("corrupted database")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/agent/geoip/check":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// Return manifest with wrong sha256 for the actual content.
			_, _ = fmt.Fprint(w, `{
				"update_available": true,
				"database": {
					"database_id": "db-corrupt",
					"version": "2026-07",
					"format": "maxmind-mmdb",
					"profile": "country",
					"source": "dbip_lite",
					"package_url": "`+helperServerURL(r)+`/download",
					"sha256": "0000000000000000000000000000000000000000000000000000000000000000",
					"size_bytes": `+fmt.Sprintf("%d", len(wrongContent))+`,
					"full_package": true
				},
				"rollout_id": "geoip-2026-07-country"
			}`)
		case "/download":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(wrongContent)
		}
	}))
	defer server.Close()

	storage := NewStorage(dir, "country")
	client := NewClient(server.URL, "node", "secret", "v1.0")

	// First, set up a working LKG.
	if err := storage.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	// Set the orginal content as if it was loaded from cache.
	testManifest := &Manifest{
		DatabaseID: "db-original",
		Version:    "2026-05",
		Format:     FormatMaxMindMMDB,
		Profile:    "country",
		Source:     "dbip_lite",
		PackageURL: "https://example.com/original.mmdb",
		SHA256:     sha256Hex(dbContent),
		FullPackage: true,
	}
	if err := storage.WriteManifest(testManifest); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if _, err := storage.WriteDatabaseFile("2026-05", dbContent); err != nil {
		t.Fatalf("WriteDatabaseFile: %v", err)
	}
	if err := storage.AtomicSwapCurrent("2026-05"); err != nil {
		t.Fatalf("AtomicSwapCurrent: %v", err)
	}
	lkg := &LKGEntry{
		Source:  "dbip_lite",
		Profile: "country",
		Format:  FormatMaxMindMMDB,
		Version: "2026-05",
		SHA256:  sha256Hex(dbContent),
	}
	if err := storage.WriteLKG(lkg); err != nil {
		t.Fatalf("WriteLKG: %v", err)
	}

	mgr := NewGeoIPManager(true, "country", FormatMaxMindMMDB, client, storage)
	mgr.LoadCached()

	// Attempt sync with corrupted package.
	err := mgr.Sync(context.Background())
	if err == nil {
		t.Fatal("expected sync error for corrupted package")
	}

	// Current should still point to the original version.
	s := mgr.Status()
	if s.Version == "2026-07" {
		t.Fatal("version should not have changed to corrupted version")
	}
	_ = s

	// LKG should still be available.
	if !mgr.storage.LKGExists() {
		t.Fatal("LKG should still exist")
	}
}

func TestManagerRollbackToLKG(t *testing.T) {
	dir := t.TempDir()
	storage := NewStorage(dir, "country")
	client := NewClient("http://dummy", "node", "secret", "v1.0")

	// Setup initial good state.
	if err := storage.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	lkg := &LKGEntry{
		Source:  "dbip_lite",
		Profile: "country",
		Format:  FormatMaxMindMMDB,
		Version: "2026-04",
		SHA256:  "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	}
	if err := storage.WriteLKG(lkg); err != nil {
		t.Fatalf("WriteLKG: %v", err)
	}
	if _, err := storage.WriteDatabaseFile("2026-04", []byte("good db")); err != nil {
		t.Fatalf("WriteDatabaseFile: %v", err)
	}

	mgr := NewGeoIPManager(true, "country", FormatMaxMindMMDB, client, storage)
	mgr.LoadCached()

	rolledBack, err := mgr.RollbackToLKG()
	if err != nil {
		t.Fatalf("RollbackToLKG: %v", err)
	}
	if !rolledBack {
		t.Fatal("expected rollback to succeed")
	}

	s := mgr.Status()
	if !s.LKGAvailable {
		t.Fatal("LKG should be available")
	}
}
