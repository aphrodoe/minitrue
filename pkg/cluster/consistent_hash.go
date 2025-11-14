package cluster

import (
    "fmt"
    "hash/crc32"
    "sort"
    "sync"
)

type ConsistentHashRing struct {
    ring         map[uint32]string 
    sortedHashes []uint32           
    virtualNodes int                
    nodes        map[string]bool    
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

func (chr *ConsistentHashRing) GetNode(key string) (string, error) {
    chr.mu.RLock()
    defer chr.mu.RUnlock()

    if len(chr.ring) == 0 {
        return "", fmt.Errorf("no nodes in ring")
    }

    hash := chr.hashKey(key)

    idx := sort.Search(len(chr.sortedHashes), func(i int) bool {
        return chr.sortedHashes[i] >= hash
    })

    if idx == len(chr.sortedHashes) {
        idx = 0
    }

    return chr.ring[chr.sortedHashes[idx]], nil
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

func (chr *ConsistentHashRing) hashKey(key string) uint32 {
    return crc32.ChecksumIEEE([]byte(key))
}

func (chr *ConsistentHashRing) Size() int {
    chr.mu.RLock()
    defer chr.mu.RUnlock()
    return len(chr.nodes)
}