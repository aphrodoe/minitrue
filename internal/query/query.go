package query

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/minitrue/internal/mqttclient"
	"github.com/minitrue/internal/storage"
)

type QueryRequest struct {
	DeviceID   string `json:"device_id"`
	MetricName string `json:"metric_name"`
	Operation  string `json:"operation"` // avg | sum | max | min
	StartTime  int64  `json:"start_time"`
	EndTime    int64  `json:"end_time"`
}

type QueryResult struct {
	DeviceID   string  `json:"device_id"`
	MetricName string  `json:"metric_name"`
	Operation  string  `json:"operation"`
	Result     float64 `json:"result"`
	Count      int     `json:"count"`
	Duration   int64   `json:"duration_ms"`
}

type Service struct {
	mqtt  *mqttclient.Client
	store storage.Storage
}

func New(m *mqttclient.Client, s storage.Storage) *Service {
	return &Service{mqtt: m, store: s}
}

func (s *Service) StartHTTP(port int) {
	http.HandleFunc("/query", s.handleQuery)
	addr := fmt.Sprintf(":%d", port)
	log.Printf("Query HTTP listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("http server error: %v", err)
	}
}

func (s *Service) handleQuery(w http.ResponseWriter, r *http.Request) {
	// Enable CORS for React frontend
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// Handle preflight OPTIONS request
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	start := time.Now()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var qr QueryRequest
	if err := json.Unmarshal(body, &qr); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if qr.DeviceID == "" || qr.MetricName == "" || qr.Operation == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}
	samples, err := s.store.Query(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	var res float64
	var count int
	if len(samples) == 0 {
		res = 0
		count = 0
	} else {
		switch qr.Operation {
		case "avg":
			var sum float64
			for _, v := range samples {
				sum += v
			}
			res = sum / float64(len(samples))
			count = len(samples)
		case "sum":
			var sum float64
			for _, v := range samples {
				sum += v
			}
			res = sum
			count = len(samples)
		case "max":
			max := samples[0]
			for _, v := range samples {
				if v > max {
					max = v
				}
			}
			res = max
			count = len(samples)
		case "min":
			min := samples[0]
			for _, v := range samples {
				if v < min {
					min = v
				}
			}
			res = min
			count = len(samples)
		default:
			http.Error(w, "unsupported operation", http.StatusBadRequest)
			return
		}
	}
	duration := time.Since(start).Milliseconds()
	out := QueryResult{
		DeviceID:   qr.DeviceID,
		MetricName: qr.MetricName,
		Operation:  qr.Operation,
		Result:     res,
		Count:      count,
		Duration:   duration,
	}
	log.Printf("[Query] %+v", out)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}




