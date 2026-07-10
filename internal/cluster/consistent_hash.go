package cluster

import (
	"fmt"
	"sort"
	"sync"

	"github.com/spaolacci/murmur3"
)

type ConsistentHashRing struct {
	ring         map[uint32]string
	sortedHashes []uint32
	virtualNodes int
	nodes        map[string]bool
	hashFunc     func([]byte) uint32
	mu           sync.RWMutex
}

func NewConsistentHashRing(virtualNodes int) *ConsistentHashRing {
	if virtualNodes <= 0 {
		virtualNodes = 150
	}

	return &ConsistentHashRing{
		ring:         make(map[uint32]string),
		sortedHashes: make([]uint32, 0),
		virtualNodes: virtualNodes,
		nodes:        make(map[string]bool),
		// Changing the hash function changes key ownership and requires a full
		// cluster data rebalance before a live upgrade. This is expected to be
		// paired with the segment migration path before production rollout.
		hashFunc: murmur3.Sum32,
	}
}

func (chr *ConsistentHashRing) AddNode(nodeID string) {
	chr.mu.Lock()
	defer chr.mu.Unlock()

	if chr.nodes[nodeID] {
		return
	}

	chr.nodes[nodeID] = true

	for i := 0; i < chr.virtualNodes; i++ {
		virtualKey := fmt.Sprintf("%s#%d", nodeID, i)
		hash := chr.hashKey(virtualKey)
		chr.ring[hash] = nodeID
		chr.sortedHashes = append(chr.sortedHashes, hash)
	}

	sort.Slice(chr.sortedHashes, func(i, j int) bool {
		return chr.sortedHashes[i] < chr.sortedHashes[j]
	})
}

func (chr *ConsistentHashRing) RemoveNode(nodeID string) {
	chr.mu.Lock()
	defer chr.mu.Unlock()

	if !chr.nodes[nodeID] {
		return
	}

	delete(chr.nodes, nodeID)

	newHashes := make([]uint32, 0)
	for _, hash := range chr.sortedHashes {
		if chr.ring[hash] != nodeID {
			newHashes = append(newHashes, hash)
		} else {
			delete(chr.ring, hash)
		}
	}

	chr.sortedHashes = newHashes
}



func (chr *ConsistentHashRing) GetNodes(key string, count int) ([]string, error) {
	chr.mu.RLock()
	defer chr.mu.RUnlock()

	if len(chr.nodes) == 0 {
		return nil, fmt.Errorf("no nodes in ring")
	}

	if count > len(chr.nodes) {
		count = len(chr.nodes)
	}

	hash := chr.hashKey(key)

	idx := sort.Search(len(chr.sortedHashes), func(i int) bool {
		return chr.sortedHashes[i] >= hash
	})

	if idx == len(chr.sortedHashes) {
		idx = 0
	}

	nodesMap := make(map[string]bool)
	nodes := make([]string, 0, count)

	for len(nodes) < count && len(nodesMap) < len(chr.nodes) {
		nodeID := chr.ring[chr.sortedHashes[idx]]
		if !nodesMap[nodeID] {
			nodesMap[nodeID] = true
			nodes = append(nodes, nodeID)
		}
		idx = (idx + 1) % len(chr.sortedHashes)
	}

	return nodes, nil
}

func (chr *ConsistentHashRing) GetAllNodes() []string {
	chr.mu.RLock()
	defer chr.mu.RUnlock()

	nodes := make([]string, 0, len(chr.nodes))
	for nodeID := range chr.nodes {
		nodes = append(nodes, nodeID)
	}

	return nodes
}

// GetNodesSnapshot returns a snapshot of nodes for a key, capturing ring version.
// This ensures consistent routing for a single operation even if ring changes mid-operation.
func (chr *ConsistentHashRing) GetNodesSnapshot(key string, count int) ([]string, uint64, error) {
	chr.mu.RLock()
	sortedHashesCopy := make([]uint32, len(chr.sortedHashes))
	copy(sortedHashesCopy, chr.sortedHashes)
	ringCopy := make(map[uint32]string)
	for k, v := range chr.ring {
		ringCopy[k] = v
	}
	nodesCopy := make(map[string]bool)
	for k := range chr.nodes {
		nodesCopy[k] = true
	}
	ringVersion := uint64(len(chr.sortedHashes)) // Use ring size as a version indicator
	chr.mu.RUnlock()

	if len(nodesCopy) == 0 {
		return nil, 0, fmt.Errorf("no nodes in ring")
	}

	if count > len(nodesCopy) {
		count = len(nodesCopy)
	}

	hash := chr.hashFunc([]byte(key))

	idx := sort.Search(len(sortedHashesCopy), func(i int) bool {
		return sortedHashesCopy[i] >= hash
	})

	if idx == len(sortedHashesCopy) {
		idx = 0
	}

	nodesMap := make(map[string]bool)
	nodes := make([]string, 0, count)

	for len(nodes) < count && len(nodesMap) < len(nodesCopy) {
		nodeID := ringCopy[sortedHashesCopy[idx]]
		if !nodesMap[nodeID] {
			nodesMap[nodeID] = true
			nodes = append(nodes, nodeID)
		}
		idx = (idx + 1) % len(sortedHashesCopy)
	}

	return nodes, ringVersion, nil
}

func (chr *ConsistentHashRing) hashKey(key string) uint32 {
	return chr.hashFunc([]byte(key))
}


