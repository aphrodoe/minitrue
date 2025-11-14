package cluster

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/minitrue/pkg/cluster"
	"github.com/minitrue/pkg/models"
	"github.com/minitrue/pkg/network"
)

type ClusterManager struct {
	gossipProtocol *cluster.GossipProtocol
	hashRing       *cluster.ConsistentHashRing
	server         *network.Server
	mu             sync.RWMutex
}

var (
	globalClusterManager *ClusterManager
	clusterManagerOnce   sync.Once
)

func GetClusterManager() *ClusterManager {
	clusterManagerOnce.Do(func() {
		globalClusterManager = &ClusterManager{}
	})
	return globalClusterManager
}

func (cm *ClusterManager) Initialize(localNode *models.NodeInfo, tcpPort int, seedNodes []string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.hashRing = GetHashRing()
	if cm.hashRing == nil {
		cm.hashRing = cluster.NewConsistentHashRing(150)
		SetHashRing(cm.hashRing)
	}

	cm.hashRing.AddNode(localNode.ID)

	networkClient := network.NewClient(5 * time.Second)

	cm.gossipProtocol = cluster.NewGossipProtocol(
		localNode,
		2*time.Second,
		networkClient,
		3,
	)

	cm.gossipProtocol.Start()

	messageHandler := NewMessageHandler(cm.gossipProtocol, cm.onNodeUpdate)

	tcpAddress := fmt.Sprintf(":%d", tcpPort)
	cm.server = network.NewServer(tcpAddress, messageHandler)

	if err := cm.server.Start(); err != nil {
		return fmt.Errorf("failed to start TCP server: %w", err)
	}

	for _, seedAddr := range seedNodes {
		if seedAddr != "" {
			go func(addr string) {
				if err := cm.gossipProtocol.AddSeedNode(addr); err != nil {
					log.Printf("[Cluster] Failed to connect to seed node %s: %v", addr, err)
				} else {
					log.Printf("[Cluster] Connected to seed node %s", addr)
				}
			}(seedAddr)
		}
	}

	go cm.syncHashRingLoop()

	return nil
}

func (cm *ClusterManager) syncHashRingLoop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		cm.syncHashRing()
	}
}

func (cm *ClusterManager) syncHashRing() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.gossipProtocol == nil || cm.hashRing == nil {
		return
	}

	clusterState := cm.gossipProtocol.GetClusterState()
	
	ringNodes := make(map[string]bool)
	for _, nodeID := range cm.hashRing.GetAllNodes() {
		ringNodes[nodeID] = true
	}

	for nodeID, nodeInfo := range clusterState.Nodes {
		if nodeInfo.Status == "active" && !ringNodes[nodeID] {
			cm.hashRing.AddNode(nodeID)
			log.Printf("[Cluster] Synced: Added node %s to hash ring", nodeID)
			ringNodes[nodeID] = true
		} else if nodeInfo.Status == "down" && ringNodes[nodeID] {
			cm.hashRing.RemoveNode(nodeID)
			log.Printf("[Cluster] Synced: Removed node %s from hash ring (down)", nodeID)
			delete(ringNodes, nodeID)
		}
	}
}

func (cm *ClusterManager) onNodeUpdate(nodeID string, add bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.hashRing == nil {
		return
	}

	if add {
		cm.hashRing.AddNode(nodeID)
		log.Printf("[Cluster] Node %s added to hash ring", nodeID)
	} else {
		cm.hashRing.RemoveNode(nodeID)
		log.Printf("[Cluster] Node %s removed from hash ring", nodeID)
	}
}

func (cm *ClusterManager) GetGossipProtocol() *cluster.GossipProtocol {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.gossipProtocol
}

func (cm *ClusterManager) GetHashRing() *cluster.ConsistentHashRing {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.hashRing
}

func (cm *ClusterManager) Stop() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.gossipProtocol != nil {
		cm.gossipProtocol.Stop()
	}
	if cm.server != nil {
		return cm.server.Stop()
	}
	return nil
}

func (cm *ClusterManager) GetNodeHTTPPort(nodeID string) (int, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.gossipProtocol == nil {
		return 0, fmt.Errorf("gossip protocol not initialized")
	}

	node := cm.gossipProtocol.GetNodeByID(nodeID)
	if node == nil {
		return 0, fmt.Errorf("node %s not found", nodeID)
	}

	return node.HTTPPort, nil
}

