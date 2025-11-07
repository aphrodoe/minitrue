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

// GetNodesForKey returns an ordered list of nodes (primary first) for a key.
// The list length will be up to replicationFactor, bounded by ring size.
func GetNodesForKey(key string, replicationFactor int) []string {
	nodes, err := hashRing.GetNodes(key, replicationFactor)
	if err != nil || len(nodes) == 0 {
		return []string{"ing1"}
	}
	return nodes
}

// GetHashRing returns the global hash ring instance (for cluster management)
func GetHashRing() *cluster.ConsistentHashRing {
	return hashRing
}

// SetHashRing sets the global hash ring instance (used by cluster manager)
func SetHashRing(ring *cluster.ConsistentHashRing) {
	hashRing = ring
}

// AddNode adds a node to the consistent hash ring
func AddNode(nodeID string) {
	hashRing.AddNode(nodeID)
}

// RemoveNode removes a node from the consistent hash ring
func RemoveNode(nodeID string) {
	hashRing.RemoveNode(nodeID)
}
