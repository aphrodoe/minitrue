package query

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/minitrue/internal/cluster"
	"github.com/minitrue/internal/models"
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
	mqtt                 *mqttclient.Client
	store                storage.Storage
	nodeID               string
	httpClient           *http.Client
	wsHub                *websocket.Hub
	restartFn            func()
	clusterStateProvider func() models.ClusterState
}

func New(m *mqttclient.Client, s storage.Storage) *Service {
	return NewWithNodeID(m, s, "")
}

func NewWithNodeID(m *mqttclient.Client, s storage.Storage, nodeID string) *Service {
	return NewWithRestart(m, s, nodeID, nil)
}

func NewWithRestart(m *mqttclient.Client, s storage.Storage, nodeID string, restartFn func()) *Service {
	// Query-mode nodes do not start ingestion or persist MQTT messages. This
	// separate client is a lightweight, read-only subscription used only to feed
	// the real-time WebSocket monitor.
	wsOpts := mqttclient.Options{
		BrokerURL: m.BrokerURL(),
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
			wsHub:                nil,
			restartFn:            restartFn,
			clusterStateProvider: defaultClusterStateProvider,
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
		wsHub:                hub,
		restartFn:            restartFn,
		clusterStateProvider: defaultClusterStateProvider,
	}
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
	http.HandleFunc("/", s.handleHealth)
	http.HandleFunc("/healthz", s.handleHealth)
	http.HandleFunc("/query", s.handleQuery)
	http.HandleFunc("/query-samples", s.handleQuerySamples)
	http.HandleFunc("/query-aggregated", s.handleQueryAggregated)
	http.HandleFunc("/delete", s.handleDelete)
	http.HandleFunc("/cluster/members", s.handleClusterMembers)
	http.HandleFunc("/internal/digest", s.handleInternalDigest)
	http.HandleFunc("/internal/sync", s.handleInternalSync)
	http.HandleFunc("/internal/keys", s.handleInternalKeys)

	if s.wsHub != nil {
		http.HandleFunc("/ws", s.handleWebSocket)
		http.HandleFunc("/ws/stats", s.handleWebSocketStats)
		log.Printf("WebSocket available at ws://localhost:%d/ws", port)
	}

	addr := fmt.Sprintf(":%d", port)
	log.Printf("Query HTTP listening on %s", addr)

	// Start background read-repair loop
	s.StartReadRepairLoop()

	// Start migration hooks
	s.StartMigrationHooks()

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("http server error: %v", err)
	}
}

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

	// Query remote nodes
	for _, nodeID := range selectedNodes {
		// Skip local node if it's in the list
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

func (s *Service) queryRemoteNodeAggregated(nodeID string, qr QueryRequest) (storage.QueryStats, error) {
	endpoint, err := s.getNodeHTTPEndpoint(nodeID)
	if err != nil {
		return storage.QueryStats{}, err
	}

	url := endpoint + "/query-aggregated"

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

func (s *Service) handleQuerySamples(w http.ResponseWriter, r *http.Request) {
	qr, ok := parseQueryRequest(w, r)
	if !ok {
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
}
