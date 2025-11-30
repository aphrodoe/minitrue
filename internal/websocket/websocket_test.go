package websocket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	gorillawebsocket "github.com/gorilla/websocket"
)

type fakeSubscriber struct {
	mu      sync.Mutex
	topic   string
	qos     byte
	handler mqtt.MessageHandler
}

func (f *fakeSubscriber) Subscribe(topic string, qos byte, handler mqtt.MessageHandler) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.topic = topic
	f.qos = qos
	f.handler = handler
	return nil
}

func (f *fakeSubscriber) waitForHandler(t *testing.T) mqtt.MessageHandler {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		handler := f.handler
		f.mu.Unlock()
		if handler != nil {
			return handler
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("timed out waiting for MQTT subscription handler")
	return nil
}

type fakeMessage struct {
	topic   string
	payload []byte
}

func (m fakeMessage) Duplicate() bool   { return false }
func (m fakeMessage) Qos() byte         { return 0 }
func (m fakeMessage) Retained() bool    { return false }
func (m fakeMessage) Topic() string     { return m.topic }
func (m fakeMessage) MessageID() uint16 { return 0 }
func (m fakeMessage) Payload() []byte   { return m.payload }
func (m fakeMessage) Ack()              {}

func TestHubBridgesMQTTMessagesToWebSocketClients(t *testing.T) {
	subscriber := &fakeSubscriber{}
	hub := NewHub(subscriber)
	go hub.Run()
	t.Cleanup(hub.Stop)

	server := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer conn.Close()

	handler := subscriber.waitForHandler(t)
	if subscriber.topic != "iot/sensors/#" {
		t.Fatalf("expected websocket bridge to subscribe to iot/sensors/#, got %q", subscriber.topic)
	}
	if subscriber.qos != 0 {
		t.Fatalf("expected websocket bridge QoS 0 before Step 6, got %d", subscriber.qos)
	}

	handler(nil, fakeMessage{
		topic: "iot/sensors/temperature",
		payload: []byte(`{
			"device_id":"sensor-0001",
			"metric_name":"temperature",
			"timestamp":12345,
			"value":21.5
		}`),
	})

	conn.SetReadDeadline(time.Now().Add(time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read websocket message: %v", err)
	}

	var point DataPoint
	if err := json.Unmarshal(data, &point); err != nil {
		t.Fatalf("failed to decode websocket payload: %v", err)
	}
	if point.DeviceID != "sensor-0001" || point.MetricName != "temperature" || point.Timestamp != 12345 || point.Value != 21.5 {
		t.Fatalf("unexpected websocket data point: %+v", point)
	}
	if point.ReceivedAt.IsZero() {
		t.Fatalf("expected ReceivedAt to be set")
	}
}
