package agent

import (
	"encoding/json"
	"fmt"
	"os"
)

// IdentityStore manages the on-disk persistence of node_id + node_secret
// obtained during registration. This allows the agent to survive restarts
// without re-registering.
type IdentityStore struct {
	filePath string
}

// NewIdentityStore creates an IdentityStore backed by the given file path.
func NewIdentityStore(filePath string) *IdentityStore {
	return &IdentityStore{filePath: filePath}
}

// Load reads the persisted identity from disk.
// Returns nil if no identity file exists yet (first start).
func (s *IdentityStore) Load() (*Identity, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read identity file: %w", err)
	}

	var id Identity
	if err := json.Unmarshal(data, &id); err != nil {
		return nil, fmt.Errorf("unmarshal identity: %w", err)
	}
	return &id, nil
}

// FilePath returns the underlying file path (used for observability).
func (s *IdentityStore) FilePath() string {
	return s.filePath
}

// Save persists the identity to disk atomically.
func (s *IdentityStore) Save(id *Identity) error {
	if id == nil {
		return fmt.Errorf("cannot save nil identity")
	}
	data, err := json.Marshal(id)
	if err != nil {
		return fmt.Errorf("marshal identity: %w", err)
	}

	tmpPath := s.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write temp identity: %w", err)
	}
	if err := os.Rename(tmpPath, s.filePath); err != nil {
		return fmt.Errorf("rename identity: %w", err)
	}
	return nil
}
