package cluster

import (
    "encoding/json"
    "fmt"
    "log"
    "math/rand"
    "sync"
    "time"

    "github.com/minitrue/pkg/models"
    "github.com/minitrue/pkg/network"
)

type GossipProtocol struct {
    localNode      *models.NodeInfo
    clusterState   *models.ClusterState
    ticker         *time.Ticker
    interval       time.Duration
    suspectTimeout time.Duration
    mu             sync.RWMutex
    client         *network.Client
    stopChan       chan struct{}
}

func NewGossipProtocol(localNode *models.NodeInfo, interval time.Duration, 
                       client *network.Client, replicationFactor int) *GossipProtocol {
    return &GossipProtocol{
        localNode:      localNode,
        clusterState: &models.ClusterState{
            Nodes:             make(map[string]*models.NodeInfo),
            ReplicationFactor: replicationFactor,
            Version:           0,
        },
        interval:       interval,
        suspectTimeout: interval * 5,
        client:         client,
        stopChan:       make(chan struct{}),
    }
}

func (gp *GossipProtocol) Start() {
    gp.mu.Lock()
    gp.localNode.LastHeartbeat = time.Now()
    gp.localNode.Status = "active"
    gp.clusterState.Nodes[gp.localNode.ID] = gp.localNode
    gp.mu.Unlock()

    gp.ticker = time.NewTicker(gp.interval)

    go gp.gossipLoop()
    go gp.failureDetectionLoop()
}

func (gp *GossipProtocol) Stop() {
    if gp.ticker != nil {
        gp.ticker.Stop()
    }
    close(gp.stopChan)
}

func (gp *GossipProtocol) gossipLoop() {
    for {
        select {
        case <-gp.ticker.C:
            gp.sendGossip()
        case <-gp.stopChan:
            return
        }
    }
}

func (gp *GossipProtocol) sendGossip() {
    gp.mu.Lock()
    gp.localNode.LastHeartbeat = time.Now()
    gp.clusterState.Version++

    msg := models.GossipMessage{
        State:   *gp.clusterState,
        From:    gp.localNode.ID,
        Version: gp.clusterState.Version,
    }
    gp.mu.Unlock()

    targets := gp.selectRandomActiveNodes(3)

    for _, nodeID := range targets {
        go gp.sendGossipToNode(nodeID, msg)
    }
}

func (gp *GossipProtocol) sendGossipToNode(nodeID string, msg models.GossipMessage) {
    gp.mu.RLock()
    node, exists := gp.clusterState.Nodes[nodeID]
    gp.mu.RUnlock()

    if !exists || node.ID == gp.localNode.ID || node.Status == "down" {
        return
    }

    internalMsg := models.InternalMessage{
        Type:    "gossip",
        Payload: msg,
        From:    gp.localNode.ID,
    }

    data, err := json.Marshal(internalMsg)
    if err != nil {
        log.Printf("Failed to marshal gossip message: %v", err)
        return
    }

    if err := gp.client.Send(node.Address, data); err != nil {
        log.Printf("Failed to send gossip to %s: %v", nodeID, err)
        gp.markNodeSuspect(nodeID)
    }
}

func (gp *GossipProtocol) HandleGossipMessage(msg models.GossipMessage) {
    gp.mu.Lock()
    defer gp.mu.Unlock()

    for nodeID, remoteNode := range msg.State.Nodes {
        localNode, exists := gp.clusterState.Nodes[nodeID]

        if !exists {
            gp.clusterState.Nodes[nodeID] = &models.NodeInfo{
                ID:            remoteNode.ID,
                Address:       remoteNode.Address,
                HTTPPort:      remoteNode.HTTPPort,
                MQTTPort:      remoteNode.MQTTPort,
                LastHeartbeat: remoteNode.LastHeartbeat,
                Status:        remoteNode.Status,
            }
            log.Printf("[%s] Discovered new node: %s at %s", 
                       gp.localNode.ID, nodeID, remoteNode.Address)
        } else if remoteNode.LastHeartbeat.After(localNode.LastHeartbeat) {
            // Update with newer information
            localNode.LastHeartbeat = remoteNode.LastHeartbeat
            localNode.Status = remoteNode.Status
            localNode.Address = remoteNode.Address
        }
    }

    if msg.Version > gp.clusterState.Version {
        gp.clusterState.Version = msg.Version
    }
}

func (gp *GossipProtocol) selectRandomActiveNodes(count int) []string {
    gp.mu.RLock()
    defer gp.mu.RUnlock()

    nodes := make([]string, 0, len(gp.clusterState.Nodes))
    for nodeID, node := range gp.clusterState.Nodes {
        if nodeID != gp.localNode.ID && node.Status == "active" {
            nodes = append(nodes, nodeID)
        }
    }

    if len(nodes) <= count {
        return nodes
    }

    rand.Shuffle(len(nodes), func(i, j int) {
        nodes[i], nodes[j] = nodes[j], nodes[i]
    })

    return nodes[:count]
}

func (gp *GossipProtocol) failureDetectionLoop() {
    ticker := time.NewTicker(gp.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            gp.detectFailures()
        case <-gp.stopChan:
            return
        }
    }
}

func (gp *GossipProtocol) detectFailures() {
    gp.mu.Lock()
    defer gp.mu.Unlock()

    now := time.Now()

    for nodeID, node := range gp.clusterState.Nodes {
        if nodeID == gp.localNode.ID {
            continue
        }

        timeSinceHeartbeat := now.Sub(node.LastHeartbeat)

        if timeSinceHeartbeat > gp.suspectTimeout {
            if node.Status != "down" {
                log.Printf("[%s] Node %s marked as DOWN (no heartbeat for %v)", 
                          gp.localNode.ID, nodeID, timeSinceHeartbeat)
                node.Status = "down"
            }
        } else if timeSinceHeartbeat > gp.interval*2 {
            if node.Status == "active" {
                log.Printf("[%s] Node %s marked as SUSPECT", gp.localNode.ID, nodeID)
                node.Status = "suspect"
            }
        }
    }
}

func (gp *GossipProtocol) markNodeSuspect(nodeID string) {
    gp.mu.Lock()
    defer gp.mu.Unlock()

    if node, exists := gp.clusterState.Nodes[nodeID]; exists {
        if node.Status == "active" {
            node.Status = "suspect"
            log.Printf("[%s] Node %s marked as SUSPECT (failed to contact)", 
                      gp.localNode.ID, nodeID)
        }
    }
}

func (gp *GossipProtocol) GetClusterState() models.ClusterState {
    gp.mu.RLock()
    defer gp.mu.RUnlock()

    stateCopy := models.ClusterState{
        Nodes:             make(map[string]*models.NodeInfo),
        ReplicationFactor: gp.clusterState.ReplicationFactor,
        Version:           gp.clusterState.Version,
    }

    for id, node := range gp.clusterState.Nodes {
        nodeCopy := *node
        stateCopy.Nodes[id] = &nodeCopy
    }

    return stateCopy
}

func (gp *GossipProtocol) GetActiveNodes() []*models.NodeInfo {
    gp.mu.RLock()
    defer gp.mu.RUnlock()

    activeNodes := make([]*models.NodeInfo, 0)
    for _, node := range gp.clusterState.Nodes {
        if node.Status == "active" {
            activeNodes = append(activeNodes, node)
        }
    }

    return activeNodes
}

func (gp *GossipProtocol) AddSeedNode(address string) error {
    gp.mu.RLock()
    seedMsg := models.GossipMessage{
        State: models.ClusterState{
            Nodes: map[string]*models.NodeInfo{
                gp.localNode.ID: gp.localNode,
            },
            ReplicationFactor: gp.clusterState.ReplicationFactor,
            Version:           gp.clusterState.Version,
        },
        From:    gp.localNode.ID,
        Version: gp.clusterState.Version,
    }
    gp.mu.RUnlock()

    internalMsg := models.InternalMessage{
        Type:    "gossip",
        Payload: seedMsg,
        From:    gp.localNode.ID,
    }

    data, err := json.Marshal(internalMsg)
    if err != nil {
        return fmt.Errorf("failed to marshal seed message: %w", err)
    }

    return gp.client.Send(address, data)
}

func (gp *GossipProtocol) GetNodeCount() int {
    gp.mu.RLock()
    defer gp.mu.RUnlock()
    return len(gp.clusterState.Nodes)
}

func (gp *GossipProtocol) IsNodeActive(nodeID string) bool {
    gp.mu.RLock()
    defer gp.mu.RUnlock()

    if node, exists := gp.clusterState.Nodes[nodeID]; exists {
        return node.Status == "active"
    }
    return false
}

func (gp *GossipProtocol) GetNodeInfo(nodeID string) (*models.NodeInfo, bool) {
    gp.mu.RLock()
    defer gp.mu.RUnlock()

    node, exists := gp.clusterState.Nodes[nodeID]
    if !exists {
        return nil, false
    }
    
    nodeCopy := *node
    return &nodeCopy, true
}

func (gp *GossipProtocol) GetNodeByID(nodeID string) *models.NodeInfo {
    gp.mu.RLock()
    defer gp.mu.RUnlock()
    
    if node, exists := gp.clusterState.Nodes[nodeID]; exists {
        nodeCopy := *node
        return &nodeCopy
    }
    return nil
}