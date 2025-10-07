package models

import "time"

type NodeInfo struct {
    ID            string    `json:"id"`
    Address       string    `json:"address"`
    HTTPPort      int       `json:"http_port"`
    LastHeartbeat time.Time `json:"last_heartbeat"`
    Status        string    `json:"status"`
}

type ClusterState struct {
    Nodes             map[string]*NodeInfo `json:"nodes"`
    ReplicationFactor int                  `json:"replication_factor"`
    Version           int64                `json:"version"`
}

type GossipMessage struct {
    State    ClusterState `json:"state"`
    From     string       `json:"from"`
    Version  int64        `json:"version"`
    RootHash string       `json:"root_hash"`
    IsFull   bool         `json:"is_full"`
}

type GossipSyncRequest struct {
    From string `json:"from"`
}

type InternalMessage struct {
    Type    string      `json:"type"`    
    Payload interface{} `json:"payload"` 
    From    string      `json:"from"` 
}
