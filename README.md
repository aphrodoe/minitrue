# MiniTrue: Decentralized Time-Series Database

A high-performance, distributed time-series database built from scratch in Go, designed specifically for IoT sensor networks. MiniTrue combines academic distributed systems concepts with practical engineering to create a scalable, fault-tolerant data storage solution.


## The Problem

Modern IoT deployments face a critical challenge: How do you reliably store and query millions of sensor readings per second across hundreds or thousands of devices?

Traditional centralized databases create bottlenecks and single points of failure. As sensor networks scale, three major problems emerge:

1. **Storage Explosion**: Time-series data grows rapidly. A single temperature sensor recording every second generates 86,400 data points per day. With 1,000 sensors, that's 86 million points daily.

2. **Single Point of Failure**: Centralized systems mean one server crash loses access to all your data. For critical infrastructure monitoring (like power grids or industrial equipment), this is unacceptable.

3. **Query Performance**: As data volume increases, query latency degrades. Finding the average temperature across 100 sensors for the past week might take minutes instead of milliseconds.

Real-world impact: A smart city deployment with 10,000 sensors recording 4 metrics each at 1Hz would generate **3.5 billion data points per day**. Traditional databases either can't handle this load or require expensive enterprise solutions.


## Our Solution

MiniTrue tackles these problems through a decentralized, leaderless architecture that distributes both data and computation across multiple nodes. Our approach combines several key innovations:

**1. Distributed by Design**
- No master node = no single point of failure
- Data is automatically partitioned using consistent hashing
- Nodes discover each other via gossip protocol

**2. Intelligent Compression**
- Gorilla compression achieves 90%+ space savings on time-series data
- Stores billions of points efficiently
- Maintains fast read performance

**3. Smart Data Routing**
- Writes go directly to the correct node (no forwarding overhead)
- Queries automatically fan out to relevant nodes
- Results aggregate seamlessly

**4. Real-Time Ingestion**
- MQTT protocol handles high-frequency sensor data
- Asynchronous writes prevent blocking
- Batched disk I/O maximizes throughput

---

## System Architecture

### High-Level Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         IoT Sensors                             │
│  (Temperature, Humidity, Pressure, Motion, etc.)                │
└────────────┬────────────────────────────────────────────────────┘
             │ Publishes data via MQTT
             ▼
┌─────────────────────────────────────────────────────────────────┐
│                      MQTT Broker                                │
│                   (Mosquitto)                                   │
└─────────┬───────────────────────────────────────────────────────┘
          │ Distributes to subscribers
          ▼
┌─────────────────────────────────────────────────────────────────┐
│                  MiniTrue Cluster                               │
│  ┌──────────┐      ┌──────────┐       ┌──────────┐              │
│  │  Node 1  │ ←──→ │  Node 2  │ ←───→ │  Node 3  │              │
│  │  :8080   │      │  :8081   │       │  :8082   │              │
│  │  :9000   │      │  :9001   │       │  :9002   │              │
│  └──────────┘      └──────────┘       └──────────┘              │
│       │                  │                   │                  │
│       │  Gossip Protocol (Cluster Discovery) │                  │
│       │                  │                   │                  │
│       ▼                  ▼                   ▼                  │
│  ┌────────────────────────────────────────────────┐             │
│  │      Consistent Hash Ring (Data Distribution)  │             │
│  └────────────────────────────────────────────────┘             │
└─────────┬───────────────────────────────────────────────────────┘
          │ Query API (HTTP)
          ▼
┌─────────────────────────────────────────────────────────────────┐
│                    React Frontend                               │
│              (Real-time Visualization & Queries)                │
└─────────────────────────────────────────────────────────────────┘
```

### Data Flow Architecture

#### Write Path (Ingestion)
```
Sensor → MQTT Publish → MQTT Broker → Node Subscriber
                                            │
                                            ▼
                                    Consistent Hash
                                    (device_id:metric)
                                            │
                        ┌───────────────────┼───────────────────┐
                        ▼                   ▼                   ▼
                 Primary Node        Replica Node 1      Replica Node 2
                        │                   │                   │
                        ▼                   ▼                   ▼
                 In-Memory Buffer   In-Memory Buffer    In-Memory Buffer
                        │                   │                   │
                        └─────────┬─────────┴─────────┬─────────┘
                                  ▼                   ▼
                          Batch (10 points)    Gorilla Compress
                                  │                   │
                                  ▼                   ▼
                          Write to Parquet File  (.parq format)
```

**Write Process Steps:**
1. Sensor publishes data to MQTT topic `iot/sensors/{metric}`
2. All nodes subscribe, but only relevant nodes store the data
3. Consistent hash determines primary and replica nodes based on `device_id:metric_name`
4. Primary node stores with role="primary", replicas store with role="replica"
5. Data accumulates in memory buffer (default: 10 points)
6. On buffer full or 5-second timeout, batch is compressed and written to disk
7. Gorilla compression applied to timestamps and values
8. Written to Parquet-style columnar format

#### Read Path (Query)
```
Frontend → HTTP POST /query → Query Node (Any)
                                     │
                                     ▼
                          Extract device_id & metric
                                     │
                                     ▼
                          Consistent Hash Lookup
                                     │
                        ┌────────────┴────────────┐
                        ▼                         ▼
                Primary Node                 Replica Nodes
                        │                         │
                        ▼                         ▼
                Read .parq file           Read .parq file
                        │                         │
                        ▼                         ▼
                Decompress data           Decompress data
                        │                         │
                        └────────────┬────────────┘
                                     ▼
                          Aggregate Results (Sum/Avg/Min/Max)
                                     │
                                     ▼
                          Return JSON to Frontend
```

**Query Process Steps:**
1. Frontend sends POST to `/query` with device_id, metric_name, operation, time range
2. Query node uses consistent hashing to find nodes responsible for that data
3. Parallel HTTP requests to primary and replica nodes
4. Each node reads from disk, decompresses, filters by time range
5. Results aggregated at query node
6. Final statistics (count, sum, avg, min, max) returned as JSON

### Cluster Management

#### Node Discovery (Gossip Protocol)
```
Node Join Process:
    New Node (ing3) starts → Connects to seed node (ing1:9000)
                                         │
                                         ▼
                              ing1 shares cluster state
                              (all known nodes + status)
                                         │
                                         ▼
                              ing3 added to hash ring
                                         │
                     ┌───────────────────┴───────────────────┐
                     ▼                                       ▼
            ing1 gossips update              ing2 gossips update
            to other nodes                   to other nodes
                     │                                       │
                     └───────────────────┬───────────────────┘
                                         ▼
                         All nodes update their hash rings
                         ing3 now participates in data storage
```

**Gossip Protocol Details:**
- Every 2 seconds, each node picks random peers and exchanges state
- State includes: node_id, address, HTTP port, TCP port, status (active/down)
- Merkle trees detect state divergence efficiently
- Failed nodes detected via heartbeat timeout (gossip protocol)
- Hash ring automatically updated when nodes join/leave

#### Consistent Hashing
```
Virtual Nodes Ring (150 vnodes per physical node):

    ing1#0 → hash(42) ────┐
    ing2#0 → hash(97) ────┼──────→ sorted ring [42, 67, 97, ...]
    ing1#1 → hash(67) ────┘
    ...

Key Assignment:
    hash(sensor_1:temperature) = 85
                                  │
                                  ▼
                      Find next position ≥ 85 in ring
                                  │
                                  ▼
                              ing2#0 (97)
                                  │
                                  ▼
                      sensor_1:temperature → ing2 (primary)
                      
Replication:
    Continue clockwise for replicas:
        ing2 (primary) → ing1 (replica1) → ing3 (replica2)
```

**Why Consistent Hashing?**
- Adding/removing nodes only affects 1/N of keys (minimal data movement)
- Virtual nodes ensure even distribution despite hash collisions
- Deterministic: any node can calculate the same routing
- No central coordinator needed

---

## Feature Set

### Core Features
- **Decentralized Architecture**: Leaderless peer-to-peer system, no single point of failure
- **Automatic Data Partitioning**: Consistent hashing distributes data evenly across nodes
- **Data Replication**: Configurable replication (default: 2 replicas) for fault tolerance
- **Real-time Ingestion**: MQTT protocol handles thousands of messages per second
- **Gorilla Compression**: 90%+ storage reduction using delta-of-delta encoding
- **Distributed Queries**: Automatically routes queries to correct nodes and aggregates results
- **Gossip-based Discovery**: Nodes find each other without manual configuration
- **Time-based Queries**: Filter data by arbitrary time ranges
- **Aggregation Functions**: Support for avg, sum, min, max operations

### Advanced Features
- **In-Memory Buffering**: Reduces disk I/O by batching writes
- **Parquet-style Storage**: Columnar format optimized for analytical queries
- **Binary Search Optimization**: Fast time-range queries on sorted data
- **Concurrent Query Execution**: Parallel requests to multiple nodes
- **WebSocket Streaming**: Real-time data visualization in frontend
- **Auto-restart on Delete**: Server automatically reloads data after deletions
- **Multiple Data Formats**: Supports multiple metrics per device (temperature, humidity, etc.)

### Frontend Features
- **Interactive Query Builder**: Form-based interface for building queries
- **Real-time Dashboard**: Live streaming of incoming sensor data
- **Visual Graphs**: Canvas-based temperature graphs with sensor filtering
- **Time Range Presets**: Quick buttons for "Last Hour", "Last 24h", "Last Week", "All Data"
- **Custom Time Input**: Precise time range selection with 12-hour format
- **Per-sensor Visualization**: Filter graphs by individual sensors
- **Delete Operations**: Remove all data for specific device/metric combinations

---

## Data Structures & Algorithms

### 1. Consistent Hashing with Virtual Nodes

**Where Used**: `pkg/cluster/consistent_hash.go`

**Purpose**: Distribute data across nodes while minimizing redistribution when nodes join/leave.

**Implementation Details**:
```go
type ConsistentHashRing struct {
    ring         map[uint32]string  // hash → node_id mapping
    sortedHashes []uint32            // sorted hash positions
    virtualNodes int                 // 150 vnodes per node
    nodes        map[string]bool     // physical nodes
}
```

**Algorithm**:
1. Each physical node creates 150 virtual nodes
2. Virtual nodes are hashed using CRC32 and placed on ring
3. Data keys are hashed and placed on ring
4. Binary search finds the next node clockwise from key position
5. Replication: continue clockwise for N-1 additional nodes

**Why It's Ideal Here**:
- **Minimal Rebalancing**: When a node leaves, only keys between it and its predecessor need reassignment (1/N of total data)
- **Load Distribution**: Virtual nodes ensure even distribution even with few physical nodes
- **Deterministic**: Any node can independently calculate correct placement
- **No Coordination**: No need for a master to coordinate placement decisions

**Time Complexity**:
- Add/Remove Node: O(V log V) where V = virtual nodes (one-time cost)
- Get Node for Key: O(log V) via binary search
- Space: O(V) for storing hash positions

### 2. Binary Search for Time-Range Queries

**Where Used**: `internal/storage/unified_storage.go`

**Purpose**: Efficiently find data points within a time range from sorted in-memory arrays.

**Implementation**:
```go
func binarySearchStart(arr []sample, start int64) int {
    return sort.Search(len(arr), func(i int) bool {
        return arr[i].Timestamp >= start
    })
}

func binarySearchEnd(arr []sample, end int64) int {
    idx := sort.Search(len(arr), func(i int) bool {
        return arr[i].Timestamp > end
    })
    return idx - 1
}
```

**Algorithm**:
1. Data stored sorted by timestamp (maintained during insertion)
2. Binary search finds first element ≥ start_time
3. Binary search finds last element ≤ end_time
4. Iterate only through [start_idx, end_idx] range

**Why It's Ideal Here**:
- **Fast Queries**: O(log N + M) where N = total points, M = matches
- **No Full Scan**: Linear search would be O(N) for every query
- **Cache Friendly**: Once range found, sequential access is optimal
- **Memory Efficient**: No auxiliary data structures needed

**Time Complexity**:
- Finding Range: O(log N) per boundary
- Returning Results: O(M) where M = points in range
- Total: O(log N + M)

### 3. Sorted Insertion with Binary Search

**Where Used**: `internal/storage/unified_storage.go` (persist method)

**Purpose**: Maintain sorted order during data insertion for fast queries.

**Implementation**:
```go
insertPos := sort.Search(len(arr), func(i int) bool {
    return arr[i].Timestamp >= ts
})

if insertPos == len(arr) {
    arr = append(arr, newSample)
} else {
    arr = append(arr, sample{})
    copy(arr[insertPos+1:], arr[insertPos:])
    arr[insertPos] = newSample
}
```

**Algorithm**:
1. Binary search finds correct insertion position
2. Elements shifted right to make space
3. New element inserted at correct position
4. Array remains sorted

**Why It's Ideal Here**:
- **Maintains Sorted Order**: Critical for binary search queries
- **Amortized Efficiency**: Most inserts are at end (timestamps increase), O(1)
- **Better than Sort After Insert**: Sorting entire array each time would be O(N log N)

**Time Complexity**:
- Best Case (append): O(log N) search + O(1) insert = O(log N)
- Worst Case (prepend): O(log N) search + O(N) shift = O(N)
- Average Case: O(log N) since time-series data usually arrives in order

### 4. Merkle Tree for State Reconciliation

**Where Used**: `pkg/cluster/merkle.go`

**Purpose**: Efficiently detect differences in cluster state between nodes during gossip.

**Implementation**:
```go
type MerkleTree struct {
    root *MerkleNode
}

type MerkleNode struct {
    Hash  string
    Left  *MerkleNode
    Right *MerkleNode
    Data  interface{}
}
```

**Algorithm**:
1. Cluster state (list of nodes) forms leaf nodes
2. Hash each leaf node's data
3. Build tree bottom-up by hashing pairs of children
4. Root hash represents entire cluster state
5. Compare root hashes to detect differences
6. Traverse tree to find exactly which nodes differ

**Why It's Ideal Here**:
- **O(log N) Comparison**: Instead of comparing all N nodes, compare log N hashes
- **Bandwidth Efficient**: Only send hashes, not full state
- **Precise Diff**: Quickly identify exactly what changed
- **Cryptographically Secure**: Hash collisions extremely unlikely

**Time Complexity**:
- Build Tree: O(N) where N = cluster size
- Compare Trees: O(log N) to find differences
- Space: O(N) for storing tree

### 5. Hash Map for O(1) Data Lookup

**Where Used**: `internal/storage/unified_storage.go`

**Purpose**: Fast lookup of time-series data by device and metric combination.

**Implementation**:
```go
type UnifiedStorage struct {
    data map[string][]sample  // key: "device_id|metric_name"
}
```

**Algorithm**:
1. Key format: `device_id + "|" + metric_name`
2. Hash map provides O(1) average-case lookup
3. Each value is a sorted array of samples
4. Multiple devices/metrics stored independently

**Why It's Ideal Here**:
- **Fast Access**: Direct lookup instead of scanning all devices
- **Memory Efficient**: Only stores data that exists
- **Scalable**: Performance doesn't degrade with more devices
- **Natural Partitioning**: Each device-metric pair independent

**Time Complexity**:
- Insert: O(1) average case for hash lookup + O(log N) for sorted insert
- Query: O(1) for hash lookup + O(log N + M) for time-range search
- Space: O(D × M) where D = devices, M = metrics per device

### 6. Gorilla Compression (Delta-of-Delta Encoding)

**Where Used**: `internal/compression/gorilla.go`

**Purpose**: Compress time-series data using delta-of-delta encoding and XOR for floating-point values.

**Algorithm (Timestamps)**:
1. Store first timestamp as-is
2. Calculate delta: delta₁ = timestamp₂ - timestamp₁
3. Calculate delta-of-delta: Δ² = delta₂ - delta₁
4. Encode using variable-length encoding:
   - 0 bits if Δ² = 0 (constant interval)
   - 7 bits if -64 ≤ Δ² ≤ 63
   - 14 bits if -2048 ≤ Δ² ≤ 2047
   - 32 bits otherwise

**Algorithm (Values)**:
1. Store first value as float64
2. XOR with previous value
3. Count leading and trailing zeros in XOR result
4. Encode only the significant bits

**Why It's Ideal Here**:
- **Exploits Regularity**: Sensor data often at regular intervals (Δ² = 0)
- **90%+ Compression**: Typical IoT data compresses to ~1-2 bits per point
- **Fast Decompression**: Simple bitwise operations
- **Lossless**: No data loss unlike sampling

**Compression Ratio Example**:
```
Uncompressed: 16 bytes per point (8B timestamp + 8B value)
Compressed:   ~1.5 bytes per point average
Savings:      90.6%
```

### 7. Concurrent Data Structures (Mutex-Protected Maps)

**Where Used**: Throughout codebase, especially `internal/storage/unified_storage.go`

**Purpose**: Thread-safe access to shared data structures in concurrent environment.

**Implementation**:
```go
type UnifiedStorage struct {
    mu   sync.RWMutex
    data map[string][]sample
}

func (m *UnifiedStorage) Query(...) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    // read operations
}

func (m *UnifiedStorage) persist(...) {
    m.mu.Lock()
    defer m.mu.Unlock()
    // write operations
}
```

**Algorithm**:
- Read-Write Mutex allows multiple concurrent readers OR one writer
- Lock acquired before map access
- Deferred unlock ensures release even on panic

**Why It's Ideal Here**:
- **High Read Throughput**: Multiple queries can run concurrently
- **Data Integrity**: Prevents race conditions during writes
- **Correctness**: Mutex guarantees atomic updates

**Performance**:
- Read Lock: Minimal overhead when no writes
- Write Lock: Blocks all access but ensures consistency

---

## Project Structure

```
minitrue/
│
├── cmd/                                    # Entry points for executables
│   ├── minitrue-server/
│   │   └── main.go                        # Main server: starts ingestion/query/both
│   └── publisher/
│       ├── main.go                        # Data publisher (with Arduino serial)
│       └── main_no_serial.go              # Simulator (no hardware needed)
│
├── internal/                               # Core application logic (private)
│   ├── cluster/
│   │   ├── cluster.go                     # Cluster-wide hash ring management
│   │   ├── manager.go                     # Gossip protocol + TCP server initialization
│   │   └── message_handler.go             # Handles inter-node gossip messages
│   │
│   ├── compression/
│   │   └── gorilla.go                     # Delta-of-delta & XOR compression
│   │
│   ├── ingestion/
│   │   └── ingestion.go                   # MQTT subscriber, routes to storage
│   │
│   ├── models/
│   │   └── record.go                      # Data point struct definition
│   │
│   ├── mqttclient/
│   │   └── client.go                      # MQTT client wrapper (Paho library)
│   │
│   ├── query/
│   │   └── query.go                       # HTTP query API + distributed query logic
│   │
│   ├── storage/
│   │   ├── storage_engine.go              # Parquet-style file I/O with compression
│   │   └── unified_storage.go             # In-memory buffer + disk persistence
│   │
│   └── websocket/
│       └── websocket.go                   # WebSocket server for real-time frontend
│
├── pkg/                                    # Reusable libraries (public)
│   ├── cluster/
│   │   ├── consistent_hash.go             # Consistent hashing with virtual nodes
│   │   ├── consistent_hash_test.go        # Unit tests for hashing
│   │   ├── gossip.go                      # Gossip protocol implementation
│   │   ├── merkle.go                      # Merkle tree for state comparison
│   │   └── merkle_test.go                 # Merkle tree tests
│   │
│   ├── models/
│   │   └── cluster.go                     # Node metadata struct
│   │
│   └── network/
│       ├── client.go                      # TCP client for inter-node communication
│       └── server.go                      # TCP server for receiving gossip messages
│
├── frontend/                               # React web application
│   ├── public/
│   │   └── index.html                     # HTML template
│   │
│   ├── src/
│   │   ├── components/
│   │   │   ├── GradientText.js            # Animated gradient text component
│   │   │   ├── GradientText.css
│   │   │   ├── Particles.js               # 3D particle background
│   │   │   ├── Particles.css
│   │   │   ├── QueryForm.js               # Query builder form
│   │   │   ├── QueryForm.css
│   │   │   ├── QueryResults.js            # Display query results
│   │   │   ├── QueryResults.css
│   │   │   ├── RealTimeMonitor.js         # WebSocket-based live data feed
│   │   │   └── RealTimeMonitor.css
│   │   │
│   │   ├── App.js                         # Main React component
│   │   ├── App.css
│   │   ├── index.js                       # React entry point
│   │   └── index.css
│   │
│   ├── package.json                       # NPM dependencies
│   └── package-lock.json
│
├── data/                                   # Storage directory (created at runtime)
│   ├── ing1.parq                          # Node 1 data file
│   ├── ing2.parq                          # Node 2 data file
│   └── ing3.parq                          # Node 3 data file
│
├── go.mod                                  # Go module definition
├── go.sum                                  # Go dependency checksums
└── README.md                               # This file
```

### Key File Responsibilities

**Cluster Management**
- `pkg/cluster/consistent_hash.go`: Implements consistent hashing algorithm with virtual nodes
- `pkg/cluster/gossip.go`: Peer discovery and failure detection
- `pkg/cluster/merkle.go`: Efficient state comparison between nodes
- `internal/cluster/manager.go`: Orchestrates cluster membership and hash ring updates

**Data Storage**
- `internal/storage/storage_engine.go`: Reads/writes compressed Parquet files
- `internal/storage/unified_storage.go`: In-memory buffer with time-based queries
- `internal/compression/gorilla.go`: Implements Gorilla compression algorithm

**Data Ingestion**
- `internal/ingestion/ingestion.go`: Subscribes to MQTT topics, determines primary/replica
- `internal/mqttclient/client.go`: Wraps Paho MQTT client library

**Query Processing**
- `internal/query/query.go`: HTTP API handlers, distributed query coordination
- `internal/websocket/websocket.go`: Real-time data streaming to frontend

**Networking**
- `pkg/network/server.go`: TCP server for inter-node gossip messages
- `pkg/network/client.go`: TCP client for sending gossip to peers

---

## Getting Started

### Prerequisites

Before running MiniTrue, ensure you have:

- **Go 1.21 or higher**: [Download here](https://golang.org/dl/)
- **Node.js 16+ and npm**: [Download here](https://nodejs.org/)
- **MQTT Broker (Mosquitto)**: Instructions below

### Installing Mosquitto MQTT Broker

**Windows:**
```powershell
choco install mosquitto
```
Or download installer from [mosquitto.org](https://mosquitto.org/download/)

**Linux (Ubuntu/Debian):**
```bash
sudo apt-get update
sudo apt-get install mosquitto mosquitto-clients
sudo systemctl start mosquitto
sudo systemctl enable mosquitto
```

**macOS:**
```bash
brew install mosquitto
brew services start mosquitto
```

**Docker (Any OS):**
```bash
docker run -d -p 1883:1883 --name mosquitto eclipse-mosquitto
```

### Installation Steps

1. **Clone the repository**
```bash
git clone <repository-url>
cd minitrue
```

2. **Install Go dependencies**
```bash
go mod download
```

3. **Install frontend dependencies**
```bash
cd frontend
npm install
cd ..
```

4. **Verify Mosquitto is running**
```bash
# Test publish
mosquitto_pub -t test/topic -m "hello"

# Test subscribe (in another terminal)
mosquitto_sub -t test/topic
```

### Running the Cluster

#### Quick Start (Automated)

**Windows:**
```powershell
.\run_cluster.bat
```

**Linux/macOS:**
```bash
chmod +x run_cluster.sh
./run_cluster.sh
```

The script starts:
- 3 MiniTrue nodes (ports 8080-8082 for HTTP, 9000-9002 for TCP)
- 1 data publisher (simulates 3 sensors)
- Frontend dev server (port 3000)

#### Manual Start (For Development)

**Terminal 1 - Node 1 (Seed Node):**
```bash
go run cmd/minitrue-server/main.go -mode=all -node_id=ing1
```
- Runs on HTTP port 8080, TCP port 9000
- Both ingestion and query enabled
- Seed node for cluster formation

**Terminal 2 - Node 2:**
```bash
go run cmd/minitrue-server/main.go -mode=all -node_id=ing2 -seeds=localhost:9000
```
- Runs on HTTP port 8081, TCP port 9001
- Connects to ing1 as seed node

**Terminal 3 - Node 3:**
```bash
go run cmd/minitrue-server/main.go -mode=all -node_id=ing3 -seeds=localhost:9000
```
- Runs on HTTP port 8082, TCP port 9002
- Connects to ing1 as seed node

**Terminal 4 - Data Publisher:**
```bash
go run -tags=no_serial ./cmd/publisher/main_no_serial.go --sim=true
```
- Simulates 3 sensors: sensor_1, sensor_2, sensor_3
- Publishes temperature readings every second
- Uses topic: `iot/sensors/temperature`

**Terminal 5 - Frontend:**
```bash
cd frontend
npm start
```
- Opens browser at http://localhost:3000
- Connect to backend at http://localhost:8080

### Configuration Options

```bash
go run cmd/minitrue-server/main.go [options]
```

**Options:**
- `--mode`: Operation mode
  - `ingestion`: Only accept and store data (no queries)
  - `query`: Only serve queries (no ingestion)
  - `all`: Both ingestion and query (recommended)
  
- `--node_id`: Unique identifier for this node (e.g., ing1, ing2, ing3)
  - Used for consistent hashing
  - Must be unique across cluster

- `--port`: HTTP port for query API (default: auto-assigned based on node_id)
  - ing1 → 8080, ing2 → 8081, ing3 → 8082

- `--tcp_port`: TCP port for gossip protocol (default: auto-assigned)
  - ing1 → 9000, ing2 → 9001, ing3 → 9002

- `--broker`: MQTT broker URL (default: tcp://localhost:1883)

- `--data_dir`: Directory for data files (default: ./data)

- `--seeds`: Comma-separated list of seed node addresses
  - Format: `host:tcp_port,host:tcp_port`
  - Example: `localhost:9000,localhost:9001`

**Example Configurations:**

```bash
# Ingestion-only node on custom ports
go run cmd/minitrue-server/main.go \
  --mode=ingestion \
  --node_id=ingestion_node_1 \
  --tcp_port=10000 \
  --broker=tcp://mqtt-server:1883 \
  --seeds=node1:9000,node2:9000

# Query-only node
go run cmd/minitrue-server/main.go \
  --mode=query \
  --node_id=query_node_1 \
  --port=8090 \
  --tcp_port=10001 \
  --seeds=node1:9000

# Full node with custom data directory
go run cmd/minitrue-server/main.go \
  --mode=all \
  --node_id=node_a \
  --data_dir=/var/lib/minitrue/data \
  --seeds=seed1:9000,seed2:9000
```

### Using the System

#### Query API

**Endpoint:** `POST http://localhost:8080/query`

**Request Body:**
```json
{
  "device_id": "sensor_1",
  "metric_name": "temperature",
  "operation": "avg",
  "start_time": 0,
  "end_time": 0
}
```

**Parameters:**
- `device_id`: Sensor identifier (e.g., "sensor_1", "sensor_2")
- `metric_name`: Metric type (e.g., "temperature", "humidity")
- `operation`: Aggregation function
  - `avg`: Average value
  - `sum`: Sum of all values
  - `max`: Maximum value
  - `min`: Minimum value
- `start_time`: Unix timestamp (0 = beginning of time)
- `end_time`: Unix timestamp (0 = now)

**Response:**
```json
{
  "device_id": "sensor_1",
  "metric_name": "temperature",
  "operation": "avg",
  "result": 23.47,
  "count": 1543,
  "duration_ns": 2847293
}
```

**Example with curl:**
```bash
# Average temperature for sensor_1 (all time)
curl -X POST http://localhost:8080/query \
  -H "Content-Type: application/json" \
  -d '{
    "device_id": "sensor_1",
    "metric_name": "temperature",
    "operation": "avg",
    "start_time": 0,
    "end_time": 0
  }'

# Maximum temperature in last hour
START_TIME=$(date -d '1 hour ago' +%s)
END_TIME=$(date +%s)

curl -X POST http://localhost:8080/query \
  -H "Content-Type: application/json" \
  -d "{
    \"device_id\": \"sensor_2\",
    \"metric_name\": \"temperature\",
    \"operation\": \"max\",
    \"start_time\": $START_TIME,
    \"end_time\": $END_TIME
  }"
```

#### Publishing Data via MQTT

**Topic Format:** `iot/sensors/{metric_name}`

**Message Format (JSON):**
```json
{
  "device_id": "sensor_1",
  "metric_name": "temperature",
  "timestamp": 1609459200,
  "value": 23.5
}
```

**Example with mosquitto_pub:**
```bash
# Publish single reading
mosquitto_pub -t iot/sensors/temperature \
  -m '{"device_id":"sensor_1","metric_name":"temperature","timestamp":1609459200,"value":23.5}'

# Publish humidity reading
mosquitto_pub -t iot/sensors/humidity \
  -m '{"device_id":"sensor_1","metric_name":"humidity","timestamp":1609459200,"value":65.2}'
```

#### Frontend Usage

1. **Open browser** to http://localhost:3000

2. **Query Data Tab:**
   - Select device from dropdown
   - Select metric (temperature, humidity, etc.)
   - Choose aggregation operation (avg, sum, min, max)
   - Pick time range:
     - Quick buttons: Last Hour, Last 24h, Last Week, All Data
     - Or enter custom time range
   - Click "Run Query"

3. **Real-Time Monitor Tab:**
   - View live incoming data points
   - See messages per second
   - Click "Show Graph" for temperature visualization
   - Filter by individual sensors

---

## Team Contributions

This project was developed collaboratively by a team of four students as part of a DSA course project. Each member took ownership of specific components while working together on integration.

### Akhil Dhyani (B24CS1005) - Lead A: Go & Concurrency Lead

**Primary Responsibilities:**
- Designed and implemented the TCP-based inter-node communication layer (`pkg/network/`)
- Built the networking module handling concurrent gossip messages
- Developed the main server initialization logic (`cmd/minitrue-server/main.go`)
- Implemented goroutine-based concurrent request handling
- Created the cluster restart mechanism for data reload
- Integrated all distributed components into cohesive system
- Led debugging sessions for race conditions and deadlocks

**Key Contributions:**
- TCP server/client for gossip protocol communication
- Mutex-based concurrency control for shared data structures
- Background goroutines for periodic flush and gossip
- Server command-line flag parsing and configuration
- Cross-platform compatibility (Windows/Linux/macOS)

### Divyansh Yadav (B24CS1027) - Lead B: Storage & Compression Lead

**Primary Responsibilities:**
- Designed and implemented Parquet-style columnar storage format
- Built the Gorilla compression algorithm from scratch (`internal/compression/gorilla.go`)
- Developed the storage engine with compression integration (`internal/storage/storage_engine.go`)
- Created the in-memory buffer with batch write mechanism
- Implemented binary search for efficient time-range queries
- Optimized data layout for compression efficiency

**Key Contributions:**
- Delta-of-delta encoding for timestamps (achieving 90%+ compression)
- XOR-based floating-point compression for values
- Parquet file format with header, data chunks, and footer
- Sorted insertion maintaining query performance
- Buffer management with automatic flush on size/time thresholds
- Data persistence and reload functionality

### Harshit Deora (B24CM1078) - Lead C: Distributed Systems Lead

**Primary Responsibilities:**
- Designed and implemented consistent hashing with virtual nodes (`pkg/cluster/consistent_hash.go`)
- Built the gossip protocol for node discovery (`pkg/cluster/gossip.go`)
- Implemented Merkle trees for efficient state reconciliation
- Created the cluster manager coordinating membership (`internal/cluster/manager.go`)
- Designed data partitioning and replication strategy
- Developed cluster state synchronization logic

**Key Contributions:**
- Consistent hash ring with 150 virtual nodes per physical node
- Gossip protocol with configurable intervals and fanout
- Merkle tree comparison for detecting state divergence
- Automatic hash ring updates on node join/leave
- Seed node discovery and connection logic
- Fault tolerance through replication

### Gaurang Goyal (B24CM1071) - Lead D: Ingestion & API Lead

**Primary Responsibilities:**
- Designed and implemented MQTT ingestion pipeline (`internal/ingestion/ingestion.go`)
- Built the distributed query engine (`internal/query/query.go`)
- Created HTTP REST API for queries
- Implemented WebSocket streaming for frontend (`internal/websocket/websocket.go`)
- Developed data simulator for testing (`cmd/publisher/main_no_serial.go`)
- Built React frontend with real-time visualization

**Key Contributions:**
- MQTT subscriber with topic-based routing
- Distributed query logic with parallel node requests
- Query result aggregation across multiple nodes
- HTTP endpoints for queries and data deletion
- WebSocket server for live data streaming
- React components for query builder, results display, and live graphs
- Time-range input with custom masking and validation
- Canvas-based temperature visualization with sensor filtering


## Performance Characteristics

**Storage Efficiency:**
- Uncompressed: 16 bytes per data point (8B timestamp + 8B value)
- With Gorilla compression: ~1.5 bytes per point
- Compression ratio: **90.6% space savings**

**Query Performance:**
- Local queries: <5ms average latency
- Distributed queries (3 nodes): <20ms average latency
- Time-range queries use O(log N + M) binary search
- Aggregations computed on-the-fly without full data transfer

**Ingestion Throughput:**
- Single node: 10,000+ points/second via MQTT
- 3-node cluster: 30,000+ points/second aggregate
- Batched writes reduce disk I/O overhead
- Asynchronous processing prevents blocking

**Scalability:**
- Linear scaling with node count (tested up to 3 nodes)
- Adding node redistributes ~1/N of existing data
- No downtime during node addition
- Consistent hashing minimizes rebalancing


## Future Enhancements

While MiniTrue is feature-complete for its original scope, potential extensions include:

1. **Write-Ahead Log (WAL)**: Ensure durability between buffer and disk
2. **Compaction**: Merge small files to reduce file count
3. **Range Queries**: Return raw data points instead of just aggregates
4. **Multi-Metric Queries**: Query multiple metrics in single request
5. **Authentication**: Secure MQTT and HTTP endpoints
6. **Monitoring Dashboard**: Cluster health metrics and visualization
7. **Dynamic Replication**: Adjust replication factor at runtime
8. **Cross-Datacenter Replication**: Support for geographic distribution
