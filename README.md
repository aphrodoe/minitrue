# MiniTrue

MiniTrue is a distributed time-series database built in Go for IoT-style telemetry workloads. It combines leaderless cluster membership, deterministic partitioning, compressed local storage, distributed aggregation queries, and a React UI for querying and live monitoring.

The current codebase runs as a three-node local cluster by default:

- `polaris`: HTTP `:8080`, TCP `:9000`
- `sirius`: HTTP `:8081`, TCP `:9001`
- `vega`: HTTP `:8082`, TCP `:9002`

All three nodes are symmetric peers. There is no permanent coordinator, no central metadata service, and no required master node.

## Overview

MiniTrue is designed around four core ideas:

1. `Leaderless cluster membership`
   Nodes discover each other through peer-to-peer gossip over TCP. Any node can join by contacting known peers.

2. `Deterministic data placement`
   Records are mapped to owners using a consistent hash ring with virtual nodes. Each key resolves to an ordered preference list of peers.

3. `Fast local analytics`
   Data is stored in chunked in-memory series with pre-aggregated chunk metadata, then persisted to a compressed custom columnar file format.

4. `Distributed query execution`
   Any reachable HTTP node can accept a query, fan it out to the responsible peers, and merge the returned aggregate stats.

## Architecture

```text
Sensors / Simulator / Arduino Serial
                |
                v
         MQTT Broker (:1883)
                |
                v
    +-------------------------------+
    |     MiniTrue cluster          |
    |                               |
    |  polaris  sirius   vega       |
    |  :8080    :8081    :8082      |
    |  :9000    :9001    :9002      |
    |                               |
    |  gossip + hash ring + storage |
    +-------------------------------+
                |
                v
      React frontend / curl / tools
```

### Cluster model

- `Membership`: peer-to-peer gossip via TCP.
- `Placement`: CRC32-based consistent hashing with `150` virtual nodes per physical node.
- `Replication view`: the code currently queries and routes writes using a preference list of size `2`.
- `Query coordination`: temporary and request-scoped only. The node that receives a request coordinates that request and nothing more.
- `Bootstrap`: in local development, nodes auto-discover peers from the known local TCP port set if `--seeds` is omitted.

### Cluster Migration

- When cluster membership changes, nodes dynamically query peers for their keys (`GET /internal/keys`) and immediately fetch records for key ranges they now own. Data from lost key ranges is kept for a grace period and gracefully cleaned up in the background.

### Local node slots

When you run `go run cmd/minitrue-server/main.go -mode=all` with no explicit `--node_id`, the server auto-assigns the first free local slot:

| Node ID | HTTP | TCP |
| --- | --- | --- |
| `polaris` | `8080` | `9000` |
| `sirius` | `8081` | `9001` |
| `vega` | `8082` | `9002` |

That is why the cluster scripts can start all three nodes with the exact same command.

## Data flow

### Write path

```text
Publisher -> MQTT topic iot/sensors/{metric}
          -> every node receives the message
          -> each node computes the preference list for device_id:metric_name
          -> primary owner persists as primary
          -> next preferred node persists as replica
          -> primary batches records in memory
          -> batch flush writes compressed columnar data to disk
```

#### Write path details

- Every node subscribes to `iot/sensors/#` (with QoS 1 for at-least-once delivery) and extracts the device ID and metric name directly from the topic to avoid parsing JSON payloads for data it does not own.
- Incoming payloads are expected to contain:
  - `device_id`
  - `metric_name`
  - `timestamp`
  - `value`
- Routing key: `device_id + ":" + metric_name`
- Primary records are appended to the disk write batch.
- Replica records are also persisted durably to disk.
- In-memory chunks ignore duplicate samples with identical timestamps to ensure redeliveries do not double-count metrics (Idempotency).
- Nodes periodically fetch a lightweight digest from peers (`GET /internal/digest?series=<key>`) and pull missing records (`GET /internal/sync?series=<key>`) to ensure high availability (Replica Read-Repair).
- Batch size is `10` records.
- A periodic flush runs every `5` seconds if the batch is non-empty.

### Query path

```text
Client -> POST /query on any healthy node
      -> receiving node resolves owners from the hash ring
      -> parallel POST /query-aggregated to responsible peers
      -> local node also executes the same aggregate lookup
      -> stats are merged
      -> avg / sum / min / max is returned
```

#### Query path details

- Main endpoint: `POST /query`
- Internal fanout endpoint: `POST /query-aggregated`
- Sample endpoint: `POST /query-samples`
- If distributed fanout fails, the query handler falls back to a local aggregate query.
- Query time is reported back as `duration_ns`.

### Delete path

```text
Client -> POST /delete
      -> remove series from in-memory map
      -> filter pending primary batch
      -> read on-disk records
      -> rewrite file without matching device_id + metric_name
      -> delete file entirely if no records remain
```

Delete is handled in place. The current implementation does not restart the node after deletion.

### Live monitor path

```text
MQTT -> backend websocket hub -> /ws on any node -> React real-time monitor
```

- The frontend discovers active nodes from `GET /cluster/members` and opens `ws://<active-node>/ws` on one node at a time.
- On disconnect, it retries another active node after `3` seconds.
- Query-mode nodes subscribe to MQTT only for this read-only WebSocket bridge; they do not persist primary or replica records.
- The monitor keeps up to `100` recent data points in the UI and can render a temperature graph.

## Storage model

MiniTrue stores time-series data in two layers:

1. `In-memory series`
   - Keyed by `device_id|metric_name`
   - Each series contains chunk objects
   - Each chunk stores:
     - `StartTime`
     - `EndTime`
     - `Sum`
     - `Min`
     - `Max`
     - `Count`
     - raw `Samples`

2. `On-disk columnar file`
   - One file per node, for example `data/polaris.parq`
   - Custom file format written by `internal/storage/storage_engine.go`
   - Columns:
     - `timestamp`
     - `value`
     - `device_id`
     - `metric_name`

### Query efficiency

The storage layer uses chunk pre-aggregation:

- If a chunk is fully inside the requested time range, aggregate stats are read in `O(1)` from chunk metadata.
- Only boundary chunks fall back to binary search plus sample scanning.
- Raw sample queries use binary search within each relevant chunk.

### Compression

The on-disk format uses custom compression helpers from [internal/compression/gorilla.go](/Users/divyansh/Projects/MiniTrue-Time-Series-Database/internal/compression/gorilla.go):

- timestamps: delta-of-delta style integer compression
- values: Gorilla-style XOR floating point compression

## Current backend behavior

### Server modes

The main server supports:

- `--mode=ingestion`: subscribes to sensor MQTT topics for ownership routing and persists only records this node owns as primary or replica. It does not start the query HTTP/WebSocket server.
- `--mode=query`: starts the query HTTP server and `/ws`. It subscribes to MQTT only as a lightweight, read-only feed for the real-time monitor and never calls primary/replica persistence for those messages.
- `--mode=all`: combines ingestion persistence with query HTTP/WebSocket serving.

`all` is the normal local development mode.

### Config flags

```bash
go run cmd/minitrue-server/main.go [options]
```

| Flag | Purpose |
| --- | --- |
| `--mode` | `ingestion`, `query`, or `all` |
| `--node_id` | explicit node identity |
| `--port` | HTTP port override |
| `--tcp_port` | TCP gossip port override |
| `--broker` | MQTT broker URL |
| `--data_dir` | storage directory |
| `--seeds` | comma-separated peer TCP addresses |

Notes:

- If you set `--port` or `--tcp_port`, you should also set `--node_id`.
- If `--seeds` is omitted in the default local setup, the node automatically tries the other local peer ports.

## Frontend behavior

The React frontend lives in [frontend/src/App.js](/Users/divyansh/Projects/MiniTrue-Time-Series-Database/frontend/src/App.js) and has two main tabs:

1. `Query Data`
2. `Real-Time Monitor`

### Query UI

The query form supports:

- device selection
- metric selection
- aggregate operation selection
- quick time presets:
  - last hour
  - last 24 hours
  - last week
  - all data
- manual 12-hour timestamp input with validation
- delete-all-data for a selected `device_id` and `metric_name`

The delete UX is implemented as a guarded confirmation flow with success and failure dialogs.

### Cluster-aware frontend client

HTTP failover is implemented in [frontend/src/clusterClient.js](/Users/divyansh/Projects/MiniTrue-Time-Series-Database/frontend/src/clusterClient.js):

- request order: active nodes discovered from `GET /cluster/members`
- retries continue when:
  - the node is unreachable
  - the node returns a `5xx`
- this is used for query and delete actions

### Real-time monitor

The live monitor:

- opens a websocket to one local node at a time
- auto-fails over to the next node if a connection cannot be established
- shows:
  - connection status
  - total message count
  - messages per second
  - recent data feed
  - optional temperature graph

## API reference

### `POST /query`

Accepts:

```json
{
  "device_id": "sensor_1",
  "metric_name": "temperature",
  "operation": "avg",
  "start_time": 0,
  "end_time": 0
}
```

Rules:

- `device_id`, `metric_name`, and `operation` are required.
- `operation` must be one of:
  - `avg`
  - `sum`
  - `max`
  - `min`
- `start_time == 0` means unbounded start.
- `end_time == 0` means unbounded end.

Returns:

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

### `POST /query-samples`

Accepts the same request shape and returns raw sample values:

```json
{
  "samples": [22.1, 22.4, 22.6]
}
```

### `POST /query-aggregated`

Internal aggregation endpoint used during distributed fanout. It returns `sum`, `count`, `min`, and `max` stats for a local store.

### `POST /delete`

Accepts:

```json
{
  "device_id": "sensor_1",
  "metric_name": "temperature"
}
```

Behavior:

- removes the selected series from memory
- filters pending primary write batches
- rewrites the node file without matching records

### `GET /ws`

WebSocket endpoint for live sensor stream updates. Query-mode nodes support this endpoint by using a read-only MQTT subscription; they do not ingest or persist data from that subscription.

### `GET /ws/stats`

Returns websocket hub metadata such as connected client count and service status.

## MQTT payload format

Topic pattern:

```text
iot/sensors/{metric_name}
```

Example message:

```json
{
  "device_id": "sensor_1",
  "metric_name": "temperature",
  "timestamp": 1710000000,
  "value": 24.2
}
```

## Running the project

### Prerequisites

- Go `1.21+`
- Node.js and npm
- an MQTT broker on `tcp://localhost:1883`

### Install dependencies

```bash
go mod download
cd frontend
npm install
cd ..
```

### Start the full local stack

Linux / macOS:

```bash
chmod +x run_cluster.sh
./run_cluster.sh
```

Windows:

```powershell
.\run_cluster.bat
```

The scripts:

- clean ports `8080`, `8081`, `8082`, `9000`, `9001`, `9002`
- start three symmetric MiniTrue nodes
- start the simulator publisher
- start the React dev server

### Manual start

Terminal 1:

```bash
go run cmd/minitrue-server/main.go -mode=all
```

Terminal 2:

```bash
go run cmd/minitrue-server/main.go -mode=all
```

Terminal 3:

```bash
go run cmd/minitrue-server/main.go -mode=all
```

Publisher:

```bash
go run cmd/publisher/main.go --sim=true
```

Frontend:

```bash
cd frontend
npm start
```

### Custom node example

```bash
go run cmd/minitrue-server/main.go \
  --mode=all \
  --node_id=node_a \
  --port=8090 \
  --tcp_port=10090 \
  --seeds=localhost:9000,localhost:9001
```

## Example usage

### Query from curl

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

The same request can be sent to `:8081` or `:8082`.

### Publish with Mosquitto

```bash
mosquitto_pub -t iot/sensors/temperature \
  -m '{"device_id":"sensor_1","metric_name":"temperature","timestamp":1710000000,"value":23.5}'
```

## Repository structure

```text
cmd/
  minitrue-server/   main cluster node entrypoint
  publisher/         simulator and Arduino serial publisher

internal/
  cluster/           gossip, hash ring, Merkle sync, message handling
  compression/       Gorilla-style compression helpers
  ingestion/         MQTT subscriber and write routing
  logger/            terminal log formatting
  models/            shared structs
  mqttclient/        MQTT wrapper
  network/           TCP client and server
  query/             HTTP API and distributed query logic
  storage/           in-memory series and on-disk file engine
  websocket/         websocket hub for live monitor

frontend/
  src/               React app, cluster-aware client, query UI, live monitor

run_cluster.sh       local automation for Unix-like systems
run_cluster.bat      local automation for Windows
```

## Important implementation notes

- The project is `leaderless`, but each individual key still has a deterministic primary owner and ordered preference list.
- The current gossip protocol is peer-to-peer and state-based. It is not backed by Raft, etcd, or an external consensus service.
- The current UI tries `/devices` and `/metrics`, but the backend does not currently expose those endpoints, so the frontend falls back to built-in defaults.
- The current simulator publishes only `temperature` readings for `sensor_1`, `sensor_2`, and `sensor_3`.
- The in-memory live monitor is cluster-aware at the client level, not globally load-balanced by a reverse proxy.

