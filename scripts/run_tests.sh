#!/bin/bash
set -e

echo "Running all tests..."
cd "$(dirname "$0")/.."

go test -v -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

echo "Tests complete!"
