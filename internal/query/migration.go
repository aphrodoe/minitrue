package query

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/minitrue/internal/cluster"
)

type KeysResponse struct {
	Keys []string `json:"keys"`
}

func (s *Service) handleInternalKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	keys := s.store.GetOwnedSeriesKeys()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(KeysResponse{Keys: keys})
}

var migrationMu sync.Mutex

func (s *Service) StartMigrationHooks() {
	cluster.RegisterMembershipChangeHook(s.onMembershipChange)

	// Start background cleanup loop for data we no longer own
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			s.cleanupUnownedData()
		}
	}()
}

func (s *Service) onMembershipChange() {
	// Prevent concurrent migrations
	if !migrationMu.TryLock() {
		return
	}
	defer migrationMu.Unlock()

	log.Printf("[Migration] Membership change detected, scanning for new keys...")

	clusterMgr := cluster.GetClusterManager()
	if clusterMgr == nil || clusterMgr.GetGossipProtocol() == nil {
		return
	}
	
	activeNodes := clusterMgr.GetGossipProtocol().GetActiveNodes()

	for _, peer := range activeNodes {
		if peer.ID == s.nodeID {
			continue
		}

		s.checkPeerKeysForMigration(peer.ID, peer.HTTPPort)
	}
}

func (s *Service) checkPeerKeysForMigration(peerID string, peerPort int) {
	url := fmt.Sprintf("http://localhost:%d/internal/keys", peerPort)
	resp, err := s.httpClient.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		return
	}
	defer resp.Body.Close()

	var keysResp KeysResponse
	if err := json.NewDecoder(resp.Body).Decode(&keysResp); err != nil {
		return
	}

	for _, key := range keysResp.Keys {
		parts := strings.Split(key, "|")
		if len(parts) != 2 {
			continue
		}
		deviceID, metric := parts[0], parts[1]
		routeKey := deviceID + ":" + metric

		nodes := cluster.GetNodesForKey(routeKey, 2)
		if len(nodes) < 2 {
			continue
		}

		// If we are now primary or replica, we should sync from this peer
		if nodes[0] == s.nodeID || nodes[1] == s.nodeID {
			// Fast path: let the read-repair pull it. Or we explicitly sync it here.
			// Using syncFromPeer ensures we get it immediately.
			log.Printf("[Migration] Node %s now owns %s (previously checked from %s), pulling data...", s.nodeID, key, peerID)
			s.syncFromPeer(peerID, deviceID, metric)
		}
	}
}

func (s *Service) cleanupUnownedData() {
	log.Printf("[Migration] Running background cleanup for unowned data...")
	keys := s.store.GetOwnedSeriesKeys()

	for _, key := range keys {
		parts := strings.Split(key, "|")
		if len(parts) != 2 {
			continue
		}
		deviceID, metric := parts[0], parts[1]
		routeKey := deviceID + ":" + metric

		nodes := cluster.GetNodesForKey(routeKey, 2)
		owns := false
		for _, n := range nodes {
			if n == s.nodeID {
				owns = true
				break
			}
		}

		if !owns {
			log.Printf("[Migration] We no longer own %s. Cleaning up gracefully...", key)
			s.store.Delete(deviceID, metric)
		}
	}
}
