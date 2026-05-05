# Upgrade and rollback

Treat Eshu upgrades like data-plane changes. The application image, Postgres
schema, graph schema, and worker behavior move together.

## Before upgrade

1. Pin the target image tag in values.
2. Render the chart with the same values used by the cluster.
3. Review changes to workloads, environment variables, probes, security
   contexts, PVCs, and ServiceMonitors.
4. Confirm Postgres and graph backups are recent enough for the rollout risk.
5. Check current queue depth, queue age, dead-letter state, and indexing
   completeness.

```bash
helm template eshu ./deploy/helm/eshu \
  --namespace eshu \
  -f values.eshu.yaml
```

## Upgrade

```bash
helm upgrade eshu ./deploy/helm/eshu \
  --namespace eshu \
  -f values.eshu.yaml
```

Watch the rollout with `kubectl get pods` and `kubectl rollout status` for the
API, MCP, ingester, and resolution-engine workloads.

## Rollback

```bash
helm history eshu --namespace eshu
helm rollback eshu <revision> --namespace eshu
```

Rollback does not replace a database restore plan. If an upgrade changes durable
state in a way that the older image cannot read, restore Postgres and graph
state according to your platform backup runbook.
