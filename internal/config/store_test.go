package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	store := NewStore(path)

	// No cache yet.
	entry, err := store.Load()
	if err != nil {
		t.Fatalf("unexpected error on empty load: %v", err)
	}
	if entry != nil {
		t.Fatal("expected nil entry for non-existent cache")
	}

	// Save.
	now := time.Now().UTC()
	payload := json.RawMessage(`{"reporting":{"heartbeat_interval_seconds":30}}`)
	hash := ComputeHash(payload)
	saved := &CacheEntry{
		Response: &ConfigResponse{
			ConfigKey:     "nodeagent.runtime_config",
			SchemaVersion: "1.0",
			ConfigVersion: 5,
			ConfigHash:    hash,
			Payload:       payload,
		},
		Parsed: &RuntimeConfig{
			SchemaVersion: "1.0",
			Reporting: ReportingConfig{
				HeartbeatIntervalSeconds: 30,
			},
		},
		FetchedAt: now,
	}
	if err := store.Save(saved); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Load.
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded entry is nil")
	}
	if loaded.Response.ConfigVersion != 5 {
		t.Fatalf("expected version 5, got %d", loaded.Response.ConfigVersion)
	}
	if loaded.Parsed.Reporting.HeartbeatIntervalSeconds != 30 {
		t.Fatalf("expected heartbeat 30, got %d", loaded.Parsed.Reporting.HeartbeatIntervalSeconds)
	}
}

func TestStore_Exists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	store := NewStore(path)

	if store.Exists() {
		t.Fatal("cache should not exist before save")
	}

	_ = store.Save(&CacheEntry{
		Response:  &ConfigResponse{ConfigKey: "nodeagent.runtime_config", ConfigVersion: 1, ConfigHash: "sha256:00", Payload: json.RawMessage(`{}`)},
		Parsed:    &RuntimeConfig{},
		FetchedAt: time.Now(),
	})
	if !store.Exists() {
		t.Fatal("cache should exist after save")
	}
}

func TestStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	store := NewStore(path)

	// Save corrupt data to the actual path (simulating a crash).
	_ = os.WriteFile(path, []byte("{corrupt"), 0644)

	// Load should fail.
	_, err := store.Load()
	if err == nil {
		t.Fatal("expected error loading corrupt cache")
	}

	// Save should still work (atomic: write to .tmp then rename).
	payload := json.RawMessage(`{"key":"value"}`)
	err = store.Save(&CacheEntry{
		Response:  &ConfigResponse{ConfigKey: "nodeagent.runtime_config", ConfigVersion: 2, ConfigHash: ComputeHash(payload), Payload: payload},
		Parsed:    &RuntimeConfig{SchemaVersion: "1.0"},
		FetchedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("save after corruption failed: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load after repair failed: %v", err)
	}
	if loaded.Response.ConfigVersion != 2 {
		t.Fatalf("expected version 2, got %d", loaded.Response.ConfigVersion)
	}
}
