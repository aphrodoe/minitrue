# Minitrue

A decentralized time-series database for IoT data, built in Go. Uses consistent hashing for data distribution, Gorilla compression for storage efficiency, and MQTT for high-throughput ingestion.

## Features

- **Decentralized**: Leaderless architecture with consistent hashing
- **Scalable**: Add nodes horizontally without downtime
- **Efficient**: Gorilla compression for timestamps and values
- **MQTT Ingestion**: High-speed data ingestion via MQTT
- **HTTP API**: Query aggregated data across nodes
- **React Frontend**: Web interface for querying data

## Prerequisites

- Go 1.21+
- Node.js 16+ and npm (for frontend)
- MQTT broker (Mosquitto)

### Installing Mosquitto

**Windows:**
```powershell
choco install mosquitto
```

**Linux:**
```bash
sudo apt-get install mosquitto mosquitto-clients
```

**macOS:**
```bash
brew install mosquitto
```

**Docker:**
```bash
docker run -it -p 1883:1883 eclipse-mosquitto
```

## Quick Start

### 1. Install Dependencies

```bash
go mod download
cd web && npm install && cd ..
```

### 2. Start MQTT Broker

```bash
mosquitto -d  # Linux/macOS
mosquitto     # Windows
```

### 3. Run Cluster

**Windows:**
```powershell
.\run_cluster.bat
```

**Linux/macOS:**
```bash
chmod +x run_cluster.sh
./run_cluster.sh
```

Or manually start nodes:

**Terminal 1 - Node 1 (Ingestion + Query):**
```bash
go run ./cmd/minitrue-server/main.go --mode=all --node_id=ing1 --port=8080
```

**Terminal 2 - Node 2 (Ingestion only):**
```bash
go run ./cmd/minitrue-server/main.go --mode=ingestion --node_id=ing2
```

**Terminal 3 - Node 3 (Ingestion only):**
```bash
go run ./cmd/minitrue-server/main.go --mode=ingestion --node_id=ing3
```

**Terminal 4 - Publisher:**
```bash
go run -tags=no_serial ./cmd/publisher/main_no_serial.go --sim=true
```

**Terminal 5 - Frontend:**
```bash
cd web
npm start
```

The frontend will open at `http://localhost:3000`. The backend query API runs on port 8080.

## Usage

### Query API

```bash
curl -X POST http://localhost:8080/query \
  -H "Content-Type: application/json" \
  -d '{
    "device_id": "sensor_1",
    "metric_name": "temperature",
    "operation": "avg",
    "start_time": 0,
    "end_time": 0
  }'
```

**Operations:** `avg`, `sum`, `max`, `min`

### MQTT Data Format

Publish to topic `iot/sensors/temperature`:

```json
{
  "device_id": "sensor_1",
  "metric_name": "temperature",
  "timestamp": 1609459200,
  "value": 23.5
}
```

## Configuration

- `--mode`: `ingestion`, `query`, or `all`
- `--node_id`: Unique node identifier (e.g., `ing1`, `ing2`)
- `--port`: HTTP port for query server (default: 8080)
- `--broker`: MQTT broker URL (default: `tcp://localhost:1883`)
- `--data_dir`: Data directory (default: `data`)

## Project Structure

```
minitrue/
├── cmd/
│   ├── minitrue-server/    # Main server
│   └── publisher/           # MQTT data publisher
├── internal/
│   ├── cluster/             # Consistent hashing
│   ├── compression/         # Gorilla compression
│   ├── ingestion/           # MQTT ingestion
│   ├── mqttclient/          # MQTT client wrapper
│   ├── query/               # HTTP query API
│   └── storage/             # Storage engine
└── web/                     # React frontend
```

## License

See LICENSE file.

