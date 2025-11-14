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
		batchSize: 10, 
		batch:     make([]models.Record, 0, 10),
		nodeID:    nodeID,
		lastFlush: time.Now(),
	}

	if err := storage.Reload(); err != nil {
		log.Printf("[Storage-%s] Warning: Failed to reload data from disk: %v", nodeID, err)
	}

	go storage.periodicFlush()

	return storage
}

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

	insertPos := sort.Search(len(arr), func(i int) bool {
		return arr[i].Timestamp >= ts
	})

	if insertPos == len(arr) {
		m.data[key] = append(arr, newSample)
	} else {
		arr = append(arr, sample{})             
		copy(arr[insertPos+1:], arr[insertPos:]) 
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

	m.mu.Unlock()

	sort.Slice(batch, func(i, j int) bool {
		return batch[i].Timestamp < batch[j].Timestamp
	})

	existing, err := m.engine.Read()
	if err != nil {
		log.Printf("[Storage-%s] No existing data, starting fresh", m.nodeID)
		existing = []models.Record{}
	}
	allRecords := append(existing, batch...)
	sort.Slice(allRecords, func(i, j int) bool {
		return allRecords[i].Timestamp < allRecords[j].Timestamp
	})

	if err := m.engine.Write(allRecords); err != nil {
		log.Printf("[Storage-%s] ERROR writing to disk: %v", m.nodeID, err)
	} else {
		log.Printf("[Storage-%s] Successfully wrote %d records to %s", m.nodeID, len(allRecords), m.filepath)
	}

	m.mu.Lock()
}

func binarySearchStart(arr []sample, start int64) int {
	return sort.Search(len(arr), func(i int) bool {
		return arr[i].Timestamp >= start
	})
}

func binarySearchEnd(arr []sample, end int64) int {
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

	if !ok || len(arr) == 0 {
		log.Printf("[Storage-%s] Query for %s returned 0 points (key not found or empty)", m.nodeID, key)
		return []float64{}, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var startIdx, endIdx int

	if start == 0 && end == 0 {
		startIdx = 0
		endIdx = len(arr) - 1
	} else if start == 0 {
		startIdx = 0
		endIdx = len(arr) - 1
	} else {
		startIdx = binarySearchStart(arr, start)
		if startIdx >= len(arr) {
			return []float64{}, nil
		}

		if end == 0 {
			endIdx = len(arr) - 1
		} else {
			endIdx = binarySearchEnd(arr, end)
			if endIdx < startIdx {
				return []float64{}, nil
			}
		}
	}

	resultCount := endIdx - startIdx + 1
	res := make([]float64, 0, resultCount)

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

	if !ok || len(arr) == 0 {
		return QueryStats{}, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var startIdx, endIdx int

	if start == 0 && end == 0 {
		startIdx = 0
		endIdx = len(arr) - 1
	} else {
		startIdx = binarySearchStart(arr, start)
		if startIdx >= len(arr) {
			return QueryStats{}, nil
		}

		if end == 0 {
			endIdx = len(arr) - 1
		} else {
			endIdx = binarySearchEnd(arr, end)
			if endIdx < startIdx {
				return QueryStats{}, nil
			}
		}
	}

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

func (m *UnifiedStorage) Delete(deviceID, metric string) error {
	key := deviceID + "|" + metric

	m.mu.Lock()

	delete(m.data, key)

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

	m.mu.Unlock()

	existing, err := m.engine.Read()
	if err != nil {
		log.Printf("[Storage-%s] Could not read disk file for deletion: %v", m.nodeID, err)
	} else {
		filteredRecords := make([]models.Record, 0, len(existing))
		for _, record := range existing {
			if record.DeviceID == "" && record.MetricName == "" {
				filteredRecords = append(filteredRecords, record)
			} else if record.DeviceID != deviceID || record.MetricName != metric {
				filteredRecords = append(filteredRecords, record)
			}
		}

		if len(filteredRecords) == 0 {
			if err := os.Remove(m.filepath); err != nil && !os.IsNotExist(err) {
				log.Printf("[Storage-%s] Error removing empty file: %v", m.nodeID, err)
			} else {
				log.Printf("[Storage-%s] Removed empty disk file after deletion", m.nodeID)
			}
		} else {
			if err := m.engine.Write(filteredRecords); err != nil {
				log.Printf("[Storage-%s] Error writing filtered records to disk: %v", m.nodeID, err)
			} else {
				log.Printf("[Storage-%s] Wrote %d filtered records to disk (removed %d)", m.nodeID, len(filteredRecords), len(existing)-len(filteredRecords))
			}
		}
	}

	m.mu.Lock()

	log.Printf("[Storage-%s] Deleted all data for device=%s metric=%s (memory and disk)", m.nodeID, deviceID, metric)

	return nil
}

func (m *UnifiedStorage) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data = make(map[string][]sample)

	records, err := m.engine.Read()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("[Storage-%s] No existing data file, starting fresh", m.nodeID)
			return nil
		}
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

	for _, record := range records {
		key := record.DeviceID + "|" + record.MetricName
		newSample := sample{
			Timestamp: record.Timestamp,
			Value:     record.Value,
			Role:      "primary", 
		}

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
	}

	m.mu.Unlock()

	if m.file != nil {
		return m.file.Close()
	}
	return nil
}
