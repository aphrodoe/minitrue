package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
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

type sample struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
	Role      string  `json:"role"`
}

type Chunk struct {
	StartTime int64
	EndTime   int64
	Sum       float64
	Min       float64
	Max       float64
	Count     int
	Samples   []sample
}

func (c *Chunk) Insert(newSample sample) {
	if c.Count == 0 {
		c.StartTime = newSample.Timestamp
		c.EndTime = newSample.Timestamp
		c.Min = newSample.Value
		c.Max = newSample.Value
		c.Sum = newSample.Value
		c.Count = 1
		c.Samples = append(c.Samples, newSample)
		return
	}

	insertPos := sort.Search(len(c.Samples), func(i int) bool {
		return c.Samples[i].Timestamp >= newSample.Timestamp
	})

	if insertPos == len(c.Samples) {
		c.Samples = append(c.Samples, newSample)
	} else {
		c.Samples = append(c.Samples, sample{})
		copy(c.Samples[insertPos+1:], c.Samples[insertPos:])
		c.Samples[insertPos] = newSample
	}

	if newSample.Timestamp < c.StartTime {
		c.StartTime = newSample.Timestamp
	}
	if newSample.Timestamp > c.EndTime {
		c.EndTime = newSample.Timestamp
	}
	if newSample.Value < c.Min {
		c.Min = newSample.Value
	}
	if newSample.Value > c.Max {
		c.Max = newSample.Value
	}
	c.Sum += newSample.Value
	c.Count++
}

type Series struct {
	Chunks []*Chunk
}

func (s *Series) Insert(newSample sample) {
	if len(s.Chunks) == 0 {
		c := &Chunk{}
		c.Insert(newSample)
		s.Chunks = append(s.Chunks, c)
		return
	}

	lastChunk := s.Chunks[len(s.Chunks)-1]

	// Handle massively delayed out-of-order writes
	if newSample.Timestamp < lastChunk.StartTime {
		idx := sort.Search(len(s.Chunks), func(i int) bool {
			return s.Chunks[i].EndTime >= newSample.Timestamp
		})
		if idx < len(s.Chunks) {
			s.Chunks[idx].Insert(newSample)
			return
		}
	}

	// 1000 points per chunk bounds the Edge Binary Search to <1000 items
	if lastChunk.Count >= 1000 {
		c := &Chunk{}
		c.Insert(newSample)
		s.Chunks = append(s.Chunks, c)
	} else {
		lastChunk.Insert(newSample)
	}
}

type UnifiedStorage struct {
	mu        sync.RWMutex
	data      map[string]*Series
	file      *os.File
	engine    *StorageEngine
	filepath  string
	batchSize int
	batch     []models.Record
	nodeID    string
	lastFlush time.Time
}

func NewUnifiedStorage(filepath string) *UnifiedStorage {
	nodeID := "unknown"
	base := filepath
	// Handle both forward and backward path separators
	if lastSlash := len(filepath) - 1; lastSlash >= 0 {
		for i := lastSlash; i >= 0; i-- {
			if filepath[i] == '/' || filepath[i] == '\\' {
				base = filepath[i+1:]
				break
			}
		}
	}
	if dotIdx := 0; dotIdx < len(base) {
		for i := 0; i < len(base); i++ {
			if base[i] == '.' {
				nodeID = base[:i]
				break
			}
		}
		if nodeID == "unknown" {
			nodeID = base
		}
	}

	storage := &UnifiedStorage{
		data:      make(map[string]*Series),
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
	series, ok := m.data[key]
	if !ok {
		series = &Series{}
		m.data[key] = series
	}
	series.Insert(newSample)

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
	series, ok := m.data[key]
	m.mu.RUnlock()

	if !ok || len(series.Chunks) == 0 {
		log.Printf("[Storage-%s] Query for %s returned 0 points", m.nodeID, key)
		return []float64{}, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if start == 0 {
		start = 0
	}
	if end == 0 {
		end = math.MaxInt64
	}

	var res []float64

	for _, chunk := range series.Chunks {
		if chunk.EndTime < start || chunk.StartTime > end {
			continue
		}

		startIdx := binarySearchStart(chunk.Samples, start)
		if startIdx >= len(chunk.Samples) {
			continue
		}

		endIdx := binarySearchEnd(chunk.Samples, end)
		if endIdx < startIdx {
			continue
		}

		for i := startIdx; i <= endIdx; i++ {
			res = append(res, chunk.Samples[i].Value)
		}
	}

	return res, nil
}

func (m *UnifiedStorage) QueryAggregated(deviceID, metric string, start, end int64) (QueryStats, error) {
	key := deviceID + "|" + metric
	m.mu.RLock()
	series, ok := m.data[key]
	m.mu.RUnlock()

	if !ok || len(series.Chunks) == 0 {
		return QueryStats{}, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if start == 0 {
		start = 0
	}
	if end == 0 {
		end = math.MaxInt64
	}

	var totalSum float64
	var totalCount int
	var min, max float64
	firstPoint := true

	updateStats := func(val float64) {
		totalSum += val
		totalCount++
		if firstPoint {
			min = val
			max = val
			firstPoint = false
		} else {
			if val < min {
				min = val
			}
			if val > max {
				max = val
			}
		}
	}

	for _, chunk := range series.Chunks {
		if chunk.EndTime < start || chunk.StartTime > end {
			continue
		}

		// Case A: Chunk falls ENTIRELY within the requested time range.
		// O(1) Pre-Aggregated operation! We never touch the raw data arrays.
		if chunk.StartTime >= start && chunk.EndTime <= end {
			totalSum += chunk.Sum
			totalCount += chunk.Count
			if firstPoint {
				min = chunk.Min
				max = chunk.Max
				firstPoint = false
			} else {
				if chunk.Min < min {
					min = chunk.Min
				}
				if chunk.Max > max {
					max = chunk.Max
				}
			}
			continue
		}

		// Case B: Chunk lies on the edge of the requested time range.
		// O(log N) + O(K) fallback where we aggregate just this boundary chunk.
		startIdx := binarySearchStart(chunk.Samples, start)
		if startIdx >= len(chunk.Samples) {
			continue
		}

		endIdx := binarySearchEnd(chunk.Samples, end)
		if endIdx < startIdx {
			continue
		}

		for i := startIdx; i <= endIdx; i++ {
			updateStats(chunk.Samples[i].Value)
		}
	}

	return QueryStats{Sum: totalSum, Count: totalCount, Min: min, Max: max}, nil
}

func (m *UnifiedStorage) Delete(deviceID, metric string) error {
	return nil
}

func (m *UnifiedStorage) Reload() error {
	return nil
}

func (m *UnifiedStorage) Close() error {
	return nil
}
