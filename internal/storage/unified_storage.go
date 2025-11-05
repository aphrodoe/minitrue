package storage

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/minitrue/internal/models"
)

// Storage interface defines two methods that storage will implement.
// PersistPrimary -> called by primary ingestion node
// PersistReplica -> called by replica ingestion nodes
type Storage interface {
	PersistPrimary(p interface{}) error
	PersistReplica(p interface{}) error
	Query(deviceID, metric string, start, end int64) ([]float64, error)
}

// UnifiedStorage implements Storage using the Gorilla-compressed storage engine
type UnifiedStorage struct {
	mu        sync.RWMutex
	data      map[string][]sample
	file      *os.File
	engine    *StorageEngine
	filepath  string
	batchSize int
	batch     []models.Record
}

type sample struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
	Role      string  `json:"role"` // "primary" or "replica"
}

// NewUnifiedStorage creates a new unified storage instance
func NewUnifiedStorage(filepath string) *UnifiedStorage {
	f, err := os.OpenFile("storage_log_replication.jsonl", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		// If file open fails, continue without logging
		f = nil
	}
	return &UnifiedStorage{
		data:      make(map[string][]sample),
		file:      f,
		engine:    NewStorageEngine(filepath),
		filepath:  filepath,
		batchSize: 1000,
		batch:     make([]models.Record, 0, 1000),
	}
}

// PersistPrimary stores a datapoint as primary
func (m *UnifiedStorage) PersistPrimary(p interface{}) error {
	return m.persist(p, "primary")
}

// PersistReplica stores a datapoint as replica
func (m *UnifiedStorage) PersistReplica(p interface{}) error {
	return m.persist(p, "replica")
}

func (m *UnifiedStorage) persist(p interface{}, role string) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}

	// Write raw JSON line (for replication log)
	if m.file != nil {
		m.file.Write(append(b, byte('\n')))
	}

	// Parse JSON to extract fields
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
	m.data[key] = append(m.data[key], sample{Timestamp: ts, Value: val, Role: role})
	
	// Add to batch for compressed storage
	if role == "primary" {
		m.batch = append(m.batch, models.Record{
			Timestamp: ts,
			Value:     val,
		})
		
		// Flush batch when it reaches batchSize
		if len(m.batch) >= m.batchSize {
			go m.flushBatch()
		}
	}
	m.mu.Unlock()
	return nil
}

func (m *UnifiedStorage) flushBatch() {
	m.mu.Lock()
	if len(m.batch) == 0 {
		m.mu.Unlock()
		return
	}
	
	batch := make([]models.Record, len(m.batch))
	copy(batch, m.batch)
	m.batch = m.batch[:0]
	m.mu.Unlock()
	
	// Read existing records
	existing, err := m.engine.Read()
	if err != nil {
		// If file doesn't exist or read fails, just write the batch
		existing = []models.Record{}
	}
	
	// Append batch to existing
	allRecords := append(existing, batch...)
	
	// Write back
	if err := m.engine.Write(allRecords); err != nil {
		// Log error but don't fail the request
		_ = err
	}
}

func (m *UnifiedStorage) Query(deviceID, metric string, start, end int64) ([]float64, error) {
	key := deviceID + "|" + metric
	m.mu.RLock()
	defer m.mu.RUnlock()
	arr, ok := m.data[key]
	if !ok || len(arr) == 0 {
		return nil, nil
	}
	res := make([]float64, 0, len(arr))
	for _, s := range arr {
		if (start == 0 && end == 0) || (s.Timestamp >= start && (end == 0 || s.Timestamp <= end)) {
			res = append(res, s.Value)
		}
	}
	return res, nil
}

// Close closes the storage file
func (m *UnifiedStorage) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Flush any remaining batch
	if len(m.batch) > 0 {
		m.flushBatch()
	}
	
	if m.file != nil {
		return m.file.Close()
	}
	return nil
}

