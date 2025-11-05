//go:build no_serial
// +build no_serial

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/minitrue/internal/mqttclient"
)

func main() {
	broker := flag.String("broker", "tcp://localhost:1883", "mqtt broker")
	sim := flag.Bool("sim", true, "simulate sensors")
	flag.Parse()

	mqttc, err := mqttclient.New(mqttclient.Options{
		BrokerURL: *broker,
		ClientID:  fmt.Sprintf("arduino-pub-%d", time.Now().UnixNano()),
	})
	if err != nil {
		log.Fatalf("mqtt connect: %v", err)
	}
	defer mqttc.Close()

	devices := []string{"sensor_1", "sensor_2", "sensor_3"}
	for {
		did := devices[rand.Intn(len(devices))]
		msg := map[string]interface{}{
			"device_id":   did,
			"metric_name": "temperature",
			"timestamp":   time.Now().Unix(),
			"value":       20.0 + rand.Float64()*10.0,
		}
		b, _ := json.Marshal(msg)
		if err := mqttc.Publish("iot/sensors/temperature", b, 0, false); err != nil {
			log.Printf("publish err: %v", err)
		} else {
			log.Printf("published simulated %s -> %s", did, string(b))
		}
		time.Sleep(1 * time.Second)
	}
}

