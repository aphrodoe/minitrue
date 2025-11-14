package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/minitrue/internal/models"
)

type Storage interface {
	PersistPrimary(p interface{}) error
	PersistReplica(p interface{}) error
	Query(deviceID, metric string, start, end int64) ([]float64, error)
	QueryAggregated(deviceID, metric string, start, end int64) (QueryStats, error)
	Delete(deviceID, metric string) error
	Reload() error
}

type QueryStats struct {
	Sum   float64
	Count int
	Min   float64
	Max   float64
}

type UnifiedStorage struct {
	mu        sync.RWMutex
	data      map[string][]sample
	file      *os.File
	engine    *StorageEngine
	filepath  string
	batchSize int
	batch     []models.Record
	nodeID    string
	lastFlush time.Time
}

type sample struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
	Role      string  `json:"role"`
}

func NewUnifiedStorage(filepath string) *UnifiedStorage {
	nodeID := "unknown"
	if len(filepath) > 5 {
		nodeID = filepath[len(filepath)-9 : len(filepath)-5]
	}

	storage := &UnifiedStorage{
		data:      make(map[string][]sample),
		file:      nil,
		engine:    NewStorageEngine(filepath),
		filepath:  filepath,
		batchSize: 10, // Reduced from 1000 for faster testing
		batch:     make([]models.Record, 0, 10),
		nodeID:    nodeID,
		lastFlush: time.Now(),
	}

	// Load existing data from disk on startup
	if err := storage.Reload(); err != nil {
		log.Printf("[Storage-%s] Warning: Failed to reload data from disk: %v", nodeID, err)
	}

	// Start periodic flush goroutine
	go storage.periodicFlush()

	return storage
}

// periodicFlush flushes data every 5 seconds regardless of batch size
func (m *UnifiedStorage) periodicFlush() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		if len(m.batch) > 0 {
			log.Printf("[Storage-%s] Periodic flush: %d records", m.nodeID, len(m.batch))
			m.flushBatchUnlocked()
		}
		m.mu.Unlock()
	}
}

func (m *UnifiedStorage) PersistPrimary(p interface{}) error {
	return m.persist(p, "primary")
}

func (m *UnifiedStorage) PersistReplica(p interface{}) error {
	return m.persist(p, "replica")
}

func (m *UnifiedStorage) persist(p interface{}, role string) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}

	var mp map[string]interface{}
	if err := json.Unmarshal(b, &mp); err != nil {
		return err
	}
	device, _ := mp["device_id"].(string)
	metric, _ := mp["metric_name"].(string)

	ts := int64(0)
	if v, ok := mp["timestamp"].(float64); ok {
		ts = int64(v)
	} else if v, ok := mp["timestamp"].(int64); ok {
		ts = v
	} else {
		ts = time.Now().Unix()
	}

	val := 0.0
	if v, ok := mp["value"].(float64); ok {
		val = v
	}

	key := device + "|" + metric

	m.mu.Lock()
	newSample := sample{Timestamp: ts, Value: val, Role: role}
	arr := m.data[key]

	// Insert in sorted order using binary search for optimal performance
	insertPos := sort.Search(len(arr), func(i int) bool {
		return arr[i].Timestamp >= ts
	})

	// Insert at the correct position to maintain sorted order
	if insertPos == len(arr) {
		// Append at the end
		m.data[key] = append(arr, newSample)
	} else {
		// Insert at position
		arr = append(arr, sample{})              // Extend slice
		copy(arr[insertPos+1:], arr[insertPos:]) // Shift elements
		arr[insertPos] = newSample
		m.data[key] = arr
	}

	if role == "primary" {
		m.batch = append(m.batch, models.Record{
			Timestamp:  ts,
			Value:      val,
			DeviceID:   device,
			MetricName: metric,
		})
		if len(m.batch) >= m.batchSize {
			log.Printf("[Storage-%s] Batch full: flushing %d records", m.nodeID, len(m.batch))
			m.flushBatchUnlocked()
		}
	}
	m.mu.Unlock()
	return nil
}

func (m *UnifiedStorage) flushBatchUnlocked() {
	if len(m.batch) == 0 {
		return
	}

	batch := make([]models.Record, len(m.batch))
	copy(batch, m.batch)
	m.batch = m.batch[:0]
	m.lastFlush = time.Now()

	// Release lock before doing I/O
	m.mu.Unlock()

	// Sort batch by timestamp to ensure optimal Gorilla compression
	sort.Slice(batch, func(i, j int) bool {
		return batch[i].Timestamp < batch[j].Timestamp
	})

	existing, err := m.engine.Read()
	if err != nil {
		log.Printf("[Storage-%s] No existing data, starting fresh", m.nodeID)
		existing = []models.Record{}
	}

	// Merge existing and batch data, maintaining sorted order for optimal compression
	allRecords := append(existing, batch...)
	sort.Slice(allRecords, func(i, j int) bool {
		return allRecords[i].Timestamp < allRecords[j].Timestamp
	})

	if err := m.engine.Write(allRecords); err != nil {
		log.Printf("[Storage-%s] ERROR writing to disk: %v", m.nodeID, err)
	} else {
		log.Printf("[Storage-%s] Successfully wrote %d records to %s", m.nodeID, len(allRecords), m.filepath)
	}

	// Re-acquire lock
	m.mu.Lock()
}

// func (m *UnifiedStorage) flushBatch() {
// 	m.mu.Lock()
// 	defer m.mu.Unlock()
// 	m.flushBatchUnlocked()
// }

// binarySearchStart finds the first index where timestamp >= start
func binarySearchStart(arr []sample, start int64) int {
	return sort.Search(len(arr), func(i int) bool {
		return arr[i].Timestamp >= start
	})
}

// binarySearchEnd finds the last index where timestamp <= end
func binarySearchEnd(arr []sample, end int64) int {
	// Find the first index where timestamp > end, then subtract 1
	idx := sort.Search(len(arr), func(i int) bool {
		return arr[i].Timestamp > end
	})
	if idx > 0 {
		return idx - 1
	}
	return -1
}

func (m *UnifiedStorage) Query(deviceID, metric string, start, end int64) ([]float64, error) {
	key := deviceID + "|" + metric
	m.mu.RLock()
	arr, ok := m.data[key]
	m.mu.RUnlock()

	// If not in memory, return empty
	if !ok || len(arr) == 0 {
		log.Printf("[Storage-%s] Query for %s returned 0 points (key not found or empty)", m.nodeID, key)
		return []float64{}, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Optimized query using binary search for time range
	var startIdx, endIdx int

	if start == 0 && end == 0 {
		// Return all data
		startIdx = 0
		endIdx = len(arr) - 1
	} else if start == 0 {
		// Start from beginning (All Data case: start=0, end=now)
		// When start=0, return ALL data regardless of end time
		// This handles the "All Data" button which sets start=0, end=now
		startIdx = 0
		endIdx = len(arr) - 1
	} else {
		// Find range boundaries using binary search
		startIdx = binarySearchStart(arr, start)
		if startIdx >= len(arr) {
			// No data in range
			return []float64{}, nil
		}

		if end == 0 {
			// No end time specified, return everything from start to end
			endIdx = len(arr) - 1
		} else {
			endIdx = binarySearchEnd(arr, end)
			if endIdx < startIdx {
				// No data in range
				return []float64{}, nil
			}
		}
	}

	// Pre-allocate result slice with exact size for better performance
	resultCount := endIdx - startIdx + 1
	res := make([]float64, 0, resultCount)

	// Iterate only through the range found by binary search
	for i := startIdx; i <= endIdx; i++ {
		res = append(res, arr[i].Value)
	}

	return res, nil
}

func (m *UnifiedStorage) QueryAggregated(deviceID, metric string, start, end int64) (QueryStats, error) {
	key := deviceID + "|" + metric
	m.mu.RLock()
	arr, ok := m.data[key]
	m.mu.RUnlock()

	// If not in memory, return empty
	if !ok || len(arr) == 0 {
		return QueryStats{}, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Optimized query using binary search for time range
	var startIdx, endIdx int

	if start == 0 && end == 0 {
		// Return all data
		startIdx = 0
		endIdx = len(arr) - 1
	} else {
		// Find range boundaries using binary search
		startIdx = binarySearchStart(arr, start)
		if startIdx >= len(arr) {
			// No data in range
			return QueryStats{}, nil
		}

		if end == 0 {
			// No end time specified, return everything from start to end
			endIdx = len(arr) - 1
		} else {
			endIdx = binarySearchEnd(arr, end)
			if endIdx < startIdx {
				// No data in range
				return QueryStats{}, nil
			}
		}
	}

	// Aggregate on the fly
	var sum float64
	var count int
	var min, max float64
	if endIdx >= startIdx {
		min = arr[startIdx].Value
		max = arr[startIdx].Value
		sum = arr[startIdx].Value
		count = 1
		for i := startIdx + 1; i <= endIdx; i++ {
			val := arr[i].Value
			sum += val
			count++
			if val < min {
				min = val
			}
			if val > max {
				max = val
			}
		}
	}

	return QueryStats{Sum: sum, Count: count, Min: min, Max: max}, nil
}

// Delete removes all data for a specific device_id and metric_name
func (m *UnifiedStorage) Delete(deviceID, metric string) error {
	key := deviceID + "|" + metric

	m.mu.Lock()

	// Remove from memory
	delete(m.data, key)

	// Filter batch to remove records with matching device_id and metric_name
	if len(m.batch) > 0 {
		originalBatchSize := len(m.batch)
		filteredBatch := make([]models.Record, 0, len(m.batch))
		for _, record := range m.batch {
			if record.DeviceID != deviceID || record.MetricName != metric {
				filteredBatch = append(filteredBatch, record)
			}
		}
		m.batch = filteredBatch
		removedCount := originalBatchSize - len(filteredBatch)
		if removedCount > 0 {
			log.Printf("[Storage-%s] Filtered batch: removed %d records for deleted device/metric", m.nodeID, removedCount)
		}
	}

	// Release lock before I/O operations
	m.mu.Unlock()

	// Delete from disk by reading, filtering, and rewriting
	existing, err := m.engine.Read()
	if err != nil {
		// If file doesn't exist or can't be read, that's okay - deletion from memory is done
		log.Printf("[Storage-%s] Could not read disk file for deletion: %v", m.nodeID, err)
	} else {
		// Filter out records with matching device_id and metric_name
		filteredRecords := make([]models.Record, 0, len(existing))
		for _, record := range existing {
			// For version 1 files (empty DeviceID/MetricName), skip filtering
			// Only filter if we have device/metric info
			if record.DeviceID == "" && record.MetricName == "" {
				// Version 1 file - keep all records (can't filter without device/metric info)
				filteredRecords = append(filteredRecords, record)
			} else if record.DeviceID != deviceID || record.MetricName != metric {
				// Version 2 file - filter by device/metric
				filteredRecords = append(filteredRecords, record)
			}
		}

		// Write filtered records back to disk
		if len(filteredRecords) == 0 {
			// If no records left, delete the file
			if err := os.Remove(m.filepath); err != nil && !os.IsNotExist(err) {
				log.Printf("[Storage-%s] Error removing empty file: %v", m.nodeID, err)
			} else {
				log.Printf("[Storage-%s] Removed empty disk file after deletion", m.nodeID)
			}
		} else {
			// Write filtered records back
			if err := m.engine.Write(filteredRecords); err != nil {
				log.Printf("[Storage-%s] Error writing filtered records to disk: %v", m.nodeID, err)
			} else {
				log.Printf("[Storage-%s] Wrote %d filtered records to disk (removed %d)", m.nodeID, len(filteredRecords), len(existing)-len(filteredRecords))
			}
		}
	}

	// Re-acquire lock
	m.mu.Lock()

	log.Printf("[Storage-%s] Deleted all data for device=%s metric=%s (memory and disk)", m.nodeID, deviceID, metric)

	return nil
}

// Reload reloads all data from disk, replacing in-memory data
func (m *UnifiedStorage) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear existing data
	m.data = make(map[string][]sample)

	// Read all records from disk
	records, err := m.engine.Read()
	if err != nil {
		// File might not exist yet, which is okay
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("[Storage-%s] No existing data file, starting fresh", m.nodeID)
			return nil
		}
		// Check if the underlying error is file not found (wrapped error)
		var pathErr *os.PathError
		if errors.As(err, &pathErr) && errors.Is(pathErr.Err, os.ErrNotExist) {
			log.Printf("[Storage-%s] No existing data file, starting fresh", m.nodeID)
			return nil
		}
		return fmt.Errorf("failed to read from disk: %w", err)
	}

	if len(records) == 0 {
		log.Printf("[Storage-%s] No records found in file", m.nodeID)
		return nil
	}

	// Group records by device_id|metric_name and convert to samples
	for _, record := range records {
		key := record.DeviceID + "|" + record.MetricName
		newSample := sample{
			Timestamp: record.Timestamp,
			Value:     record.Value,
			Role:      "primary", // All data from disk is treated as primary
		}

		// Insert in sorted order
		arr := m.data[key]
		insertPos := sort.Search(len(arr), func(i int) bool {
			return arr[i].Timestamp >= record.Timestamp
		})

		if insertPos == len(arr) {
			m.data[key] = append(arr, newSample)
		} else {
			arr = append(arr, sample{})
			copy(arr[insertPos+1:], arr[insertPos:])
			arr[insertPos] = newSample
			m.data[key] = arr
		}
	}

	log.Printf("[Storage-%s] Reloaded %d records from disk into %d keys", m.nodeID, len(records), len(m.data))
	return nil
}

func (m *UnifiedStorage) Close() error {
	m.mu.Lock()

	log.Printf("[Storage-%s] Closing storage, flushing remaining %d records", m.nodeID, len(m.batch))

	if len(m.batch) > 0 {
		m.flushBatchUnlocked()
		// flushBatchUnlocked unlocks and re-locks, so lock is still held here
	}

	m.mu.Unlock()

	if m.file != nil {
		return m.file.Close()
	}
	return nil
}
