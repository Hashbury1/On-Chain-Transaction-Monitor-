# Operational Runbooks

## Overview

These runbooks provide step-by-step procedures for handling common operational scenarios. Each runbook includes symptoms, diagnosis steps, resolution procedures, and prevention measures.

---

## 🚨 Runbook 1: RPC Provider Outage

### Severity: P1 - Critical

### Symptoms
- Alert: "RPCProviderDown" firing
- Metrics show `rpc_errors_total` spiking
- `websocket_reconnections_total` increasing
- `block_lag_seconds` growing
- Logs showing connection errors

### Diagnosis

**Step 1: Verify the Issue**
```bash
# Check ingestion service health
curl http://ingestion-service:8081/health

# Check recent logs
kubectl logs -n blockchain-monitor deployment/ingestion-service --tail=100

# Check Prometheus metrics
# Visit: http://prometheus:9090/graph
# Query: rate(rpc_errors_total[5m])
```

**Step 2: Identify Root Cause**
- Check Alchemy/Infura status page: https://status.alchemy.com
- Test RPC endpoint manually:
```bash
# WebSocket test
wscat -c $RPC_PRIMARY_URL

# HTTP test
curl -X POST $RPC_FALLBACK_URL \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

### Resolution

**If Primary Provider Down:**

1. **Automatic Failover Should Activate**
   - System should automatically switch to fallback RPC
   - Monitor logs for "Connected to fallback RPC provider"
   - Verify `websocket_reconnections_total` stops increasing

2. **If Automatic Failover Fails:**
   ```bash
   # Manually restart ingestion service to force failover
   kubectl rollout restart deployment/ingestion-service -n blockchain-monitor
   
   # Watch the rollout
   kubectl rollout status deployment/ingestion-service -n blockchain-monitor
   ```

3. **Monitor Recovery:**
   ```bash
   # Check block lag returning to normal
   # Should be <10 seconds
   # Query in Prometheus: block_lag_seconds
   
   # Verify events are being ingested
   # Query: rate(events_ingested_total[1m])
   ```

**If Both Providers Down:**

1. **Pause Ingestion Temporarily**
   ```bash
   # Scale ingestion to 0 to prevent error spam
   kubectl scale deployment/ingestion-service --replicas=0 -n blockchain-monitor
   ```

2. **Update Team**
   - Post in #incidents Slack channel
   - Update status page if customer-facing

3. **Add Temporary Provider**
   - Get emergency RPC from public node or backup provider
   - Update ConfigMap with new RPC URL
   ```bash
   kubectl edit configmap ingestion-config -n blockchain-monitor
   # Update RPC_FALLBACK_URL
   ```

4. **Resume Ingestion**
   ```bash
   kubectl scale deployment/ingestion-service --replicas=3 -n blockchain-monitor
   ```

### Post-Resolution

1. **Backfill Missed Blocks**
   - Identify block range that was missed
   ```bash
   # Get last processed block from database
   psql $DATABASE_URL -c "SELECT MAX(block_number) FROM events;"
   
   # Get current block number
   curl -X POST $RPC_URL -H "Content-Type: application/json" \
     -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
   
   # Run backfill script
   ./scripts/backfill-blocks.sh START_BLOCK END_BLOCK
   ```

2. **Verify Data Integrity**
   ```bash
   # Check for gaps in block numbers
   psql $DATABASE_URL -c "
   SELECT block_number FROM (
     SELECT block_number, 
            LAG(block_number) OVER (ORDER BY block_number) as prev_block
     FROM events
     WHERE created_at > NOW() - INTERVAL '1 hour'
   ) t
   WHERE block_number - prev_block > 1;
   "
   ```

### Prevention

1. **Improve Monitoring**
   - Add provider-specific health checks
   - Alert on degraded performance before complete failure
   - Monitor provider status pages automatically

2. **Add More Fallbacks**
   - Configure tertiary RPC provider
   - Consider running own node for ultimate reliability

3. **Improve Failover**
   - Reduce failover time from 30s to 10s
   - Add automatic recovery when primary comes back

---

## 📉 Runbook 2: High Notification Failure Rate

### Severity: P2 - High

### Symptoms
- Alert: "HighNotificationFailureRate" firing
- Metrics show `notifications_sent_total{status="failed"}` increasing
- `circuit_breaker_state` showing open (value=1)
- `dlq_messages_total` increasing
- User reports of missing notifications

### Diagnosis

**Step 1: Identify Affected Destinations**
```bash
# Check which destinations are failing
# Prometheus query:
rate(notifications_sent_total{status="failed"}[5m]) by (destination)

# Check circuit breaker states
circuit_breaker_state
```

**Step 2: Test Destinations**
```bash
# Test Discord webhook
curl -X POST "YOUR_DISCORD_WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{"content":"Test from runbook"}'

# Test Slack webhook
curl -X POST "YOUR_SLACK_WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{"text":"Test from runbook"}'
```

**Step 3: Check Recent Errors**
```bash
# View worker logs for errors
kubectl logs -n blockchain-monitor deployment/worker-service --tail=100 | grep ERROR

# Check DLQ messages
kubectl exec -it rabbitmq-0 -n blockchain-monitor -- \
  rabbitmqadmin list queues name messages | grep dlq
```

### Resolution

**If Destination Service Down (Discord/Slack):**

1. **Verify Service Status**
   - Check https://discordstatus.com
   - Check https://status.slack.com

2. **Circuit Breaker Should Handle**
   - Verify circuit is open (prevents hammering)
   - Messages should be in queue, not lost

3. **Wait for Recovery**
   - Circuit breaker will automatically retry in half-open state after 30s
   - If service recovers, circuit will close automatically

4. **Monitor Recovery**
   ```bash
   # Watch circuit breaker states
   watch -n 5 'curl -s http://worker-service:8082/metrics | grep circuit_breaker_state'
   ```

**If Rate Limited:**

1. **Check Rate Limit Headers**
   - Review worker logs for 429 responses
   - Identify rate limit thresholds

2. **Add Rate Limiting**
   ```bash
   # Update worker configuration
   kubectl edit configmap worker-config -n blockchain-monitor
   
   # Add/adjust:
   # RATE_LIMIT_PER_MINUTE: 60
   # RATE_LIMIT_PER_DESTINATION: 30
   ```

3. **Restart Workers**
   ```bash
   kubectl rollout restart deployment/worker-service -n blockchain-monitor
   ```

**If Webhook URLs Invalid:**

1. **Identify Bad Subscriptions**
   ```bash
   # Query database for recent failures
   psql $DATABASE_URL -c "
   SELECT destination, COUNT(*) as failure_count, last_error
   FROM notifications
   WHERE status = 'failed' 
     AND created_at > NOW() - INTERVAL '1 hour'
   GROUP BY destination, last_error
   ORDER BY failure_count DESC;
   "
   ```

2. **Disable Bad Subscriptions**
   ```bash
   # Via API
   curl -X PUT http://api-service/api/v1/subscriptions/BAD_SUB_ID \
     -H "Authorization: Bearer $TOKEN" \
     -d '{"active": false}'
   
   # Or via database
   psql $DATABASE_URL -c "
   UPDATE subscriptions 
   SET active = false 
   WHERE webhook_url = 'BAD_URL';
   "
   ```

3. **Notify Subscription Owner**
   - Send email to subscription owner
   - Provide error details
   - Request updated webhook URL

**If System Overload:**

1. **Scale Workers**
   ```bash
   # Increase worker replicas
   kubectl scale deployment/worker-service --replicas=10 -n blockchain-monitor
   
   # Monitor queue depth
   watch -n 5 'curl -s http://rabbitmq:15672/api/queues | jq .'
   ```

2. **Prioritize Critical Notifications**
   - Implement priority queue if needed
   - Pause non-critical notifications temporarily

### Post-Resolution

1. **Process DLQ Messages**
   ```bash
   # Review DLQ
   kubectl exec -it rabbitmq-0 -n blockchain-monitor -- \
     rabbitmqadmin get queue=blockchain-events-dlq count=10
   
   # Replay messages (after fixing root cause)
   ./scripts/replay-dlq.sh blockchain-events-dlq
   ```

2. **Verify Delivery**
   ```bash
   # Check success rate
   # Prometheus query:
   sum(rate(notifications_sent_total{status="success"}[5m])) / 
   sum(rate(notifications_sent_total[5m]))
   # Should be >0.99
   ```

3. **Write Postmortem**
   - Document incident timeline
   - Root cause analysis
   - Action items to prevent recurrence

### Prevention

1. **Improve Monitoring**
   - Add per-destination success rate alerts
   - Monitor rate limit headers proactively
   - Alert on sustained high DLQ depth

2. **Add Webhook Validation**
   - Test webhooks before activating subscriptions
   - Periodic health checks on active webhooks
   - Auto-disable after N consecutive failures

3. **Implement Backpressure**
   - Reject new subscriptions if system overloaded
   - Implement admission control

---

## 📊 Runbook 3: High Queue Backlog

### Severity: P2 - High

### Symptoms
- Alert: "QueueBacklog" firing
- Metrics show `queue_depth` >10,000 and growing
- `worker_processing_duration_seconds` increasing
- Processing lag increasing
- Dashboard shows queue growing faster than consumption

### Diagnosis

**Step 1: Identify Bottleneck**
```bash
# Check queue stats
kubectl exec -it rabbitmq-0 -n blockchain-monitor -- \
  rabbitmqadmin list queues name messages consumers message_rate

# Check worker processing time
# Prometheus query:
histogram_quantile(0.95, worker_processing_duration_seconds_bucket)

# Check worker count
kubectl get pods -n blockchain-monitor -l app=worker-service | grep Running | wc -l
```

**Step 2: Identify Cause**
```bash
# Check if ingestion rate spiked
# Prometheus query:
rate(events_ingested_total[5m])

# Check if workers are slow
# Look for slow webhook responses
kubectl logs -n blockchain-monitor deployment/worker-service --tail=200 | \
  grep "latency" | \
  awk '{print $NF}' | \
  sort -n | \
  tail -20

# Check for dead workers
kubectl get pods -n blockchain-monitor -l app=worker-service
```

**Step 3: Check System Resources**
```bash
# Check CPU/Memory
kubectl top pods -n blockchain-monitor

# Check database connections
psql $DATABASE_URL -c "
SELECT count(*) as active_connections, 
       state, 
       wait_event_type
FROM pg_stat_activity 
GROUP BY state, wait_event_type;
"

# Check Redis
kubectl exec -it redis-0 -n blockchain-monitor -- redis-cli INFO stats
```

### Resolution

**If Ingestion Spike:**

1. **Scale Workers Immediately**
   ```bash
   # Scale to 20 workers
   kubectl scale deployment/worker-service --replicas=20 -n blockchain-monitor
   
   # Enable horizontal pod autoscaler for future
   kubectl autoscale deployment/worker-service \
     --min=5 --max=30 \
     --cpu-percent=70 \
     -n blockchain-monitor
   ```

2. **Monitor Queue Drain**
   ```bash
   # Watch queue depth decrease
   watch -n 10 'kubectl exec -it rabbitmq-0 -n blockchain-monitor -- \
     rabbitmqadmin list queues name messages'
   ```

**If Workers Slow:**

1. **Identify Slow Operations**
   - Check worker logs for timeouts
   - Identify which destinations are slow
   - Check database query performance

2. **Optimize Performance**
   ```bash
   # If database slow, check slow queries
   psql $DATABASE_URL -c "
   SELECT query, calls, mean_exec_time, max_exec_time
   FROM pg_stat_statements
   ORDER BY mean_exec_time DESC
   LIMIT 10;
   "
   
   # Add missing indexes if needed
   psql $DATABASE_URL -c "
   CREATE INDEX CONCURRENTLY idx_notifications_created 
   ON notifications(created_at) 
   WHERE status = 'pending';
   "
   ```

3. **Implement Batch Processing**
   - If possible, batch webhook calls
   - Reduce database round-trips

**If Workers Crashed:**

1. **Check Pod Status**
   ```bash
   kubectl get pods -n blockchain-monitor -l app=worker-service
   ```

2. **Review Crash Logs**
   ```bash
   # Get logs from crashed pod
   kubectl logs -n blockchain-monitor POD_NAME --previous
   ```

3. **Fix and Redeploy**
   - Fix bug if identified
   - Deploy fix
   - Scale back up

**If System Overload:**

1. **Temporary Rate Limiting**
   ```bash
   # Limit ingestion temporarily
   kubectl set env deployment/ingestion-service \
     MAX_EVENTS_PER_SECOND=5000 \
     -n blockchain-monitor
   ```

2. **Add Resources**
   - Increase worker CPU/memory limits
   - Scale database if bottleneck
   - Increase RabbitMQ resources

### Post-Resolution

1. **Verify Queue Empty**
   ```bash
   # Check queue depth back to normal (<100)
   kubectl exec -it rabbitmq-0 -n blockchain-monitor -- \
     rabbitmqadmin list queues name messages
   ```

2. **Check for Data Loss**
   ```bash
   # Verify no messages lost
   # Sum of ingested should equal sent + in-queue
   # Prometheus query:
   sum(increase(events_ingested_total[1h])) - 
   (sum(increase(notifications_sent_total[1h])) + queue_depth)
   # Should be close to 0
   ```

3. **Review Autoscaling**
   - Ensure HPA is configured correctly
   - Adjust scaling thresholds if needed

### Prevention

1. **Implement Autoscaling**
   ```yaml
   # HPA based on queue depth
   apiVersion: autoscaling/v2
   kind: HorizontalPodAutoscaler
   metadata:
     name: worker-autoscaler
   spec:
     scaleTargetRef:
       apiVersion: apps/v1
       kind: Deployment
       name: worker-service
     minReplicas: 5
     maxReplicas: 30
     metrics:
     - type: External
       external:
         metric:
           name: queue_depth
         target:
           type: AverageValue
           averageValue: "500"
   ```

2. **Add Capacity Planning**
   - Monitor growth trends
   - Scale infrastructure proactively
   - Load test regularly

3. **Improve Monitoring**
   - Alert on queue depth growth rate
   - Alert on processing time degradation
   - Dashboard showing queue depth vs worker count

---

## 🔍 Runbook 4: High Event Processing Lag

### Severity: P2 - High

### Symptoms
- Alert: "HighEventProcessingLag" firing
- Metrics show `block_lag_seconds` >30
- Events arriving late
- User complaints about delayed notifications

### Diagnosis

**Step 1: Measure Lag**
```bash
# Check current lag
# Prometheus query: block_lag_seconds

# Get current blockchain height
curl -X POST $RPC_URL -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | \
  jq -r '.result' | \
  xargs printf "%d\n"

# Get last processed block
psql $DATABASE_URL -c "SELECT MAX(block_number) FROM events;"
```

**Step 2: Identify Bottleneck**
```bash
# Check ingestion rate
# Prometheus: rate(events_ingested_total[5m])

# Check if RPC slow
# Prometheus: histogram_quantile(0.95, rpc_request_duration_seconds_bucket)

# Check queue processing
# Prometheus: queue_depth
```

### Resolution

**If RPC Provider Slow:**

1. **Switch Providers**
   ```bash
   # Temporarily switch to faster provider
   kubectl set env deployment/ingestion-service \
     RPC_PRIMARY_URL=$FASTER_RPC_URL \
     -n blockchain-monitor
   ```

2. **Optimize RPC Calls**
   - Batch requests if possible
   - Use `eth_getLogs` instead of individual `getTransactionReceipt`
   - Implement parallel processing

**If Processing Slow:**

1. **Profile Application**
   ```bash
   # Enable pprof
   kubectl port-forward deployment/ingestion-service 6060:6060
   
   # Get CPU profile
   go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
   
   # Analyze
   (pprof) top10
   (pprof) list SLOW_FUNCTION
   ```

2. **Optimize Code**
   - Remove blocking operations
   - Add concurrency where safe
   - Optimize database queries

**If Catching Up from Outage:**

1. **Temporarily Increase Resources**
   ```bash
   # Scale up temporarily
   kubectl scale deployment/ingestion-service --replicas=5 -n blockchain-monitor
   
   # Increase CPU limits
   kubectl set resources deployment/ingestion-service \
     --limits=cpu=2000m,memory=2Gi \
     -n blockchain-monitor
   ```

2. **Monitor Catch-Up Progress**
   ```bash
   # Watch lag decrease
   watch -n 30 'curl -s http://prometheus:9090/api/v1/query?query=block_lag_seconds | \
     jq .data.result[0].value[1]'
   ```

3. **Restore Normal Configuration**
   ```bash
   # Once caught up, scale back down
   kubectl scale deployment/ingestion-service --replicas=3 -n blockchain-monitor
   ```

### Prevention

1. **Add Performance Testing**
   - Regular load tests
   - Benchmark against SLOs
   - Identify degradation early

2. **Optimize Regularly**
   - Regular code profiling
   - Database query optimization
   - Index maintenance

3. **Improve Monitoring**
   - Alert on lag growth rate
   - Correlate lag with RPC latency
   - Predict capacity issues

---

## 📞 Escalation Matrix

### P1 - Critical (Service Down)
- **Response Time**: Immediate
- **Escalation**: Page on-call engineer immediately
- **Communication**: Update status page within 5 minutes
- **Examples**: RPC provider down, all workers crashed, database unavailable

### P2 - High (Service Degraded)
- **Response Time**: 15 minutes
- **Escalation**: Slack #incidents channel, tag on-call
- **Communication**: Update stakeholders within 30 minutes
- **Examples**: High failure rate, queue backlog, high lag

### P3 - Medium (Isolated Issues)
- **Response Time**: 1 hour
- **Escalation**: Create ticket, assign to on-call
- **Communication**: Daily standup update
- **Examples**: Single webhook failing, minor performance degradation

### P4 - Low (Cosmetic/Future)
- **Response Time**: Next business day
- **Escalation**: Create backlog ticket
- **Communication**: Weekly review
- **Examples**: Dashboard improvements, non-critical features

---

## 🎯 Key Metrics to Monitor

| Metric | Good | Warning | Critical |
|--------|------|---------|----------|
| Notification Success Rate | >99% | 95-99% | <95% |
| Block Lag | <10s | 10-30s | >30s |
| Queue Depth | <1000 | 1000-10000 | >10000 |
| Worker Processing Time (P95) | <1s | 1-5s | >5s |
| RPC Error Rate | <0.1% | 0.1-1% | >1% |
| Circuit Breaker Open | 0 | 1-2 | >2 |
| DLQ Depth | 0 | 1-100 | >100 |

---

## 📝 Post-Incident Template

```markdown
# Incident Postmortem: [TITLE]

**Date**: YYYY-MM-DD
**Severity**: P1/P2/P3
**Duration**: X hours Y minutes
**Impact**: Description of user impact

## Timeline
- HH:MM - Incident began
- HH:MM - Alert fired
- HH:MM - On-call acknowledged
- HH:MM - Root cause identified
- HH:MM - Fix deployed
- HH:MM - Service restored
- HH:MM - Incident closed

## Root Cause
Detailed explanation of what caused the incident.

## Resolution
Steps taken to resolve the incident.

## Impact
- X users affected
- Y notifications delayed/failed
- Z minutes of degraded service

## Action Items
1. [ ] Immediate fix (Owner: NAME, Due: DATE)
2. [ ] Monitoring improvement (Owner: NAME, Due: DATE)
3. [ ] Documentation update (Owner: NAME, Due: DATE)
4. [ ] Code improvement (Owner: NAME, Due: DATE)

## Lessons Learned
- What went well
- What could be improved
- Preventive measures

## Follow-up
Review date: YYYY-MM-DD
```

---

**Remember**: 
- Stay calm during incidents
- Communicate clearly and often
- Document everything
- Learn from every incident
- Prevention is better than cure

For questions or updates to these runbooks, contact the DevOps team in #devops-oncall.
