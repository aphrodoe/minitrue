#!/usr/bin/env bash
set -e

echo "Starting mosquitto..."
mosquitto -d || echo "mosquitto may already be running or not installed"

echo "Starting ingestion node ing1 (primary candidate) on port 8080..."
nohup go run main.go --mode=ingestion --node_id=ing1 --port=8080 > ing1.log 2>&1 &

echo "Starting ingestion node ing2 (replica) on port 8081..."
nohup go run main.go --mode=ingestion --node_id=ing2 --port=8081 > ing2.log 2>&1 &

echo "Starting ingestion node ing3 (replica) on port 8082..."
nohup go run main.go --mode=ingestion --node_id=ing3 --port=8082 > ing3.log 2>&1 &

echo "Starting query node (HTTP :8081)..."
nohup go run main.go --mode=query --node_id=q1 --port=8081 > query.log 2>&1 &

echo "Starting simulated publisher (sim)..."
nohup go run arduino_publisher.go --sim > publisher.log 2>&1 &

echo "All started. Open web/index.html and run queries against http://localhost:8081/query"
