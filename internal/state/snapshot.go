package state

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/webdobe/dploy/internal/operation"
)

// Snapshot is the metadata record produced by a capture operation.
//
// dploy does not own the snapshot storage backend; scripts are
// responsible for actually persisting the captured data (dumps, tarballs,
// uploads to object storage, etc.). This record exists so future restore
// operations, audit, and snapshot listings can locate and reason about
// what was captured and whether it's usable.
type Snapshot struct {
	ID         string                 `json:"id"`
	Env        string                 `json:"env"`
	Class      string                 `json:"class,omitempty"`
	Resources  []string               `json:"resources,omitempty"`
	Status     operation.ResultStatus `json:"status"`
	Sanitized  bool                   `json:"sanitized,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	FinishedAt time.Time              `json:"finished_at"`
	PolicySrc  string                 `json:"policy_source,omitempty"`
}

// SnapshotStore records and lists capture metadata.
type SnapshotStore interface {
	Record(snap *Snapshot) error
	List(env string) ([]*Snapshot, error)
	Get(env, id string) (*Snapshot, error)
}

// ErrSnapshotNotFound is returned by Get when no snapshot with the
// requested (env, id) pair exists.
var ErrSnapshotNotFound = errors.New("snapshot: not found")

// FileSnapshotStore keeps one JSON file per snapshot under
// <dir>/<env>/<id>.json. Snapshots are append-only; unlike state
// records, they accumulate rather than overwrite.
type FileSnapshotStore struct {
	dir string
}

// NewFileSnapshotStore constructs a store rooted at dir. The directory
// is created on demand when Record is called.
func NewFileSnapshotStore(dir string) *FileSnapshotStore {
	return &FileSnapshotStore{dir: dir}
}

// Record writes snap to <dir>/<env>/<id>.json.
func (s *FileSnapshotStore) Record(snap *Snapshot) error {
	if snap.ID == "" {
		return errors.New("snapshot: cannot record without ID")
	}
	if snap.Env == "" {
		return errors.New("snapshot: cannot record without env")
	}

	envDir := filepath.Join(s.dir, snap.Env)
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		return fmt.Errorf("snapshot: create %s: %w", envDir, err)
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("snapshot: marshal: %w", err)
	}

	path := filepath.Join(envDir, snap.ID+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("snapshot: write %s: %w", path, err)
	}
	return nil
}

// List returns all snapshots for env, sorted by CreatedAt descending
// (newest first). Returns an empty slice if the env has no snapshots.
func (s *FileSnapshotStore) List(env string) ([]*Snapshot, error) {
	envDir := filepath.Join(s.dir, env)
	entries, err := os.ReadDir(envDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("snapshot: list %s: %w", envDir, err)
	}

	var out []*Snapshot
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(envDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("snapshot: read %s: %w", e.Name(), err)
		}
		var snap Snapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			return nil, fmt.Errorf("snapshot: parse %s: %w", e.Name(), err)
		}
		out = append(out, &snap)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// Get reads the snapshot metadata for the given (env, id). Returns
// ErrSnapshotNotFound if no such record exists — callers typically
// surface this as a user-facing "snapshot X not found in env Y" error.
func (s *FileSnapshotStore) Get(env, id string) (*Snapshot, error) {
	path := filepath.Join(s.dir, env, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrSnapshotNotFound
		}
		return nil, fmt.Errorf("snapshot: read %s: %w", path, err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("snapshot: parse %s: %w", path, err)
	}
	return &snap, nil
}

// NewSnapshotID builds a snapshot identifier from the environment name,
// the operation's start time, and a small random suffix for uniqueness.
// Format: "<env>-YYYYMMDD-HHMMSS-<hex>".
func NewSnapshotID(env string, t time.Time) string {
	var rb [3]byte
	// If crypto/rand fails (extraordinarily rare on this platform), fall
	// back to a zero suffix rather than erroring — time precision plus
	// env name is already quite unique for interactive use.
	_, _ = rand.Read(rb[:])
	return fmt.Sprintf("%s-%s-%x", env, t.UTC().Format("20060102-150405"), rb[:])
}

// Compile-time check that FileSnapshotStore satisfies SnapshotStore.
var _ SnapshotStore = (*FileSnapshotStore)(nil)
