package query

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/minitrue/internal/mqttclient"
    "github.com/minitrue/internal/storage"
)

type QueryRequest struct {
    DeviceID   string `json:"device_id"`
    MetricName string `json:"metric_name"`
    Operation  string `json:"operation"`
    StartTime  int64  `json:"start_time"`
    EndTime    int64  `json:"end_time"`
}

type QueryResult struct {
    DeviceID   string  `json:"device_id"`
    MetricName string  `json:"metric_name"`
    Operation  string  `json:"operation"`
    Result     float64 `json:"result"`
    Count      int     `json:"count"`
    Duration   int64   `json:"duration_ns"`
}

type Service struct {
    mqtt      *mqttclient.Client
    store     storage.Storage
    nodeID    string
    restartFn func()
}

func New(m *mqttclient.Client, s storage.Storage) *Service { return NewWithNodeID(m, s, "") }
func NewWithNodeID(m *mqttclient.Client, s storage.Storage, nodeID string) *Service { return NewWithRestart(m, s, nodeID, nil) }
func NewWithRestart(m *mqttclient.Client, s storage.Storage, nodeID string, restartFn func()) *Service {
    return &Service{mqtt: m, store: s, nodeID: nodeID, restartFn: restartFn}
}

func (s *Service) StartHTTP(port int) {
    http.HandleFunc("/query", s.handleQuery)
    addr := fmt.Sprintf(":%d", port)
    _ = http.ListenAndServe(addr, nil)
}

func setCORS(w http.ResponseWriter, r *http.Request) bool {
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
    w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
    if r.Method == "OPTIONS" { w.WriteHeader(http.StatusOK); return true }
    return false
}

func parseQueryRequest(w http.ResponseWriter, r *http.Request) (QueryRequest, bool) {
    if setCORS(w, r) { return QueryRequest{}, false }
    body, err := io.ReadAll(r.Body)
    if err != nil { http.Error(w, "bad request", http.StatusBadRequest); return QueryRequest{}, false }
    var qr QueryRequest
    if err := json.Unmarshal(body, &qr); err != nil { http.Error(w, "invalid json", http.StatusBadRequest); return QueryRequest{}, false }
    return qr, true
}

func (s *Service) handleQuery(w http.ResponseWriter, r *http.Request) {
    start := time.Now()
    qr, ok := parseQueryRequest(w, r)
    if !ok { return }
    stats, err := s.store.QueryAggregated(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
    if err != nil { http.Error(w, "storage error", http.StatusInternalServerError); return }
    var result float64
    if qr.Operation == "avg" && stats.Count > 0 { result = stats.Sum / float64(stats.Count) }
    if qr.Operation == "sum" { result = stats.Sum }
    if qr.Operation == "max" { result = stats.Max }
    if qr.Operation == "min" { result = stats.Min }
    _ = json.NewEncoder(w).Encode(QueryResult{DeviceID: qr.DeviceID, MetricName: qr.MetricName, Operation: qr.Operation, Result: result, Count: stats.Count, Duration: time.Since(start).Nanoseconds()})
}
