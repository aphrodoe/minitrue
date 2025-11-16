package cluster

import (
	"github.com/minitrue/pkg/cluster"
)

var (
	hashRing *cluster.ConsistentHashRing
)

func init() {
	hashRing = cluster.NewConsistentHashRing(150)
}

func GetPrimaryNode(deviceID string) string {
	node, err := hashRing.GetNode(deviceID)
	if err != nil {
		return "ing1"
	}
	return node
}

func GetNodesForKey(key string, replicationFactor int) []string {
	nodes, err := hashRing.GetNodes(key, replicationFactor)
	if err != nil || len(nodes) == 0 {
		return []string{"ing1"}
	}
	return nodes
}

func GetHashRing() *cluster.ConsistentHashRing {
	return hashRing
}

func SetHashRing(ring *cluster.ConsistentHashRing) {
	hashRing = ring
}

func AddNode(nodeID string) {
	hashRing.AddNode(nodeID)
}

func RemoveNode(nodeID string) {
	hashRing.RemoveNode(nodeID)
}
