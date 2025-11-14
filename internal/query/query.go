package query

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/minitrue/internal/cluster"
	"github.com/minitrue/internal/mqttclient"
	"github.com/minitrue/internal/storage"
	"github.com/minitrue/internal/websocket"
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

func combineStats(stats []storage.QueryStats) storage.QueryStats {
	if len(stats) == 0 {
		return storage.QueryStats{}
	}
	combined := stats[0]
	for i := 1; i < len(stats); i++ {
		combined.Sum += stats[i].Sum
		combined.Count += stats[i].Count
		if stats[i].Min < combined.Min {
			combined.Min = stats[i].Min
		}
		if stats[i].Max > combined.Max {
			combined.Max = stats[i].Max
		}
	}
	return combined
}

type Service struct {
	mqtt       *mqttclient.Client
	store      storage.Storage
	nodeID     string
	httpClient *http.Client
	wsHub      *websocket.Hub
	restartFn  func() 
}

func New(m *mqttclient.Client, s storage.Storage) *Service {
	return NewWithNodeID(m, s, "")
}

func NewWithNodeID(m *mqttclient.Client, s storage.Storage, nodeID string) *Service {
	return NewWithRestart(m, s, nodeID, nil)
}

func NewWithRestart(m *mqttclient.Client, s storage.Storage, nodeID string, restartFn func()) *Service {
	wsOpts := mqttclient.Options{
		BrokerURL: "tcp://localhost:1883",
		ClientID:  fmt.Sprintf("minitrue-ws-%s-%d", nodeID, time.Now().UnixNano()),
	}
	wsMqttClient, err := mqttclient.New(wsOpts)
	if err != nil {
		log.Printf("[WebSocket] Failed to create MQTT client: %v", err)
		return &Service{
			mqtt:   m,
			store:  s,
			nodeID: nodeID,
			httpClient: &http.Client{
				Timeout: 5 * time.Second,
			},
			wsHub:     nil,
			restartFn: restartFn,
		}
	}

	hub := websocket.NewHub(wsMqttClient)
	go hub.Run()

	return &Service{
		mqtt:   m,
		store:  s,
		nodeID: nodeID,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		wsHub:     hub,
		restartFn: restartFn,
	}
}

func (s *Service) StartHTTP(port int) {
	http.HandleFunc("/query", s.handleQuery)
	http.HandleFunc("/query-samples", s.handleQuerySamples)
	http.HandleFunc("/query-aggregated", s.handleQueryAggregated)
	http.HandleFunc("/delete", s.handleDelete)

	if s.wsHub != nil {
		http.HandleFunc("/ws", s.handleWebSocket)
		http.HandleFunc("/ws/stats", s.handleWebSocketStats)
		log.Printf("WebSocket available at ws://localhost:%d/ws", port)
	}

	addr := fmt.Sprintf(":%d", port)
	log.Printf("Query HTTP listening on %s", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("http server error: %v", err)
	}
}

func (s *Service) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.wsHub == nil {
		http.Error(w, "WebSocket not available", http.StatusServiceUnavailable)
		return
	}
	log.Printf("[WebSocket] New connection request from %s", r.RemoteAddr)
	s.wsHub.ServeWS(w, r)
}

func (s *Service) handleWebSocketStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if s.wsHub == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected_clients": 0,
			"timestamp":         time.Now().Unix(),
			"status":            "unavailable",
		})
		return
	}

	stats := map[string]interface{}{
		"connected_clients": s.wsHub.GetClientCount(),
		"timestamp":         time.Now().Unix(),
		"status":            "active",
	}

	json.NewEncoder(w).Encode(stats)
}

func (s *Service) handleQuery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

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

	stats, err := s.distributedQueryAggregated(qr)
	if err != nil {
		log.Printf("[Query] Distributed query error: %v, falling back to local", err)
		stats, err = s.store.QueryAggregated(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
		if err != nil {
			http.Error(w, "storage error", http.StatusInternalServerError)
			return
		}
	}

	var res float64
	var count int
	if stats.Count == 0 {
		res = 0
		count = 0
	} else {
		switch qr.Operation {
		case "avg":
			res = stats.Sum / float64(stats.Count)
			count = stats.Count
		case "sum":
			res = stats.Sum
			count = stats.Count
		case "max":
			res = stats.Max
			count = stats.Count
		case "min":
			res = stats.Min
			count = stats.Count
		default:
			http.Error(w, "unsupported operation", http.StatusBadRequest)
			return
		}
	}
	duration := time.Since(start).Nanoseconds()
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

func (s *Service) distributedQuery(qr QueryRequest) ([]float64, error) {
	hashRing := cluster.GetHashRing()
	if hashRing == nil {
		return nil, fmt.Errorf("hash ring not initialized")
	}

	// Use same keying as ingestion to target the right nodes
	key := qr.DeviceID + ":" + qr.MetricName
	selectedNodes := cluster.GetNodesForKey(key, 2)
	if len(selectedNodes) == 0 {
		return nil, fmt.Errorf("no nodes in cluster")
	}

	log.Printf("[Query] Querying %d nodes for device=%s metric=%s", len(selectedNodes), qr.DeviceID, qr.MetricName)

	// Query nodes concurrently
	var wg sync.WaitGroup
	resultChan := make(chan []float64, len(selectedNodes)+1)
	errorChan := make(chan error, len(selectedNodes)+1)

	// Query local node first
	wg.Add(1)
	go func() {
		defer wg.Done()
		samples, err := s.store.Query(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
		if err != nil {
			log.Printf("[Query] Local query error: %v", err)
			errorChan <- err
			return
		}
		if len(samples) > 0 {
			resultChan <- samples
		}
	}()

	for _, nodeID := range selectedNodes {
		if s.nodeID != "" && nodeID == s.nodeID {
			continue
		}
		wg.Add(1)
		go func(nID string) {
			defer wg.Done()
			samples, err := s.queryRemoteNode(nID, qr)
			if err != nil {
				log.Printf("[Query] Failed to query node %s: %v", nID, err)
				errorChan <- err
				return
			}
			if len(samples) > 0 {
				resultChan <- samples
			}
		}(nodeID)
	}

	wg.Wait()
	close(resultChan)
	close(errorChan)

	allSamples := make([]float64, 0)
	for samples := range resultChan {
		allSamples = append(allSamples, samples...)
	}

	if len(allSamples) == 0 && len(errorChan) > 0 {
		return nil, fmt.Errorf("all queries failed")
	}

	log.Printf("[Query] Aggregated %d samples from %d candidate nodes", len(allSamples), len(selectedNodes))
	return allSamples, nil
}

func (s *Service) distributedQueryAggregated(qr QueryRequest) (storage.QueryStats, error) {
	hashRing := cluster.GetHashRing()
	if hashRing == nil {
		return storage.QueryStats{}, fmt.Errorf("hash ring not initialized")
	}

	key := qr.DeviceID + ":" + qr.MetricName
	selectedNodes := cluster.GetNodesForKey(key, 2)
	if len(selectedNodes) == 0 {
		return storage.QueryStats{}, fmt.Errorf("no nodes in cluster")
	}

	log.Printf("[Query] Querying %d nodes for device=%s metric=%s", len(selectedNodes), qr.DeviceID, qr.MetricName)

	var wg sync.WaitGroup
	resultChan := make(chan storage.QueryStats, len(selectedNodes)+1)
	errorChan := make(chan error, len(selectedNodes)+1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		stats, err := s.store.QueryAggregated(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
		if err != nil {
			log.Printf("[Query] Local query error: %v", err)
			errorChan <- err
			return
		}
		if stats.Count > 0 {
			resultChan <- stats
		}
	}()

	for _, nodeID := range selectedNodes {
		if s.nodeID != "" && nodeID == s.nodeID {
			continue
		}
		wg.Add(1)
		go func(nID string) {
			defer wg.Done()
			stats, err := s.queryRemoteNodeAggregated(nID, qr)
			if err != nil {
				log.Printf("[Query] Failed to query node %s: %v", nID, err)
				errorChan <- err
				return
			}
			if stats.Count > 0 {
				resultChan <- stats
			}
		}(nodeID)
	}

	wg.Wait()
	close(resultChan)
	close(errorChan)

	allStats := make([]storage.QueryStats, 0)
	for stats := range resultChan {
		allStats = append(allStats, stats)
	}

	combined := combineStats(allStats)

	if combined.Count == 0 && len(errorChan) > 0 {
		return storage.QueryStats{}, fmt.Errorf("all queries failed")
	}

	log.Printf("[Query] Aggregated stats from %d candidate nodes", len(selectedNodes))
	return combined, nil
}

func (s *Service) queryRemoteNode(nodeID string, qr QueryRequest) ([]float64, error) {
	clusterMgr := cluster.GetClusterManager()
	if clusterMgr == nil {
		return nil, fmt.Errorf("cluster manager not initialized")
	}

	nodePort, err := clusterMgr.GetNodeHTTPPort(nodeID)
	if err != nil {
		nodePort = s.getNodePort(nodeID)
		if nodePort == 0 {
			return nil, fmt.Errorf("unknown node port for %s: %w", nodeID, err)
		}
	}

	url := fmt.Sprintf("http://localhost:%d/query-samples", nodePort)

	reqBody, err := json.Marshal(qr)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query-samples failed with status %d: %s", resp.StatusCode, string(body))
	}

	var samplesResponse struct {
		Samples []float64 `json:"samples"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&samplesResponse); err != nil {
		return nil, fmt.Errorf("failed to decode samples response: %w", err)
	}

	log.Printf("[Query] Node %s returned %d samples", nodeID, len(samplesResponse.Samples))
	return samplesResponse.Samples, nil
}

func (s *Service) queryRemoteNodeAggregated(nodeID string, qr QueryRequest) (storage.QueryStats, error) {
	port := s.getNodePort(nodeID)
	if port == 0 {
		return storage.QueryStats{}, fmt.Errorf("unknown node port for %s", nodeID)
	}

	url := fmt.Sprintf("http://localhost:%d/query-aggregated", port)

	reqBody, err := json.Marshal(qr)
	if err != nil {
		return storage.QueryStats{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return storage.QueryStats{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return storage.QueryStats{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return storage.QueryStats{}, fmt.Errorf("query-aggregated failed with status %d: %s", resp.StatusCode, string(body))
	}

	var stats storage.QueryStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return storage.QueryStats{}, fmt.Errorf("failed to decode stats response: %w", err)
	}

	log.Printf("[Query] Node %s returned stats: %+v", nodeID, stats)
	return stats, nil
}

func (s *Service) getNodePort(nodeID string) int {
	portMap := map[string]int{
		"ing1": 8080,
		"ing2": 8081,
		"ing3": 8082,
	}
	if port, ok := portMap[nodeID]; ok {
		return port
	}
	return 0
}

func (s *Service) handleQuerySamples(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

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
	if qr.DeviceID == "" || qr.MetricName == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	samples, err := s.store.Query(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	response := struct {
		Samples []float64 `json:"samples"`
	}{
		Samples: samples,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Service) handleQueryAggregated(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

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
	if qr.DeviceID == "" || qr.MetricName == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	stats, err := s.store.QueryAggregated(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var deleteReq struct {
		DeviceID   string `json:"device_id"`
		MetricName string `json:"metric_name"`
	}
	if err := json.Unmarshal(body, &deleteReq); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if deleteReq.DeviceID == "" || deleteReq.MetricName == "" {
		http.Error(w, "missing device_id or metric_name", http.StatusBadRequest)
		return
	}

	if err := s.store.Delete(deleteReq.DeviceID, deleteReq.MetricName); err != nil {
		log.Printf("[Delete] Error deleting data: %v", err)
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	log.Printf("[Delete] Deleted all data for device=%s metric=%s", deleteReq.DeviceID, deleteReq.MetricName)

	response := map[string]interface{}{
		"message": fmt.Sprintf("Successfully deleted all data for device=%s metric=%s", deleteReq.DeviceID, deleteReq.MetricName),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)

	if s.restartFn != nil {
		log.Printf("[Delete] Triggering server restart...")
		go func() {
			time.Sleep(100 * time.Millisecond)
			s.restartFn()
		}()
	}
}
