// Package state persists operation results so status, logs, and future
// rollback eligibility have something to read.
//
// v1 uses a simple JSON-per-environment file layout under .dploy/.
// No database is required.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/webdobe/dploy/internal/operation"
)

// Store is the contract for recording and reading operation results.
type Store interface {
	Record(result *operation.Result) error
	Latest(environment string) (*operation.Result, error)
}

// FileStore is a Store backed by JSON files in a directory.
type FileStore struct {
	dir string
}

// NewFileStore constructs a FileStore rooted at dir. The directory is
// created on first Record call; it does not have to exist yet.
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

// Record writes result to disk keyed by environment name. It overwrites
// the previous latest for that environment.
//
// Recording failure must not erase the primary operation result in logs,
// so callers should preserve result independently of this call.
func (s *FileStore) Record(result *operation.Result) error {
	if result.Environment == "" {
		return fmt.Errorf("state: cannot record result with no environment")
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("state: create %s: %w", s.dir, err)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("state: marshal: %w", err)
	}

	path := filepath.Join(s.dir, result.Environment+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("state: write %s: %w", path, err)
	}
	return nil
}

// Latest returns the most recently recorded result for environment, or
// nil if none has been recorded yet.
func (s *FileStore) Latest(environment string) (*operation.Result, error) {
	path := filepath.Join(s.dir, environment+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("state: read %s: %w", path, err)
	}

	var r operation.Result
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("state: parse %s: %w", path, err)
	}
	return &r, nil
}

// Compile-time check that FileStore satisfies Store.
var _ Store = (*FileStore)(nil)
