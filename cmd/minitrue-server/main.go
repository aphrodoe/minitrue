package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/minitrue/internal/ingestion"
	"github.com/minitrue/internal/mqttclient"
	"github.com/minitrue/internal/query"
	"github.com/minitrue/internal/storage"
)

func main() {
	mode := flag.String("mode", "ingestion", "mode: ingestion | query | all")
	nodeID := flag.String("node_id", "ing1", "node identifier (must be unique)")
	port := flag.Int("port", 8080, "HTTP port for query server")
	broker := flag.String("broker", "tcp://localhost:1883", "MQTT broker URL")
	dataDir := flag.String("data_dir", "data", "directory for storing data files")
	flag.Parse()

	log.Printf("Starting node %s in mode=%s (broker=%s)", *nodeID, *mode, *broker)

	// Ensure data directory exists
	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Initialize unified storage with Gorilla compression
	storageFile := filepath.Join(*dataDir, fmt.Sprintf("%s.parq", *nodeID))
	store := storage.NewUnifiedStorage(storageFile)
	defer store.Close()

	// MQTT client
	mqttOpts := mqttclient.Options{
		BrokerURL: *broker,
		ClientID:  fmt.Sprintf("minitrue-%s-%d", *nodeID, time.Now().UnixNano()),
	}
	mqttc, err := mqttclient.New(mqttOpts)
	if err != nil {
		log.Fatalf("MQTT client error: %v", err)
	}
	defer mqttc.Close()

	// Start services based on mode
	switch *mode {
	case "ingestion":
		ing := ingestion.New(mqttc, store, *nodeID)
		ing.Start()
		log.Printf("[%s] Ingestion service started", *nodeID)
	case "query":
		q := query.New(mqttc, store)
		go q.StartHTTP(*port)
		log.Printf("[%s] Query HTTP server running on :%d", *nodeID, *port)
	case "all":
		ing := ingestion.New(mqttc, store, *nodeID)
		ing.Start()
		log.Printf("[%s] Ingestion service started", *nodeID)
		
		q := query.New(mqttc, store)
		go q.StartHTTP(*port)
		log.Printf("[%s] Query HTTP server running on :%d", *nodeID, *port)
	default:
		log.Fatalf("Unknown mode: %s (must be: ingestion, query, or all)", *mode)
	}

	// Wait for shutdown signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	log.Printf("[%s] Shutting down...", *nodeID)
}