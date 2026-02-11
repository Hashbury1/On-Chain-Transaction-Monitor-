.PHONY: build run-ingestion run-worker clean deps docker-up docker-down all

# Build both services
build:
	@echo "Building On-Chain Transaction Monitor services..."
	@mkdir -p bin
	go build -o bin/ingestion cmd/ingestion/main.go
	go build -o bin/worker cmd/worker/main.go
	@echo "✓ Build complete!"
	@echo "  - bin/ingestion"
	@echo "  - bin/worker"

# Install dependencies
deps:
	@echo "Installing dependencies..."
	go get github.com/prometheus/client_golang/prometheus
	go get github.com/prometheus/client_golang/prometheus/promhttp
	go get github.com/sirupsen/logrus
	go mod tidy
	@echo "✓ Dependencies installed"

# Run ingestion service
run-ingestion:
	@echo "Starting ingestion service..."
	./bin/ingestion

# Run worker service
run-worker:
	@echo "Starting worker service..."
	./bin/worker

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/*
	@echo "✓ Clean complete"

# Start Docker infrastructure
docker-up:
	@echo "Starting Docker infrastructure..."
	cd infrastructure/docker-compose && docker-compose up -d
	@echo "✓ Infrastructure started"
	@echo "  - PostgreSQL: localhost:5432"
	@echo "  - Redis: localhost:6379"
	@echo "  - RabbitMQ: localhost:15672 (guest/guest)"
	@echo "  - Prometheus: localhost:9090"
	@echo "  - Grafana: localhost:3000 (admin/admin)"

# Stop Docker infrastructure
docker-down:
	@echo "Stopping Docker infrastructure..."
	cd infrastructure/docker-compose && docker-compose down
	@echo "✓ Infrastructure stopped"

# Build everything
all: deps build

# Show help
help:
	@echo "On-Chain Transaction Monitor - Available commands:"
	@echo ""
	@echo "  make deps           - Install Go dependencies"
	@echo "  make build          - Build both services"
	@echo "  make run-ingestion  - Run ingestion service"
	@echo "  make run-worker     - Run worker service"
	@echo "  make docker-up      - Start Docker infrastructure"
	@echo "  make docker-down    - Stop Docker infrastructure"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make all            - Install deps and build"
	@echo ""