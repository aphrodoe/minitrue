package ingestion

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/minitrue/internal/cluster"
	"github.com/minitrue/internal/mqttclient"
	"github.com/minitrue/internal/storage"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type DataPoint struct {
	DeviceID   string  `json:"device_id"`
	MetricName string  `json:"metric_name"`
	Timestamp  int64   `json:"timestamp"`
	Value      float64 `json:"value"`
}

type routingFields struct {
	DeviceID   string `json:"device_id"`
	MetricName string `json:"metric_name"`
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
	if err := s.mqtt.Subscribe(topic, 1, s.handle); err != nil {
		log.Fatalf("failed to subscribe: %v", err)
	}
	log.Printf("[%s] Ingestion service started and listening for sensor data", s.nodeID)
}

func (s *Service) handle(client mqtt.Client, msg mqtt.Message) {
	deviceID, metricName, ok := routingKeyFromTopic(msg.Topic())
	if !ok {
		var fields routingFields
		if err := json.Unmarshal(msg.Payload(), &fields); err != nil {
			log.Printf("[%s][ingestion] failed to parse routing fields: %v payload=%s", s.nodeID, err, string(msg.Payload()))
			return
		}
		deviceID = fields.DeviceID
		metricName = fields.MetricName
	}
	if deviceID == "" {
		log.Printf("[%s][ingestion] missing device_id in routing fields", s.nodeID)
		return
	}

	key := deviceID + ":" + metricName
	hashRing := cluster.GetHashRing()

	if hashRing != nil {
		allNodes := hashRing.GetAllNodes()
		if len(allNodes) > 0 {
			nodes := cluster.GetNodesForKey(key, 2)
			if len(nodes) > 0 && !s.ownsKey(nodes) {
				return
			}
		}
	}

	var p DataPoint
	if err := json.Unmarshal(msg.Payload(), &p); err != nil {
		log.Printf("[%s][ingestion] failed to parse json: %v payload=%s", s.nodeID, err, string(msg.Payload()))
		return
	}
	if p.DeviceID == "" {
		p.DeviceID = deviceID
	}
	if p.MetricName == "" {
		p.MetricName = metricName
	}

	persistLocally := func(reason string) {
		if err := s.store.PersistPrimary(p); err != nil {
			log.Printf("[%s][ingestion] PersistPrimary error: %v", s.nodeID, err)
			return
		}
		log.Printf("[%s][ingestion] PRIMARY stored %s/%s = %.2f %s", s.nodeID, p.DeviceID, p.MetricName, p.Value, reason)
	}

	if hashRing == nil {
		persistLocally("(hash ring nil)")
		return
	}

	allNodes := hashRing.GetAllNodes()
	if len(allNodes) == 0 {
		persistLocally("(ring empty)")
		return
	}

	nodes := cluster.GetNodesForKey(key, 2)
	if len(nodes) == 0 {
		persistLocally("(no nodes for key)")
		return
	}

	primaryNode := nodes[0]

	if primaryNode == s.nodeID {
		persistLocally("")
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
}

func routingKeyFromTopic(topic string) (string, string, bool) {
	parts := strings.Split(topic, "/")
	if len(parts) == 4 && parts[0] == "iot" && parts[1] == "sensors" {
		return parts[2], parts[3], true
	}
	return "", "", false
}

func (s *Service) ownsKey(nodes []string) bool {
	for _, node := range nodes {
		if node == s.nodeID {
			return true
		}
	}
	return false
}

