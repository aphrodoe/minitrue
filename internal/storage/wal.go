// Package storage — Write-Ahead Log (WAL)
//
// The WAL guarantees that every primary write that returns 202 Accepted to the
// router survives a process crash. The design:
//
//   - Append-only, newline-delimited JSON (one WALEntry per line).
//   - Every Append() calls file.Sync() before returning — the entry is on
//     stable storage before in-memory state is updated.
//   - After a successful batch flush to .parq segment files, Truncate() resets
//     the WAL file to zero bytes (a single O(1) syscall).
//   - On startup, Reload() replays all WAL entries. Because chunk Insert() is
//     idempotent on duplicate timestamps, replaying already-flushed entries is
//     safe — they are silently discarded.
package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// WALEntry is a single record in the Write-Ahead Log.
// Only primary writes are logged; replica data is recovered via read-repair.
type WALEntry struct {
	DeviceID   string  `json:"device_id"`
	MetricName string  `json:"metric_name"`
	Timestamp  int64   `json:"timestamp"`
	Value      float64 `json:"value"`
	Role       string  `json:"role"`
}

// WAL is a concurrency-safe append-only log file.
type WAL struct {
	mu   sync.Mutex
	file *os.File
	path string
}

// NewWAL opens (or creates) the WAL file at path.
// The file is opened with O_APPEND so every Write goes to the end.
func NewWAL(path string) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("wal: open %s: %w", path, err)
	}
	return &WAL{file: f, path: path}, nil
}

// Append serialises e as a JSON line and fsyncs to disk before returning.
// This is the hot path — called on every primary ingest.
func (w *WAL) Append(e WALEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("wal: marshal entry: %w", err)
	}
	b = append(b, '\n')

	if _, err := w.file.Write(b); err != nil {
		return fmt.Errorf("wal: write entry: %w", err)
	}

	// fsync guarantees the entry reaches stable storage before we update
	// in-memory state. Without this, a kernel buffer flush could be lost.
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("wal: sync: %w", err)
	}

	return nil
}

// ReadAll scans the WAL from the beginning and returns all valid entries.
// Malformed or partial lines (e.g. from a mid-write crash) are silently
// skipped — the WAL is designed to be tolerant of a torn final record.
func (w *WAL) ReadAll() ([]WALEntry, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Seek to the beginning for replay.
	if _, err := w.file.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("wal: seek to start: %w", err)
	}

	var entries []WALEntry
	scanner := bufio.NewScanner(w.file)
	// Increase the buffer for very large single-line entries, just in case.
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e WALEntry
		if err := json.Unmarshal(line, &e); err != nil {
			// Tolerate a torn final record from a mid-write crash.
			continue
		}
		entries = append(entries, e)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("wal: scan: %w", err)
	}

	return entries, nil
}

// Truncate resets the WAL to zero bytes after a successful batch flush.
// This is an O(1) operation — one Truncate + one Seek syscall.
func (w *WAL) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.file.Truncate(0); err != nil {
		return fmt.Errorf("wal: truncate: %w", err)
	}
	// Reset the write cursor to the start so subsequent Appends go to offset 0.
	if _, err := w.file.Seek(0, 0); err != nil {
		return fmt.Errorf("wal: seek after truncate: %w", err)
	}
	return nil
}

// Close flushes and closes the underlying file.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}
