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
	mqtt       *mqttclient.Client
	store      storage.Storage
	nodeID     string
	httpClient *http.Client
}

func New(m *mqttclient.Client, s storage.Storage) *Service {
	return NewWithNodeID(m, s, "")
}

func NewWithNodeID(m *mqttclient.Client, s storage.Storage, nodeID string) *Service {
	return &Service{
		mqtt:   m,
		store:  s,
		nodeID: nodeID,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (s *Service) StartHTTP(port int) {
	http.HandleFunc("/query", s.handleQuery)
	http.HandleFunc("/query-samples", s.handleQuerySamples)
	addr := fmt.Sprintf(":%d", port)
	log.Printf("Query HTTP listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("http server error: %v", err)
	}
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

	// Use distributed query to get samples from all nodes
	samples, err := s.distributedQuery(qr)
	if err != nil {
		log.Printf("[Query] Distributed query error: %v, falling back to local", err)
		// Fallback to local query
		samples, err = s.store.Query(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
		if err != nil {
			http.Error(w, "storage error", http.StatusInternalServerError)
			return
		}
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

// distributedQuery queries all nodes in the cluster and aggregates results
func (s *Service) distributedQuery(qr QueryRequest) ([]float64, error) {
	hashRing := cluster.GetHashRing()
	if hashRing == nil {
		return nil, fmt.Errorf("hash ring not initialized")
	}

	// Get all nodes that might have data for this device
	// Since data can be replicated, we query all nodes to get complete results
	allNodes := hashRing.GetAllNodes()
	if len(allNodes) == 0 {
		return nil, fmt.Errorf("no nodes in cluster")
	}

	log.Printf("[Query] Querying %d nodes for device=%s metric=%s", len(allNodes), qr.DeviceID, qr.MetricName)

	// Query all nodes concurrently
	var wg sync.WaitGroup
	resultChan := make(chan []float64, len(allNodes)+1)
	errorChan := make(chan error, len(allNodes)+1)

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

	// Query remote nodes
	for _, nodeID := range allNodes {
		// Skip local node if it's in the list
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

	// Aggregate all samples
	allSamples := make([]float64, 0)
	for samples := range resultChan {
		allSamples = append(allSamples, samples...)
	}

	// If we got errors but no results, return error
	if len(allSamples) == 0 && len(errorChan) > 0 {
		return nil, fmt.Errorf("all queries failed")
	}

	log.Printf("[Query] Aggregated %d samples from %d nodes", len(allSamples), len(allNodes)+1)
	return allSamples, nil
}

// queryRemoteNode queries a remote node via HTTP to get raw samples
func (s *Service) queryRemoteNode(nodeID string, qr QueryRequest) ([]float64, error) {
	// Get node port from cluster manager (gossip protocol)
	clusterMgr := cluster.GetClusterManager()
	if clusterMgr == nil {
		return nil, fmt.Errorf("cluster manager not initialized")
	}

	nodePort, err := clusterMgr.GetNodeHTTPPort(nodeID)
	if err != nil {
		// Fallback to static port mapping if gossip hasn't discovered the node yet
		nodePort = s.getNodePort(nodeID)
		if nodePort == 0 {
			return nil, fmt.Errorf("unknown node port for %s: %w", nodeID, err)
		}
	}

	// Query the /query-samples endpoint to get raw samples
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

// getNodePort returns the HTTP port for a node
// This is a simplified implementation - in production, use gossip protocol
func (s *Service) getNodePort(nodeID string) int {
	// Default port mapping - in production, get from gossip protocol
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

// handleQuerySamples handles requests for raw samples (used by distributed queries)
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