package ingestion

import (
	"encoding/json"
	"log"

	"github.com/minitrue/internal/cluster"
	"github.com/minitrue/internal/mqttclient"
	"github.com/minitrue/internal/storage"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// DataPoint represents a single measurement from a device
// The combination of DeviceID and MetricName will be used to distribute primaries across nodes
// to avoid one node being primary for all data of a device.
type DataPoint struct {
	DeviceID   string  `json:"device_id"`
	MetricName string  `json:"metric_name"`
	Timestamp  int64   `json:"timestamp"`
	Value      float64 `json:"value"`
}

type Service struct {
	mqtt   *mqttclient.Client
	store  storage.Storage
	nodeID string
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

	key := p.DeviceID + ":" + p.MetricName
	
	hashRing := cluster.GetHashRing()
	if hashRing == nil {
		log.Printf("[%s][ingestion] Hash ring nil, storing locally", s.nodeID)
		if err := s.store.PersistPrimary(p); err != nil {
			log.Printf("[%s][ingestion] PersistPrimary error: %v", s.nodeID, err)
			return
		}
		log.Printf("[%s][ingestion] PRIMARY stored %s/%s = %.2f (hash ring nil)", s.nodeID, p.DeviceID, p.MetricName, p.Value)
		return
	}
	
	allNodes := hashRing.GetAllNodes()
	if len(allNodes) == 0 {
		log.Printf("[%s][ingestion] Hash ring empty, storing locally", s.nodeID)
		if err := s.store.PersistPrimary(p); err != nil {
			log.Printf("[%s][ingestion] PersistPrimary error: %v", s.nodeID, err)
			return
		}
		log.Printf("[%s][ingestion] PRIMARY stored %s/%s = %.2f (ring empty)", s.nodeID, p.DeviceID, p.MetricName, p.Value)
		return
	}
	
	nodes := cluster.GetNodesForKey(key, 2)
	if len(nodes) == 0 {
		log.Printf("[%s][ingestion] GetNodesForKey returned empty, storing locally", s.nodeID)
		if err := s.store.PersistPrimary(p); err != nil {
			log.Printf("[%s][ingestion] PersistPrimary error: %v", s.nodeID, err)
			return
		}
		log.Printf("[%s][ingestion] PRIMARY stored %s/%s = %.2f (no nodes for key)", s.nodeID, p.DeviceID, p.MetricName, p.Value)
		return
	}
	
	primaryNode := nodes[0]

	if primaryNode == s.nodeID {
		if err := s.store.PersistPrimary(p); err != nil {
			log.Printf("[%s][ingestion] PersistPrimary error: %v", s.nodeID, err)
			return
		}
		log.Printf("[%s][ingestion] PRIMARY stored %s/%s = %.2f", s.nodeID, p.DeviceID, p.MetricName, p.Value)
		return
	}

	if len(nodes) > 1 {
		replicaNode := nodes[1]
		if replicaNode == s.nodeID {
			if err := s.store.PersistReplica(p); err != nil {
				log.Printf("[%s][ingestion] PersistReplica error: %v", s.nodeID, err)
				return
			}
			log.Printf("[%s][ingestion] REPLICA stored %s/%s = %.2f (primary=%s)", s.nodeID, p.DeviceID, p.MetricName, p.Value, primaryNode)
			return
		}
	}

	_ = client
}