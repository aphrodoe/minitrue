# Minitrue - A Decentralized Time-Series Database

A decentralized, high-performance time-series database for IoT, built in Go.

## Core Features

- **Decentralized**: No single point of failure, leaderless architecture using Consistent Hashing
- **Scalable**: Horizontally scalable by simply adding more nodes
- **High-Performance**: Built with Gorilla Compression and a custom Parquet-style storage format
- **Resilient**: Survives node failures thanks to a Gossip protocol and data replication
- **MQTT Ingestion**: High-speed data ingestion via MQTT protocol
- **Distributed Queries**: HTTP API for querying aggregated data across nodes

## Architecture Overview

The system consists of three main components:

1. **Storage Engine (Lead B)**: Parquet-style on-disk format with Gorilla compression for timestamps and float values
2. **Distributed Systems (Lead C)**: Consistent hashing for data routing and Gossip protocol for cluster membership
3. **Ingestion & API (Lead D)**: MQTT-based ingestion and HTTP query API

## Prerequisites

- Go 1.21 or higher
- Node.js 16+ and npm (for React frontend)
- An MQTT broker (e.g., Mosquitto) - install separately or use Docker

### Installing Mosquitto (MQTT Broker)

**Windows:**
```powershell
# Using Chocolatey
choco install mosquitto

# Or download from: https://mosquitto.org/download/
```

**Linux:**
```bash
sudo apt-get install mosquitto mosquitto-clients  # Debian/Ubuntu
sudo yum install mosquitto                        # CentOS/RHEL
```

**macOS:**
```bash
brew install mosquitto
```

**Using Docker:**
```bash
docker run -it -p 1883:1883 -p 9001:9001 eclipse-mosquitto
```

## Installation & Running

### 1. Install Dependencies

```bash
go mod download
```

### 2. Start MQTT Broker

Make sure Mosquitto is running:
```bash
# Linux/macOS
mosquitto -d

# Windows (as a service or run manually)
mosquitto
```

### 3. Running a Single Node

**Ingestion + Query (all-in-one):**
```bash
go run ./cmd/minitrue-server/main.go --mode=all --node_id=ing1 --port=8080
```

**Ingestion only:**
```bash
go run ./cmd/minitrue-server/main.go --mode=ingestion --node_id=ing1
```

**Query only:**
```bash
go run ./cmd/minitrue-server/main.go --mode=query --node_id=ing1 --port=8080
```

### 4. Running a Multi-Node Cluster

**Windows:**
```powershell
.\run_cluster.bat
```

**Linux/macOS:**
```bash
chmod +x run_cluster.sh
./run_cluster.sh
```

Or manually start multiple nodes:

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

**Terminal 4 - Data Publisher (Simulator):**
```bash
go run ./cmd/publisher/main.go --sim=true
```

### 5. Running the React Frontend

**Install Node.js dependencies:**
```bash
cd web
npm install
```

**Start the React development server:**
```bash
npm start
```

The frontend will open automatically at `http://localhost:3000`. If it doesn't, open your browser and navigate to that URL.

**Note:** Make sure your backend server is running on port 8080. The React app is configured to proxy API requests to `http://localhost:8080`.

### 6. Querying Data

**Using the React Frontend (Recommended):**
1. Open `http://localhost:3000` in your browser
2. Fill in the query form:
   - Device ID: `sensor_1` (or `sensor_2`, `sensor_3`)
   - Metric Name: `temperature`
   - Operation: Select from dropdown (avg, sum, max, min)
   - Time Range: Use quick buttons or set Unix timestamps (0 = all data)
3. Click "Run Query"
4. View results in the dashboard

**Using cURL (Alternative):**
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

**Query Operations:**
- `avg` - Average value
- `sum` - Sum of values
- `max` - Maximum value
- `min` - Minimum value

**Example Response:**
```json
{
  "device_id": "sensor_1",
  "metric_name": "temperature",
  "operation": "avg",
  "result": 23.45,
  "count": 10,
  "duration_ms": 5
}
```

## Project Structure

```
minitrue/
├── cmd/
│   ├── minitrue-server/    # Main server entry point
│   ├── publisher/           # MQTT data publisher/simulator
│   └── simulator/           # Data simulator (placeholder)
├── internal/
│   ├── cluster/             # Cluster management (consistent hashing)
│   ├── compression/         # Gorilla compression implementation
│   ├── ingestion/           # MQTT ingestion service
│   ├── models/               # Data models
│   ├── mqttclient/           # MQTT client wrapper
│   ├── query/                # HTTP query API
│   └── storage/              # Storage engine (Parquet-style + Gorilla)
├── pkg/
│   ├── cluster/              # Consistent hashing & Gossip protocol
│   ├── models/               # Cluster models
│   └── network/              # Network client for inter-node communication
└── web/
    └── index.html            # Query dashboard UI
```

## Configuration

### Command-Line Flags

- `--mode`: Operation mode (`ingestion`, `query`, or `all`)
- `--node_id`: Unique node identifier (e.g., `ing1`, `ing2`)
- `--port`: HTTP port for query server (default: 8080)
- `--broker`: MQTT broker URL (default: `tcp://localhost:1883`)
- `--data_dir`: Directory for storing data files (default: `data`)

### Example

```bash
go run ./cmd/minitrue-server/main.go \
  --mode=all \
  --node_id=node1 \
  --port=8080 \
  --broker=tcp://localhost:1883 \
  --data_dir=./data
```

## Data Format

**Ingestion Format (MQTT Topic: `iot/sensors/#`):**
```json
{
  "device_id": "sensor_1",
  "metric_name": "temperature",
  "timestamp": 1609459200,
  "value": 23.5
}
```

## Testing Storage Engine

Test the storage engine directly:
```bash
go run ./cmd/storage-test/main.go
```

## Development

### Building

```bash
# Build the server
go build -o minitrue-server ./cmd/minitrue-server

# Build the publisher
go build -o publisher ./cmd/publisher
```

### Running Tests

```bash
go test ./...
```

## Troubleshooting

1. **MQTT Connection Failed**: Ensure Mosquitto is running and accessible
2. **Port Already in Use**: Change the port using `--port` flag
3. **Data Not Persisting**: Check that the `data_dir` directory exists and is writable
4. **Nodes Not Discovering Each Other**: Ensure all nodes can communicate and consistent hashing is properly initialized
5. **React Frontend Won't Connect**: 
   - Ensure backend is running on port 8080
   - Check browser console for CORS errors
   - Verify proxy settings in `web/package.json`
6. **npm install fails**: Make sure Node.js 16+ is installed (`node --version`)

## License

See LICENSE file for details.