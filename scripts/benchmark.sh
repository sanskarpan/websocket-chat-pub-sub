#!/bin/bash
set -e

echo "Running benchmark tests..."
cd "$(dirname "$0")/.."

echo "Building server..."
go build -o build/server ./cmd/server

echo "Starting server in background..."
./build/server &
SERVER_PID=$!

sleep 3

echo "Running WebSocket benchmark..."
# Add your k6 or other benchmark tool here
# Example: k6 run test/load/websocket.js

kill $SERVER_PID

echo "Benchmark complete!"
