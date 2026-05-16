package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Store manages the last-known-good config on disk.
// Thread-safe via sync.RWMutex.
type Store struct {
	mu       sync.RWMutex
	filePath string
}

// NewStore creates a Store backed by the given file path.
// The file is created lazily on first write.
func NewStore(filePath string) *Store {
	return &Store{filePath: filePath}
}

// Load reads the last-known-good cache entry from disk.
// Returns nil if no cache exists yet (fresh start or first fetch).
func (s *Store) Load() (*CacheEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no cache yet — not an error
		}
		return nil, fmt.Errorf("read cache file: %w", err)
	}

	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal cache: %w", err)
	}
	return &entry, nil
}

// Save persists a cache entry to disk atomically (write temp + rename).
func (s *Store) Save(entry *CacheEntry) error {
	if entry == nil {
		return fmt.Errorf("cannot save nil cache entry")
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Write to a temp file first, then rename for atomicity.
	tmpPath := s.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp cache: %w", err)
	}
	if err := os.Rename(tmpPath, s.filePath); err != nil {
		return fmt.Errorf("rename cache: %w", err)
	}
	return nil
}

// Exists returns true if a cache file exists on disk.
func (s *Store) Exists() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, err := os.Stat(s.filePath)
	return err == nil
}
