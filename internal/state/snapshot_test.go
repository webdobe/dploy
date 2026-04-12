package state

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/webdobe/dploy/internal/operation"
)

func TestFileSnapshotStore_RecordCreatesFile(t *testing.T) {
	dir := t.TempDir()
	s := NewFileSnapshotStore(dir)

	snap := &Snapshot{
		ID:         "production-20260412-120000-abcdef",
		Env:        "production",
		Class:      "production",
		Resources:  []string{"database"},
		Status:     operation.StatusSuccess,
		Sanitized:  true,
		CreatedAt:  time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 4, 12, 12, 0, 10, 0, time.UTC),
	}
	if err := s.Record(snap); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// File lives under <dir>/<env>/<id>.json
	path := filepath.Join(dir, "production", "production-20260412-120000-abcdef.json")
	list, err := s.List("production")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List returned %d snapshots; want 1 (expected file at %s)", len(list), path)
	}
	got := list[0]
	if got.ID != snap.ID || got.Env != "production" || !got.Sanitized {
		t.Errorf("round-trip: %+v", got)
	}
	if got.Status != operation.StatusSuccess {
		t.Errorf("Status = %q; want success", got.Status)
	}
}

func TestFileSnapshotStore_RecordRejectsMissingFields(t *testing.T) {
	s := NewFileSnapshotStore(t.TempDir())

	if err := s.Record(&Snapshot{Env: "production"}); err == nil {
		t.Error("expected error when ID is missing")
	}
	if err := s.Record(&Snapshot{ID: "snap-id"}); err == nil {
		t.Error("expected error when Env is missing")
	}
}

func TestFileSnapshotStore_ListSortsNewestFirst(t *testing.T) {
	s := NewFileSnapshotStore(t.TempDir())

	// Record three snapshots out of chronological order.
	times := []time.Time{
		time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 12, 11, 0, 0, 0, time.UTC),
	}
	for i, ts := range times {
		snap := &Snapshot{
			ID:        "snap-" + ts.Format("150405"),
			Env:       "production",
			Status:    operation.StatusSuccess,
			CreatedAt: ts,
		}
		if err := s.Record(snap); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}

	list, err := s.List("production")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List returned %d; want 3", len(list))
	}
	// Newest first.
	if list[0].CreatedAt.Hour() != 12 || list[1].CreatedAt.Hour() != 11 || list[2].CreatedAt.Hour() != 10 {
		t.Errorf("order = %v %v %v; want descending CreatedAt", list[0].CreatedAt, list[1].CreatedAt, list[2].CreatedAt)
	}
}

func TestFileSnapshotStore_ListReturnsNilForUnknownEnv(t *testing.T) {
	s := NewFileSnapshotStore(t.TempDir())
	list, err := s.List("never-captured")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list != nil {
		t.Errorf("List = %v; want nil for env with no snapshots", list)
	}
}

func TestFileSnapshotStore_GetReturnsRecordedSnapshot(t *testing.T) {
	s := NewFileSnapshotStore(t.TempDir())

	snap := &Snapshot{
		ID:        "production-20260412-120000-abcdef",
		Env:       "production",
		Class:     "production",
		Resources: []string{"database"},
		Status:    operation.StatusSuccess,
		Sanitized: true,
		CreatedAt: time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC),
	}
	if err := s.Record(snap); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := s.Get("production", snap.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != snap.ID || got.Env != "production" || !got.Sanitized {
		t.Errorf("round-trip: %+v", got)
	}
	if len(got.Resources) != 1 || got.Resources[0] != "database" {
		t.Errorf("Resources = %v; want [database]", got.Resources)
	}
}

func TestFileSnapshotStore_GetReturnsNotFoundForUnknownID(t *testing.T) {
	s := NewFileSnapshotStore(t.TempDir())

	_, err := s.Get("production", "does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing snapshot")
	}
	if !errors.Is(err, ErrSnapshotNotFound) {
		t.Errorf("err = %v; want ErrSnapshotNotFound", err)
	}
}

func TestNewSnapshotID_Format(t *testing.T) {
	ts := time.Date(2026, 4, 12, 12, 30, 45, 0, time.UTC)
	id := NewSnapshotID("production", ts)

	// Expected shape: production-20260412-123045-<6 hex chars>
	if !strings.HasPrefix(id, "production-20260412-123045-") {
		t.Errorf("id = %q; expected prefix 'production-20260412-123045-'", id)
	}
	suffix := strings.TrimPrefix(id, "production-20260412-123045-")
	if len(suffix) != 6 {
		t.Errorf("suffix len = %d; want 6 hex chars", len(suffix))
	}
}

func TestNewSnapshotID_UniquenessAtSameTime(t *testing.T) {
	// Two IDs generated "at the same instant" should differ thanks to
	// the random suffix. Lock this in — timestamp-only IDs would collide.
	ts := time.Now()
	a := NewSnapshotID("x", ts)
	b := NewSnapshotID("x", ts)
	if a == b {
		t.Errorf("generated identical IDs for the same env+time: %q", a)
	}
}
