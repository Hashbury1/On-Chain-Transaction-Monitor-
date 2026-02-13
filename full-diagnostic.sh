#!/bin/bash

echo "=========================================="
echo "COMPLETE GRAFANA DIAGNOSTIC"
echo "=========================================="
echo ""

echo "1️⃣  INGESTION SERVICE CHECK"
echo "----------------------------"
if curl -s http://localhost:8081/health > /dev/null 2>&1; then
    echo "✅ Service is running"
    echo ""
    echo "Sample metrics:"
    curl -s http://localhost:8081/metrics | grep events_ingested_total | head -3
else
    echo "❌ Service NOT running"
    echo "Run: export \$(grep -v '^#' .env | grep -v '^\$' | xargs) && ./bin/ingestion"
fi
echo ""

echo "2️⃣  PROMETHEUS TARGETS"
echo "----------------------------"
echo "Checking what Prometheus sees..."
curl -s http://localhost:9090/api/v1/targets | jq -r '.data.activeTargets[] | "\(.labels.job): \(.health) - Last Error: \(.lastError // "none")"'
echo ""

echo "3️⃣  PROMETHEUS DATA CHECK"
echo "----------------------------"
RESULT_COUNT=$(curl -s 'http://localhost:9090/api/v1/query?query=events_ingested_total' | jq '.data.result | length')
echo "Metric series found: $RESULT_COUNT"

if [ "$RESULT_COUNT" -gt 0 ]; then
    echo "✅ Prometheus HAS data!"
    echo ""
    echo "Sample data:"
    curl -s 'http://localhost:9090/api/v1/query?query=events_ingested_total' | jq -r '.data.result[] | "\(.metric.contract): \(.value[1])"'
else
    echo "❌ Prometheus has NO data"
fi
echo ""

echo "4️⃣  GRAFANA CHECK"
echo "----------------------------"
if curl -s http://localhost:3000/api/health > /dev/null 2>&1; then
    echo "✅ Grafana is running"
    echo "URL: http://localhost:3000"
else
    echo "❌ Grafana NOT running"
fi
echo ""

echo "5️⃣  GRAFANA DATASOURCE TEST"
echo "----------------------------"
echo "Testing if Grafana can query Prometheus..."

# Test Grafana API
GRAFANA_TEST=$(curl -s -u admin:admin 'http://localhost:3000/api/datasources/proxy/1/api/v1/query?query=up' 2>&1)

if echo "$GRAFANA_TEST" | grep -q '"status":"success"'; then
    echo "✅ Grafana CAN query Prometheus"
else
    echo "❌ Grafana CANNOT query Prometheus"
    echo "Error: $GRAFANA_TEST"
fi
echo ""

echo "=========================================="
echo "TROUBLESHOOTING SUMMARY"
echo "=========================================="

if [ "$RESULT_COUNT" -eq 0 ]; then
    echo "❌ PROBLEM: Prometheus has no data"
    echo ""
    echo "Solutions:"
    echo "1. Check Prometheus config has correct IP:"
    echo "   cat monitoring/prometheus/prometheus.yml"
    echo ""
    echo "2. Your host IP is probably:"
    hostname -I | awk '{print "   " $1}'
    echo ""
    echo "3. Update prometheus.yml to use this IP"
    echo "4. Then run:"
    echo "   docker cp monitoring/prometheus/prometheus.yml tx-monitor-prometheus:/etc/prometheus/prometheus.yml"
    echo "   docker restart tx-monitor-prometheus"
elif echo "$GRAFANA_TEST" | grep -q '"status":"success"'; then
    echo "✅ EVERYTHING WORKING!"
    echo ""
    echo "Open Grafana and try:"
    echo "1. Go to: http://localhost:3000"
    echo "2. Click 'Explore' (compass icon)"
    echo "3. Query: events_ingested_total"
    echo "4. Time range: Last 5 minutes"
    echo "5. Click 'Run query'"
else
    echo "❌ PROBLEM: Grafana can't connect to Prometheus"
    echo ""
    echo "Solution:"
    echo "1. Go to Grafana: http://localhost:3000"
    echo "2. ☰ → Connections → Data sources"
    echo "3. Click 'Prometheus'"
    echo "4. Set URL to: http://prometheus:9090"
    echo "5. Click 'Save & test'"
fi
