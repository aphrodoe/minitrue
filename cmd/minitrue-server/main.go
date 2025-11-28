package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/minitrue/internal/cluster"
	"github.com/minitrue/internal/ingestion"
	"github.com/minitrue/internal/models"
	"github.com/minitrue/internal/mqttclient"
	"github.com/minitrue/internal/query"
	"github.com/minitrue/internal/storage"
)

type nodeSlot struct {
	ID       string
	HTTPPort int
	TCPPort  int
}

var defaultNodeSlots = []nodeSlot{
	{ID: "polaris", HTTPPort: 8080, TCPPort: 9000},
	{ID: "sirius", HTTPPort: 8081, TCPPort: 9001},
	{ID: "vega", HTTPPort: 8082, TCPPort: 9002},
}

func main() {
	mode := flag.String("mode", "ingestion", "mode: ingestion | query | all")
	nodeID := flag.String("node_id", "", "node identifier (leave empty to auto-assign a local cluster slot)")
	port := flag.Int("port", 0, "HTTP port for query server (0 = auto-assign based on node ID)")
	tcpPort := flag.Int("tcp_port", 0, "TCP port for internode communication (0 = auto-assign based on node ID)")
	broker := flag.String("broker", "tcp://localhost:1883", "MQTT broker URL")
	dataDir := flag.String("data_dir", "data", "directory for storing data files")
	seedNodes := flag.String("seeds", "", "comma-separated list of peer node addresses (leave empty for symmetric local peer discovery)")
	flag.Parse()

	originalArgs := os.Args
	executable, err := os.Executable()
	if err != nil {
		executable = os.Args[0]
	}

	restartFn := func() {
		log.Printf("[Restart] Restarting server...")

		var cmd *exec.Cmd

		execPath := strings.ToLower(executable)
		if strings.Contains(execPath, "go-build") ||
			strings.Contains(execPath, filepath.Join("tmp", "go-build")) ||
			strings.Contains(execPath, filepath.Join("var", "folders")) {
			args := []string{"run", "cmd/minitrue-server/main.go"}
			args = append(args, originalArgs[1:]...)
			cmd = exec.Command("go", args...)
		} else {
			cmd = exec.Command(executable, originalArgs[1:]...)
		}

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Start(); err != nil {
			log.Printf("[Restart] Failed to restart: %v", err)
			return
		}

		time.Sleep(200 * time.Millisecond)

		log.Printf("[Restart] Exiting current process...")
		os.Exit(0)
	}

	resolvedNodeID, actualHTTPPort, actualTCPPort, err := resolveLocalNodeConfig(*nodeID, *port, *tcpPort)
	if err != nil {
		log.Fatalf("Failed to resolve node configuration: %v", err)
	}

	seedNodesList := resolveSeedNodes(*seedNodes, actualTCPPort)

	log.Printf("Starting node %s in mode=%s (broker=%s, tcp_port=%d, http_port=%d, peers=%v)", resolvedNodeID, *mode, *broker, actualTCPPort, actualHTTPPort, seedNodesList)

	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	storageFile := filepath.Join(*dataDir, fmt.Sprintf("%s.parq", resolvedNodeID))
	store := storage.NewUnifiedStorage(storageFile)
	defer store.Close()

	localNode := &models.NodeInfo{
		ID:       resolvedNodeID,
		Address:  fmt.Sprintf("localhost:%d", actualTCPPort),
		HTTPPort: actualHTTPPort,
		MQTTPort: 1883,
		Status:   "active",
	}

	clusterMgr := cluster.GetClusterManager()
	if err := clusterMgr.Initialize(localNode, actualTCPPort, seedNodesList); err != nil {
		log.Fatalf("Failed to initialize cluster manager: %v", err)
	}
	defer clusterMgr.Stop()

	log.Printf("[%s] Cluster manager initialized (TCP server on port %d)", resolvedNodeID, actualTCPPort)

	mqttOpts := mqttclient.Options{
		BrokerURL: *broker,
		ClientID:  fmt.Sprintf("minitrue-%s-%d", resolvedNodeID, time.Now().UnixNano()),
	}
	mqttc, err := mqttclient.New(mqttOpts)
	if err != nil {
		log.Fatalf("MQTT client error: %v", err)
	}
	defer mqttc.Close()

	switch *mode {
	case "ingestion":
		ing := ingestion.New(mqttc, store, resolvedNodeID)
		ing.Start()
		log.Printf("[%s] Ingestion service started", resolvedNodeID)
	case "query":
		q := query.NewWithRestart(mqttc, store, resolvedNodeID, restartFn)
		go q.StartHTTP(actualHTTPPort)
		log.Printf("[%s] Query HTTP server running on :%d", resolvedNodeID, actualHTTPPort)
	case "all":
		ing := ingestion.New(mqttc, store, resolvedNodeID)
		ing.Start()
		log.Printf("[%s] Ingestion service started", resolvedNodeID)

		q := query.NewWithRestart(mqttc, store, resolvedNodeID, restartFn)
		go q.StartHTTP(actualHTTPPort)
		log.Printf("[%s] Query HTTP server running on :%d", resolvedNodeID, actualHTTPPort)
	default:
		log.Fatalf("Unknown mode: %s (must be: ingestion, query, or all)", *mode)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	log.Printf("[%s] Shutting down...", resolvedNodeID)
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

func resolveLocalNodeConfig(nodeID string, httpPort, tcpPort int) (string, int, int, error) {
	if nodeID != "" {
		actualHTTPPort := httpPort
		if actualHTTPPort == 0 {
			actualHTTPPort = getDefaultHTTPPort(nodeID)
		}

		actualTCPPort := tcpPort
		if actualTCPPort == 0 {
			actualTCPPort = getDefaultTCPPort(nodeID)
		}

		return nodeID, actualHTTPPort, actualTCPPort, nil
	}

	if httpPort != 0 || tcpPort != 0 {
		return "", 0, 0, fmt.Errorf("explicit port overrides require --node_id so the node identity stays stable")
	}

	for _, slot := range defaultNodeSlots {
		if isPortInUse(slot.TCPPort) || isPortInUse(slot.HTTPPort) {
			continue
		}
		return slot.ID, slot.HTTPPort, slot.TCPPort, nil
	}

	return "", 0, 0, fmt.Errorf("no free default local node slots available; specify --node_id/--port/--tcp_port explicitly")
}

func resolveSeedNodes(seedNodes string, localTCPPort int) []string {
	if seedNodes != "" {
		parts := strings.Split(seedNodes, ",")
		resolved := make([]string, 0, len(parts))
		for _, part := range parts {
			addr := strings.TrimSpace(part)
			if addr != "" {
				resolved = append(resolved, addr)
			}
		}
		return resolved
	}

	autoSeeds := make([]string, 0, len(defaultNodeSlots)-1)
	for _, slot := range defaultNodeSlots {
		if slot.TCPPort == localTCPPort {
			continue
		}
		autoSeeds = append(autoSeeds, fmt.Sprintf("localhost:%d", slot.TCPPort))
	}
	return autoSeeds
}

func isPortInUse(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err == nil {
		conn.Close()
		return true
	}
	return false
}
