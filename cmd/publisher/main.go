// publisher generates simulated sensor data and sends it to the minitrue-router
// HTTP gateway.
// Usage:
//
//	go run cmd/publisher/main.go --sim=true --router=http://localhost:7070/route
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/minitrue/internal/logger"
	"github.com/minitrue/internal/models"
	"github.com/tarm/serial"
)

func main() {
	logger.SetupBeautifulLogging()

	serialPort := flag.String("port", "/dev/tty.usbmodem14101", "serial port for Arduino")
	baud := flag.Int("baud", 9600, "serial baud rate")
	routerURL := flag.String("router", envStr("MINITRUE_ROUTER_URL", "http://localhost:7070/route"), "minitrue-router /route endpoint")
	sim := flag.Bool("sim", true, "simulate sensors instead of reading serial port")
	flag.Parse()

	// Keep the health-check HTTP server so Render's web service stays happy.
	healthPort := envStr("PORT", "8080")
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok","service":"publisher"}`))
		})
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok","service":"publisher"}`))
		})
		log.Printf("[Publisher] Health-check server on :%s", healthPort)
		if err := http.ListenAndServe(":"+healthPort, mux); err != nil {
			log.Printf("[Publisher] Health-check server error: %v", err)
		}
	}()

	httpClient := &http.Client{Timeout: 5 * time.Second}

	publish := func(dp models.DataPoint) {
		b, err := json.Marshal(dp)
		if err != nil {
			log.Printf("[Publisher] marshal error: %v", err)
			return
		}
		resp, err := httpClient.Post(*routerURL, "application/json", bytes.NewReader(b))
		if err != nil {
			log.Printf("[Publisher] POST error: %v", err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
			log.Printf("[Publisher] router returned %d for %s/%s", resp.StatusCode, dp.DeviceID, dp.MetricName)
			return
		}
		log.Printf("[Publisher] sent %s/%s = %.4f", dp.DeviceID, dp.MetricName, dp.Value)
	}

	if *sim {
		devices := []string{"sensor_1", "sensor_2", "sensor_3"}
		for {
			did := devices[rand.Intn(len(devices))]
			publish(models.DataPoint{
				DeviceID:   did,
				MetricName: "temperature",
				Timestamp:  time.Now().Unix(),
				Value:      20.0 + rand.Float64()*10.0,
			})
			time.Sleep(1 * time.Second)
		}
	}

	// Serial path (Arduino).
	c := &serial.Config{Name: *serialPort, Baud: *baud}
	s, err := serial.OpenPort(c)
	if err != nil {
		log.Fatalf("[Publisher] open serial: %v", err)
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
		publish(models.DataPoint{
			DeviceID:   deviceID,
			MetricName: "temperature",
			Timestamp:  time.Now().Unix(),
			Value:      value,
		})
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[Publisher] serial read error: %v", err)
	}
}

func envStr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
