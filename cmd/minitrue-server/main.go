package main

import (
    "flag"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/minitrue/internal/cluster"
    "github.com/minitrue/internal/ingestion"
    "github.com/minitrue/internal/models"
    "github.com/minitrue/internal/mqttclient"
    "github.com/minitrue/internal/query"
    "github.com/minitrue/internal/storage"
)

func main() {
    mode := flag.String("mode", "ingestion", "mode: ingestion | query | all")
    nodeID := flag.String("node_id", "", "node identifier")
    port := flag.Int("port", 8080, "HTTP port")
    tcpPort := flag.Int("tcp_port", 9000, "TCP port")
    broker := flag.String("broker", "tcp://localhost:1883", "MQTT broker URL")
    dataDir := flag.String("data_dir", "data", "data directory")
    seedNodes := flag.String("seeds", "", "seed nodes")
    flag.Parse()

    _ = seedNodes
    _ = os.MkdirAll(*dataDir, 0755)
    store := storage.NewUnifiedStorage(fmt.Sprintf("%s/%s.parq", *dataDir, *nodeID))
    defer store.Close()

    localNode := &models.NodeInfo{ID: *nodeID, Address: fmt.Sprintf("localhost:%d", *tcpPort), HTTPPort: *port, MQTTPort: 1883, Status: "active"}
    clusterMgr := cluster.GetClusterManager()
    if err := clusterMgr.Initialize(localNode, *tcpPort, nil); err != nil { log.Fatalf("cluster init: %v", err) }
    defer clusterMgr.Stop()

    mqttc, err := mqttclient.New(mqttclient.Options{BrokerURL: *broker, ClientID: fmt.Sprintf("minitrue-%d", time.Now().UnixNano())})
    if err != nil { log.Fatalf("MQTT client error: %v", err) }
    defer mqttc.Close()

    switch *mode {
    case "ingestion":
        ingestion.New(mqttc, store, *nodeID).Start()
    case "query":
        go query.NewWithRestart(mqttc, store, *nodeID, nil).StartHTTP(*port)
    case "all":
        ingestion.New(mqttc, store, *nodeID).Start()
        go query.NewWithRestart(mqttc, store, *nodeID, nil).StartHTTP(*port)
    }

    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    <-c
}
