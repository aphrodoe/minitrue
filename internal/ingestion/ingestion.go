package ingestion

import (
	"encoding/json"
	"log"

	"github.com/minitrue/internal/cluster"
	"github.com/minitrue/internal/mqttclient"
	"github.com/minitrue/internal/storage"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// DataPoint is the incoming JSON structure from devices
type DataPoint struct {
	DeviceID   string  `json:"device_id"`
	MetricName string  `json:"metric_name"`
	Timestamp  int64   `json:"timestamp"`
	Value      float64 `json:"value"`
}

type Service struct {
	mqtt   *mqttclient.Client
	store  storage.Storage
	nodeID string // this node's identifier, e.g., "ing1"
}

func New(m *mqttclient.Client, s storage.Storage, nodeID string) *Service {
	return &Service{mqtt: m, store: s, nodeID: nodeID}
}

func (s *Service) Start() {
	topic := "iot/sensors/#"
	log.Printf("[%s] Ingestion subscribing to %s\n", s.nodeID, topic)
	if err := s.mqtt.Subscribe(topic, 0, s.handle); err != nil {
		log.Fatalf("failed to subscribe: %v", err)
	}
	log.Printf("[%s] Ingestion service started and listening for sensor data", s.nodeID)
}

// handle processes incoming MQTT messages. Replication + primary decision happens here.
func (s *Service) handle(client mqtt.Client, msg mqtt.Message) {
	var p DataPoint
	if err := json.Unmarshal(msg.Payload(), &p); err != nil {
		log.Printf("[%s][ingestion] failed to parse json: %v payload=%s", s.nodeID, err, string(msg.Payload()))
		return
	}
	if p.DeviceID == "" {
		log.Printf("[%s][ingestion] missing device_id in payload", s.nodeID)
		return
	}

	// Determine which node is the primary for this device (calls external hashing).
	primaryNode := cluster.GetPrimaryNode(p.DeviceID)

	// If this node is the primary, call PersistPrimary; otherwise PersistReplica.
	if primaryNode == s.nodeID {
		// Primary stores as primary
		if err := s.store.PersistPrimary(p); err != nil {
			log.Printf("[%s][ingestion] PersistPrimary error: %v", s.nodeID, err)
			return
		}
		log.Printf("[%s][ingestion] PRIMARY stored %s/%s = %v", s.nodeID, p.DeviceID, p.MetricName, p.Value)
	} else {
		// Replica stores as replica
		if err := s.store.PersistReplica(p); err != nil {
			log.Printf("[%s][ingestion] PersistReplica error: %v", s.nodeID, err)
			return
		}
		log.Printf("[%s][ingestion] REPLICA stored %s/%s = %v (primary=%s)", s.nodeID, p.DeviceID, p.MetricName, p.Value, primaryNode)
	}
	_ = client // keep signature (unused here)
}




