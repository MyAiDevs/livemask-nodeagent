package geoip

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStorageEnsureDirs(t *testing.T) {
	dir := t.TempDir()
	s := NewStorage(dir, "country")
	if err := s.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "country")); os.IsNotExist(err) {
		t.Fatal("profile dir was not created")
	}
}

func TestStorageWriteAndReadManifest(t *testing.T) {
	dir := t.TempDir()
	s := NewStorage(dir, "country")
	if err := s.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	m := &Manifest{
		DatabaseID: "db-456",
		Version:    "2026-05",
		Format:     FormatDBIPMMDB,
		Profile:    "country",
		Source:     "dbip_lite",
		PackageURL: "https://objects.example/test.mmdb",
		SHA256:     "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		SizeBytes:  1000,
		FullPackage: true,
	}

	if err := s.WriteManifest(m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	loaded, err := s.ReadManifest()
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if loaded == nil {
		t.Fatal("manifest should not be nil")
	}
	if loaded.DatabaseID != "db-456" {
		t.Fatalf("DatabaseID: got %s", loaded.DatabaseID)
	}
	if loaded.Version != "2026-05" {
		t.Fatalf("Version: got %s", loaded.Version)
	}
}

func TestStorageWriteAndReadLKG(t *testing.T) {
	dir := t.TempDir()
	s := NewStorage(dir, "country")
	if err := s.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	entry := &LKGEntry{
		Source:    "dbip_lite",
		Profile:   "country",
		Format:    FormatMaxMindMMDB,
		Version:   "2026-05",
		SHA256:    "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		HealthyAt: time.Now(),
	}

	if err := s.WriteLKG(entry); err != nil {
		t.Fatalf("WriteLKG: %v", err)
	}

	loaded, err := s.ReadLKG()
	if err != nil {
		t.Fatalf("ReadLKG: %v", err)
	}
	if loaded == nil {
		t.Fatal("LKG should not be nil")
	}
	if loaded.Version != "2026-05" {
		t.Fatalf("Version: got %s", loaded.Version)
	}
	if loaded.Source != "dbip_lite" {
		t.Fatalf("Source: got %s", loaded.Source)
	}
}

func TestStorageWriteDatabaseFileAndAtomicSwap(t *testing.T) {
	dir := t.TempDir()
	s := NewStorage(dir, "country")
	if err := s.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	dbContent := []byte("mock database content")
	dbPath, err := s.WriteDatabaseFile("2026-05", dbContent)
	if err != nil {
		t.Fatalf("WriteDatabaseFile: %v", err)
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not written")
	}

	// Verify content.
	readBack, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(readBack) != string(dbContent) {
		t.Fatalf("content mismatch")
	}

	// Atomic swap.
	if err := s.AtomicSwapCurrent("2026-05"); err != nil {
		t.Fatalf("AtomicSwapCurrent: %v", err)
	}

	// Verify symlink.
	currentPath, err := s.CurrentDatabasePath()
	if err != nil {
		t.Fatalf("CurrentDatabasePath: %v", err)
	}
	if currentPath == "" {
		t.Fatal("current path should not be empty")
	}

	// Read through symlink.
	symlinkContent, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("read via symlink: %v", err)
	}
	if string(symlinkContent) != string(dbContent) {
		t.Fatalf("symlink content mismatch")
	}
}

func TestStorageWriteDatabaseFileExceedsMaxSize(t *testing.T) {
	dir := t.TempDir()
	s := NewStorage(dir, "country")
	if err := s.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	bigData := make([]byte, maxDatabaseFileSize+1)
	_, err := s.WriteDatabaseFile("oversized", bigData)
	if err == nil {
		t.Fatal("expected error for oversized database file")
	}
}

func TestStorageRollbackToLKG(t *testing.T) {
	dir := t.TempDir()
	s := NewStorage(dir, "country")

	// Create LKG first.
	lkg := &LKGEntry{
		Source:    "dbip_lite",
		Profile:   "country",
		Format:    FormatMaxMindMMDB,
		Version:   "lkg-version",
		SHA256:    "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		HealthyAt: time.Now(),
	}

	// Must ensure dirs before WriteLKG.
	if err := s.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	if err := s.WriteLKG(lkg); err != nil {
		t.Fatalf("WriteLKG: %v", err)
	}

	// Write LKG database file.
	_, err := s.WriteDatabaseFile("lkg-version", []byte("lkg content"))
	if err != nil {
		t.Fatalf("WriteDatabaseFile: %v", err)
	}

	// Set current to something else.
	_, err = s.WriteDatabaseFile("bad-version", []byte("bad content"))
	if err != nil {
		t.Fatalf("WriteDatabaseFile: %v", err)
	}
	if err := s.AtomicSwapCurrent("bad-version"); err != nil {
		t.Fatalf("AtomicSwapCurrent: %v", err)
	}

	// Rollback.
	rolledBack, err := s.RollbackToLKG()
	if err != nil {
		t.Fatalf("RollbackToLKG: %v", err)
	}
	if !rolledBack {
		t.Fatal("expected rollback to succeed")
	}

	// Verify current now points to LKG.
	currentPath, err := s.CurrentDatabasePath()
	if err != nil {
		t.Fatalf("CurrentDatabasePath: %v", err)
	}
	if !strings.Contains(currentPath, "lkg-version") {
		t.Fatalf("expected current path to contain lkg-version, got %s", currentPath)
	}
}

func TestStorageRollbackToLKGNoLKG(t *testing.T) {
	dir := t.TempDir()
	s := NewStorage(dir, "country")
	if err := s.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	rolledBack, err := s.RollbackToLKG()
	if err != nil {
		t.Fatalf("RollbackToLKG: %v", err)
	}
	if rolledBack {
		t.Fatal("expected no rollback when no LKG")
	}
}

func TestStorageCurrentDatabasePath_NoSymlink(t *testing.T) {
	dir := t.TempDir()
	s := NewStorage(dir, "country")
	path, err := s.CurrentDatabasePath()
	if err != nil {
		t.Fatalf("CurrentDatabasePath: %v", err)
	}
	if path != "" {
		t.Fatal("expected empty path when no symlink")
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"../etc/passwd", "__etc_passwd"},
		{"../../secret", "____secret"},
		{"path\\traversal", "path_traversal"},
		{"normal-version-2026-05", "normal-version-2026-05"},
	}
	for _, tc := range tests {
		got := sanitizeFilename(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestStorageProfileDirPreventsTraversal(t *testing.T) {
	dir := t.TempDir()
	s := NewStorage(dir, "../../etc")
	if err := s.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	// The profile dir should be sanitized.
	if strings.Contains(s.profileDir(), "..") {
		t.Fatal("profile dir should not contain '..'")
	}
	if !strings.HasPrefix(s.profileDir(), dir) {
		t.Fatal("profile dir should be within base dir")
	}
}
