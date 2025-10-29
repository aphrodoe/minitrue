package storage

import (
    "encoding/json"
    "os"
    "sync"
    "time"
)

// Storage interface defines two methods that Lead B's storage will implement later.
// PersistPrimary -> called by primary ingestion node
// PersistReplica -> called by replica ingestion nodes
type Storage interface {
    PersistPrimary(p interface{}) error
    PersistReplica(p interface{}) error
    Query(deviceID, metric string, start, end int64) ([]float64, error)
}

// MockStorage (demo) implements Storage. Replace with your real storage functions later.
type MockStorage struct {
    mu   sync.RWMutex
    data map[string][]sample
    file *os.File
}

type sample struct {
    Timestamp int64   `json:"timestamp"`
    Value     float64 `json:"value"`
    Role      string  `json:"role"` // "primary" or "replica"
}

func NewMockStorage() *MockStorage {
    f, _ := os.OpenFile("storage_log_replication.jsonl", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
    return &MockStorage{
        data: make(map[string][]sample),
        file: f,
    }
}

// PersistPrimary stores a datapoint as primary
func (m *MockStorage) PersistPrimary(p interface{}) error {
    return m.persist(p, "primary")
}

// PersistReplica stores a datapoint as replica
func (m *MockStorage) PersistReplica(p interface{}) error {
    return m.persist(p, "replica")
}

func (m *MockStorage) persist(p interface{}, role string) error {
    b, _ := json.Marshal(p)
    // write raw JSON line (demo)
    m.file.Write(append(b, byte('\n')))

    // also keep in memory for quick queries
    var mp map[string]interface{}
    if err := json.Unmarshal(b, &mp); err != nil {
        return err
    }
    device, _ := mp["device_id"].(string)
    metric, _ := mp["metric_name"].(string)
    ts := int64(0)
    if v, ok := mp["timestamp"].(float64); ok {
        ts = int64(v)
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
    m.mu.Unlock()
    return nil
}

func (m *MockStorage) Query(deviceID, metric string, start, end int64) ([]float64, error) {
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
