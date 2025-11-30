package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/minitrue/internal/models"
	"github.com/minitrue/internal/storage"
	"github.com/minitrue/internal/websocket"
)

func TestHandleClusterMembersReturnsClusterState(t *testing.T) {
	service := &Service{
		clusterStateProvider: func() models.ClusterState {
			return models.ClusterState{
				Nodes: map[string]*models.NodeInfo{
					"node-a": {
						ID:            "node-a",
						Address:       "localhost:9001",
						HTTPPort:      8080,
						MQTTPort:      1883,
						LastHeartbeat: time.Unix(100, 0),
						Status:        "active",
					},
					"node-b": {
						ID:            "node-b",
						Address:       "localhost:9002",
						HTTPPort:      8081,
						MQTTPort:      1883,
						LastHeartbeat: time.Unix(101, 0),
						Status:        "down",
					},
				},
				ReplicationFactor: 2,
				Version:           7,
			}
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/cluster/members", nil)
	rr := httptest.NewRecorder()

	service.handleClusterMembers(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var state models.ClusterState
	if err := json.Unmarshal(rr.Body.Bytes(), &state); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if state.Version != 7 {
		t.Fatalf("expected version 7, got %d", state.Version)
	}
	if state.ReplicationFactor != 2 {
		t.Fatalf("expected replication factor 2, got %d", state.ReplicationFactor)
	}
	if len(state.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(state.Nodes))
	}
	if state.Nodes["node-a"].ID != "node-a" || state.Nodes["node-a"].Address != "localhost:9001" || state.Nodes["node-a"].HTTPPort != 8080 || state.Nodes["node-a"].Status != "active" {
		t.Fatalf("node-a response missing required fields: %+v", state.Nodes["node-a"])
	}
	if state.Nodes["node-b"].Status != "down" {
		t.Fatalf("expected node-b down status, got %+v", state.Nodes["node-b"])
	}
}

func TestHandleClusterMembersRejectsNonGet(t *testing.T) {
	service := &Service{}
	req := httptest.NewRequest(http.MethodPost, "/cluster/members", nil)
	rr := httptest.NewRecorder()

	service.handleClusterMembers(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}

type recordingStore struct {
	mu            sync.Mutex
	primaryWrites int
	replicaWrites int
}

func (s *recordingStore) PersistPrimary(p interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.primaryWrites++
	return nil
}

func (s *recordingStore) PersistReplica(p interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replicaWrites++
	return nil
}

func (s *recordingStore) Query(deviceID, metric string, start, end int64) ([]float64, error) {
	return nil, nil
}

func (s *recordingStore) QueryAggregated(deviceID, metric string, start, end int64) (storage.QueryStats, error) {
	return storage.QueryStats{}, nil
}

func (s *recordingStore) Delete(deviceID, metric string) error { return nil }
func (s *recordingStore) Reload() error                        { return nil }

func (s *recordingStore) GetSeriesDigest(deviceID, metric string) (uint32, int, error) {
	return 0, 0, nil
}

func (s *recordingStore) GetSeriesRecords(deviceID, metric string) ([]models.Record, error) {
	return nil, nil
}

func (s *recordingStore) GetOwnedSeriesKeys() []string {
	return nil
}

func (s *recordingStore) writeCounts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.primaryWrites, s.replicaWrites
}

type queryFakeSubscriber struct {
	mu      sync.Mutex
	handler mqtt.MessageHandler
}

func (s *queryFakeSubscriber) Subscribe(topic string, qos byte, handler mqtt.MessageHandler) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = handler
	return nil
}

func (s *queryFakeSubscriber) waitForHandler(t *testing.T) mqtt.MessageHandler {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		handler := s.handler
		s.mu.Unlock()
		if handler != nil {
			return handler
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for WebSocket MQTT handler")
	return nil
}

type queryFakeMessage struct {
	payload []byte
}

func (m queryFakeMessage) Duplicate() bool   { return false }
func (m queryFakeMessage) Qos() byte         { return 0 }
func (m queryFakeMessage) Retained() bool    { return false }
func (m queryFakeMessage) Topic() string     { return "iot/sensors/temperature" }
func (m queryFakeMessage) MessageID() uint16 { return 0 }
func (m queryFakeMessage) Payload() []byte   { return m.payload }
func (m queryFakeMessage) Ack()              {}

func TestQueryWebSocketBridgeDoesNotPersistMQTTMessages(t *testing.T) {
	store := &recordingStore{}
	subscriber := &queryFakeSubscriber{}
	hub := websocket.NewHub(subscriber)
	service := &Service{
		store: store,
		wsHub: hub,
	}
	if service.wsHub == nil {
		t.Fatal("expected query service websocket hub")
	}

	go service.wsHub.Run()
	t.Cleanup(service.wsHub.Stop)

	handler := subscriber.waitForHandler(t)
	handler(nil, queryFakeMessage{payload: []byte(`{
		"device_id":"sensor-0001",
		"metric_name":"temperature",
		"timestamp":12345,
		"value":21.5
	}`)})

	primaryWrites, replicaWrites := store.writeCounts()
	if primaryWrites != 0 || replicaWrites != 0 {
		t.Fatalf("query-mode websocket bridge must not persist MQTT messages: primary=%d replica=%d", primaryWrites, replicaWrites)
	}
}
