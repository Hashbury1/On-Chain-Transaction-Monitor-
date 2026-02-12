#!/bin/bash

echo "=== Container Status ==="
docker ps --format "table {{.Names}}\t{{.Status}}" | grep tx-monitor

echo ""
echo "=== Database Connection ==="
docker exec tx-monitor-db psql -U blockchain_user -d blockchain_monitor -c "SELECT 1;" > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo "✓ Database connected"
else
    echo "✗ Database connection failed"
fi

echo ""
echo "=== Database Tables ==="
docker exec tx-monitor-db psql -U blockchain_user -d blockchain_monitor -c "\dt" 2>/dev/null | grep -E "events|notifications|subscriptions" || echo "✗ Tables not created"

echo ""
echo "=== Event Count ==="
docker exec tx-monitor-db psql -U blockchain_user -d blockchain_monitor -c "SELECT COUNT(*) as events FROM events;" 2>/dev/null

echo ""
echo "=== Subscription Count ==="
docker exec tx-monitor-db psql -U blockchain_user -d blockchain_monitor -c "SELECT COUNT(*) as subscriptions FROM subscriptions;" 2>/dev/null

echo ""
echo "=== Service Health Endpoints ==="
curl -s http://localhost:8081/health > /dev/null 2>&1 && echo "✓ Ingestion service: OK" || echo "✗ Ingestion service: DOWN"
curl -s http://localhost:8082/health > /dev/null 2>&1 && echo "✓ Worker service: OK" || echo "✗ Worker service: DOWN"

echo ""
echo "=== Web Interfaces ==="
echo "Grafana: http://localhost:3000 (admin/admin)"
echo "Prometheus: http://localhost:9090"
echo "RabbitMQ: http://localhost:15672 (guest/guest)"
