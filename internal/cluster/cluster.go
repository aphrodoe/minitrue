package cluster

import (
	"github.com/minitrue/pkg/cluster"
)

var (
	// Global consistent hash ring instance
	hashRing *cluster.ConsistentHashRing
)

func init() {
	// Initialize the consistent hash ring with default virtual nodes
	hashRing = cluster.NewConsistentHashRing(150)
	
	// Add default nodes (these can be overridden by gossip protocol)
	hashRing.AddNode("ing1")
	hashRing.AddNode("ing2")
	hashRing.AddNode("ing3")
}

// GetPrimaryNode returns the node id (string) that is the PRIMARY for a given deviceID.
// Uses consistent hashing to determine the primary node.
func GetPrimaryNode(deviceID string) string {
	node, err := hashRing.GetNode(deviceID)
	if err != nil {
		// Fallback to first node if ring is empty
		return "ing1"
	}
	return node
}

// GetHashRing returns the global hash ring instance (for cluster management)
func GetHashRing() *cluster.ConsistentHashRing {
	return hashRing
}

// AddNode adds a node to the consistent hash ring
func AddNode(nodeID string) {
	hashRing.AddNode(nodeID)
}

// RemoveNode removes a node from the consistent hash ring
func RemoveNode(nodeID string) {
	hashRing.RemoveNode(nodeID)
}