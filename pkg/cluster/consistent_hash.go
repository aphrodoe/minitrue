package cluster

import (
    "fmt"
    "hash/crc32"
    "sort"
    "sync"
)

// ConsistentHashRing implements consistent hashing with virtual nodes
type ConsistentHashRing struct {
    ring         map[uint32]string // hash -> nodeID
    sortedHashes []uint32           // sorted list of all hash positions
    virtualNodes int                // number of virtual nodes per physical node
    nodes        map[string]bool    // set of all physical nodes
    mu           sync.RWMutex       // read-write lock for thread safety
}

// NewConsistentHashRing creates a new consistent hash ring
func NewConsistentHashRing(virtualNodes int) *ConsistentHashRing {
    if virtualNodes <= 0 {
        virtualNodes = 150 // Default value
    }
    
    return &ConsistentHashRing{
        ring:         make(map[uint32]string),
        sortedHashes: make([]uint32, 0),
        virtualNodes: virtualNodes,
        nodes:        make(map[string]bool),
    }
}

// AddNode adds a node to the ring
func (chr *ConsistentHashRing) AddNode(nodeID string) {
    chr.mu.Lock()
    defer chr.mu.Unlock()

    if chr.nodes[nodeID] {
        return // Already exists
    }

    chr.nodes[nodeID] = true

    // Add virtual nodes
    for i := 0; i < chr.virtualNodes; i++ {
        virtualKey := fmt.Sprintf("%s#%d", nodeID, i)
        hash := chr.hashKey(virtualKey)
        chr.ring[hash] = nodeID
        chr.sortedHashes = append(chr.sortedHashes, hash)
    }

    // Sort hashes
    sort.Slice(chr.sortedHashes, func(i, j int) bool {
        return chr.sortedHashes[i] < chr.sortedHashes[j]
    })
}

// RemoveNode removes a node from the ring
func (chr *ConsistentHashRing) RemoveNode(nodeID string) {
    chr.mu.Lock()
    defer chr.mu.Unlock()

    if !chr.nodes[nodeID] {
        return // Doesn't exist
    }

    delete(chr.nodes, nodeID)

    // Remove virtual nodes
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

// GetNode returns the node responsible for a key
func (chr *ConsistentHashRing) GetNode(key string) (string, error) {
    chr.mu.RLock()
    defer chr.mu.RUnlock()

    if len(chr.ring) == 0 {
        return "", fmt.Errorf("no nodes in ring")
    }

    hash := chr.hashKey(key)

    // Binary search for the first node with hash >= key hash
    idx := sort.Search(len(chr.sortedHashes), func(i int) bool {
        return chr.sortedHashes[i] >= hash
    })

    // Wrap around if necessary
    if idx == len(chr.sortedHashes) {
        idx = 0
    }

    return chr.ring[chr.sortedHashes[idx]], nil
}

// GetNodes returns N nodes responsible for a key (for replication)
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

    // Find starting position
    idx := sort.Search(len(chr.sortedHashes), func(i int) bool {
        return chr.sortedHashes[i] >= hash
    })

    if idx == len(chr.sortedHashes) {
        idx = 0
    }

    // Collect unique nodes
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

// GetAllNodes returns all nodes in the ring
func (chr *ConsistentHashRing) GetAllNodes() []string {
    chr.mu.RLock()
    defer chr.mu.RUnlock()

    nodes := make([]string, 0, len(chr.nodes))
    for nodeID := range chr.nodes {
        nodes = append(nodes, nodeID)
    }

    return nodes
}

// hashKey computes the hash of a key
func (chr *ConsistentHashRing) hashKey(key string) uint32 {
    return crc32.ChecksumIEEE([]byte(key))
}

// Size returns the number of nodes in the ring
func (chr *ConsistentHashRing) Size() int {
    chr.mu.RLock()
    defer chr.mu.RUnlock()
    return len(chr.nodes)
}