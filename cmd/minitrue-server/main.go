package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
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
	port := flag.Int("port", 8080, "HTTP port for query server")
	tcpPort := flag.Int("tcp_port", 9000, "TCP port for internode communication")
	broker := flag.String("broker", "tcp://localhost:1883", "MQTT broker URL")
	dataDir := flag.String("data_dir", "data", "directory for storing data files")
	seedNodes := flag.String("seeds", "", "comma-separated list of seed node addresses (e.g., localhost:9001,localhost:9002)")
	flag.Parse()

	log.Printf("Starting node %s in mode=%s (broker=%s, tcp_port=%d)", *nodeID, *mode, *broker, *tcpPort)

	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	storageFile := filepath.Join(*dataDir, fmt.Sprintf("%s.parq", *nodeID))
	store := storage.NewUnifiedStorage(storageFile)
	defer store.Close()

	// Initialize cluster manager (gossip protocol + TCP server)
	localNode := &models.NodeInfo{
		ID:       *nodeID,
		Address:  fmt.Sprintf("localhost:%d", *tcpPort),
		HTTPPort: *port,
		MQTTPort: 1883,
		Status:   "active",
	}

	seedNodesList := []string{}
	if *seedNodes != "" {
		seedNodesList = strings.Split(*seedNodes, ",")
	}

	clusterMgr := cluster.GetClusterManager()
	if err := clusterMgr.Initialize(localNode, *tcpPort, seedNodesList); err != nil {
		log.Fatalf("Failed to initialize cluster manager: %v", err)
	}
	defer clusterMgr.Stop()

	log.Printf("[%s] Cluster manager initialized (TCP server on port %d)", *nodeID, *tcpPort)

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
		go q.StartHTTP(*port)
		log.Printf("[%s] Query HTTP server running on :%d", *nodeID, *port)
	case "all":
		ing := ingestion.New(mqttc, store, *nodeID)
		ing.Start()
		log.Printf("[%s] Ingestion service started", *nodeID)
		
		q := query.NewWithNodeID(mqttc, store, *nodeID)
		go q.StartHTTP(*port)
		log.Printf("[%s] Query HTTP server running on :%d", *nodeID, *port)
	default:
		log.Fatalf("Unknown mode: %s (must be: ingestion, query, or all)", *mode)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	log.Printf("[%s] Shutting down...", *nodeID)
}