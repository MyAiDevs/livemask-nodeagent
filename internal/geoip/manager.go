package geoip

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// GeoIPManager orchestrates GeoIP database manifest checking, package
// download, checksum verification, atomic installation, LKG fallback,
// and status reporting.
// TASK-NODEAGENT-GEOIP-001.
type GeoIPManager struct {
	client  *Client
	storage *Storage

	enabled bool
	profile string
	format  Format

	// polling/retry configuration.
	checkInterval time.Duration

	mu            sync.RWMutex
	manifest      *Manifest
	currentPath   string
	lkgEntry      *LKGEntry
	status        Status
	lastCheckAt   *time.Time
	lastSyncAt    *time.Time
	lastError     string
	pollCtx       context.Context
	pollCancel    context.CancelFunc
	pollWg        sync.WaitGroup
	statusHooks   []func(GeoIPStatus)
}

// ManagerOption allows functional configuration of GeoIPManager.
type ManagerOption func(*GeoIPManager)

// WithCheckInterval sets the interval between GeoIP check polls.
func WithCheckInterval(d time.Duration) ManagerOption {
	return func(m *GeoIPManager) {
		m.checkInterval = d
	}
}

// NewGeoIPManager creates a new GeoIPManager.
// enabled controls whether geoip sync is active.
// profile is the GeoIP database profile (e.g., "country", "city", "asn").
// format is the expected database format.
// client is the Backend API client.
// storage is the local file manager.
func NewGeoIPManager(
	enabled bool,
	profile string,
	format Format,
	client *Client,
	storage *Storage,
	opts ...ManagerOption,
) *GeoIPManager {
	if !IsKnownFormat(format) {
		log.Printf("[geoip] WARNING: unknown format %q, manager will reject manifests", format)
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := &GeoIPManager{
		client:        client,
		storage:       storage,
		enabled:       enabled,
		profile:       profile,
		format:        format,
		status:        StatusDisabled,
		checkInterval: 24 * time.Hour,
		pollCtx:       ctx,
		pollCancel:    cancel,
	}
	if !enabled {
		return m
	}
	m.status = StatusReady
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Check queries Backend to see if a GeoIP update is available.
// It validates the returned manifest (format, profile, etc.) and updates
// internal state but does not download or install anything.
// TASK-NODEAGENT-GEOIP-001.
func (m *GeoIPManager) Check(ctx context.Context) (*CheckResponse, error) {
	m.mu.Lock()
	m.status = StatusSyncing
	m.mu.Unlock()

	now := time.Now()

	currentVersion := ""
	m.mu.RLock()
	if m.manifest != nil {
		currentVersion = m.manifest.Version
	}
	m.mu.RUnlock()

	// The check call does not include secrets — safe to log.
	log.Printf("[geoip] checking for update (current_version=%s, format=%s, profile=%s)",
		currentVersion, m.format, m.profile)

	resp, err := m.client.Check(ctx, currentVersion, m.format, m.profile)
	if err != nil {
		m.mu.Lock()
		m.status = StatusFailed
		m.lastCheckAt = &now
		m.lastError = redactError(err)
		m.mu.Unlock()
		m.fireStatusHooks()
		return nil, fmt.Errorf("geoip check: %w", err)
	}

	m.mu.Lock()
	m.lastCheckAt = &now
	m.lastError = ""

	m.status = StatusReady
	m.mu.Unlock()

	if resp.UpdateAvailable && resp.Database != nil {
		// Validate the manifest fields.
		if err := m.validateManifest(resp.Database); err != nil {
			m.mu.Lock()
			m.status = StatusFailed
			m.lastError = redactError(err)
			m.mu.Unlock()
			m.fireStatusHooks()
			return nil, fmt.Errorf("manifest validation: %w", err)
		}

		log.Printf("[geoip] update available: version=%s, format=%s, source=%s",
			resp.Database.Version, resp.Database.Format, resp.Database.Source)
	} else {
		log.Printf("[geoip] already current (version=%s)", currentVersion)
	}
	m.fireStatusHooks()

	return resp, nil
}

// Sync performs a full sync cycle: check for update, download package,
// verify checksum, atomically install, and report the result.
// TASK-NODEAGENT-GEOIP-001.
func (m *GeoIPManager) Sync(ctx context.Context) error {
	if !m.enabled {
		return fmt.Errorf("geoip sync is disabled")
	}

	now := time.Now()

	m.mu.Lock()
	m.status = StatusSyncing
	m.mu.Unlock()
	m.fireStatusHooks()

	// Step 1: Check for update.
	resp, err := m.Check(ctx)
	if err != nil {
		m.mu.Lock()
		m.status = StatusFailed
		m.lastError = redactError(err)
		m.mu.Unlock()
		m.fireStatusHooks()
		return fmt.Errorf("geoip check in sync: %w", err)
	}

	// If no update available, we're done.
	if resp == nil || !resp.UpdateAvailable || resp.Database == nil {
		m.mu.Lock()
		m.status = StatusReady
		m.lastSyncAt = &now
		m.mu.Unlock()
		m.fireStatusHooks()
		return nil
	}

	manifest := resp.Database
	rolloutID := resp.RolloutID

	// Step 2: Download package.
	log.Printf("[geoip] downloading package: version=%s, size=%d bytes", manifest.Version, manifest.SizeBytes)
	data, err := m.client.DownloadPackage(ctx, manifest.PackageURL)
	if err != nil {
		m.mu.Lock()
		m.status = StatusFailed
		m.lastError = redactError(err)
		m.mu.Unlock()
		m.fireStatusHooks()
		m.reportEventAsync(rolloutID, "", manifest.Version, "failed", "download_failed",
			manifest.SHA256, redactError(err))
		return fmt.Errorf("geoip download: %w", err)
	}

	// Step 3: Verify SHA-256 directly in memory.
	log.Printf("[geoip] verifying checksum")
	if err := verifyBytesSHA256(data, manifest.SHA256); err != nil {
		_ = m.storage.CleanupTemp()
		m.mu.Lock()
		m.status = StatusFailed
		m.lastError = redactError(err)
		m.mu.Unlock()
		m.fireStatusHooks()
		m.reportEventAsync(rolloutID, "", manifest.Version, "failed", "checksum_mismatch",
			manifest.SHA256, redactError(err))
		return fmt.Errorf("geoip checksum: %w", err)
	}
	_ = m.storage.CleanupTemp()

	// Step 4: Write database file atomically to version directory.
	prevVersion := ""
	m.mu.RLock()
	if m.manifest != nil {
		prevVersion = m.manifest.Version
	}
	m.mu.RUnlock()

	dbPath, err := m.storage.WriteDatabaseFile(manifest.Version, data)
	if err != nil {
		m.mu.Lock()
		m.status = StatusFailed
		m.lastError = redactError(err)
		m.mu.Unlock()
		m.fireStatusHooks()
		return fmt.Errorf("geoip write database: %w", err)
	}

	// Step 5: Atomic swap.
	if err := m.storage.AtomicSwapCurrent(manifest.Version); err != nil {
		m.mu.Lock()
		m.status = StatusFailed
		m.lastError = redactError(err)
		m.mu.Unlock()
		m.fireStatusHooks()
		// Try LKG fallback.
		m.attemptLKGFallback()
		return fmt.Errorf("geoip atomic swap: %w", err)
	}

	// Step 6: Save manifest.
	if err := m.storage.WriteManifest(manifest); err != nil {
		log.Printf("[geoip] warning: failed to persist manifest: %v", err)
	}

	// Step 7: Mark as LKG.
	lkg := &LKGEntry{
		Source:    manifest.Source,
		Profile:   m.profile,
		Format:    manifest.Format,
		Version:   manifest.Version,
		SHA256:    manifest.SHA256,
		HealthyAt: time.Now(),
	}
	if saveErr := m.storage.WriteLKG(lkg); saveErr != nil {
		log.Printf("[geoip] warning: failed to persist lkg: %v", saveErr)
	}

	// Step 8: Update internal state.
	m.mu.Lock()
	m.manifest = manifest
	m.currentPath = dbPath
	m.lkgEntry = lkg
	m.status = StatusReady
	m.lastCheckAt = &now
	m.lastSyncAt = &now
	m.lastError = ""
	m.mu.Unlock()
	m.fireStatusHooks()

	log.Printf("[geoip] sync complete: version=%s, path=%s", manifest.Version, dbPath)

	// Step 9: Report event (best-effort, async).
	m.reportEventAsync(rolloutID, prevVersion, manifest.Version, "installed", "",
		manifest.SHA256, "")

	return nil
}

// Status returns the current GeoIP sync status snapshot.
func (m *GeoIPManager) Status() GeoIPStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := GeoIPStatus{
		Enabled:      m.enabled,
		Status:       m.status,
		Profile:      m.profile,
		Format:       m.format,
		CurrentPath:  m.currentPath,
		LastError:    m.lastError,
	}
	if m.storage != nil {
		s.LKGAvailable = m.storage.LKGExists()
	}
	if m.lkgEntry != nil {
		s.LKGAvailable = true
	}
	if m.manifest != nil {
		s.DatabaseID = m.manifest.DatabaseID
		s.Version = m.manifest.Version
		s.Format = m.manifest.Format
		s.Source = m.manifest.Source
		s.SHA256 = m.manifest.SHA256
		if !m.manifest.GeneratedAt.IsZero() {
			u := m.manifest.GeneratedAt.Unix()
			s.GeneratedAt = &u
		}
		if !m.manifest.ExpiresAt.IsZero() {
			u := m.manifest.ExpiresAt.Unix()
			s.ExpiresAt = &u
		}
	}
	if m.lastCheckAt != nil {
		u := m.lastCheckAt.Unix()
		s.LastCheckAt = &u
	}
	if m.lastSyncAt != nil {
		u := m.lastSyncAt.Unix()
		s.LastSyncAt = &u
	}
	return s
}

// LoadCached attempts to load the cached manifest and LKG from disk.
// Returns true if data was loaded, false if no cache exists.
// TASK-NODEAGENT-GEOIP-001.
func (m *GeoIPManager) LoadCached() bool {
	// Load manifest.
	manifest, err := m.storage.ReadManifest()
	if err != nil {
		log.Printf("[geoip] failed to read cached manifest: %v", err)
	}
	_ = manifest

	// Load LKG.
	lkg, err := m.storage.ReadLKG()
	if err != nil {
		log.Printf("[geoip] failed to read lkg: %v", err)
	}

	// Resolve current database path.
	dbPath, err := m.storage.CurrentDatabasePath()
	if err != nil {
		log.Printf("[geoip] failed to resolve current path: %v", err)
	}

	hasData := false

	m.mu.Lock()
	if manifest != nil {
		// Validate the cached manifest format/profile.
		if IsKnownFormat(manifest.Format) || manifest.Format == "" {
			m.manifest = manifest
			hasData = true
		} else {
			log.Printf("[geoip] cached manifest has unknown format %q, ignoring", manifest.Format)
		}
	}
	if lkg != nil {
		m.lkgEntry = lkg
		hasData = true
	}
	if dbPath != "" {
		m.currentPath = dbPath
		hasData = true
	}
	if m.enabled && hasData {
		m.status = StatusReady
	}
	m.mu.Unlock()

	if hasData {
		log.Printf("[geoip] loaded cached data (manifest_version=%s, current_path=%s)",
			func() string {
				if m.manifest != nil {
					return m.manifest.Version
				}
				return "none"
			}(),
			dbPath)
	}
	return hasData
}

// RollbackToLKG rolls back to the last-known-good database version.
// TASK-NODEAGENT-GEOIP-001.
func (m *GeoIPManager) RollbackToLKG() (bool, error) {
	ok, err := m.storage.RollbackToLKG()
	if err != nil {
		m.mu.Lock()
		m.status = StatusFailed
		m.lastError = redactError(err)
		m.mu.Unlock()
		m.fireStatusHooks()
		return false, fmt.Errorf("rollback to lkg: %w", err)
	}
	if !ok {
		return false, nil
	}

	// Reload current path after rollback.
	dbPath, err := m.storage.CurrentDatabasePath()
	if err == nil {
		m.mu.Lock()
		m.currentPath = dbPath
		m.status = StatusReady
		m.lastError = ""
		m.mu.Unlock()
	} else {
		log.Printf("[geoip] warning: after rollback, could not resolve current path: %v", err)
		m.mu.Lock()
		m.status = StatusReady
		m.mu.Unlock()
	}
	m.fireStatusHooks()

	return true, nil
}

// IsFresh returns true if the current GeoIP database has been synced
// and is within its expiration window.
func (m *GeoIPManager) IsFresh() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.enabled || m.manifest == nil {
		return false
	}

	// If expires_at is set, check it.
	if !m.manifest.ExpiresAt.IsZero() {
		return time.Now().Before(m.manifest.ExpiresAt)
	}

	// If no expires_at, consider fresh if synced within check interval * 2.
	if m.lastSyncAt != nil {
		return time.Since(*m.lastSyncAt) < m.checkInterval*2
	}

	return false
}

// StartPoll launches a background goroutine that periodically checks for
// GeoIP updates. Call StopPoll to stop.
func (m *GeoIPManager) StartPoll() {
	if !m.enabled {
		return
	}
	m.pollWg.Add(1)
	go func() {
		defer m.pollWg.Done()
		for {
			select {
			case <-m.pollCtx.Done():
				return
			case <-time.After(m.checkInterval):
				if err := m.Sync(m.pollCtx); err != nil {
					log.Printf("[geoip] poll sync error: %v", err)
				}
			}
		}
	}()
	log.Printf("[geoip] poll started (interval=%v)", m.checkInterval)
}

// StopPoll stops the background polling goroutine.
func (m *GeoIPManager) StopPoll() {
	m.pollCancel()
	m.pollWg.Wait()
}

// OnStatusChange registers a callback that fires when the GeoIP status changes.
func (m *GeoIPManager) OnStatusChange(hook func(GeoIPStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusHooks = append(m.statusHooks, hook)
}

func (m *GeoIPManager) fireStatusHooks() {
	status := m.Status()
	m.mu.RLock()
	hooks := make([]func(GeoIPStatus), len(m.statusHooks))
	copy(hooks, m.statusHooks)
	m.mu.RUnlock()
	for _, h := range hooks {
		h(status)
	}
}

// validateManifest checks that the manifest format and profile are known
// and match the manager's expectations.
func (m *GeoIPManager) validateManifest(manifest *Manifest) error {
	if manifest == nil {
		return fmt.Errorf("manifest is nil")
	}
	if manifest.PackageURL == "" {
		return fmt.Errorf("manifest package_url is empty")
	}
	if manifest.SHA256 == "" {
		return fmt.Errorf("manifest sha256 is empty")
	}

	// Check format.
	if manifest.Format == "" {
		return fmt.Errorf("manifest format is empty")
	}
	if !IsKnownFormat(manifest.Format) {
		return fmt.Errorf("unknown format %q: must be one of maxmind-mmdb, ip2location-bin, dbip-mmdb, geoip2-cn-mmdb, geoip2-cn-dat", manifest.Format)
	}

	// If the manager has a specific format configured, it must match.
	if m.format != "" && manifest.Format != m.format {
		return fmt.Errorf("format mismatch: manager expects %q, manifest provides %q", m.format, manifest.Format)
	}

	// Check profile.
	if manifest.Profile == "" {
		return fmt.Errorf("manifest profile is empty")
	}
	if m.profile != "" && manifest.Profile != m.profile {
		return fmt.Errorf("profile mismatch: manager expects %q, manifest provides %q", m.profile, manifest.Profile)
	}

	// Validate sha256 format.
	if _, err := ValidateSHA256Format(manifest.SHA256); err != nil {
		return fmt.Errorf("invalid sha256 in manifest: %w", err)
	}

	// Validate package_url scheme.
	if !strings.HasPrefix(manifest.PackageURL, "http://") &&
		!strings.HasPrefix(manifest.PackageURL, "https://") {
		return fmt.Errorf("package_url must be http or https")
	}

	return nil
}

// attemptLKGFallback tries to rollback to LKG after a failure.
// This is called internally when atomic swap fails.
func (m *GeoIPManager) attemptLKGFallback() {
	ok, err := m.RollbackToLKG()
	if err != nil {
		log.Printf("[geoip] LKG fallback failed: %v", err)
		return
	}
	if ok {
		log.Printf("[geoip] LKG fallback successful")
	} else {
		log.Printf("[geoip] no LKG available for fallback")
	}
}

// reportEventAsync sends a sync event to Backend best-effort.
func (m *GeoIPManager) reportEventAsync(rolloutID, fromVersion, toVersion, status, reason, sha256, message string) {
	event := &SyncEvent{
		RolloutID:   rolloutID,
		FromVersion: fromVersion,
		ToVersion:   toVersion,
		Status:      status,
		Reason:      reason,
		CurrentSHA256: sha256,
		Message:     message,
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.client.ReportEvent(ctx, event); err != nil {
			log.Printf("[geoip] event report failed (will retry later): %v", err)
		}
	}()
}

// verifyBytesSHA256 verifies that the data matches the expected SHA-256 hex digest.
func verifyBytesSHA256(data []byte, expected string) error {
	expected = stripSHA256Prefix(expected)
	h := sha256.Sum256(data)
	got := fmt.Sprintf("%x", h)
	if got != expected {
		return fmt.Errorf("sha256 mismatch: got %s, expected %s", got, expected)
	}
	return nil
}

// redactError returns a redacted version of an error suitable for logging
// and status display. It prevents secrets from leaking in error messages.
func redactError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Redact common sensitive patterns.
	redactions := []string{
		"node_secret",
		"nodeSecret",
		"NodeSecret",
		"X-Signature",
		"x-signature",
		"xSignature",
		"XSignature",
	}
	for _, r := range redactions {
		if strings.Contains(msg, r) {
			msg = "redacted error (sensitive content removed)"
			break
		}
	}
	// Limit length.
	if len(msg) > 500 {
		msg = msg[:500] + "..."
	}
	return msg
}
