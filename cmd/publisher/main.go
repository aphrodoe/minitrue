//go:build !no_serial
// +build !no_serial

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/minitrue/internal/mqttclient"
	"github.com/tarm/serial"
)

func main() {
	port := flag.String("port", "/dev/tty.usbmodem14101", "serial port for arduino")
	baud := flag.Int("baud", 9600, "serial baud rate")
	broker := flag.String("broker", "tcp://localhost:1883", "mqtt broker")
	sim := flag.Bool("sim", true, "simulate sensors instead of reading serial")
	flag.Parse()

	mqttc, err := mqttclient.New(mqttclient.Options{
		BrokerURL: *broker,
		ClientID:  fmt.Sprintf("arduino-pub-%d", time.Now().UnixNano()),
	})
	if err != nil {
		log.Fatalf("mqtt connect: %v", err)
	}
	defer mqttc.Close()

	if *sim {
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

	c := &serial.Config{Name: *port, Baud: *baud}
	s, err := serial.OpenPort(c)
	if err != nil {
		log.Fatalf("open serial: %v", err)
	}
	scanner := bufio.NewScanner(s)
	for scanner.Scan() {
		line := scanner.Text()
		var deviceID string
		var value float64
		if strings.Contains(line, ",") {
			parts := strings.Split(line, ",")
			deviceID = strings.TrimSpace(parts[0])
			fmt.Sscanf(strings.TrimSpace(parts[1]), "%f", &value)
		} else {
			deviceID = "sensor_1"
			fmt.Sscanf(line, "%*s %f", &value)
		}
		msg := map[string]interface{}{
			"device_id":   deviceID,
			"metric_name": "temperature",
			"timestamp":   time.Now().Unix(),
			"value":       value,
		}
		b, _ := json.Marshal(msg)
		if err := mqttc.Publish("iot/sensors/temperature", b, 0, false); err != nil {
			log.Printf("publish err: %v", err)
		} else {
			log.Printf("published %s", string(b))
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("serial read err: %v", err)
	}
}