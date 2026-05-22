# cmd/ingester

## Purpose

`cmd/ingester` builds the `eshu-ingester` binary. The process owns repository
sync, discovery, parsing, fact emission, and source-local projection for Git
repository scopes. In Kubernetes it is the long-running runtime that owns the
workspace PVC.

## Ownership boundary

The ingester runs collector and projector services in one process. It commits
facts to Postgres, fills the projector queue, drains source-local projection,
and runs deferred relationship maintenance after collector batch drains.
Cross-domain materialization belongs to the reducer, HTTP reads belong to API
and MCP runtimes, and schema DDL belongs to `eshu-bootstrap-data-plane`.

Startup flow:

```text
main.run
  -> telemetry.NewBootstrap / NewProviders
  -> runtime.OpenPostgres
  -> openIngesterCanonicalWriter
  -> buildIngesterService
  -> collector.Service + projector.Service via compositeRunner
  -> app.NewHostedWithStatusServer
```

`compositeRunner` cancels both collector and projector on the first returned
error. `SIGINT` and `SIGTERM` cancel the process context.

## Exported surface

`cmd/ingester` is a `main` package and exports no Go API. Its contract is the
process interface:

- `eshu-ingester --version` and `eshu-ingester -v` exit before telemetry,
  Postgres, graph, queue, or HTTP setup.
- `/healthz`, `/readyz`, `/metrics`, and `/admin/status` are mounted through
  the shared runtime admin surface.
- `/admin/recovery` is mounted only when the recovery handler can resolve the
  API key.
- `ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED=true` makes repository selection check
  queued GitHub, GitLab, and Bitbucket refresh triggers before scheduled
  polling.
- `ESHU_REPO_SCHEDULED_SYNC_ENABLED=false` requires webhook trigger handoff and
  prevents fallback to broad scheduled repository selection.

## Dependencies

- `internal/collector` for Git source selection, sync, snapshotting, parsing,
  and fact writes.
- `internal/projector` for source-local projection and reducer intent enqueue.
- `internal/storage/postgres` for fact, content, projector queue, reducer queue,
  recovery, status, and observer stores.
- `internal/storage/cypher` for canonical graph writer construction.
- `internal/runtime` for datastore opening, runtime config, retry policy,
  memory limit, pprof, and admin server wiring.
- `internal/app`, `internal/recovery`, and `internal/telemetry` for process
  hosting and observability.

## Telemetry

The ingester inherits collector and projector signals. Key metrics include
`eshu_dp_repo_snapshot_duration_seconds`,
`eshu_dp_repos_snapshotted_total`,
`eshu_dp_facts_emitted_total`,
`eshu_dp_facts_committed_total`,
`eshu_dp_large_repo_semaphore_wait_seconds`,
`eshu_dp_projections_completed_total`, and
`eshu_dp_projector_stage_duration_seconds`. Hosted Git sync emits bounded
structured logs for start, progress, completion, and failure with credential
values redacted.

Compose exposes the ingester metrics endpoint on `http://localhost:19465/metrics`.

## Gotchas / invariants

- The ingester is the only long-running runtime that should mount the workspace
  PVC in Kubernetes.
- `IngestionStore.SkipRelationshipBackfill = true` keeps per-commit writes
  cheap. `AfterBatchDrained` runs `BackfillAllRelationshipEvidence` and then
  `ReopenDeploymentMappingWorkItems`; both must succeed.
- Version probes must stay at the top of startup so image checks do not require
  datastore credentials.
- `ESHU_PROJECTOR_WORKERS` defaults to `min(NumCPU, 8)`, except
  `local_authoritative` + NornicDB defaults to NumCPU unless explicitly set.
- `ESHU_QUERY_PROFILE=local_lightweight` or `ESHU_DISABLE_NEO4J=true` skips
  canonical graph writes for local code-search workflows.
- NornicDB grouped writes are disabled by default. Enabling
  `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true` requires conformance evidence.
- NornicDB entity containment is batched into row-scoped entity upserts by
  default; disable it only for measured fallback comparisons.
- Entity-phase concurrency uses `ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY`; set it
  to `1` for serial comparison, not as a shipped workaround for write races.

## Verification

Use the smallest command that proves the changed contract:

```bash
cd go
go test ./cmd/ingester -count=1
go doc -cmd ./cmd/ingester
go run ./cmd/eshu docs verify ../go/cmd/ingester --limit 1000 \
  --fail-on contradicted,missing_evidence
```

If a change touches collector/projector wiring, queue behavior, graph writes,
or NornicDB knobs, also run the focused package tests for the touched internal
package and the performance-evidence gate from the repo root.

## Related docs

- `docs/public/architecture.md`
- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/nornicdb-tuning.md`
- `go/internal/collector/README.md`
- `go/internal/projector/README.md`
