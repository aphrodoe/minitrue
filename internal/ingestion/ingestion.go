package ingestion

import (
    "encoding/json"
    "log"

    mqtt "github.com/eclipse/paho.mqtt.golang"
    "github.com/minitrue/internal/mqttclient"
    "github.com/minitrue/internal/storage"
)

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
    if err := s.mqtt.Subscribe(topic, 0, s.handle); err != nil {
        log.Fatalf("failed to subscribe: %v", err)
    }
}

func (s *Service) handle(client mqtt.Client, msg mqtt.Message) {
    var p DataPoint
    if err := json.Unmarshal(msg.Payload(), &p); err != nil {
        return
    }
    if p.DeviceID == "" { return }
    _ = s.store.PersistPrimary(p)
}
