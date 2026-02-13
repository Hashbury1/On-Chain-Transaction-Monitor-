#!/bin/bash

echo "1. Checking ingestion service metrics..."
curl -s http://localhost:8081/metrics | grep events_ingested_total | head -5

echo ""
echo "2. Checking if Prometheus can reach service..."
docker exec tx-monitor-prometheus wget -qO- http://host.docker.internal:8081/metrics 2>&1 | grep -c events_ingested_total

echo ""
echo "3. Checking Prometheus targets..."
curl -s http://localhost:9090/api/v1/targets | jq '.data.activeTargets[] | {job: .labels.job, health: .health}'

echo ""
echo "4. Checking if Prometheus has data..."
curl -s 'http://localhost:9090/api/v1/query?query=events_ingested_total' | jq '.data.result | length'

echo ""
echo "Next steps:"
echo "- If step 1 shows metrics: ✅ Service is working"
echo "- If step 2 returns 0: ❌ Network issue - Prometheus can't reach service"
echo "- If step 3 shows 'down': ❌ Prometheus not scraping"
echo "- If step 4 returns 0: ❌ Prometheus has no data"
