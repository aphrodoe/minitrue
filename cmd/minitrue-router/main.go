// minitrue-router is the stateless ingestion gateway for the MiniTrue cluster.
//
// It accepts sensor data via POST /route, resolves ownership via the consistent
// hash ring (learned through the same TCP gossip protocol used by storage nodes),
// and forwards each payload directly to the primary and replica storage nodes
// via HTTP POST /ingest.
//
// Usage:
//
//	go run cmd/minitrue-router/main.go \
//	  --node_id=router-1 \
//	  --port=7070 \
//	  --seeds=localhost:9000,localhost:9001,localhost:9002
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/minitrue/internal/cluster"
	"github.com/minitrue/internal/logger"
	"github.com/minitrue/internal/models"
	"github.com/minitrue/internal/router"
)

func main() {
	logger.SetupBeautifulLogging()

	nodeID := flag.String("node_id", envStr("MINITRUE_ROUTER_ID", "router-1"), "router identity (used in log lines)")
	port := flag.Int("port", envInt("MINITRUE_ROUTER_PORT", 7070), "HTTP port for incoming write requests")
	tcpPort := flag.Int("tcp_port", envInt("MINITRUE_ROUTER_TCP_PORT", 9100), "TCP port for gossip (must not clash with storage nodes)")
	seeds := flag.String("seeds", envStr("MINITRUE_SEEDS", ""), "comma-separated TCP addresses of storage nodes")
	flag.Parse()

	seedList := parseSeeds(*seeds)
	if len(seedList) == 0 {
		// Default: try all three standard local storage node TCP ports.
		seedList = []string{"localhost:9000", "localhost:9001", "localhost:9002"}
	}

	log.Printf("[Router] Starting %s — HTTP :%d, TCP gossip :%d, seeds=%v", *nodeID, *port, *tcpPort, seedList)

	// Join the gossip ring so the router gets a live view of the hash ring.
	localNode := &models.NodeInfo{
		ID:       *nodeID,
		Address:  fmt.Sprintf("localhost:%d", *tcpPort),
		HTTPPort: *port,
		Status:   "active",
	}

	clusterMgr := cluster.GetClusterManager()
	if err := clusterMgr.Initialize(localNode, *tcpPort, seedList); err != nil {
		log.Fatalf("[Router] Failed to initialise cluster manager: %v", err)
	}
	defer clusterMgr.Stop()

	// Give gossip a moment to discover nodes before we start accepting writes.
	time.Sleep(500 * time.Millisecond)

	r := router.New(*nodeID)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"node_id": *nodeID,
			"time":    time.Now().Unix(),
		})
	})
	mux.Handle("/route", r)

	addr := fmt.Sprintf(":%d", *port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("[Router] HTTP gateway listening on %s — send writes to POST /route", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[Router] HTTP server error: %v", err)
		}
	}()

	// Graceful shutdown on SIGINT / SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Printf("[Router] Shutting down %s...", *nodeID)
}

func parseSeeds(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func envStr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return fallback
}
