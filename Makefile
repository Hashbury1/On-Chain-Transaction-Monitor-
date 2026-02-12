.PHONY: build run-ingestion run-worker clean deps docker-up docker-down all help

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
	go get github.com/lib/pq
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

# Docker commands
docker-up:
	@echo "Starting Docker infrastructure..."
	cd infrastructure/docker-compose && docker compose up -d
	@sleep 5
	@echo "Initializing database..."
	@-cd infrastructure/docker-compose && docker exec -i tx-monitor-db psql -U blockchain_user -d blockchain_monitor < init-db.sql 2>/dev/null
	@echo "✓ Infrastructure started"
	@echo ""
	@echo "Access points:"
	@echo "  - PostgreSQL: localhost:5432 (blockchain_user/blockchain_pass)"
	@echo "  - Redis: localhost:6379"
	@echo "  - RabbitMQ: http://localhost:15672 (guest/guest)"
	@echo "  - Prometheus: http://localhost:9090"
	@echo "  - Grafana: http://localhost:3000 (admin/admin)"

docker-down:
	@echo "Stopping Docker infrastructure..."
	cd infrastructure/docker-compose && docker compose down
	@echo "✓ Infrastructure stopped"

docker-logs:
	@echo "Showing Docker logs (Ctrl+C to exit)..."
	cd infrastructure/docker-compose && docker compose logs -f

docker-status:
	@echo "Docker infrastructure status:"
	cd infrastructure/docker-compose && docker compose ps

db-init:
	@echo "Initializing database..."
	cd infrastructure/docker-compose && docker exec -i tx-monitor-db psql -U blockchain_user -d blockchain_monitor < init-db.sql
	@echo "✓ Database initialized"

db-shell:
	@echo "Opening database shell (type \q to exit)..."
	docker exec -it tx-monitor-db psql -U blockchain_user -d blockchain_monitor

db-query:
	@echo "Event count:"
	@docker exec -it tx-monitor-db psql -U blockchain_user -d blockchain_monitor -c "SELECT COUNT(*) FROM events;"
	@echo ""
	@echo "Subscription count:"
	@docker exec -it tx-monitor-db psql -U blockchain_user -d blockchain_monitor -c "SELECT COUNT(*) FROM subscriptions;"

# Build everything
all: deps build

# Show help
help:
	@echo "On-Chain Transaction Monitor - Available commands:"
	@echo ""
	@echo "Building:"
	@echo "  make deps           - Install Go dependencies"
	@echo "  make build          - Build both services"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make all            - Install deps and build"
	@echo ""
	@echo "Running:"
	@echo "  make run-ingestion  - Run ingestion service"
	@echo "  make run-worker     - Run worker service"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-up      - Start all infrastructure"
	@echo "  make docker-down    - Stop all infrastructure"
	@echo "  make docker-status  - Show container status"
	@echo "  make docker-logs    - Show container logs"
	@echo ""
	@echo "Database:"
	@echo "  make db-init        - Initialize database schema"
	@echo "  make db-shell       - Open PostgreSQL shell"
	@echo "  make db-query       - Show quick stats"
	@echo ""

.DEFAULT_GOAL := help
