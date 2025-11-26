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
    port := flag.Int("port", 0, "HTTP port")
    tcpPort := flag.Int("tcp_port", 0, "TCP port")
    broker := flag.String("broker", "tcp://localhost:1883", "MQTT broker URL")
    dataDir := flag.String("data_dir", "data", "data directory")
    seedNodes := flag.String("seeds", "", "seed nodes")
    flag.Parse()

    _ = seedNodes
    _ = os.MkdirAll(*dataDir, 0755)
    store := storage.NewUnifiedStorage(fmt.Sprintf("%s/%s.parq", *dataDir, *nodeID))
    defer store.Close()

    resolvedNodeID := *nodeID
    if resolvedNodeID == "" { resolvedNodeID = "polaris" }
    actualHTTPPort := *port
    if actualHTTPPort == 0 { actualHTTPPort = getDefaultHTTPPort(resolvedNodeID) }
    actualTCPPort := *tcpPort
    if actualTCPPort == 0 { actualTCPPort = getDefaultTCPPort(resolvedNodeID) }
    localNode := &models.NodeInfo{ID: resolvedNodeID, Address: fmt.Sprintf("localhost:%d", actualTCPPort), HTTPPort: actualHTTPPort, MQTTPort: 1883, Status: "active"}
    clusterMgr := cluster.GetClusterManager()
    if err := clusterMgr.Initialize(localNode, actualTCPPort, nil); err != nil { log.Fatalf("cluster init: %v", err) }
    defer clusterMgr.Stop()

    mqttc, err := mqttclient.New(mqttclient.Options{BrokerURL: *broker, ClientID: fmt.Sprintf("minitrue-%d", time.Now().UnixNano())})
    if err != nil { log.Fatalf("MQTT client error: %v", err) }
    defer mqttc.Close()

    switch *mode {
    case "ingestion":
        ingestion.New(mqttc, store, resolvedNodeID).Start()
    case "query":
        go query.NewWithRestart(mqttc, store, resolvedNodeID, nil).StartHTTP(actualHTTPPort)
    case "all":
        ingestion.New(mqttc, store, resolvedNodeID).Start()
        go query.NewWithRestart(mqttc, store, resolvedNodeID, nil).StartHTTP(actualHTTPPort)
    }

    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    <-c
}

func getDefaultTCPPort(nodeID string) int {
	switch nodeID {
	case "polaris":
		return 9000
	case "sirius":
		return 9001
	case "vega":
		return 9002
	}
	return 9000
}

func getDefaultHTTPPort(nodeID string) int {
	switch nodeID {
	case "polaris":
		return 8080
	case "sirius":
		return 8081
	case "vega":
		return 8082
	}
	return 8080
}
