package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"
)

type BeautifulWriter struct {
	out io.Writer
}

func NewBeautifulWriter(out io.Writer) *BeautifulWriter {
	return &BeautifulWriter{out: out}
}

func humanize(body string) string {
	// Standard slot replacements
	body = strings.ReplaceAll(body, "ing1", "polaris")
	body = strings.ReplaceAll(body, "ing2", "sirius")
	body = strings.ReplaceAll(body, "ing3", "vega")

	// ==========================================
	// Ingestion Phrasing
	// ==========================================
	if strings.Contains(body, "PRIMARY stored") {
		parts := strings.Split(body, "stored ")
		if len(parts) == 2 {
			return fmt.Sprintf("Ingested primary telemetry record: \033[1m%s\033[0m", parts[1])
		}
	}
	if strings.Contains(body, "REPLICA stored") {
		parts := strings.Split(body, "stored ")
		if len(parts) == 2 {
			return fmt.Sprintf("Synced replica backup record: \033[1m%s\033[0m", parts[1])
		}
	}
	if strings.Contains(body, "Ingestion service started and listening for sensor data") {
		return "Ingestion worker online $\033[32m\u2192\033[0m actively listening for live IoT telemetry"
	}
	if strings.Contains(body, "Ingestion subscribing to") {
		return "Subscribing ingestion gateway to target telemetry topic..."
	}

	// ==========================================
	// Storage and Persistence Phrasing
	// ==========================================
	if strings.Contains(body, "Periodic flush:") {
		return "Active telemetry buffer flushed to write-queue"
	}
	if strings.Contains(body, "Successfully wrote") {
		return "Sealed pending telemetry buffer into immutable Parquet disk block"
	}
	if strings.Contains(body, "Reloaded") && strings.Contains(body, "records from disk") {
		return "Re-indexed historical Parquet records from local persistent blocks"
	}
	if strings.Contains(body, "No existing data, starting fresh") {
		return "No historical Parquet partition found. Starting fresh data ledger."
	}

	// ==========================================
	// Cluster and Consensus Phrasing
	// ==========================================
	if strings.Contains(body, "Cluster manager initialized") {
		return "Decentralized consensus cluster engine online"
	}
	if strings.Contains(body, "Discovered new node") {
		return "Discovered new cluster peer node via active Gossip rings"
	}
	if strings.Contains(body, "Connected to peer node") {
		return "Established persistent replication tunnel with a cluster peer"
	}
	if strings.Contains(body, "marked as DOWN") {
		return "Peer node lost heartbeats $\033[31m\u2192\033[0m eviction notice registered"
	}
	if strings.Contains(body, "marked as SUSPECT") {
		return "Peer node missing heartbeat responses $\033[33m\u2192\033[0m entering suspect grace-period"
	}

	// ==========================================
	// Query Gateway Phrasing
	// ==========================================
	if strings.Contains(body, "Querying") && strings.Contains(body, "nodes for") {
		return "Scattering aggregate query across active decentralized peer list..."
	}
	if strings.Contains(body, "Aggregated stats from") {
		return "Gossip query returned $\033[32m\u2192\033[0m successfully merged candidate statistics"
	}
	if strings.Contains(body, "Query HTTP server running on") {
		return "Decentralized REST Query Gateway running on active port"
	}
	if strings.Contains(body, "WebSocket available at") {
		return "Real-time query WebSocket pipeline open"
	}

	// ==========================================
	// Simulation and Feeds Phrasing
	// ==========================================
	if strings.Contains(body, "published simulated") {
		parts := strings.Split(body, "simulated ")
		if len(parts) == 2 {
			return fmt.Sprintf("Dispatched simulated sensor feed: %s", parts[1])
		}
	}

	return body
}

func (w *BeautifulWriter) Write(p []byte) (n int, err error) {
	str := string(p)

	// Strip standard Go log date/time prefix if present
	msg := str
	hasTimestamp := false
	if len(str) >= 20 && str[4] == '/' && str[7] == '/' && str[13] == ':' && str[16] == ':' {
		msg = str[20:]
		hasTimestamp = true
	}

	// Create custom clean timestamp
	timestamp := fmt.Sprintf("\033[90m%s\033[0m", time.Now().Format("15:04:05.000"))

	// Default parameters
	nodeID := "system"
	module := "SYSTEM"
	body := msg
	icon := "⚙️"
	nodeColor := "\033[90m" // Dark Gray default

	// Parse tags like [polaris][ingestion] or [Storage-polaris]
	if strings.HasPrefix(msg, "[") {
		endIdx := strings.Index(msg, "]")
		if endIdx > 0 {
			tag1 := msg[1:endIdx]
			remaining := msg[endIdx+1:]

			if strings.HasPrefix(remaining, "[") {
				endIdx2 := strings.Index(remaining, "]")
				if endIdx2 > 0 {
					tag2 := remaining[1:endIdx2]
					nodeID = tag1
					module = strings.ToUpper(tag2)
					body = remaining[endIdx2+1:]
				}
			} else {
				// Single tag
				tag := tag1
				if strings.HasPrefix(tag, "Storage-") {
					nodeID = tag[8:]
					module = "STORAGE"
					body = remaining
				} else if tag == "polaris" || tag == "sirius" || tag == "vega" {
					nodeID = tag
					module = "NODE"
					body = remaining
				} else {
					module = strings.ToUpper(tag)
					body = remaining
				}
			}
		}
	} else if strings.Contains(msg, "published") {
		nodeID = "sim"
		module = "PUBLISH"
		body = msg
	} else if strings.Contains(strings.ToLower(msg), "websocket") {
		module = "WEBSOCKET"
		body = msg
	}

	body = strings.TrimSpace(body)

	// Determine node color (polaris = Cyan, sirius = Purple, vega = Yellow, others = Gray)
	switch nodeID {
	case "polaris":
		nodeColor = "\033[36m" // Cyan
	case "sirius":
		nodeColor = "\033[35m" // Magenta/Purple
	case "vega":
		nodeColor = "\033[33m" // Yellow
	case "sim":
		nodeColor = "\033[32m" // Green
	}

	// Module customizations
	moduleColor := "\033[34m" // Blue default
	switch module {
	case "STORAGE":
		moduleColor = "\033[35m" // Magenta/Purple
		icon = "💾"
		if strings.Contains(body, "Successfully wrote") {
			icon = "💾"
		} else if strings.Contains(body, "Reloaded") {
			icon = "♻️"
		} else if strings.Contains(body, "ERROR") || strings.Contains(body, "Failed") {
			icon = "🛑"
		}
	case "INGESTION":
		moduleColor = "\033[32m" // Green
		if strings.Contains(body, "PRIMARY stored") {
			icon = "🟢"
		} else if strings.Contains(body, "REPLICA stored") {
			icon = "🟡"
		}
	case "NETWORK":
		moduleColor = "\033[33m" // Yellow
		icon = "📡"
	case "WEBSOCKET":
		moduleColor = "\033[96m" // Light Cyan
		icon = "🔌"
	case "CLUSTER":
		moduleColor = "\033[95m" // Light Purple
		icon = "🔗"
		if strings.Contains(body, "Connected to peer") {
			icon = "⚡"
		} else if strings.Contains(body, "Discovered new node") {
			icon = "✨"
		} else if strings.Contains(body, "marked as DOWN") {
			icon = "🛑"
		} else if strings.Contains(body, "marked as SUSPECT") {
			icon = "⚠️"
		}
	case "QUERY":
		moduleColor = "\033[36m" // Cyan
		icon = "🔎"
		if strings.Contains(body, "Aggregated stats") {
			icon = "📊"
		}
	case "DELETE":
		moduleColor = "\033[31m" // Red
		icon = "🗑️"
	case "PUBLISH":
		moduleColor = "\033[93m" // Bright Yellow
		icon = "📤"
	}

	// Make the body extremely human-readable and aesthetic
	body = humanize(body)

	// Format final visual line
	var formattedLine string
	nodePart := fmt.Sprintf("\033[1m%s[%s]\033[0m", nodeColor, nodeID)
	modulePart := fmt.Sprintf("%s[%s]\033[0m", moduleColor, module)

	if hasTimestamp {
		formattedLine = fmt.Sprintf("%s %s %s %s %s\n", timestamp, nodePart, modulePart, icon, body)
	} else {
		formattedLine = fmt.Sprintf("%s %s %s %s\n", nodePart, modulePart, icon, body)
	}

	return w.out.Write([]byte(formattedLine))
}

func SetupBeautifulLogging() {
	log.SetOutput(NewBeautifulWriter(os.Stdout))
	log.SetFlags(log.Ldate | log.Ltime)
}
