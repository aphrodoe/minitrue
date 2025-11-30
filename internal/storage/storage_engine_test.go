package storage

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minitrue/internal/models"
)

func testRecords() []models.Record {
	return []models.Record{
		{Timestamp: 100, Value: 21.5, DeviceID: "sensor-0001", MetricName: "temperature"},
		{Timestamp: 101, Value: 22.0, DeviceID: "sensor-0001", MetricName: "temperature"},
		{Timestamp: 102, Value: 40.5, DeviceID: "sensor-0002", MetricName: "humidity"},
	}
}

func TestStorageEngineWritesFormatVersion3AndReadsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "segment.parq")
	engine := NewStorageEngine(path)
	records := testRecords()

	if err := engine.Write(records); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if got := binary.LittleEndian.Uint32(data[4:8]); got != FormatVersion {
		t.Fatalf("expected format version %d, got %d", FormatVersion, got)
	}
	if FormatVersion != 3 {
		t.Fatalf("Step 4a requires format version 3, got %d", FormatVersion)
	}

	got, err := engine.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if len(got) != len(records) {
		t.Fatalf("expected %d records, got %d", len(records), len(got))
	}
	for i := range records {
		if got[i] != records[i] {
			t.Fatalf("record %d mismatch: got %+v want %+v", i, got[i], records[i])
		}
	}
}

func TestStorageEngineDetectsCorruptColumnDataChecksum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "segment.parq")
	engine := NewStorageEngine(path)

	if err := engine.Write(testRecords()); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if len(data) <= HeaderSize+1 {
		t.Fatalf("written file too small to corrupt column data")
	}

	data[HeaderSize+1] ^= 0xff
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to write corrupted file: %v", err)
	}

	_, err = engine.Read()
	if err == nil {
		t.Fatal("expected checksum error, got nil")
	}
	if !errors.Is(err, ErrCorruptSegment) {
		t.Fatalf("expected ErrCorruptSegment, got %v", err)
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected clear checksum mismatch error, got %v", err)
	}
}
