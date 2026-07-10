package query

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/minitrue/internal/cluster"
	"github.com/minitrue/internal/models"
	"github.com/minitrue/internal/storage"
	"github.com/minitrue/internal/websocket"
)

// QueryRequest is the payload for POST /query and POST /query-aggregated.
type QueryRequest struct {
	DeviceID   string `json:"device_id"`
	MetricName string `json:"metric_name"`
	Operation  string `json:"operation"`
	StartTime  int64  `json:"start_time"`
	EndTime    int64  `json:"end_time"`
}

// QueryResult is the response shape for POST /query.
type QueryResult struct {
	DeviceID   string  `json:"device_id"`
	MetricName string  `json:"metric_name"`
	Operation  string  `json:"operation"`
	Result     float64 `json:"result"`
	Count      int     `json:"count"`
	Duration   int64   `json:"duration_ns"`
}



// Service owns the HTTP server and all read/query/ingest logic for a node.
type Service struct {
	store                storage.Storage
	nodeID               string
	httpClient           *http.Client
	wsHub                *websocket.Hub
	restartFn            func()
	clusterStateProvider func() models.ClusterState
	syncManager          *SyncManager
}



func NewWithRestart(s storage.Storage, nodeID string, restartFn func()) *Service {
	hub := websocket.NewHub()
	go hub.Run()

	svc := &Service{
		store:  s,
		nodeID: nodeID,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		wsHub:                hub,
		restartFn:            restartFn,
		clusterStateProvider: defaultClusterStateProvider,
	}

	svc.syncManager = NewSyncManager(svc.syncFromPeer)
	svc.syncManager.Start(4) // Start 4 concurrent workers for cluster sync operations

	return svc
}

func defaultClusterStateProvider() models.ClusterState {
	clusterMgr := cluster.GetClusterManager()
	if clusterMgr == nil {
		return models.ClusterState{Nodes: make(map[string]*models.NodeInfo)}
	}

	gossipProtocol := clusterMgr.GetGossipProtocol()
	if gossipProtocol == nil {
		return models.ClusterState{Nodes: make(map[string]*models.NodeInfo)}
	}

	return gossipProtocol.GetClusterState()
}

func (s *Service) StartHTTP(port int) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleHealth)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/ingest", s.handleIngest)
	mux.HandleFunc("/query", s.handleQuery)
	mux.HandleFunc("/query-samples", s.handleQuerySamples)
	mux.HandleFunc("/query-samples-records", s.handleQuerySamplesRecords)
	mux.HandleFunc("/query-aggregated", s.handleQueryAggregated)
	mux.HandleFunc("/delete", s.handleDelete)
	mux.HandleFunc("/cluster/members", s.handleClusterMembers)
	mux.HandleFunc("/internal/digest", s.handleInternalDigest)
	mux.HandleFunc("/internal/sync", s.handleInternalSync)
	mux.HandleFunc("/internal/keys", s.handleInternalKeys)
	mux.HandleFunc("/keys", s.handlePublicKeys)

	if s.wsHub != nil {
		mux.HandleFunc("/ws", s.handleWebSocket)
		mux.HandleFunc("/ws/stats", s.handleWebSocketStats)
		log.Printf("[%s] WebSocket available at ws://localhost:%d/ws", s.nodeID, port)
	}

	addr := fmt.Sprintf(":%d", port)
	log.Printf("[%s] HTTP server listening on %s", s.nodeID, addr)

	// Start background read-repair loop.
	s.StartReadRepairLoop()

	// Register membership-change hooks for data migration.
	s.StartMigrationHooks()

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[%s] HTTP server error: %v", s.nodeID, err)
	}
}

// ---------------------------------------------------------------------------
// Ingest handler — called exclusively by minitrue-router
// ---------------------------------------------------------------------------

// handleIngest accepts targeted writes from the router.
// The header X-Write-Role must be either "primary" or "replica".
func (s *Service) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	role := r.Header.Get("X-Write-Role")
	if role != "primary" && role != "replica" {
		http.Error(w, "missing or invalid X-Write-Role header (must be primary|replica)", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var dp models.DataPoint
	if err := json.Unmarshal(body, &dp); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if dp.DeviceID == "" || dp.MetricName == "" {
		http.Error(w, "missing device_id or metric_name", http.StatusBadRequest)
		return
	}

	var persistErr error
	if role == "primary" {
		persistErr = s.store.PersistPrimary(dp)
	} else {
		persistErr = s.store.PersistReplica(dp)
	}

	if persistErr != nil {
		log.Printf("[%s][ingest] %s persist error: %v", s.nodeID, role, persistErr)
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	log.Printf("[%s][ingest] %s stored %s/%s = %.4f", s.nodeID, strings.ToUpper(role), dp.DeviceID, dp.MetricName, dp.Value)

	// Push to live WebSocket monitor (non-blocking).
	if s.wsHub != nil {
		s.wsHub.Broadcast(dp)
	}

	w.WriteHeader(http.StatusAccepted)
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r) {
		return
	}

	if r.URL.Path != "/" && r.URL.Path != "/healthz" {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"node_id": s.nodeID,
			"time":    time.Now().Unix(),
		})
	}
}

// ---------------------------------------------------------------------------
// WebSocket
// ---------------------------------------------------------------------------

func (s *Service) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.wsHub == nil {
		http.Error(w, "WebSocket not available", http.StatusServiceUnavailable)
		return
	}
	log.Printf("[WebSocket] New connection from %s", r.RemoteAddr)
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

	json.NewEncoder(w).Encode(map[string]interface{}{
		"connected_clients": s.wsHub.GetClientCount(),
		"timestamp":         time.Now().Unix(),
		"status":            "active",
	})
}

// ---------------------------------------------------------------------------
// CORS helper
// ---------------------------------------------------------------------------

func setCORS(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Cluster members
// ---------------------------------------------------------------------------

func (s *Service) handleClusterMembers(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r) {
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	provider := s.clusterStateProvider
	if provider == nil {
		provider = defaultClusterStateProvider
	}

	state := provider()
	if state.Nodes == nil {
		state.Nodes = make(map[string]*models.NodeInfo)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(state)
}

// ---------------------------------------------------------------------------
// Query helpers
// ---------------------------------------------------------------------------

func parseQueryRequest(w http.ResponseWriter, r *http.Request) (QueryRequest, bool) {
	if setCORS(w, r) {
		return QueryRequest{}, false
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return QueryRequest{}, false
	}
	var qr QueryRequest
	if err := json.Unmarshal(body, &qr); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return QueryRequest{}, false
	}
	return qr, true
}

func (s *Service) handleQuery(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	qr, ok := parseQueryRequest(w, r)
	if !ok {
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

func (s *Service) distributedQueryAggregated(qr QueryRequest) (storage.QueryStats, error) {
	hashRing := cluster.GetHashRing()
	if hashRing == nil {
		return storage.QueryStats{}, fmt.Errorf("hash ring not initialized")
	}

	key := qr.DeviceID + ":" + qr.MetricName

	provider := s.clusterStateProvider
	if provider == nil {
		provider = defaultClusterStateProvider
	}
	state := provider()
	rf := state.ReplicationFactor
	if rf < 1 {
		rf = 1
	}

	selectedNodes := cluster.GetNodesForKey(key, rf)
	if len(selectedNodes) == 0 {
		return storage.QueryStats{}, fmt.Errorf("no nodes in cluster")
	}

	quorum := (len(selectedNodes) / 2) + 1
	log.Printf("[Query] Quorum=%d. Querying %d nodes for device=%s metric=%s", quorum, len(selectedNodes), qr.DeviceID, qr.MetricName)

	var wg sync.WaitGroup
	resultChan := make(chan storage.QueryStats, len(selectedNodes))
	errorChan := make(chan error, len(selectedNodes))

	// Fan-out to all replica nodes.
	for _, nodeID := range selectedNodes {
		wg.Add(1)
		go func(nID string) {
			defer wg.Done()
			var stats storage.QueryStats
			var err error
			if s.nodeID != "" && nID == s.nodeID {
				stats, err = s.store.QueryAggregated(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
			} else {
				stats, err = s.queryRemoteNodeAggregated(nID, qr)
			}
			if err != nil {
				log.Printf("[Query] Failed to query node %s: %v", nID, err)
				errorChan <- err
				return
			}
			resultChan <- stats
		}(nodeID)
	}

	wg.Wait()
	close(resultChan)
	close(errorChan)

	if len(resultChan) < quorum {
		return storage.QueryStats{}, fmt.Errorf("failed to reach quorum (got %d, needed %d)", len(resultChan), quorum)
	}

	// Resolve conflicts by picking the response with the most complete data.
	var bestStats storage.QueryStats
	for stats := range resultChan {
		if stats.Count > bestStats.Count {
			bestStats = stats
		}
	}

	log.Printf("[Query] Met quorum %d. Returning best stats (count=%d)", quorum, bestStats.Count)
	return bestStats, nil
}

func (s *Service) queryRemoteNodeAggregated(nodeID string, qr QueryRequest) (storage.QueryStats, error) {
	endpoint, err := s.getNodeHTTPEndpoint(nodeID)
	if err != nil {
		return storage.QueryStats{}, err
	}

	reqURL := endpoint + "/query-aggregated"

	reqBody, err := json.Marshal(qr)
	if err != nil {
		return storage.QueryStats{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", reqURL, bytes.NewBuffer(reqBody))
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
		return storage.QueryStats{}, fmt.Errorf("query-aggregated returned %d: %s", resp.StatusCode, string(body))
	}

	var stats storage.QueryStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return storage.QueryStats{}, fmt.Errorf("failed to decode stats response: %w", err)
	}

	log.Printf("[Query] Node %s returned stats: %+v", nodeID, stats)
	return stats, nil
}

func (s *Service) getNodeHTTPEndpoint(nodeID string) (string, error) {
	clusterMgr := cluster.GetClusterManager()
	if clusterMgr == nil {
		return "", fmt.Errorf("cluster manager not initialized")
	}

	node, err := clusterMgr.GetNodeInfo(nodeID)
	if err != nil {
		return "", err
	}
	if node.HTTPPort <= 0 {
		return "", fmt.Errorf("unknown HTTP port for %s", nodeID)
	}

	address := strings.TrimSpace(node.Address)
	if address == "" {
		address = "localhost"
	}

	protocol := "http"
	host := address
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		parsed, err := url.Parse(address)
		if err != nil {
			return "", fmt.Errorf("invalid node address for %s: %w", nodeID, err)
		}
		protocol = parsed.Scheme
		host = parsed.Hostname()
	} else if strings.Contains(address, ":") {
		if parsed, err := url.Parse("tcp://" + address); err == nil && parsed.Hostname() != "" {
			host = parsed.Hostname()
		} else {
			host = strings.Split(address, ":")[0]
		}
	}

	if host == "" {
		host = "localhost"
	}

	return fmt.Sprintf("%s://%s:%d", protocol, host, node.HTTPPort), nil
}

// ---------------------------------------------------------------------------
// Query samples (raw points)
// ---------------------------------------------------------------------------

func (s *Service) handleQuerySamples(w http.ResponseWriter, r *http.Request) {
	qr, ok := parseQueryRequest(w, r)
	if !ok {
		return
	}

	if qr.DeviceID == "" || qr.MetricName == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	samples, err := s.distributedQuerySamples(qr)
	if err != nil {
		log.Printf("[Query] Distributed samples query error: %v, falling back to local", err)
		samples, err = s.store.Query(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
		if err != nil {
			http.Error(w, "storage error", http.StatusInternalServerError)
			return
		}
	}

	response := struct {
		Samples []float64 `json:"samples"`
	}{
		Samples: samples,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// ---------------------------------------------------------------------------
// Query aggregated (internal fan-out target)
// ---------------------------------------------------------------------------

func (s *Service) handleQueryAggregated(w http.ResponseWriter, r *http.Request) {
	qr, ok := parseQueryRequest(w, r)
	if !ok {
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

// ---------------------------------------------------------------------------
// Query samples with timestamps (for conflict resolution)
// ---------------------------------------------------------------------------

func (s *Service) handleQuerySamplesRecords(w http.ResponseWriter, r *http.Request) {
	qr, ok := parseQueryRequest(w, r)
	if !ok {
		return
	}

	if qr.DeviceID == "" || qr.MetricName == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	records, err := s.store.QueryWithTimestamps(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	response := struct {
		Records []models.Record `json:"records"`
	}{
		Records: records,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// ---------------------------------------------------------------------------
// Quorum Reads for Raw Samples
// ---------------------------------------------------------------------------

func (s *Service) distributedQuerySamples(qr QueryRequest) ([]float64, error) {
	hashRing := cluster.GetHashRing()
	if hashRing == nil {
		return nil, fmt.Errorf("hash ring not initialized")
	}

	key := qr.DeviceID + ":" + qr.MetricName
	provider := s.clusterStateProvider
	if provider == nil {
		provider = defaultClusterStateProvider
	}
	state := provider()
	rf := state.ReplicationFactor
	if rf < 1 {
		rf = 1
	}

	selectedNodes := cluster.GetNodesForKey(key, rf)
	if len(selectedNodes) == 0 {
		return nil, fmt.Errorf("no nodes in cluster")
	}

	quorum := (len(selectedNodes) / 2) + 1
	log.Printf("[Query] Samples Quorum=%d. Querying %d nodes for device=%s metric=%s", quorum, len(selectedNodes), qr.DeviceID, qr.MetricName)

	var wg sync.WaitGroup
	resultChan := make(chan []models.Record, len(selectedNodes))
	errorChan := make(chan error, len(selectedNodes))

	for _, nodeID := range selectedNodes {
		wg.Add(1)
		go func(nID string) {
			defer wg.Done()
			var records []models.Record
			var err error
			if s.nodeID != "" && nID == s.nodeID {
				records, err = s.store.QueryWithTimestamps(qr.DeviceID, qr.MetricName, qr.StartTime, qr.EndTime)
			} else {
				records, err = s.queryRemoteNodeSamplesWithTimestamps(nID, qr)
			}
			if err != nil {
				log.Printf("[Query] Failed to query node %s for samples: %v", nID, err)
				errorChan <- err
				return
			}
			resultChan <- records
		}(nodeID)
	}

	wg.Wait()
	close(resultChan)
	close(errorChan)

	if len(resultChan) < quorum {
		return nil, fmt.Errorf("failed to reach quorum (got %d, needed %d)", len(resultChan), quorum)
	}

	// Merge records with timestamp deduplication for conflict resolution
	mergedRecords := mergeRecordsByTimestamp(resultChan)

	// Extract just the values for backward compatibility
	var result []float64
	for _, r := range mergedRecords {
		result = append(result, r.Value)
	}

	log.Printf("[Query] Met quorum %d. Returning merged samples (count=%d)", quorum, len(result))
	return result, nil
}

// mergeRecordsByTimestamp deduplicates records across replicas by timestamp
func mergeRecordsByTimestamp(resultChan <-chan []models.Record) []models.Record {
	recordMap := make(map[int64]models.Record)

	for records := range resultChan {
		for _, r := range records {
			if existing, ok := recordMap[r.Timestamp]; !ok {
				recordMap[r.Timestamp] = r
			} else {
				// If duplicate timestamp exists, keep the one received first
				// In practice, they should be identical on replicas
				_ = existing
			}
		}
	}

	result := make([]models.Record, 0, len(recordMap))
	for _, r := range recordMap {
		result = append(result, r)
	}

	// Sort by timestamp for consistent ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp < result[j].Timestamp
	})

	return result
}

func (s *Service) queryRemoteNodeSamples(nodeID string, qr QueryRequest) ([]float64, error) {
	endpoint, err := s.getNodeHTTPEndpoint(nodeID)
	if err != nil {
		return nil, err
	}

	reqURL := endpoint + "/query-samples"

	reqBody, err := json.Marshal(qr)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", reqURL, bytes.NewBuffer(reqBody))
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
		return nil, fmt.Errorf("query-samples returned %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Samples []float64 `json:"samples"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode samples response: %w", err)
	}

	log.Printf("[Query] Node %s returned %d samples", nodeID, len(response.Samples))
	return response.Samples, nil
}

func (s *Service) queryRemoteNodeSamplesWithTimestamps(nodeID string, qr QueryRequest) ([]models.Record, error) {
	endpoint, err := s.getNodeHTTPEndpoint(nodeID)
	if err != nil {
		return nil, err
	}

	reqURL := endpoint + "/query-samples-records"

	reqBody, err := json.Marshal(qr)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", reqURL, bytes.NewBuffer(reqBody))
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
		return nil, fmt.Errorf("query-samples-records returned %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Records []models.Record `json:"records"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode records response: %w", err)
	}

	log.Printf("[Query] Node %s returned %d records", nodeID, len(response.Records))
	return response.Records, nil
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r) {
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
		log.Printf("[Delete] Error: %v", err)
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	log.Printf("[Delete] Deleted device=%s metric=%s", deleteReq.DeviceID, deleteReq.MetricName)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message": fmt.Sprintf("Successfully deleted all data for device=%s metric=%s", deleteReq.DeviceID, deleteReq.MetricName),
	})
}

// ---------------------------------------------------------------------------
// Public Keys (Fan-out)
// ---------------------------------------------------------------------------

func (s *Service) handlePublicKeys(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r) {
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	provider := s.clusterStateProvider
	if provider == nil {
		provider = defaultClusterStateProvider
	}
	state := provider()

	var allNodes []string
	if state.Nodes != nil {
		for nodeID, nodeInfo := range state.Nodes {
			if nodeInfo.Status == "alive" {
				allNodes = append(allNodes, nodeID)
			}
		}
	}

	// If no other nodes, or running in standalone mode, just add ourselves
	if len(allNodes) == 0 && s.nodeID != "" {
		allNodes = append(allNodes, s.nodeID)
	}

	deviceSet := make(map[string]struct{})
	metricSet := make(map[string]struct{})

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, nodeID := range allNodes {
		wg.Add(1)
		go func(nID string) {
			defer wg.Done()
			var keys []string

			if s.nodeID != "" && nID == s.nodeID {
				keys = s.store.GetOwnedSeriesKeys()
			} else {
				endpoint, err := s.getNodeHTTPEndpoint(nID)
				if err == nil {
					resp, err := s.httpClient.Get(endpoint + "/internal/keys")
					if err == nil && resp.StatusCode == http.StatusOK {
						var keysResp struct {
							Keys []string `json:"keys"`
						}
						if err := json.NewDecoder(resp.Body).Decode(&keysResp); err == nil {
							keys = keysResp.Keys
						}
						resp.Body.Close()
					}
				}
			}

			mu.Lock()
			defer mu.Unlock()
			for _, key := range keys {
				parts := strings.Split(key, "|")
				if len(parts) == 2 {
					deviceSet[parts[0]] = struct{}{}
					metricSet[parts[1]] = struct{}{}
				}
			}
		}(nodeID)
	}

	wg.Wait()

	devices := make([]string, 0, len(deviceSet))
	for d := range deviceSet {
		devices = append(devices, d)
	}

	metrics := make([]string, 0, len(metricSet))
	for m := range metricSet {
		metrics = append(metrics, m)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"devices": devices,
		"metrics": metrics,
	})
}
