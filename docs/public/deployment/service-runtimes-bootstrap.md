# Bootstrap Runtime Services

Use this page for the two bootstrap paths: schema bootstrap and one-shot
bootstrap indexing. Use [Service Runtimes](service-runtimes.md) for the full
runtime map.

## Schema Bootstrap

`eshu-bootstrap-data-plane` applies Postgres and graph-backend schema DDL, then
exits. It writes no application data.

It owns this sequence:

1. Apply Postgres storage schema.
2. Apply graph constraints and indexes through the configured backend.
3. Record the graph backend, schema fingerprint, and explicit compatibility
   list only after every graph statement succeeds.
4. Exit with code `0` on success.

Invalid graph backend values fail startup. Invalid or non-positive graph schema
statement timeouts fail before DDL runs.

## Deployment Contract

Compose runs `db-migrate` with `/usr/local/bin/eshu-bootstrap-data-plane` after
Postgres and the graph backend are healthy. Steady-state services depend on
that one-shot service completing successfully.

Graph-writing runtimes (`eshu-bootstrap-index`, ingester/projector, standalone
projector, and resolution engine) read the latest Postgres
`graph_schema_applications` row for their backend at startup. They start only
when the latest applied fingerprint exactly matches their compiled schema
fingerprint or explicitly lists it as compatible. Missing markers and
incompatible latest markers fail startup with guidance to run
`eshu-bootstrap-data-plane` before graph writes begin.

Rolling upgrades are conservative: exact-match is the default. Additive schema
changes may declare older writer fingerprints compatible in the marker row.
Destructive schema changes leave the list empty so stale pods refuse before
runtime graph writes fail.

That startup check decides whether a writer may **start**. A writer already past
it keeps writing unless something checks again, and with
`schemaBootstrap.useHelmHooks=true` the bootstrap Job records the new marker
while the previous generation of pods is still serving. The ingester and
projector therefore re-read the marker on their write path as well, roughly
every 30 seconds, and refuse a write once the applied marker stops admitting
them. The refusal is retryable: the work stays in the queue for the pod that
replaces them, and it appears on a retrying queue row carrying the same
expected/applied fingerprint message the startup refusal uses. A marker the
writer cannot read is not a refusal — it holds the last decision, so an
unreachable Postgres does not stop those writers at once.

Two kinds of writer never make that call, so nothing checks a marker for them
after startup. One is a pod from a release built before the write-path check
existed. The other is the resolution engine, whose graph writers are checked at
startup only.

So upgrading across a schema change that leaves the compatible list empty still
means stopping the old writers before bootstrap records the marker — scale
ingester, projector, and resolution engine to zero, run the upgrade, then scale
back up — or accepting that pods of the outgoing release keep writing until
Kubernetes replaces them.

Helm renders `deploy/helm/eshu/templates/job-schema-bootstrap.yaml`. With
`schemaBootstrap.useHelmHooks=true`, the Job runs as a pre-install/pre-upgrade
hook. Do not attach schema verification to every runtime pod; repeated graph
schema checks can saturate a large existing backend during rolling updates.

Do not combine Helm-hook schema bootstrap with bundled NornicDB:

```yaml
schemaBootstrap:
  useHelmHooks: true
nornicdb:
  enabled: true
  capabilities:
    relationshipMergePropertyIdentity: true
```

Helm rejects that render because hooks run before the chart-managed NornicDB
Service and Deployment exist. Deploy NornicDB separately first, or set
`schemaBootstrap.useHelmHooks=false` and provide ordering through your release
or GitOps workflow.

## Environment

| Variable | Required | Purpose |
| --- | --- | --- |
| `ESHU_POSTGRES_DSN` | yes | Postgres connection string. |
| `ESHU_GRAPH_BACKEND` | no | `nornicdb` or `neo4j`; default is `nornicdb`. |
| `NEO4J_URI` | yes | Bolt URI for NornicDB or Neo4j. |
| `NEO4J_USERNAME` / `NEO4J_PASSWORD` | yes | Bolt client credentials. |
| `DEFAULT_DATABASE` | no | Bolt database name, default `nornic`. |
| `ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT` | no | Per graph DDL statement deadline, default `2m`. |
| `ESHU_GRAPH_SCHEMA_ADOPT_EXISTING` | no | Adopt a complete existing graph schema by writing the fingerprint marker. |
| `ESHU_GRAPH_SCHEMA_FORCE_REAPPLY` | no | Apply graph schema despite a matching marker. For disaster recovery, after the graph was wiped and Postgres kept. |

Existing-schema adoption inspects `SHOW CONSTRAINTS` and `SHOW INDEXES`, then
fails closed if inspection errors. Unset adoption is opportunistic for NornicDB
and disabled for Neo4j; truthy values require adoption support. When inspection
finds an incomplete NornicDB schema, bootstrap forwards only missing objects to
the strict DDL pass. Existing indexes and constraints are skipped before they
reach the backend, avoiding repeated populated-index backfills during additive
schema upgrades.

## Bootstrap Index

`eshu-bootstrap-index` performs one-shot initial indexing. Use it to materialize
an initial repository set, reduce cold-start time on a new environment, validate
end-to-end indexing, or recover after operator-controlled reset work.

It is packaged for Docker Compose and direct process use. It is not a
steady-state workload in the public Helm chart, and it does not expose
`/healthz`, `/readyz`, `/metrics`, or `/admin/status`.

Deployment flows should run `eshu-bootstrap-data-plane` before
`eshu-bootstrap-index`. Direct local or CI bootstrap-index runs still verify the
latest graph schema marker before opening the projection writer. If no marker
exists, bootstrap-index applies the same strict checked-in graph schema, writes
the marker only after all graph statements succeed, then opens the normal
projection writer. If a latest marker exists but is incompatible, bootstrap-index
fails closed instead of applying schema or writing graph data.

Repeated restarts or long-running bootstrap activity are incidents. Use the
ingester, workflow coordinator, hosted collectors, and resolution engine for
normal freshness.

## Related Pages

- [Helm Runtime Values](../deploy/kubernetes/helm-runtime-values.md)
- [Storage](../deploy/kubernetes/storage.md)
- [Bootstrap Index Service](../services/bootstrap-index.md)
- [Telemetry Overview](../reference/telemetry/index.md)
