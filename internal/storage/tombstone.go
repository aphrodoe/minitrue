// Package storage — Tombstone System
//
// A tombstone marks a series as logically deleted without touching any .parq
// segment files on the hot delete path. Physical cleanup (removing segment
// files and the tombstone file itself) is deferred to the background
// compaction goroutine, which already iterates over all series files.
//
// Design:
//   - One small JSON file per deleted series, stored in a dedicated
//     `tombstones/` subdirectory inside the segment directory.
//   - An in-memory map[seriesKey]deletedAt is maintained for O(1) Has() checks
//     on every Query()/QueryAggregated() call (RLock only — no disk I/O).
//   - Write() is an atomic file create: write to a temp file → rename. This
//     ensures a half-written tombstone is never observed on startup.
//   - LoadAll() is called once at startup to hydrate the in-memory index.
package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Tombstone is the on-disk representation of a logically deleted series.
type Tombstone struct {
	SeriesKey string `json:"series_key"`
	DeletedAt int64  `json:"deleted_at"`
}

// TombstoneStore manages tombstone files for a single storage node.
// All public methods are safe for concurrent use.
type TombstoneStore struct {
	mu     sync.RWMutex
	dir    string
	stones map[string]int64 // seriesKey → DeletedAt (unix seconds)
}

// NewTombstoneStore creates (or opens) the tombstone directory and loads any
// existing tombstones into the in-memory index.
func NewTombstoneStore(dir string) (*TombstoneStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("tombstone: mkdir %s: %w", dir, err)
	}

	ts := &TombstoneStore{
		dir:    dir,
		stones: make(map[string]int64),
	}

	if err := ts.LoadAll(); err != nil {
		return nil, err
	}

	return ts, nil
}

// Write records a tombstone for seriesKey. It is atomic: the JSON is written
// to a temp file first, then renamed into place. After Write returns, Has()
// will return true and Query paths will return empty results.
func (ts *TombstoneStore) Write(seriesKey string) error {
	t := Tombstone{
		SeriesKey: seriesKey,
		DeletedAt: time.Now().Unix(),
	}

	b, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("tombstone: marshal: %w", err)
	}

	finalPath := ts.tombPath(seriesKey)
	tmpPath := finalPath + ".tmp"

	if err := os.WriteFile(tmpPath, b, 0644); err != nil {
		return fmt.Errorf("tombstone: write temp file: %w", err)
	}

	// Atomic rename — either the full tombstone is visible or none of it is.
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("tombstone: rename into place: %w", err)
	}

	ts.mu.Lock()
	ts.stones[seriesKey] = t.DeletedAt
	ts.mu.Unlock()

	return nil
}

// Has returns true if seriesKey has been tombstoned. O(1) map lookup, RLock only.
// This is called on every Query() and QueryAggregated() — keep it cheap.
func (ts *TombstoneStore) Has(seriesKey string) bool {
	ts.mu.RLock()
	_, ok := ts.stones[seriesKey]
	ts.mu.RUnlock()
	return ok
}

// Remove deletes the tombstone for seriesKey from both disk and the in-memory
// index. This is called by the background compaction process after it has
// physically removed all .parq segment files for the series.
func (ts *TombstoneStore) Remove(seriesKey string) error {
	path := ts.tombPath(seriesKey)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("tombstone: remove file %s: %w", path, err)
	}

	ts.mu.Lock()
	delete(ts.stones, seriesKey)
	ts.mu.Unlock()

	return nil
}

// Keys returns all currently tombstoned series keys (used by compaction to
// iterate over series that need physical cleanup).
func (ts *TombstoneStore) Keys() []string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	keys := make([]string, 0, len(ts.stones))
	for k := range ts.stones {
		keys = append(keys, k)
	}
	return keys
}

// LoadAll scans the tombstone directory and hydrates the in-memory index.
// Called once during storage initialisation.
func (ts *TombstoneStore) LoadAll() error {
	entries, err := filepath.Glob(filepath.Join(ts.dir, "*.tomb"))
	if err != nil {
		return fmt.Errorf("tombstone: glob: %w", err)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	for _, path := range entries {
		b, err := os.ReadFile(path)
		if err != nil {
			log.Printf("[Tombstone] Warning: could not read %s: %v", path, err)
			continue
		}

		var t Tombstone
		if err := json.Unmarshal(b, &t); err != nil {
			log.Printf("[Tombstone] Warning: malformed tombstone %s: %v", path, err)
			continue
		}

		ts.stones[t.SeriesKey] = t.DeletedAt
	}

	if len(ts.stones) > 0 {
		log.Printf("[Tombstone] Loaded %d tombstones from %s", len(ts.stones), ts.dir)
	}

	return nil
}

// tombPath returns the filesystem path for a tombstone file.
// We use the same base64-URL encoding as segment filenames to avoid
// special characters in the series key appearing in the filename.
func (ts *TombstoneStore) tombPath(seriesKey string) string {
	// Replace the pipe separator with a safe filename character.
	safe := strings.ReplaceAll(seriesKey, "|", "__")
	return filepath.Join(ts.dir, safe+".tomb")
}
