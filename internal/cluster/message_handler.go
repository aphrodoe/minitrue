package cluster

import (
	"encoding/json"
	"log"
	"net"

	"github.com/minitrue/pkg/cluster"
	"github.com/minitrue/pkg/models"
)

// MessageHandler handles incoming TCP messages and routes them to gossip protocol
type MessageHandler struct {
	gossipProtocol *cluster.GossipProtocol
	hashRingUpdater func(nodeID string, add bool)
}

// NewMessageHandler creates a new message handler
func NewMessageHandler(gossipProtocol *cluster.GossipProtocol, hashRingUpdater func(string, bool)) *MessageHandler {
	return &MessageHandler{
		gossipProtocol: gossipProtocol,
		hashRingUpdater: hashRingUpdater,
	}
}

// HandleMessage processes incoming messages
func (mh *MessageHandler) HandleMessage(data []byte, conn net.Conn) error {
	var msg models.InternalMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return err
	}

	switch msg.Type {
	case "gossip":
		// Extract gossip message from payload
		payloadBytes, err := json.Marshal(msg.Payload)
		if err != nil {
			return err
		}

		var gossipMsg models.GossipMessage
		if err := json.Unmarshal(payloadBytes, &gossipMsg); err != nil {
			return err
		}

		// Handle gossip message
		mh.gossipProtocol.HandleGossipMessage(gossipMsg)
		
		// Update hash ring based on discovered nodes
		mh.updateHashRingFromGossip(gossipMsg)

	default:
		log.Printf("[MessageHandler] Unknown message type: %s", msg.Type)
	}

	return nil
}

// updateHashRingFromGossip updates the consistent hash ring based on gossip messages
func (mh *MessageHandler) updateHashRingFromGossip(msg models.GossipMessage) {
	hashRing := GetHashRing()
	if hashRing == nil {
		return
	}

	currentNodes := make(map[string]bool)
	for _, nodeID := range hashRing.GetAllNodes() {
		currentNodes[nodeID] = true
	}

	// Add new nodes discovered via gossip
	for nodeID, nodeInfo := range msg.State.Nodes {
		if nodeInfo.Status == "active" && !currentNodes[nodeID] {
			log.Printf("[MessageHandler] Adding node %s to hash ring", nodeID)
			hashRing.AddNode(nodeID)
			if mh.hashRingUpdater != nil {
				mh.hashRingUpdater(nodeID, true)
			}
			currentNodes[nodeID] = true
		} else if nodeInfo.Status == "down" && currentNodes[nodeID] {
			log.Printf("[MessageHandler] Removing node %s from hash ring (down)", nodeID)
			hashRing.RemoveNode(nodeID)
			if mh.hashRingUpdater != nil {
				mh.hashRingUpdater(nodeID, false)
			}
			delete(currentNodes, nodeID)
		}
	}
}

