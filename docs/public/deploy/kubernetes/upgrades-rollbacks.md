# Upgrade and rollback

Treat Eshu upgrades like data-plane changes. The application image, Postgres
schema, graph schema, and worker behavior move together.

## Before upgrade

1. Pin the target image tag in values.
2. Render the chart with the same values used by the cluster.
3. Review changes to workloads, environment variables, probes, security
   contexts, PVCs, and ServiceMonitors.
4. Confirm Postgres backups are recent enough for the rollout risk. Graph
   backups are useful for fast rollback, but the default NornicDB graph is
   rebuildable projection state when Postgres facts and source systems remain
   available.
5. Check current queue depth, queue age, dead-letter state, and indexing
   completeness.
6. Write an upgrade-state declaration for durable Postgres state, queue state,
   graph rebuild assumptions, and preserved volumes.

```bash
helm template eshu ./deploy/helm/eshu \
  --namespace eshu \
  -f values.eshu.yaml

scripts/verify-hosted-helm-rollout-proof.sh \
  --mode upgrade \
  --out-dir .proof/helm-upgrade \
  --namespace eshu \
  --release eshu \
  --values values.eshu.yaml \
  --upgrade-state upgrade-state.json
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
Postgres state in a way that the older image cannot read, restore Postgres
according to your platform backup runbook. If only the NornicDB graph volume is
lost or unreadable, preserve it when forensic evidence matters, recreate the
graph PVC, run schema bootstrap, and rebuild projection from facts or source
systems.

Before relying on a rollback plan, run the rollout proof in rollback mode with a
declaration that separately names the Helm rollback command, Postgres restore
decision point, graph rebuild plan, and operator decision boundary.
