package storage

import (
	"github.com/minitrue/internal/models"
	"github.com/spaolacci/murmur3"
)

func (m *UnifiedStorage) GetSeriesDigest(deviceID, metric string) (uint32, int, error) {
	key := deviceID + ":" + metric

	// Force a flush so we don't have pending batches that aren't in segments.
	m.mu.Lock()
	m.flushBatchLocked()
	m.mu.Unlock()

	segmentFiles, err := m.segmentFilesForSeries(key)
	if err != nil {
		return 0, 0, err
	}

	checksum := murmur3.New32()
	totalRecords := 0

	for _, file := range segmentFiles {
		segHash, segCount, err := GetSegmentDigest(file)
		if err != nil {
			continue // ignore corrupt or unreadable segments for digest
		}
		// Write the segment checksum and count to our aggregate hash
		buf := make([]byte, 8)
		buf[0] = byte(segHash >> 24)
		buf[1] = byte(segHash >> 16)
		buf[2] = byte(segHash >> 8)
		buf[3] = byte(segHash)
		buf[4] = byte(segCount >> 24)
		buf[5] = byte(segCount >> 16)
		buf[6] = byte(segCount >> 8)
		buf[7] = byte(segCount)
		checksum.Write(buf)
		totalRecords += segCount
	}

	return checksum.Sum32(), totalRecords, nil
}

func (m *UnifiedStorage) GetSeriesRecords(deviceID, metric string) ([]models.Record, error) {
	key := deviceID + ":" + metric

	m.mu.Lock()
	m.flushBatchLocked()
	m.mu.Unlock()

	segmentFiles, err := m.segmentFilesForSeries(key)
	if err != nil {
		return nil, err
	}

	var allRecords []models.Record
	for _, file := range segmentFiles {
		records, err := NewStorageEngine(file).Read()
		if err != nil {
			continue
		}
		allRecords = append(allRecords, records...)
	}

	return allRecords, nil
}

func (m *UnifiedStorage) GetOwnedSeriesKeys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys
}
