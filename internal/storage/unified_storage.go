package storage

import (
	"encoding/json"
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
		arr = append(arr, sample{}) // Extend slice
		copy(arr[insertPos+1:], arr[insertPos:]) // Shift elements
		arr[insertPos] = newSample
		m.data[key] = arr
	}
	
	if role == "primary" {
		m.batch = append(m.batch, models.Record{
			Timestamp: ts,
			Value:     val,
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
	defer m.mu.RUnlock()
	arr, ok := m.data[key]
	if !ok || len(arr) == 0 {
		return nil, nil
	}
	
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

func (m *UnifiedStorage) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	log.Printf("[Storage-%s] Closing storage, flushing remaining %d records", m.nodeID, len(m.batch))
	
	if len(m.batch) > 0 {
		m.flushBatchUnlocked()
	}
	
	if m.file != nil {
		return m.file.Close()
	}
	return nil
}