package websocket

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/minitrue/internal/mqttclient"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type DataPoint struct {
	DeviceID   string    `json:"device_id"`
	MetricName string    `json:"metric_name"`
	Timestamp  int64     `json:"timestamp"`
	Value      float64   `json:"value"`
	ReceivedAt time.Time `json:"received_at"`
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan DataPoint
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
	mqttClient *mqttclient.Client
}

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan DataPoint
}

func NewHub(mqttClient *mqttclient.Client) *Hub {
	return &Hub{
		broadcast:  make(chan DataPoint, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		mqttClient: mqttClient,
	}
}

func (h *Hub) Run() {
	topic := "iot/sensors/#"
	if err := h.mqttClient.Subscribe(topic, 0, h.handleMQTTMessage); err != nil {
		log.Printf("[WebSocket] Failed to subscribe to MQTT: %v", err)
	} else {
		log.Printf("[WebSocket] Subscribed to MQTT topic: %s", topic)
	}

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("[WebSocket] Client connected. Total clients: %d", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("[WebSocket] Client disconnected. Total clients: %d", len(h.clients))

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) handleMQTTMessage(client mqtt.Client, msg mqtt.Message) {
	var dataPoint DataPoint
	if err := json.Unmarshal(msg.Payload(), &dataPoint); err != nil {
		log.Printf("[WebSocket] Failed to parse MQTT message: %v", err)
		return
	}

	dataPoint.ReceivedAt = time.Now()
	
	select {
	case h.broadcast <- dataPoint:
	default:
		log.Printf("[WebSocket] Broadcast channel full, dropping message")
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WebSocket] Failed to upgrade connection: %v", err)
		return
	}

	client := &Client{
		hub:  h,
		conn: conn,
		send: make(chan DataPoint, 256),
	}
	client.hub.register <- client

	go client.writePump()
	go client.readPump()
}

const (
	writeWait = 10 * time.Second

	pongWait = 60 * time.Second

	pingPeriod = (pongWait * 9) / 10

	maxMessageSize = 512
)

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[WebSocket] Error reading message: %v", err)
			}
			break
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}

			data, err := json.Marshal(message)
			if err != nil {
				log.Printf("[WebSocket] Failed to marshal message: %v", err)
				continue
			}

			w.Write(data)

			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				msg := <-c.send
				data, err := json.Marshal(msg)
				if err != nil {
					continue
				}
				w.Write(data)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) GetClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}