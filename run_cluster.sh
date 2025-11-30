#!/bin/bash

echo "Starting MiniTrue Cluster..."

cleanup() {
    echo "Stopping all processes..."
    kill $(jobs -p) 2>/dev/null
    exit
}
trap cleanup SIGINT SIGTERM

# Kill any lingering processes on our standard ports
echo "Cleaning up lingering ports..."
for port in 7070 8080 8081 8082 9000 9001 9002 9100; do
    pid=$(lsof -t -i:$port 2>/dev/null)
    if [ ! -z "$pid" ]; then
        echo "Killing lingering process on port $port (PID: $pid)..."
        kill -9 $pid 2>/dev/null
    fi
done

# --- Storage nodes ---
# All three nodes run in 'all' mode and wait for writes via POST /ingest
# from the router. They no longer connect to any MQTT broker.

echo "Starting storage node 1 (polaris)..."
go run cmd/minitrue-server/main.go -mode=all &
sleep 1.5

echo "Starting storage node 2 (sirius)..."
go run cmd/minitrue-server/main.go -mode=all &
sleep 1.5

echo "Starting storage node 3 (vega)..."
go run cmd/minitrue-server/main.go -mode=all &
sleep 1.5

# --- Stateless ingestion router ---
# Joins the gossip ring and forwards incoming writes to primary + replica only.
echo "Starting minitrue-router (HTTP :7070)..."
go run cmd/minitrue-router/main.go \
  --node_id=router-1 \
  --port=7070 \
  --tcp_port=9100 \
  --seeds=localhost:9000,localhost:9001,localhost:9002 &
sleep 1.5

# --- Publisher / simulator ---
# Posts directly to the router over HTTP — no MQTT broker needed.
# Run on a different health check port to avoid conflict with storage nodes
echo "Starting publisher (sim mode -> router at :7070)..."
PORT=9999 go run cmd/publisher/main.go --sim=true --router=http://localhost:7070/route &
sleep 1

# --- Frontend ---
echo "Starting frontend..."
cd frontend
npm start &

wait
