package storage

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/minitrue/internal/models"
)

func TestUnifiedStorageWritesImmutableSegmentPerFlushAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node-a.parq")
	store := NewUnifiedStorage(path)

	const (
		deviceID   = "sensor-0001"
		metricName = "temperature"
		seriesKey  = deviceID + "|" + metricName
	)

	var expectedSum float64
	for i := 0; i < 30; i++ {
		value := float64(20 + i)
		expectedSum += value
		if err := store.PersistPrimary(models.Record{
			Timestamp:  int64(1000 + i),
			Value:      value,
			DeviceID:   deviceID,
			MetricName: metricName,
		}); err != nil {
			t.Fatalf("PersistPrimary(%d) failed: %v", i, err)
		}
	}

	files, err := store.segmentFilesForSeries(seriesKey)
	if err != nil {
		t.Fatalf("failed to list segment files: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 immutable segment files, got %d: %v", len(files), files)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected legacy whole-node file %s not to be rewritten, stat err=%v", path, err)
	}

	stats, err := store.QueryAggregated(deviceID, metricName, 0, 0)
	if err != nil {
		t.Fatalf("QueryAggregated before reload failed: %v", err)
	}
	assertStats(t, stats, QueryStats{Sum: expectedSum, Count: 30, Min: 20, Max: 49})

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	reloaded := NewUnifiedStorage(path)
	defer reloaded.Close()

	reloadedFiles, err := reloaded.segmentFilesForSeries(seriesKey)
	if err != nil {
		t.Fatalf("failed to list reloaded segment files: %v", err)
	}
	if len(reloadedFiles) != 3 {
		t.Fatalf("expected 3 segment files after reload, got %d: %v", len(reloadedFiles), reloadedFiles)
	}

	reloadedStats, err := reloaded.QueryAggregated(deviceID, metricName, 0, 0)
	if err != nil {
		t.Fatalf("QueryAggregated after reload failed: %v", err)
	}
	assertStats(t, reloadedStats, QueryStats{Sum: expectedSum, Count: 30, Min: 20, Max: 49})
}

func TestUnifiedStorageCompactsSegmentsAndPreservesReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node-a.parq")
	store := NewUnifiedStorage(path)
	store.compactionSegmentThreshold = 3

	const (
		deviceID   = "sensor-0002"
		metricName = "humidity"
		seriesKey  = deviceID + "|" + metricName
	)

	var expectedSum float64
	for i := 0; i < 40; i++ {
		value := float64(i + 1)
		expectedSum += value
		if err := store.PersistPrimary(models.Record{
			Timestamp:  int64(2000 + i),
			Value:      value,
			DeviceID:   deviceID,
			MetricName: metricName,
		}); err != nil {
			t.Fatalf("PersistPrimary(%d) failed: %v", i, err)
		}
	}

	filesBefore, err := store.segmentFilesForSeries(seriesKey)
	if err != nil {
		t.Fatalf("failed to list segment files before compaction: %v", err)
	}
	if len(filesBefore) != 4 {
		t.Fatalf("expected 4 segments before compaction, got %d: %v", len(filesBefore), filesBefore)
	}

	wantStats := QueryStats{Sum: expectedSum, Count: 40, Min: 1, Max: 40}
	statsBefore, err := store.QueryAggregated(deviceID, metricName, 0, 0)
	if err != nil {
		t.Fatalf("QueryAggregated before compaction failed: %v", err)
	}
	assertStats(t, statsBefore, wantStats)

	queryDone := make(chan error, 1)
	go func() {
		for i := 0; i < 100; i++ {
			stats, err := store.QueryAggregated(deviceID, metricName, 0, 0)
			if err != nil {
				queryDone <- err
				return
			}
			if math.Abs(stats.Sum-wantStats.Sum) > 0.000001 || stats.Count != wantStats.Count {
				queryDone <- errUnexpectedStats(stats, wantStats)
				return
			}
		}
		queryDone <- nil
	}()

	if err := store.compactSegments(); err != nil {
		t.Fatalf("compactSegments failed: %v", err)
	}
	if err := <-queryDone; err != nil {
		t.Fatalf("concurrent query failed during compaction: %v", err)
	}

	filesAfter, err := store.segmentFilesForSeries(seriesKey)
	if err != nil {
		t.Fatalf("failed to list segment files after compaction: %v", err)
	}
	if len(filesAfter) != 1 {
		t.Fatalf("expected 1 compacted segment after compaction, got %d: %v", len(filesAfter), filesAfter)
	}

	statsAfter, err := store.QueryAggregated(deviceID, metricName, 0, 0)
	if err != nil {
		t.Fatalf("QueryAggregated after compaction failed: %v", err)
	}
	assertStats(t, statsAfter, wantStats)

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	reloaded := NewUnifiedStorage(path)
	defer reloaded.Close()

	reloadedStats, err := reloaded.QueryAggregated(deviceID, metricName, 0, 0)
	if err != nil {
		t.Fatalf("QueryAggregated after reload failed: %v", err)
	}
	assertStats(t, reloadedStats, wantStats)
}

func TestUnifiedStorageDeleteRemovesSeriesSegments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node-a.parq")
	store := NewUnifiedStorage(path)

	const (
		deviceID   = "sensor-0003"
		metricName = "pressure"
		seriesKey  = deviceID + "|" + metricName
	)

	for i := 0; i < 20; i++ {
		if err := store.PersistPrimary(models.Record{
			Timestamp:  int64(3000 + i),
			Value:      float64(i),
			DeviceID:   deviceID,
			MetricName: metricName,
		}); err != nil {
			t.Fatalf("PersistPrimary(%d) failed: %v", i, err)
		}
	}

	filesBefore, err := store.segmentFilesForSeries(seriesKey)
	if err != nil {
		t.Fatalf("failed to list segment files before delete: %v", err)
	}
	if len(filesBefore) != 2 {
		t.Fatalf("expected 2 segment files before delete, got %d: %v", len(filesBefore), filesBefore)
	}

	if err := store.Delete(deviceID, metricName); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	filesAfter, err := store.segmentFilesForSeries(seriesKey)
	if err != nil {
		t.Fatalf("failed to list segment files after delete: %v", err)
	}
	if len(filesAfter) != 0 {
		t.Fatalf("expected no segment files after delete, got %d: %v", len(filesAfter), filesAfter)
	}

	stats, err := store.QueryAggregated(deviceID, metricName, 0, 0)
	if err != nil {
		t.Fatalf("QueryAggregated after delete failed: %v", err)
	}
	assertStats(t, stats, QueryStats{})

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	reloaded := NewUnifiedStorage(path)
	defer reloaded.Close()

	reloadedStats, err := reloaded.QueryAggregated(deviceID, metricName, 0, 0)
	if err != nil {
		t.Fatalf("QueryAggregated after reload failed: %v", err)
	}
	assertStats(t, reloadedStats, QueryStats{})
}

func errUnexpectedStats(got, want QueryStats) error {
	return fmt.Errorf("unexpected stats: got %+v want %+v", got, want)
}

func assertStats(t *testing.T, got, want QueryStats) {
	t.Helper()

	if math.Abs(got.Sum-want.Sum) > 0.000001 || got.Count != want.Count || got.Min != want.Min || got.Max != want.Max {
		t.Fatalf("unexpected stats: got %+v want %+v", got, want)
	}
}
