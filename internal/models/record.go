package models

// Record is the canonical on-disk/in-memory storage unit.
type Record struct {
	Timestamp  int64   `json:"timestamp"`
	Value      float64 `json:"value"`
	DeviceID   string  `json:"device_id"`
	MetricName string  `json:"metric_name"`
}

// DataPoint is the wire format accepted by the router's POST /route endpoint
// and forwarded to storage nodes' POST /ingest endpoint.
type DataPoint struct {
	DeviceID   string  `json:"device_id"`
	MetricName string  `json:"metric_name"`
	Timestamp  int64   `json:"timestamp"`
	Value      float64 `json:"value"`
}

