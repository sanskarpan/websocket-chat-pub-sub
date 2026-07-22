.PHONY: build run test clean docker-build docker-run migrate

BINARY_NAME=server
BUILD_DIR=./build

build:
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/server

run:
	go run ./cmd/server

test:
	go test -v ./...

clean:
	rm -rf $(BUILD_DIR)
	rm -f $(BINARY_NAME)

docker-build:
	docker build -t websocket-chat:latest .

docker-run:
	docker-compose up -d

docker-stop:
	docker-compose down

migrate:
	@echo "Running database migrations..."
	@echo "Make sure PostgreSQL is running before executing migrations"

deps:
	go mod download
	go mod tidy

lint:
	golangci-lint run ./...

tidy:
	go mod tidy
	go mod verify
