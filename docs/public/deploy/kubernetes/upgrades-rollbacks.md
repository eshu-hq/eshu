# Upgrade and rollback

Treat Eshu upgrades like data-plane changes. The application image, Postgres
schema, graph schema, and worker behavior move together.

## Before upgrade

1. Pin the target image tag in values.
2. Render the chart with the same values used by the cluster.
3. Review changes to workloads, environment variables, probes, security
   contexts, PVCs, and ServiceMonitors.
4. Confirm Postgres backups are recent enough for the rollout risk. Graph
   backups are useful for fast rollback. When the rollout selects NornicDB, its
   graph is rebuildable projection state while Postgres facts and source
   systems remain available.
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

First determine whether the target revision predates Postgres migration 096
(`provenance_edge_identity_upgrade_096`). Migration 096 is a forward-only
compatibility fence: a pre-096 reducer can read the upgraded schema, but every
terminal transition for affected provenance work is requeued because the old
writer cannot clear the capability flag. Do not use a Helm-only rollback to a
pre-096 reducer.

If migration 096 is present, stop all reducer pods and prefer rolling forward
with the compatible reducer and NornicDB build. A full rollback requires
coordinated restoration of both Postgres and graph backups taken before
migration 096. Deploy the pre-096 application and backend only after both
stores are restored, then resume reducers. Do not disable or drop the migration
triggers against upgraded state.

For revisions that do not cross a forward-only migration boundary, use the
normal Helm rollback flow:

```bash
helm history eshu --namespace eshu
helm rollback eshu <revision> --namespace eshu
```

Rollback does not replace a database restore plan. Compatibility includes both
schema readability and the older worker's ability to complete durable queue
transitions. If an upgrade changes either contract, restore Postgres according
to your platform backup runbook. If only the NornicDB graph volume is lost or
unreadable and no coordinated restore is required, preserve it when forensic
evidence matters, recreate the graph PVC, run schema bootstrap, and rebuild
projection from facts or source systems.

Before relying on a rollback plan, run the rollout proof in rollback mode with a
declaration that separately names the Helm rollback command, Postgres restore
decision point, graph rebuild plan, and operator decision boundary.
