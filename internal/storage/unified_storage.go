package storage

import (
    "encoding/json"
    "log"
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

    insertPos := sort.Search(len(c.Samples), func(i int) bool { return c.Samples[i].Timestamp >= newSample.Timestamp })
    if insertPos == len(c.Samples) {
        c.Samples = append(c.Samples, newSample)
    } else {
        c.Samples = append(c.Samples, sample{})
        copy(c.Samples[insertPos+1:], c.Samples[insertPos:])
        c.Samples[insertPos] = newSample
    }

    if newSample.Timestamp < c.StartTime { c.StartTime = newSample.Timestamp }
    if newSample.Timestamp > c.EndTime { c.EndTime = newSample.Timestamp }
    if newSample.Value < c.Min { c.Min = newSample.Value }
    if newSample.Value > c.Max { c.Max = newSample.Value }
    c.Sum += newSample.Value
    c.Count++
}

type Series struct { Chunks []*Chunk }

func (s *Series) Insert(newSample sample) {
    if len(s.Chunks) == 0 {
        c := &Chunk{}
        c.Insert(newSample)
        s.Chunks = append(s.Chunks, c)
        return
    }

    lastChunk := s.Chunks[len(s.Chunks)-1]
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
    engine    *StorageEngine
    filepath  string
    batchSize int
    batch     []models.Record
    nodeID    string
    lastFlush time.Time
}

func NewUnifiedStorage(filepath string) *UnifiedStorage {
    storage := &UnifiedStorage{
        data:      make(map[string]*Series),
        engine:    NewStorageEngine(filepath),
        filepath:  filepath,
        batchSize: 10,
        batch:     make([]models.Record, 0, 10),
        nodeID:    "unknown",
        lastFlush: time.Now(),
    }
    go storage.periodicFlush()
    return storage
}

func (m *UnifiedStorage) periodicFlush() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    for range ticker.C {
        m.mu.Lock()
        m.mu.Unlock()
    }
}

func (m *UnifiedStorage) PersistPrimary(p interface{}) error { return m.persist(p, "primary") }
func (m *UnifiedStorage) PersistReplica(p interface{}) error { return m.persist(p, "replica") }

func (m *UnifiedStorage) persist(p interface{}, role string) error {
    b, err := json.Marshal(p)
    if err != nil { return err }
    var mp map[string]interface{}
    if err := json.Unmarshal(b, &mp); err != nil { return err }
    device, _ := mp["device_id"].(string)
    metric, _ := mp["metric_name"].(string)
    ts := int64(mp["timestamp"].(float64))
    val := mp["value"].(float64)
    key := device + "|" + metric

    m.mu.Lock()
    newSample := sample{Timestamp: ts, Value: val, Role: role}
    series, ok := m.data[key]
    if !ok {
        series = &Series{}
        m.data[key] = series
    }
    series.Insert(newSample)
    m.mu.Unlock()
    return nil
}

func (m *UnifiedStorage) Query(deviceID, metric string, start, end int64) ([]float64, error) {
    return []float64{}, nil
}

func (m *UnifiedStorage) QueryAggregated(deviceID, metric string, start, end int64) (QueryStats, error) {
    return QueryStats{}, nil
}

func (m *UnifiedStorage) Delete(deviceID, metric string) error { return nil }
func (m *UnifiedStorage) Reload() error { return nil }
func (m *UnifiedStorage) Close() error { return nil }

func (m *UnifiedStorage) flushBatchUnlocked() { log.Printf("flushing batch") }
