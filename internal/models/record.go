package models

type Record struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
	DeviceID  string  `json:"device_id"`
	MetricName string `json:"metric_name"`
}

