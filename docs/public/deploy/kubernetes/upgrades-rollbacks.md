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

When the release has the legacy `<release>-nornicdb-data` PVC, stop graph
writers and protect that volume before rendering the v1.3.2 upgrade. The chart
fails the live upgrade until
`nornicdb.persistence.allowFreshVolumeMigration=true` acknowledges the fresh
`<release>-nornicdb-v132-data` claim and graph rebuild. It keeps both managed
claims so rollback never depends on reusing one binary's graph files with the
other binary.

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

### Relationship identity cutover

The release that introduces keyed `RUNS_ON.identity_key` and workload
`DEPENDS_ON.identity_key` relationships is a non-rolling reducer upgrade. Old
reducers write propertyless relationships, while new reducers write keyed
identities. Do not let those revisions overlap, including across separately
configured resolution-engine lanes.

Before the Helm upgrade, let reducer queues drain, scale every
resolution-engine Deployment to zero, and wait until every old reducer pod is
gone. Also wait until Postgres reports no unexpired `claimed` or `running`
reducer lease:

```bash
kubectl scale deployment \
  -l app.kubernetes.io/component=resolution-engine \
  --replicas=0 --namespace eshu
kubectl wait --for=delete pod \
  -l app.kubernetes.io/component=resolution-engine \
  --timeout=6m --namespace eshu

psql "$ESHU_POSTGRES_DSN" -v ON_ERROR_STOP=1 -c \
  "SELECT count(*) FROM fact_work_items
   WHERE stage = 'reducer'
     AND status IN ('claimed', 'running')
     AND claim_until > clock_timestamp();"
```

Proceed only when that query returns zero. A normal `helm upgrade` then starts
only new reducer binaries. Run the graph rebuild procedure after the upgrade;
its full replay replaces duplicate or propertyless legacy `RUNS_ON` and workload
`DEPENDS_ON` edges with one keyed identity per endpoint pair. This coordinated
stop applies to every lane Deployment; changing one lane at a time is unsafe.

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
