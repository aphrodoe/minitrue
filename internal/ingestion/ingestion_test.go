package ingestion

import (
	"testing"
)

func TestRoutingKeyFromTopic(t *testing.T) {
	tests := []struct {
		topic      string
		wantDevice string
		wantMetric string
		wantOk     bool
	}{
		{"iot/sensors/sensor-001/temperature", "sensor-001", "temperature", true},
		{"iot/sensors/device-1/voltage", "device-1", "voltage", true},
		{"iot/sensors/invalid", "", "", false},
		{"other/sensors/device/metric", "", "", false},
	}

	for _, tt := range tests {
		gotDevice, gotMetric, gotOk := routingKeyFromTopic(tt.topic)
		if gotDevice != tt.wantDevice || gotMetric != tt.wantMetric || gotOk != tt.wantOk {
			t.Errorf("routingKeyFromTopic(%q) = %q, %q, %v, want %q, %q, %v",
				tt.topic, gotDevice, gotMetric, gotOk, tt.wantDevice, tt.wantMetric, tt.wantOk)
		}
	}
}

func TestOwnsKey(t *testing.T) {
	s := &Service{nodeID: "node-1"}

	if !s.ownsKey([]string{"node-1", "node-2"}) {
		t.Error("expected true, node-1 is in the list")
	}

	if s.ownsKey([]string{"node-2", "node-3"}) {
		t.Error("expected false, node-1 is not in the list")
	}
}
