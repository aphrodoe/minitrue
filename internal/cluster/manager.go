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

// ClusterManager manages the cluster coordination (gossip + hash ring)
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

// GetClusterManager returns the global cluster manager instance
func GetClusterManager() *ClusterManager {
	clusterManagerOnce.Do(func() {
		globalClusterManager = &ClusterManager{}
	})
	return globalClusterManager
}

// Initialize initializes the cluster manager with gossip protocol and TCP server
func (cm *ClusterManager) Initialize(localNode *models.NodeInfo, tcpPort int, seedNodes []string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Initialize hash ring
	cm.hashRing = GetHashRing()
	if cm.hashRing == nil {
		cm.hashRing = cluster.NewConsistentHashRing(150)
		SetHashRing(cm.hashRing)
	}

	// Add local node to hash ring
	cm.hashRing.AddNode(localNode.ID)

	// Create network client
	networkClient := network.NewClient(5 * time.Second)

	// Create gossip protocol
	cm.gossipProtocol = cluster.NewGossipProtocol(
		localNode,
		2*time.Second, // gossip interval
		networkClient,
		3, // replication factor
	)

	// Add local node to gossip cluster state
	cm.gossipProtocol.Start()

	// Create message handler
	messageHandler := NewMessageHandler(cm.gossipProtocol, cm.onNodeUpdate)

	// Create TCP server
	tcpAddress := fmt.Sprintf(":%d", tcpPort)
	cm.server = network.NewServer(tcpAddress, messageHandler)

	// Start TCP server
	if err := cm.server.Start(); err != nil {
		return fmt.Errorf("failed to start TCP server: %w", err)
	}

	// Try to connect to seed nodes
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

	return nil
}

// onNodeUpdate is called when nodes are added or removed
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

// GetGossipProtocol returns the gossip protocol instance
func (cm *ClusterManager) GetGossipProtocol() *cluster.GossipProtocol {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.gossipProtocol
}

// GetHashRing returns the hash ring
func (cm *ClusterManager) GetHashRing() *cluster.ConsistentHashRing {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.hashRing
}

// Stop stops the cluster manager
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

// GetNodeHTTPPort returns the HTTP port for a node ID
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

