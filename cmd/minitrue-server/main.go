package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/minitrue/internal/cluster"
	"github.com/minitrue/internal/ingestion"
	"github.com/minitrue/internal/mqttclient"
	"github.com/minitrue/internal/query"
	"github.com/minitrue/internal/storage"
	"github.com/minitrue/pkg/models"
)

func main() {
	mode := flag.String("mode", "ingestion", "mode: ingestion | query | all")
	nodeID := flag.String("node_id", "ing1", "node identifier (must be unique)")
	port := flag.Int("port", 0, "HTTP port for query server (0 = auto-assign based on node ID)")
	tcpPort := flag.Int("tcp_port", 0, "TCP port for internode communication (0 = auto-assign based on node ID)")
	broker := flag.String("broker", "tcp://localhost:1883", "MQTT broker URL")
	dataDir := flag.String("data_dir", "data", "directory for storing data files")
	seedNodes := flag.String("seeds", "", "comma-separated list of seed node addresses (e.g., localhost:9001,localhost:9002)")
	flag.Parse()

	// Auto-assign ports based on node ID if not specified
	actualTCPPort := *tcpPort
	if actualTCPPort == 0 {
		actualTCPPort = getDefaultTCPPort(*nodeID)
	}

	actualHTTPPort := *port
	if actualHTTPPort == 0 {
		actualHTTPPort = getDefaultHTTPPort(*nodeID)
	}

	log.Printf("Starting node %s in mode=%s (broker=%s, tcp_port=%d, http_port=%d)", *nodeID, *mode, *broker, actualTCPPort, actualHTTPPort)

	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	storageFile := filepath.Join(*dataDir, fmt.Sprintf("%s.parq", *nodeID))
	store := storage.NewUnifiedStorage(storageFile)
	defer store.Close()

	// Initialize cluster manager (gossip protocol + TCP server)
	localNode := &models.NodeInfo{
		ID:       *nodeID,
		Address:  fmt.Sprintf("localhost:%d", actualTCPPort),
		HTTPPort: actualHTTPPort,
		MQTTPort: 1883,
		Status:   "active",
	}

	seedNodesList := []string{}
	if *seedNodes != "" {
		seedNodesList = strings.Split(*seedNodes, ",")
	}

	clusterMgr := cluster.GetClusterManager()
	if err := clusterMgr.Initialize(localNode, actualTCPPort, seedNodesList); err != nil {
		log.Fatalf("Failed to initialize cluster manager: %v", err)
	}
	defer clusterMgr.Stop()

	log.Printf("[%s] Cluster manager initialized (TCP server on port %d)", *nodeID, actualTCPPort)

	mqttOpts := mqttclient.Options{
		BrokerURL: *broker,
		ClientID:  fmt.Sprintf("minitrue-%s-%d", *nodeID, time.Now().UnixNano()),
	}
	mqttc, err := mqttclient.New(mqttOpts)
	if err != nil {
		log.Fatalf("MQTT client error: %v", err)
	}
	defer mqttc.Close()

	switch *mode {
	case "ingestion":
		ing := ingestion.New(mqttc, store, *nodeID)
		ing.Start()
		log.Printf("[%s] Ingestion service started", *nodeID)
	case "query":
		q := query.NewWithNodeID(mqttc, store, *nodeID)
		go q.StartHTTP(actualHTTPPort)
		log.Printf("[%s] Query HTTP server running on :%d", *nodeID, actualHTTPPort)
	case "all":
		ing := ingestion.New(mqttc, store, *nodeID)
		ing.Start()
		log.Printf("[%s] Ingestion service started", *nodeID)
		
		q := query.NewWithNodeID(mqttc, store, *nodeID)
		go q.StartHTTP(actualHTTPPort)
		log.Printf("[%s] Query HTTP server running on :%d", *nodeID, actualHTTPPort)
	default:
		log.Fatalf("Unknown mode: %s (must be: ingestion, query, or all)", *mode)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	log.Printf("[%s] Shutting down...", *nodeID)
}

// getDefaultTCPPort returns a default TCP port based on node ID
// ing1 -> 9000, ing2 -> 9001, ing3 -> 9002, etc.
func getDefaultTCPPort(nodeID string) int {
	if len(nodeID) >= 3 && nodeID[:3] == "ing" {
		if len(nodeID) > 3 {
			numStr := nodeID[3:]
			if num, err := strconv.Atoi(numStr); err == nil {
				return 9000 + num - 1
			}
		}
	}
	// Default fallback
	return 9000
}

// getDefaultHTTPPort returns a default HTTP port based on node ID
// ing1 -> 8080, ing2 -> 8081, ing3 -> 8082, etc.
func getDefaultHTTPPort(nodeID string) int {
	if len(nodeID) >= 3 && nodeID[:3] == "ing" {
		if len(nodeID) > 3 {
			numStr := nodeID[3:]
			if num, err := strconv.Atoi(numStr); err == nil {
				return 8080 + num - 1
			}
		}
	}
	// Default fallback
	return 8080
}