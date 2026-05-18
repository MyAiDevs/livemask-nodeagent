package geoip

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultCacheDir is the default directory for GeoIP data under
	// the nodeagent runtime directory.
	DefaultCacheDir = "/var/lib/nodeagent/geoip"

	// manifestFilename is the name of the current manifest JSON file.
	manifestFilename = "manifest.json"
	// lkgFilename is the name of the last-known-good metadata file.
	lkgFilename = "lkg.json"
	// currentSymlink is the name of the symlink pointing to the active
	// database version directory.
	currentSymlink = "current"
	// previousSymlink is the name of the symlink pointing to the previous
	// database version directory.
	previousSymlink = "previous"
	// tempDownloadPrefix is the prefix for temp download files.
	tempDownloadPrefix = ".download-"

	// maxDatabaseFileSize is the maximum allowed database file size (100 MB).
	maxDatabaseFileSize = 100 * 1024 * 1024
)

// Storage manages the on-disk GeoIP database files.
// TASK-NODEAGENT-GEOIP-001.
type Storage struct {
	baseDir string
	profile string
}

// NewStorage creates a Storage rooted at baseDir for the given profile.
func NewStorage(baseDir, profile string) *Storage {
	return &Storage{
		baseDir: baseDir,
		profile: profile,
	}
}

// profileDir returns the directory for the given profile (e.g., country/).
func (s *Storage) profileDir() string {
	// Sanitize profile name: strip path separators to prevent traversal.
	sanitized := strings.ReplaceAll(s.profile, "/", "_")
	sanitized = strings.ReplaceAll(sanitized, "\\", "_")
	sanitized = strings.ReplaceAll(sanitized, "..", "_")
	return filepath.Join(s.baseDir, sanitized)
}

// versionDir returns the directory for a specific version under the profile.
func (s *Storage) versionDir(version string) string {
	// Sanitize version string.
	sanitized := sanitizeFilename(version)
	return filepath.Join(s.profileDir(), sanitized)
}

// currentPath returns the path to the current database file via symlink.
func (s *Storage) currentPath() string {
	return filepath.Join(s.profileDir(), currentSymlink)
}

// manifestPath returns the path to the current manifest JSON.
func (s *Storage) manifestPath() string {
	return filepath.Join(s.profileDir(), manifestFilename)
}

// lkgPath returns the path to the last-known-good metadata file.
func (s *Storage) lkgPath() string {
	return filepath.Join(s.profileDir(), lkgFilename)
}

// EnsureDirs creates the directory structure if it does not exist.
func (s *Storage) EnsureDirs() error {
	return os.MkdirAll(s.profileDir(), 0755)
}

// TempDownloadPath returns a unique temp file path for downloading.
func (s *Storage) TempDownloadPath() string {
	return filepath.Join(s.profileDir(), tempDownloadPrefix+fmt.Sprintf("%d", time.Now().UnixNano()))
}

// DatabaseFilePath returns the expected database file path for a given version.
// The filename is derived from the profile (e.g., "GeoIP-Country.mmdb").
func (s *Storage) DatabaseFilePath(version string) string {
	ext := ".db"
	// Use a reasonable extension based on the profile but we keep it generic.
	return filepath.Join(s.versionDir(version), "GeoIP-"+s.profile+ext)
}

// WriteManifest saves the manifest atomically.
func (s *Storage) WriteManifest(m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	tmpPath := s.manifestPath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write manifest temp: %w", err)
	}
	if err := os.Rename(tmpPath, s.manifestPath()); err != nil {
		return fmt.Errorf("rename manifest: %w", err)
	}
	return nil
}

// ReadManifest reads the current manifest from disk.
func (s *Storage) ReadManifest() (*Manifest, error) {
	data, err := os.ReadFile(s.manifestPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}
	return &m, nil
}

// WriteLKG persists the last-known-good entry atomically.
func (s *Storage) WriteLKG(entry *LKGEntry) error {
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lkg: %w", err)
	}
	tmpPath := s.lkgPath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write lkg temp: %w", err)
	}
	if err := os.Rename(tmpPath, s.lkgPath()); err != nil {
		return fmt.Errorf("rename lkg: %w", err)
	}
	return nil
}

// ReadLKG reads the last-known-good entry from disk.
func (s *Storage) ReadLKG() (*LKGEntry, error) {
	data, err := os.ReadFile(s.lkgPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read lkg: %w", err)
	}
	var entry LKGEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal lkg: %w", err)
	}
	return &entry, nil
}

// LKGExists returns true if a last-known-good entry exists.
func (s *Storage) LKGExists() bool {
	_, err := os.Stat(s.lkgPath())
	return err == nil
}

// CurrentDatabasePath resolves the current symlink to the actual database file.
func (s *Storage) CurrentDatabasePath() (string, error) {
	link := s.currentPath()
	target, err := os.Readlink(link)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read current symlink: %w", err)
	}
	// If the symlink points to a directory, look for the database file inside.
	if fi, statErr := os.Stat(target); statErr == nil && fi.IsDir() {
		entries, readErr := os.ReadDir(target)
		if readErr != nil {
			return "", fmt.Errorf("read version dir: %w", readErr)
		}
		for _, e := range entries {
			if !e.IsDir() && e.Name() != manifestFilename && e.Name() != lkgFilename {
				return filepath.Join(target, e.Name()), nil
			}
		}
		return "", fmt.Errorf("no database file in version dir %s", target)
	}
	return target, nil
}

// WriteDatabaseFile writes the downloaded data to a temp file and atomically
// moves it to the versioned directory. Returns the final database file path.
func (s *Storage) WriteDatabaseFile(version string, data []byte) (string, error) {
	if int64(len(data)) > maxDatabaseFileSize {
		return "", fmt.Errorf("database file too large: %d bytes > %d limit", len(data), maxDatabaseFileSize)
	}

	verDir := s.versionDir(version)
	if err := os.MkdirAll(verDir, 0755); err != nil {
		return "", fmt.Errorf("create version dir: %w", err)
	}

	finalPath := s.DatabaseFilePath(version)
	tmpPath := finalPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return "", fmt.Errorf("write database temp: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("rename database: %w", err)
	}
	return finalPath, nil
}

// AtomicSwapCurrent atomically swaps the current and previous symlinks to
// point to the new version directory. It creates the version directory if it
// does not exist and uses a symlink-based approach:
//
//	previous -> old current target
//	current -> new version directory / database file
//
// TASK-NODEAGENT-GEOIP-001.
func (s *Storage) AtomicSwapCurrent(version string) error {
	verDir := s.versionDir(version)
	dbPath := s.DatabaseFilePath(version)

	// Verify the database file exists.
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("database file %s does not exist: %w", dbPath, err)
	}

	currentLink := s.currentPath()
	previousLink := filepath.Join(s.profileDir(), previousSymlink)

	// Read old symlink target for previous pointer.
	if oldTarget, err := os.Readlink(currentLink); err == nil {
		// Move old current -> previous.
		_ = os.Remove(previousLink)
		if symErr := os.Symlink(oldTarget, previousLink); symErr != nil {
			log.Printf("[geoip] warning: failed to create previous symlink: %v", symErr)
		}
	}

	// Remove old current symlink and create new one.
	_ = os.Remove(currentLink)
	if err := os.Symlink(verDir, currentLink); err != nil {
		return fmt.Errorf("create current symlink: %w", err)
	}

	return nil
}

// RollbackToLKG rolls back the current symlink to the last-known-good version.
// Returns true if rollback was performed, false if no LKG is available.
// TASK-NODEAGENT-GEOIP-001.
func (s *Storage) RollbackToLKG() (bool, error) {
	lkg, err := s.ReadLKG()
	if err != nil {
		return false, fmt.Errorf("read lkg for rollback: %w", err)
	}
	if lkg == nil || lkg.Version == "" {
		return false, nil
	}

	verDir := s.versionDir(lkg.Version)
	if _, err := os.Stat(verDir); os.IsNotExist(err) {
		return false, fmt.Errorf("lkg version dir %s does not exist", verDir)
	}

	currentLink := s.currentPath()
	_ = os.Remove(currentLink)
	if err := os.Symlink(verDir, currentLink); err != nil {
		return false, fmt.Errorf("create current symlink to lkg: %w", err)
	}

	log.Printf("[geoip] rolled back to LKG version %s", lkg.Version)
	return true, nil
}

// CleanupTemp removes temp download files.
func (s *Storage) CleanupTemp() error {
	dir := s.profileDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read profile dir for cleanup: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), tempDownloadPrefix) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}

// sanitizeFilename removes path traversal characters from a filename.
func sanitizeFilename(name string) string {
	cleaned := strings.ReplaceAll(name, "/", "_")
	cleaned = strings.ReplaceAll(cleaned, "\\", "_")
	cleaned = strings.ReplaceAll(cleaned, "..", "_")
	cleaned = strings.ReplaceAll(cleaned, "~", "_")
	return cleaned
}
