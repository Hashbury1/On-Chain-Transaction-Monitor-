#!/bin/bash

# Blockchain Monitor - Project Structure Setup Script
# This script creates the complete directory structure and starter files

set -e

PROJECT_NAME="blockchain-monitor"
BASE_DIR="$PWD/$PROJECT_NAME"

echo "🚀 Setting up Blockchain Monitor project structure..."

# Create main directory
mkdir -p "$BASE_DIR"
cd "$BASE_DIR"

# Create directory structure
echo "📁 Creating directory structure..."

# Services
mkdir -p services/ingestion/{cmd,internal/{blockchain,processor,publisher},config,pkg}
mkdir -p services/worker/{cmd,internal/{consumer,notifier,retry},config,pkg}
mkdir -p services/api/{cmd,internal/{handlers,models,repository,middleware},config,pkg}

# Infrastructure
mkdir -p infrastructure/terraform/{aws,gcp,modules/{vpc,ecs,rds,redis,monitoring}}
mkdir -p infrastructure/kubernetes/{base,overlays/{staging,production},helm}
mkdir -p infrastructure/docker-compose

# Monitoring
mkdir -p monitoring/prometheus/{rules,targets}
mkdir -p monitoring/grafana/{dashboards,provisioning/{datasources,dashboards}}
mkdir -p monitoring/logging

# Scripts
mkdir -p scripts/{deployment,database,testing}

# Tests
mkdir -p tests/{integration,e2e,load}

# Documentation
mkdir -p docs/{architecture,runbooks,api}

# CI/CD
mkdir -p .github/workflows

# Config
mkdir -p config/{development,staging,production}

echo "✅ Directory structure created!"

# Create .gitignore
echo "📝 Creating .gitignore..."
cat > .gitignore << 'EOF'
# Binaries
*.exe
*.exe~
*.dll
*.so
*.dylib
bin/
dist/

# Test coverage
*.out
coverage.html
*.test

# IDE
.idea/
.vscode/
*.swp
*.swo
*~

# Environment
.env
.env.*
!.env.example

# Terraform
*.tfstate
*.tfstate.*
.terraform/
*.tfvars
!example.tfvars

# Logs
*.log
logs/

# OS
.DS_Store
Thumbs.db

# Dependencies
node_modules/
vendor/

# Secrets
secrets/
*.pem
*.key
!*.key.example

# Build
build/
dist/
EOF

# Create .env.example
echo "📝 Creating .env.example..."
cat > .env.example << 'EOF'
# Blockchain Configuration
RPC_PRIMARY_URL=wss://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY
RPC_FALLBACK_URL=https://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY
CHAIN_ID=1
START_BLOCK=latest
CONFIRMATION_BLOCKS=12

# Message Queue
MESSAGE_QUEUE_TYPE=rabbitmq  # rabbitmq or kafka
RABBITMQ_URL=amqp://guest:guest@localhost:5672/
RABBITMQ_EXCHANGE=blockchain-events
RABBITMQ_QUEUE=notifications
KAFKA_BROKERS=localhost:9092
KAFKA_TOPIC=blockchain-events

# Database
DATABASE_URL=postgresql://user:password@localhost:5432/blockchain_monitor?sslmode=disable
DATABASE_MAX_CONNECTIONS=25
DATABASE_MAX_IDLE_CONNECTIONS=5

# Redis
REDIS_URL=redis://localhost:6379/0
REDIS_PASSWORD=
REDIS_DB=0

# API Configuration
API_PORT=8080
API_HOST=0.0.0.0
JWT_SECRET=your-super-secret-jwt-key-change-in-production
RATE_LIMIT_PER_MINUTE=60

# Monitoring
METRICS_PORT=9090
PROMETHEUS_PORT=9091
GRAFANA_PORT=3000

# Logging
LOG_LEVEL=info  # debug, info, warn, error
LOG_FORMAT=json  # json or text

# Notification Services
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/YOUR_WEBHOOK
SLACK_WEBHOOK_URL=https://hooks.slack.com/services/YOUR_WEBHOOK

# Retry Configuration
MAX_RETRY_ATTEMPTS=5
RETRY_BACKOFF_MULTIPLIER=2
RETRY_MAX_BACKOFF_SECONDS=60

# Circuit Breaker
CIRCUIT_BREAKER_THRESHOLD=5
CIRCUIT_BREAKER_TIMEOUT_SECONDS=30
CIRCUIT_BREAKER_HALF_OPEN_REQUESTS=3

# AWS (if using)
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=
AWS_SECRET_ACCESS_KEY=

# Deployment
ENVIRONMENT=development  # development, staging, production
SERVICE_NAME=blockchain-monitor
VERSION=v1.0.0
EOF

# Create Makefile
echo "📝 Creating Makefile..."
cat > Makefile << 'EOF'
.PHONY: help build test run clean docker-build docker-up docker-down deploy

# Variables
GO_VERSION := 1.21
DOCKER_REGISTRY := your-registry
APP_VERSION := $(shell git describe --tags --always --dirty)
SERVICES := ingestion worker api

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install: ## Install dependencies
	@echo "Installing dependencies..."
	cd services/ingestion && go mod download
	cd services/worker && go mod download
	cd services/api && go mod download

build: ## Build all services
	@echo "Building services..."
	@for service in $(SERVICES); do \
		echo "Building $$service..."; \
		cd services/$$service && go build -o ../../bin/$$service cmd/main.go && cd ../..; \
	done

test: ## Run unit tests
	@echo "Running unit tests..."
	@for service in $(SERVICES); do \
		echo "Testing $$service..."; \
		cd services/$$service && go test -v -race -coverprofile=coverage.out ./... && cd ../..; \
	done

test-integration: ## Run integration tests
	@echo "Running integration tests..."
	docker-compose -f infrastructure/docker-compose/docker-compose.test.yml up --abort-on-container-exit
	docker-compose -f infrastructure/docker-compose/docker-compose.test.yml down

test-load: ## Run load tests
	@echo "Running load tests..."
	k6 run tests/load/scenarios.js

lint: ## Run linters
	@echo "Running linters..."
	@for service in $(SERVICES); do \
		echo "Linting $$service..."; \
		cd services/$$service && golangci-lint run && cd ../..; \
	done

security-scan: ## Run security scans
	@echo "Running security scans..."
	trivy fs --security-checks vuln,config .
	gosec -quiet ./...

run-ingestion: ## Run ingestion service locally
	@echo "Starting ingestion service..."
	go run services/ingestion/cmd/main.go

run-worker: ## Run worker service locally
	@echo "Starting worker service..."
	go run services/worker/cmd/main.go

run-api: ## Run API service locally
	@echo "Starting API service..."
	go run services/api/cmd/main.go

docker-build: ## Build Docker images
	@echo "Building Docker images..."
	@for service in $(SERVICES); do \
		echo "Building $$service image..."; \
		docker build -t $(DOCKER_REGISTRY)/$$service:$(APP_VERSION) -f services/$$service/Dockerfile services/$$service; \
	done

docker-push: ## Push Docker images
	@echo "Pushing Docker images..."
	@for service in $(SERVICES); do \
		docker push $(DOCKER_REGISTRY)/$$service:$(APP_VERSION); \
	done

docker-up: ## Start local Docker environment
	docker-compose -f infrastructure/docker-compose/docker-compose.yml up -d

docker-down: ## Stop local Docker environment
	docker-compose -f infrastructure/docker-compose/docker-compose.yml down

docker-logs: ## View Docker logs
	docker-compose -f infrastructure/docker-compose/docker-compose.yml logs -f

migrate-up: ## Run database migrations
	@echo "Running migrations..."
	./scripts/database/migrate.sh up

migrate-down: ## Rollback database migrations
	@echo "Rolling back migrations..."
	./scripts/database/migrate.sh down

terraform-plan: ## Plan Terraform changes
	cd infrastructure/terraform/aws && terraform plan

terraform-apply: ## Apply Terraform changes
	cd infrastructure/terraform/aws && terraform apply

deploy-staging: ## Deploy to staging
	./scripts/deployment/deploy.sh staging

deploy-production: ## Deploy to production
	./scripts/deployment/deploy.sh production

clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -rf bin/
	rm -rf dist/
	@for service in $(SERVICES); do \
		rm -f services/$$service/coverage.out; \
	done

monitor: ## Open monitoring dashboard
	@echo "Opening Grafana at http://localhost:3000"
	open http://localhost:3000 || xdg-open http://localhost:3000

.DEFAULT_GOAL := help
EOF

# Create docker-compose.yml for local development
echo "📝 Creating docker-compose.yml..."
cat > infrastructure/docker-compose/docker-compose.yml << 'EOF'
version: '3.8'

services:
  postgres:
    image: postgres:15-alpine
    container_name: blockchain-monitor-db
    environment:
      POSTGRES_USER: blockchain_user
      POSTGRES_PASSWORD: blockchain_pass
      POSTGRES_DB: blockchain_monitor
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U blockchain_user"]
      interval: 10s
      timeout: 5s
      retries: 5

  redis:
    image: redis:7-alpine
    container_name: blockchain-monitor-redis
    ports:
      - "6379:6379"
    volumes:
      - redis_data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5

  rabbitmq:
    image: rabbitmq:3-management-alpine
    container_name: blockchain-monitor-rabbitmq
    environment:
      RABBITMQ_DEFAULT_USER: guest
      RABBITMQ_DEFAULT_PASS: guest
    ports:
      - "5672:5672"
      - "15672:15672"  # Management UI
    volumes:
      - rabbitmq_data:/var/lib/rabbitmq
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5

  prometheus:
    image: prom/prometheus:latest
    container_name: blockchain-monitor-prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--web.console.libraries=/usr/share/prometheus/console_libraries'
      - '--web.console.templates=/usr/share/prometheus/consoles'
    ports:
      - "9090:9090"
    volumes:
      - ../../monitoring/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml
      - ../../monitoring/prometheus/rules:/etc/prometheus/rules
      - prometheus_data:/prometheus

  grafana:
    image: grafana/grafana:latest
    container_name: blockchain-monitor-grafana
    environment:
      GF_SECURITY_ADMIN_USER: admin
      GF_SECURITY_ADMIN_PASSWORD: admin
      GF_USERS_ALLOW_SIGN_UP: 'false'
    ports:
      - "3000:3000"
    volumes:
      - ../../monitoring/grafana/provisioning:/etc/grafana/provisioning
      - ../../monitoring/grafana/dashboards:/var/lib/grafana/dashboards
      - grafana_data:/var/lib/grafana
    depends_on:
      - prometheus

volumes:
  postgres_data:
  redis_data:
  rabbitmq_data:
  prometheus_data:
  grafana_data:

networks:
  default:
    name: blockchain-monitor-network
EOF

# Create Prometheus config
echo "📝 Creating Prometheus configuration..."
cat > monitoring/prometheus/prometheus.yml << 'EOF'
global:
  scrape_interval: 15s
  evaluation_interval: 15s
  external_labels:
    cluster: 'blockchain-monitor'
    environment: 'development'

rule_files:
  - '/etc/prometheus/rules/*.yml'

scrape_configs:
  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']

  - job_name: 'ingestion-service'
    static_configs:
      - targets: ['host.docker.internal:8081']
    metrics_path: '/metrics'

  - job_name: 'worker-service'
    static_configs:
      - targets: ['host.docker.internal:8082']
    metrics_path: '/metrics'

  - job_name: 'api-service'
    static_configs:
      - targets: ['host.docker.internal:8080']
    metrics_path: '/metrics'

  - job_name: 'postgres'
    static_configs:
      - targets: ['postgres-exporter:9187']

  - job_name: 'redis'
    static_configs:
      - targets: ['redis-exporter:9121']

  - job_name: 'rabbitmq'
    static_configs:
      - targets: ['rabbitmq:15692']
EOF

# Create alert rules
cat > monitoring/prometheus/rules/alerts.yml << 'EOF'
groups:
  - name: blockchain_monitor_alerts
    interval: 30s
    rules:
      - alert: HighNotificationFailureRate
        expr: rate(notifications_sent_total{status="failed"}[5m]) > 0.05
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High notification failure rate"
          description: "Notification failure rate is {{ $value | humanizePercentage }} for {{ $labels.destination }}"

      - alert: HighEventProcessingLag
        expr: block_lag_seconds > 30
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High event processing lag"
          description: "Event processing lag is {{ $value }}s behind current block"

      - alert: QueueBacklog
        expr: queue_depth > 10000
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Message queue backlog"
          description: "Queue depth is {{ $value }} messages"

      - alert: RPCProviderDown
        expr: up{job="ingestion-service"} == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "RPC provider connection lost"
          description: "Ingestion service cannot connect to RPC provider"

      - alert: ServiceDown
        expr: up == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Service {{ $labels.job }} is down"
          description: "Service {{ $labels.job }} has been down for more than 2 minutes"

      - alert: HighMemoryUsage
        expr: container_memory_usage_bytes / container_spec_memory_limit_bytes > 0.9
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High memory usage on {{ $labels.container }}"
          description: "Memory usage is {{ $value | humanizePercentage }}"

      - alert: DeadLetterQueueGrowing
        expr: rate(dlq_messages_total[10m]) > 0
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "Dead letter queue receiving messages"
          description: "DLQ has received {{ $value }} messages in last 10 minutes"
EOF

# Create setup script
echo "📝 Creating setup script..."
cat > scripts/setup-local.sh << 'EOF'
#!/bin/bash

set -e

echo "🚀 Setting up local development environment..."

# Check prerequisites
command -v docker >/dev/null 2>&1 || { echo "❌ Docker is required but not installed."; exit 1; }
command -v docker-compose >/dev/null 2>&1 || { echo "❌ Docker Compose is required but not installed."; exit 1; }

# Copy environment file
if [ ! -f .env ]; then
    echo "📝 Creating .env file from .env.example..."
    cp .env.example .env
    echo "⚠️  Please edit .env with your RPC endpoints and secrets"
fi

# Start infrastructure
echo "🐳 Starting Docker infrastructure..."
cd infrastructure/docker-compose
docker-compose up -d
cd ../..

# Wait for services
echo "⏳ Waiting for services to be ready..."
sleep 10

# Run migrations
echo "🗄️  Running database migrations..."
./scripts/database/migrate.sh up

echo "✅ Local environment ready!"
echo ""
echo "📊 Access points:"
echo "  - Grafana: http://localhost:3000 (admin/admin)"
echo "  - Prometheus: http://localhost:9090"
echo "  - RabbitMQ: http://localhost:15672 (guest/guest)"
echo "  - PostgreSQL: localhost:5432"
echo "  - Redis: localhost:6379"
echo ""
echo "🚀 Start services with:"
echo "  make run-ingestion"
echo "  make run-worker"
echo "  make run-api"
EOF

chmod +x scripts/setup-local.sh

# Create migration script
cat > scripts/database/migrate.sh << 'EOF'
#!/bin/bash

set -e

DIRECTION=${1:-up}

echo "Running database migrations: $DIRECTION"

# Add migration logic here
# For now, just create basic schema

if [ "$DIRECTION" = "up" ]; then
    psql $DATABASE_URL << 'SQL'
    CREATE TABLE IF NOT EXISTS events (
        id SERIAL PRIMARY KEY,
        event_id VARCHAR(255) UNIQUE NOT NULL,
        chain_id INTEGER NOT NULL,
        block_number BIGINT NOT NULL,
        tx_hash VARCHAR(66) NOT NULL,
        log_index INTEGER NOT NULL,
        contract_address VARCHAR(42) NOT NULL,
        event_name VARCHAR(255) NOT NULL,
        event_data JSONB NOT NULL,
        processed_at TIMESTAMP DEFAULT NOW(),
        created_at TIMESTAMP DEFAULT NOW()
    );

    CREATE INDEX idx_events_contract ON events(contract_address);
    CREATE INDEX idx_events_block ON events(block_number);
    CREATE INDEX idx_events_created ON events(created_at);

    CREATE TABLE IF NOT EXISTS notifications (
        id SERIAL PRIMARY KEY,
        event_id VARCHAR(255) REFERENCES events(event_id),
        destination VARCHAR(50) NOT NULL,
        status VARCHAR(20) NOT NULL,
        retry_count INTEGER DEFAULT 0,
        last_error TEXT,
        sent_at TIMESTAMP,
        created_at TIMESTAMP DEFAULT NOW()
    );

    CREATE INDEX idx_notifications_status ON notifications(status);
    CREATE INDEX idx_notifications_event ON notifications(event_id);

    CREATE TABLE IF NOT EXISTS subscriptions (
        id SERIAL PRIMARY KEY,
        contract_address VARCHAR(42) NOT NULL,
        event_name VARCHAR(255),
        webhook_url TEXT NOT NULL,
        webhook_type VARCHAR(20) NOT NULL,
        filters JSONB,
        active BOOLEAN DEFAULT true,
        created_at TIMESTAMP DEFAULT NOW()
    );

    CREATE INDEX idx_subscriptions_contract ON subscriptions(contract_address);
SQL

    echo "✅ Migrations applied successfully"
else
    echo "⚠️  Migration rollback not implemented yet"
fi
EOF

chmod +x scripts/database/migrate.sh

# Create deployment script
cat > scripts/deployment/deploy.sh << 'EOF'
#!/bin/bash

set -e

ENVIRONMENT=${1:-staging}

echo "🚀 Deploying to $ENVIRONMENT..."

# Build and tag images
echo "🐳 Building Docker images..."
make docker-build

# Push images
echo "📤 Pushing images to registry..."
make docker-push

# Deploy based on environment
if [ "$ENVIRONMENT" = "staging" ]; then
    echo "📦 Deploying to staging cluster..."
    kubectl apply -k infrastructure/kubernetes/overlays/staging/
elif [ "$ENVIRONMENT" = "production" ]; then
    echo "📦 Deploying to production cluster..."
    kubectl apply -k infrastructure/kubernetes/overlays/production/
else
    echo "❌ Unknown environment: $ENVIRONMENT"
    exit 1
fi

echo "✅ Deployment complete!"
echo "🔍 Check deployment status:"
echo "  kubectl get pods -n blockchain-monitor"
EOF

chmod +x scripts/deployment/deploy.sh

echo "✅ Project structure created successfully!"
echo ""
echo "📚 Next steps:"
echo "  1. cd $PROJECT_NAME"
echo "  2. Edit .env with your RPC endpoints and secrets"
echo "  3. Run: ./scripts/setup-local.sh"
echo "  4. Start coding in services/ directories"
echo ""
echo "🚀 Quick start commands:"
echo "  make help              - Show all available commands"
echo "  make docker-up         - Start local infrastructure"
echo "  make run-ingestion     - Run ingestion service"
echo "  make run-worker        - Run worker service"
echo "  make run-api           - Run API service"
echo ""
echo "📊 Monitor at: http://localhost:3000 (Grafana)"
