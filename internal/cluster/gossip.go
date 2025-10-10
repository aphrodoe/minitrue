package cluster

import (
    "encoding/json"
    "fmt"
    "log"
    "math/rand"
    "sync"
    "time"

    "github.com/minitrue/internal/models"
    "github.com/minitrue/internal/network"
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

func NewGossipProtocol(localNode *models.NodeInfo, interval time.Duration, client *network.Client, replicationFactor int) *GossipProtocol {
    return &GossipProtocol{localNode: localNode, clusterState: &models.ClusterState{Nodes: make(map[string]*models.NodeInfo), ReplicationFactor: replicationFactor, Version: 0}, interval: interval, suspectTimeout: interval * 5, client: client, stopChan: make(chan struct{})}
}

func (gp *GossipProtocol) Start() {
    gp.mu.Lock()
    gp.localNode.LastHeartbeat = time.Now()
    gp.localNode.Status = "active"
    gp.clusterState.Nodes[gp.localNode.ID] = gp.localNode
    gp.mu.Unlock()
    gp.ticker = time.NewTicker(gp.interval)
    go gp.gossipLoop()
}

func (gp *GossipProtocol) Stop() { if gp.ticker != nil { gp.ticker.Stop() }; close(gp.stopChan) }

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
    stateCopy := models.ClusterState{Nodes: make(map[string]*models.NodeInfo), ReplicationFactor: gp.clusterState.ReplicationFactor, Version: gp.clusterState.Version}
    for id, node := range gp.clusterState.Nodes { nodeCopy := *node; stateCopy.Nodes[id] = &nodeCopy }
    msg := models.GossipMessage{State: stateCopy, From: gp.localNode.ID, Version: gp.clusterState.Version}
    gp.mu.Unlock()
    for _, nodeID := range gp.selectRandomActiveNodes(3) { go gp.sendGossipToNode(nodeID, msg) }
}

func (gp *GossipProtocol) sendGossipToNode(nodeID string, msg models.GossipMessage) {
    gp.mu.RLock(); node, exists := gp.clusterState.Nodes[nodeID]; gp.mu.RUnlock()
    if !exists || node.ID == gp.localNode.ID || node.Status == "down" { return }
    internalMsg := models.InternalMessage{Type: "gossip", Payload: msg, From: gp.localNode.ID}
    data, err := json.Marshal(internalMsg)
    if err != nil { log.Printf("Failed to marshal gossip message: %v", err); return }
    if err := gp.client.Send(node.Address, data); err != nil { log.Printf("Failed to send gossip to %s: %v", nodeID, err) }
}

func (gp *GossipProtocol) HandleGossipMessage(msg models.GossipMessage) {
    gp.mu.Lock(); defer gp.mu.Unlock()
    for nodeID, remoteNode := range msg.State.Nodes {
        localNode, exists := gp.clusterState.Nodes[nodeID]
        if !exists {
            gp.clusterState.Nodes[nodeID] = &models.NodeInfo{ID: remoteNode.ID, Address: remoteNode.Address, HTTPPort: remoteNode.HTTPPort, MQTTPort: remoteNode.MQTTPort, LastHeartbeat: remoteNode.LastHeartbeat, Status: remoteNode.Status}
        } else if remoteNode.LastHeartbeat.After(localNode.LastHeartbeat) {
            localNode.LastHeartbeat = remoteNode.LastHeartbeat
            localNode.Status = remoteNode.Status
            localNode.Address = remoteNode.Address
        }
    }
    if msg.Version > gp.clusterState.Version { gp.clusterState.Version = msg.Version }
}

func (gp *GossipProtocol) selectRandomActiveNodes(count int) []string {
    gp.mu.RLock(); defer gp.mu.RUnlock()
    nodes := make([]string, 0, len(gp.clusterState.Nodes))
    for nodeID, node := range gp.clusterState.Nodes { if nodeID != gp.localNode.ID && node.Status == "active" { nodes = append(nodes, nodeID) } }
    if len(nodes) <= count { return nodes }
    rand.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })
    return nodes[:count]
}

func (gp *GossipProtocol) GetClusterState() models.ClusterState {
    gp.mu.RLock(); defer gp.mu.RUnlock()
    stateCopy := models.ClusterState{Nodes: make(map[string]*models.NodeInfo), ReplicationFactor: gp.clusterState.ReplicationFactor, Version: gp.clusterState.Version}
    for id, node := range gp.clusterState.Nodes { nodeCopy := *node; stateCopy.Nodes[id] = &nodeCopy }
    return stateCopy
}

func (gp *GossipProtocol) AddSeedNode(address string) error {
    seedMsg := models.GossipMessage{State: gp.GetClusterState(), From: gp.localNode.ID, Version: gp.clusterState.Version}
    internalMsg := models.InternalMessage{Type: "gossip", Payload: seedMsg, From: gp.localNode.ID}
    data, err := json.Marshal(internalMsg)
    if err != nil { return fmt.Errorf("failed to marshal seed message: %w", err) }
    return gp.client.Send(address, data)
}

func (gp *GossipProtocol) GetNodeByID(nodeID string) *models.NodeInfo {
    gp.mu.RLock(); defer gp.mu.RUnlock()
    if node, exists := gp.clusterState.Nodes[nodeID]; exists { nodeCopy := *node; return &nodeCopy }
    return nil
}
