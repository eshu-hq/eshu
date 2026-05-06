# Eshu Observability Configuration

This directory contains Prometheus alerting rules and OTEL collector configuration for Eshu.

## Files

- `otel-collector-config.yaml` - OpenTelemetry Collector configuration for metrics and traces
- `alerts.yaml` - Standalone Prometheus alert rules (for direct Prometheus deployment)
- `prometheus-rule.yaml` - Kubernetes PrometheusRule CR (for kube-prometheus-stack)

## Alert Groups

### eshu.pipeline (5 alerts)
Pipeline health monitoring for fact emission, projection, and reduction.

| Alert | Severity | Threshold | Purpose |
|-------|----------|-----------|---------|
| EshuFactQueueStale | critical | >1 hour | Oldest work item aging detection |
| EshuProjectionErrorRateHigh | warning | >5% failure rate | Projection stage failures |
| EshuReducerErrorRateHigh | warning | >5% failure rate | Reducer intent execution failures |
| EshuSharedProjectionBacklog | warning | >500 pending | Shared projection capacity monitoring |
| EshuCollectorStalled | critical | 15min no facts | Git collector stall detection |

### eshu.api (3 alerts)
HTTP API and MCP request monitoring.

| Alert | Severity | Threshold | Purpose |
|-------|----------|-----------|---------|
| EshuAPIErrorRateHigh | warning | >1% 5xx rate | HTTP request failures |
| EshuAPIP99LatencyHigh | warning | >5s P99 | API performance degradation |
| EshuMCPToolErrorRateHigh | warning | >2% failure rate | MCP tool invocation failures |

### eshu.database (3 alerts)
Neo4j and Postgres performance monitoring.

| Alert | Severity | Threshold | Purpose |
|-------|----------|-----------|---------|
| EshuPostgresLatencyHigh | warning | >1s P99 | Postgres query performance |
| EshuNeo4jLatencyHigh | warning | >2s P99 | Neo4j query performance |
| EshuNeo4jQueryErrors | critical | >0.1 errors/sec | Neo4j connectivity or query failures |

### eshu.throughput (3 alerts)
Pipeline throughput and backlog monitoring.

| Alert | Severity | Threshold | Purpose |
|-------|----------|-----------|---------|
| EshuFactEmissionRateDropped | warning | >50% drop | Collector throughput degradation |
| EshuProjectionThroughputMismatch | warning | 2x rate mismatch | Projection backlog growth |
| EshuReducerIntentBacklog | warning | 1.5x rate mismatch | Reducer intent queue growth |

## Deployment

### Standalone Prometheus

Load alert rules directly:

```bash
# Add to prometheus.yml
rule_files:
  - /etc/prometheus/rules/eshu-alerts.yaml

# Copy alerts to Prometheus container
kubectl create configmap eshu-alerts --from-file=alerts.yaml
kubectl patch deployment prometheus -p '{"spec":{"template":{"spec":{"volumes":[{"name":"eshu-alerts","configMap":{"name":"eshu-alerts"}}]}}}}'
```

### kube-prometheus-stack

Deploy as PrometheusRule CR:

```bash
kubectl apply -f prometheus-rule.yaml
```

The PrometheusRule will be automatically discovered by the Prometheus Operator and loaded.

## Verification

Check that alerts are loaded:

```bash
# Via kubectl
kubectl get prometheusrules eshu-alerts -o yaml

# Via Prometheus UI
# Navigate to Status -> Rules
# Filter for "eshu."
```

Test alert evaluation:

```bash
# Force an alert to fire (example: stop ingester)
kubectl scale statefulset eshu-ingester --replicas=0

# Wait 15 minutes, then check Prometheus UI -> Alerts
# EshuCollectorStalled should fire

# Restore
kubectl scale statefulset eshu-ingester --replicas=1
```

## Runbook References

Every alert includes a detailed runbook with:

1. Initial diagnostic commands (kubectl, logs, metrics)
2. Jaeger trace filtering guidance (service, phase, attributes)
3. Common failure patterns and their causes
4. Specific metrics to check for root cause analysis
5. Remediation steps and tuning guidance
6. Escalation criteria

Example runbook workflow for `EshuProjectionErrorRateHigh`:

```bash
# 1. Check failure rate by status
curl -s 'http://prometheus:9090/api/v1/query?query=rate(eshu_dp_projections_completed_total[5m])' | jq

# 2. Open Jaeger UI
# Filter: service_name=eshu-ingester, pipeline_phase=projection
# Look for error spans with failure_class tag

# 3. Check stage-specific failures
curl -s 'http://prometheus:9090/api/v1/query?query=eshu_dp_projector_stage_duration_seconds' | jq

# 4. Review logs with structured filtering
kubectl logs -l app=eshu-ingester | grep 'failure_class'

# 5. Check backing service health
curl -s 'http://prometheus:9090/api/v1/query?query=rate(eshu_dp_neo4j_query_errors_total[5m])' | jq
```

## Metric Sources

All metrics referenced in alerts are emitted by:

- **Go services**: `go/internal/telemetry/instruments.go` (eshu_dp_* prefix)
- **Shared runtime status metrics**: `go/internal/status/*` and mounted
  `/metrics` handlers (eshu_runtime_* families retained for operator continuity)
- **Instrumented storage**: `go/internal/storage/{neo4j,postgres}/instrumented.go`

See `docs/docs/reference/telemetry/index.md` for complete metric catalog.

## Integration with Grafana Dashboards

These alerts complement the existing dashboards:

- `docs/dashboards/pipeline-slo.json` - Overall pipeline success rate and error budget
- `docs/dashboards/ingester.json` - Ingester-specific metrics and queue depth
- `docs/dashboards/reducer.json` - Reducer execution and shared projection metrics
- `docs/dashboards/database-performance.json` - Neo4j and Postgres query latency
- `docs/dashboards/overview.json` - High-level service health overview

Alert annotations include references to specific dashboard panels for context.

## Alertmanager Configuration

Example Alertmanager routing for Eshu alerts:

```yaml
route:
  receiver: 'default'
  group_by: ['alertname', 'service', 'component']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
  routes:
    - match:
        severity: critical
      receiver: 'pagerduty-critical'
      continue: true
    - match:
        severity: warning
      receiver: 'slack-warnings'

receivers:
  - name: 'pagerduty-critical'
    pagerduty_configs:
      - service_key: '<your-pagerduty-key>'
  - name: 'slack-warnings'
    slack_configs:
      - api_url: '<your-slack-webhook>'
        channel: '#eshu-alerts'
        title: '{{ .GroupLabels.alertname }}'
        text: '{{ range .Alerts }}{{ .Annotations.summary }}\n{{ end }}'
```

## Tuning Alert Thresholds

Thresholds are based on observed behavior in the SLO dashboard and telemetry docs. Adjust based on your deployment:

| Alert | Current Threshold | Tuning Guidance |
|-------|-------------------|-----------------|
| EshuFactQueueStale | 1 hour | Increase for slower repos, decrease for strict SLOs |
| Projection/ReducerErrorRate | 5% | Lower to 2-3% for production, higher for dev |
| SharedProjectionBacklog | 500 intents | Scale with partition count (100 per partition) |
| APIErrorRate | 1% | Lower to 0.5% for strict availability SLOs |
| APIP99Latency | 5s | Lower to 2-3s for interactive use cases |
| PostgresP99 | 1s | Lower to 500ms for high-throughput scenarios |
| Neo4jP99 | 2s | Lower to 1s if query optimization is complete |

Edit thresholds in `alerts.yaml` or `prometheus-rule.yaml` and reapply.
