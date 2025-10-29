package main

import (
    "flag"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    "dsa-leadd/internal/ingestion"
    "dsa-leadd/internal/mqttclient"
    "dsa-leadd/internal/query"
    "dsa-leadd/internal/storage"
)

func main() {
    mode := flag.String("mode", "ingestion", "mode: ingestion | query | output | all")
    nodeID := flag.String("node_id", "ing1", "node identifier (must be unique)")
    port := flag.Int("port", 8080, "HTTP port for query server")
    broker := flag.String("broker", "tcp://localhost:1883", "MQTT broker URL")
    flag.Parse()

    log.Printf("Starting node %s in mode=%s (broker=%s)\n", *nodeID, *mode, *broker)

    // Storage (mock). Replace with real storage provider later.
    store := storage.NewMockStorage()

    // MQTT client
    mqttOpts := mqttclient.Options{
        BrokerURL: *broker,
        ClientID:  fmt.Sprintf("leadD-%s-%d", *nodeID, time.Now().UnixNano()),
    }
    mqttc, err := mqttclient.New(mqttOpts)
    if err != nil {
        log.Fatalf("mqtt client error: %v", err)
    }
    defer mqttc.Close()

    switch *mode {
    case "ingestion":
        ing := ingestion.New(mqttc, store, *nodeID)
        ing.Start()
    case "query":
        q := query.New(mqttc, store)
        go q.StartHTTP(*port)
        log.Printf("Query HTTP server running on :%d\n", *port)
    case "output":
        log.Println("Output mode: no special behavior implemented in this sample.")
    case "all":
        ing := ingestion.New(mqttc, store, *nodeID)
        ing.Start()
        q := query.New(mqttc, store)
        go q.StartHTTP(*port)
        log.Printf("HTTP server running on :%d", *port)
    default:
        log.Fatalf("unknown mode %s", *mode)
    }

    // wait for shutdown
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    <-c
    log.Println("Shutting down...")
}
