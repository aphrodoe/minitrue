package query

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minitrue/internal/cluster"
	"github.com/minitrue/internal/models"
)

type DigestResponse struct {
	Checksum uint32 `json:"checksum"`
	Count    int    `json:"count"`
}

type SyncResponse struct {
	Records []models.Record `json:"records"`
}

func (s *Service) handleInternalDigest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	seriesKey := r.URL.Query().Get("series")
	if seriesKey == "" {
		http.Error(w, "missing series", http.StatusBadRequest)
		return
	}

	parts := strings.Split(seriesKey, "|")
	if len(parts) != 2 {
		http.Error(w, "invalid series key", http.StatusBadRequest)
		return
	}

	checksum, count, err := s.store.GetSeriesDigest(parts[0], parts[1])
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(DigestResponse{Checksum: checksum, Count: count})
}

func (s *Service) handleInternalSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	seriesKey := r.URL.Query().Get("series")
	if seriesKey == "" {
		http.Error(w, "missing series", http.StatusBadRequest)
		return
	}

	parts := strings.Split(seriesKey, "|")
	if len(parts) != 2 {
		http.Error(w, "invalid series key", http.StatusBadRequest)
		return
	}

	records, err := s.store.GetSeriesRecords(parts[0], parts[1])
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SyncResponse{Records: records})
}

func (s *Service) StartReadRepairLoop() {
	go func() {
		// Run every 2 minutes
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			s.performReadRepair()
		}
	}()
}

func (s *Service) performReadRepair() {
	hashRing := cluster.GetHashRing()
	if hashRing == nil {
		return
	}

	keys := s.store.GetOwnedSeriesKeys()
	for _, key := range keys {
		parts := strings.Split(key, "|")
		if len(parts) != 2 {
			continue
		}
		deviceID, metric := parts[0], parts[1]
		routeKey := deviceID + ":" + metric

		nodes := cluster.GetNodesForKey(routeKey, 2)
		if len(nodes) < 2 {
			continue // Not enough nodes to repair
		}

		// Only check if we are the primary or replica
		var peer string
		if nodes[0] == s.nodeID {
			peer = nodes[1] // we are primary, check replica
		} else if nodes[1] == s.nodeID {
			peer = nodes[0] // we are replica, check primary
		} else {
			continue // We don't own this anymore
		}

		s.repairWithPeer(peer, deviceID, metric)
	}
}

func (s *Service) repairWithPeer(peerID, deviceID, metric string) {
	endpoint, err := s.getNodeHTTPEndpoint(peerID)
	if err != nil {
		return
	}

	seriesKey := deviceID + "|" + metric
	digestURL := endpoint + "/internal/digest?series=" + url.QueryEscape(seriesKey)

	resp, err := s.httpClient.Get(digestURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		return
	}
	defer resp.Body.Close()

	var remoteDigest DigestResponse
	if err := json.NewDecoder(resp.Body).Decode(&remoteDigest); err != nil {
		return
	}

	localChecksum, localCount, err := s.store.GetSeriesDigest(deviceID, metric)
	if err != nil {
		return
	}

	if localChecksum == remoteDigest.Checksum && localCount == remoteDigest.Count {
		return // in sync
	}

	// Out of sync. If we have fewer records, fetch from peer and persist.
	if localCount < remoteDigest.Count {
		log.Printf("[ReadRepair] Node %s out of sync for %s (local=%d, remote=%d). Fetching...", s.nodeID, seriesKey, localCount, remoteDigest.Count)
		s.syncFromPeer(peerID, deviceID, metric)
	}
}

func (s *Service) syncFromPeer(peerID, deviceID, metric string) {
	endpoint, err := s.getNodeHTTPEndpoint(peerID)
	if err != nil {
		return
	}

	seriesKey := deviceID + "|" + metric
	syncURL := endpoint + "/internal/sync?series=" + url.QueryEscape(seriesKey)

	resp, err := s.httpClient.Get(syncURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("[ReadRepair] Failed to fetch sync data from %s: %v", peerID, err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var syncResp SyncResponse
	if err := json.Unmarshal(body, &syncResp); err != nil {
		return
	}

	// Persist missing records using idempotent writes
	for _, record := range syncResp.Records {
		s.store.PersistReplica(record)
	}

	log.Printf("[ReadRepair] Successfully applied %d records from %s for %s", len(syncResp.Records), peerID, seriesKey)
}
