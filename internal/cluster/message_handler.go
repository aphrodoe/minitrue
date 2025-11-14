package cluster

import (
	"encoding/json"
	"log"
	"net"

	"github.com/minitrue/pkg/cluster"
	"github.com/minitrue/pkg/models"
)

type MessageHandler struct {
	gossipProtocol *cluster.GossipProtocol
	hashRingUpdater func(nodeID string, add bool)
}

func NewMessageHandler(gossipProtocol *cluster.GossipProtocol, hashRingUpdater func(string, bool)) *MessageHandler {
	return &MessageHandler{
		gossipProtocol: gossipProtocol,
		hashRingUpdater: hashRingUpdater,
	}
}

func (mh *MessageHandler) HandleMessage(data []byte, conn net.Conn) error {
	var msg models.InternalMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return err
	}

	switch msg.Type {
	case "gossip":
		payloadBytes, err := json.Marshal(msg.Payload)
		if err != nil {
			return err
		}

		var gossipMsg models.GossipMessage
		if err := json.Unmarshal(payloadBytes, &gossipMsg); err != nil {
			return err
		}

		mh.gossipProtocol.HandleGossipMessage(gossipMsg)
		
		mh.updateHashRingFromGossip(gossipMsg)

	default:
		log.Printf("[MessageHandler] Unknown message type: %s", msg.Type)
	}

	return nil
}

func (mh *MessageHandler) updateHashRingFromGossip(msg models.GossipMessage) {
	hashRing := GetHashRing()
	if hashRing == nil {
		return
	}

	currentNodes := make(map[string]bool)
	for _, nodeID := range hashRing.GetAllNodes() {
		currentNodes[nodeID] = true
	}

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

