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
func NewWithRestart(m *mqttclient.Client, s storage.Storage, nodeID string, restartFn func()) *Service { return &Service{mqtt: m, store: s, nodeID: nodeID, restartFn: restartFn} }

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

func (s *Service) StartHTTP(port int) {
    http.HandleFunc("/query", s.handleQuery)
    http.HandleFunc("/query-samples", s.handleQuerySamples)
    http.HandleFunc("/query-aggregated", s.handleQueryAggregated)
    _ = http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
}

func (s *Service) handleQuery(w http.ResponseWriter, r *http.Request) {
    start := time.Now()
    qr, ok := parseQueryRequest(w, r)
    if !ok { return }
    stats, err := s.store.QueryAggregated(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
    if err != nil { http.Error(w, "storage error", http.StatusInternalServerError); return }
    var result float64
    switch qr.Operation {
    case "avg": if stats.Count > 0 { result = stats.Sum / float64(stats.Count) }
    case "sum": result = stats.Sum
    case "max": result = stats.Max
    case "min": result = stats.Min
    default: http.Error(w, "unsupported operation", http.StatusBadRequest); return
    }
    _ = json.NewEncoder(w).Encode(QueryResult{DeviceID: qr.DeviceID, MetricName: qr.MetricName, Operation: qr.Operation, Result: result, Count: stats.Count, Duration: time.Since(start).Nanoseconds()})
}

func (s *Service) handleQuerySamples(w http.ResponseWriter, r *http.Request) {
    qr, ok := parseQueryRequest(w, r)
    if !ok { return }
    samples, err := s.store.Query(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
    if err != nil { http.Error(w, "storage error", http.StatusInternalServerError); return }
    _ = json.NewEncoder(w).Encode(struct { Samples []float64 `json:"samples"` }{Samples: samples})
}

func (s *Service) handleQueryAggregated(w http.ResponseWriter, r *http.Request) {
    qr, ok := parseQueryRequest(w, r)
    if !ok { return }
    stats, err := s.store.QueryAggregated(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
    if err != nil { http.Error(w, "storage error", http.StatusInternalServerError); return }
    _ = json.NewEncoder(w).Encode(stats)
}
