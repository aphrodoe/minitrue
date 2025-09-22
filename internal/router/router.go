// Package router implements the stateless ingestion gateway for MiniTrue.
//
// The Router receives a DataPoint, resolves its primary and replica storage
// nodes via the consistent hash ring, and forwards the payload to both nodes
// synchronously over HTTP before acknowledging the caller.
package router

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
	"github.com/minitrue/internal/models"
)

// Router is a stateless HTTP gateway that routes incoming DataPoints to the
// correct storage nodes determined by the consistent hash ring.
type Router struct {
	nodeID     string
	httpClient *http.Client
}

// New creates a Router. nodeID is only used for log lines.
func New(nodeID string) *Router {
	return &Router{
		nodeID: nodeID,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// ServeHTTP implements http.Handler. It is bound to POST /route.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(req.Body)
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

	if dp.Timestamp == 0 {
		dp.Timestamp = time.Now().Unix()
	}

	if err := r.Route(dp); err != nil {
		log.Printf("[Router] route error for %s/%s: %v", dp.DeviceID, dp.MetricName, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// Route resolves the primary and replica nodes for dp and forwards the payload
// to both synchronously. It returns an error if either forward fails.
func (r *Router) Route(dp models.DataPoint) error {
	hashRing := cluster.GetHashRing()
	if hashRing == nil {
		return fmt.Errorf("hash ring not initialised")
	}

	key := dp.DeviceID + ":" + dp.MetricName
	nodes := cluster.GetNodesForKey(key, 2)
	if len(nodes) == 0 {
		return fmt.Errorf("no nodes available in ring for key %q", key)
	}

	payload, err := json.Marshal(dp)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	type result struct {
		node string
		role string
		err  error
	}

	ch := make(chan result, len(nodes))
	var wg sync.WaitGroup

	roles := []string{"primary", "replica"}
	for i, nodeID := range nodes {
		role := roles[i]
		wg.Add(1)
		go func(nID, role string) {
			defer wg.Done()
			err := r.forwardTo(nID, role, payload)
			ch <- result{node: nID, role: role, err: err}
		}(nodeID, role)
	}

	wg.Wait()
	close(ch)

	var errs []error
	for res := range ch {
		if res.err != nil {
			log.Printf("[Router] forward to %s (%s) failed: %v", res.node, res.role, res.err)
			errs = append(errs, fmt.Errorf("%s(%s): %w", res.node, res.role, res.err))
		} else {
			log.Printf("[Router] forwarded to %s (%s) %s/%s=%.4f", res.node, res.role, dp.DeviceID, dp.MetricName, dp.Value)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("forward errors: %v", errs)
	}

	return nil
}

// forwardTo sends payload to a single storage node's /ingest endpoint.
func (r *Router) forwardTo(nodeID, role string, payload []byte) error {
	endpoint, err := r.nodeEndpoint(nodeID)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, endpoint+"/ingest", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Write-Role", role)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("node returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// nodeEndpoint resolves the HTTP base URL for a storage node by querying the
// cluster manager (which has a live gossip view of all nodes).
func (r *Router) nodeEndpoint(nodeID string) (string, error) {
	clusterMgr := cluster.GetClusterManager()
	if clusterMgr == nil {
		return "", fmt.Errorf("cluster manager not initialised")
	}

	node, err := clusterMgr.GetNodeInfo(nodeID)
	if err != nil {
		return "", fmt.Errorf("node %s not found in cluster: %w", nodeID, err)
	}
	if node.HTTPPort <= 0 {
		return "", fmt.Errorf("node %s has no HTTP port registered", nodeID)
	}

	// node.Address is the TCP gossip address (host:tcpPort). We strip the port
	// and use node.HTTPPort for the HTTP endpoint.
	host := node.Address
	if idx := len(host) - 1; idx >= 0 {
		for i := len(host) - 1; i >= 0; i-- {
			if host[i] == ':' {
				host = host[:i]
				break
			}
		}
	}
	if host == "" {
		host = "localhost"
	}

	return fmt.Sprintf("http://%s:%d", host, node.HTTPPort), nil
}
