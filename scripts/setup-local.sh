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
