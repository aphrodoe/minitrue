#!/bin/bash
set -e

echo "Starting Minitrue Cluster..."

# Start MQTT broker (if not running)
if ! pgrep -x "mosquitto" > /dev/null; then
    echo "Starting mosquitto..."
    mosquitto -d || echo "mosquitto may already be running or not installed"
    sleep 2
fi

# Create logs directory
mkdir -p logs

# Start Node 1 (Ingestion + Query)
echo "Starting node ing1 (ingestion + query) on port 8080..."
go run ./cmd/minitrue-server/main.go --mode=all --node_id=ing1 --port=8080 > logs/ing1.log 2>&1 &
NODE1_PID=$!
sleep 2

# Start Node 2 (Ingestion only)
echo "Starting node ing2 (ingestion) on port 8081..."
go run ./cmd/minitrue-server/main.go --mode=ingestion --node_id=ing2 > logs/ing2.log 2>&1 &
NODE2_PID=$!
sleep 2

# Start Node 3 (Ingestion only)
echo "Starting node ing3 (ingestion) on port 8082..."
go run ./cmd/minitrue-server/main.go --mode=ingestion --node_id=ing3 > logs/ing3.log 2>&1 &
NODE3_PID=$!
sleep 2

# Start publisher
echo "Starting simulated publisher..."
go run -tags=no_serial ./cmd/publisher/main_no_serial.go --sim=true > logs/publisher.log 2>&1 &
PUB_PID=$!

echo ""
echo "Cluster started!"
echo "  Node 1 (ing1): http://localhost:8080/query"
echo "  Logs: logs/ing1.log"
echo ""
echo "To stop, press Ctrl+C or run: kill $NODE1_PID $NODE2_PID $NODE3_PID $PUB_PID"
echo ""
echo "Open web/index.html in your browser to query data"

# Wait for interrupt
trap "echo 'Shutting down...'; kill $NODE1_PID $NODE2_PID $NODE3_PID $PUB_PID 2>/dev/null; exit" INT TERM

wait



