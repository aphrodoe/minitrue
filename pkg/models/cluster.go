package models

import "time"

// NodeInfo represents information about a cluster node
type NodeInfo struct {
    ID            string    `json:"id"`
    Address       string    `json:"address"`
    HTTPPort      int       `json:"http_port"`
    MQTTPort      int       `json:"mqtt_port"`
    LastHeartbeat time.Time `json:"last_heartbeat"`
    Status        string    `json:"status"` // "active", "suspect", "down"
}

// ClusterState represents the state of the entire cluster
type ClusterState struct {
    Nodes             map[string]*NodeInfo `json:"nodes"`
    ReplicationFactor int                  `json:"replication_factor"`
    Version           int64                `json:"version"`
}

// GossipMessage represents a gossip protocol message
type GossipMessage struct {
    State   ClusterState `json:"state"`
    From    string       `json:"from"`
    Version int64        `json:"version"`
}

// InternalMessage represents internal cluster communication
type InternalMessage struct {
    Type    string      `json:"type"`    // "gossip", "replicate", etc.
    Payload interface{} `json:"payload"` // The actual message content
    From    string      `json:"from"`    // Sender node ID
}