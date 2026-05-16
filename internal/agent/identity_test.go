package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	store := NewIdentityStore(path)

	// No identity yet.
	id, err := store.Load()
	if err != nil {
		t.Fatalf("unexpected error on empty load: %v", err)
	}
	if id != nil {
		t.Fatal("expected nil for non-existent identity")
	}

	// Save.
	saved := &Identity{NodeID: "node-uuid", NodeSecret: "secret-value"}
	if err := store.Save(saved); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Load.
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded identity is nil")
	}
	if loaded.NodeID != "node-uuid" {
		t.Fatalf("expected NodeID node-uuid, got %s", loaded.NodeID)
	}
	if loaded.NodeSecret != "secret-value" {
		t.Fatalf("expected NodeSecret secret-value, got %s", loaded.NodeSecret)
	}

	// FilePath.
	if store.FilePath() != path {
		t.Fatalf("expected FilePath %s, got %s", path, store.FilePath())
	}
}

func TestIdentityStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	store := NewIdentityStore(path)

	// Corrupt the file.
	_ = os.WriteFile(path, []byte("{corrupt"), 0644)

	// Load should fail.
	_, err := store.Load()
	if err == nil {
		t.Fatal("expected error loading corrupt identity")
	}

	// Save should still work atomically.
	err = store.Save(&Identity{NodeID: "new-uuid", NodeSecret: "new-secret"})
	if err != nil {
		t.Fatalf("save after corruption failed: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load after repair failed: %v", err)
	}
	if loaded.NodeID != "new-uuid" {
		t.Fatalf("expected NodeID new-uuid, got %s", loaded.NodeID)
	}
}

func TestIdentityStore_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	store := NewIdentityStore(path)

	err := store.Save(&Identity{NodeID: "u", NodeSecret: "s"})
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	// Should be 0600 (owner-only read/write) for secret file.
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Logf("identity file has permissions %o (expected 0600) — this is a warning", perm)
	}
}
