#!/bin/bash

echo "Starting MiniTrue Cluster..."

# Stop any background jobs if the script is interrupted
cleanup() {
    echo "Stopping all processes..."
    kill $(jobs -p) 2>/dev/null
    exit
}
trap cleanup SIGINT SIGTERM

# Proactively kill any lingering processes on ports we use
echo "Cleaning up lingering ports..."
for port in 8080 8081 8082 9000 9001 9002; do
    pid=$(lsof -t -i:$port 2>/dev/null)
    if [ ! -z "$pid" ]; then
        echo "Killing lingering process on port $port (PID: $pid)..."
        kill -9 $pid 2>/dev/null
    fi
done

# Start Node 1
go run cmd/minitrue-server/main.go -mode=all &
sleep 1.5

# Start Node 2
go run cmd/minitrue-server/main.go -mode=all &
sleep 1.5

# Start Node 3
go run cmd/minitrue-server/main.go -mode=all &
sleep 1.5

# Start Data Publisher (Simulator)
go run cmd/publisher/main.go --sim=true &
sleep 1.5

# Start Frontend
cd frontend
npm start &

# Wait for all processes
wait
