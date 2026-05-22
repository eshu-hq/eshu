# internal/storage/postgres

## Purpose

`internal/storage/postgres` owns Eshu's relational persistence: facts, queues,
content rows, status, recovery data, decisions, webhook and AWS freshness
triggers, workflow control, Terraform-state admin/read models, and reducer
support stores.

## Ownership boundary

This package owns Postgres schema and typed storage adapters. It does not own
collector observation, parser semantics, graph-write Cypher, reducer truth
decisions, or public HTTP/MCP handlers. Callers must preserve the transaction,
lease, freshness, retry, and idempotency contracts documented on the storage
helpers they use.

## Exported surface

See `doc.go` for the package contract. Main surfaces include:

- schema bootstrap helpers and `ExecQueryer`/transaction wrappers
- `IngestionStore`, `FactStore`, `ContentWriter`, and `ContentStore`
- `ProjectorQueue`, `ReducerQueue`, workflow control, and queue observers
- graph phase state/repair, shared intent and acceptance stores
- status, recovery, decision, webhook, AWS freshness, AWS scan, and workflow
  coordinator stores
- Terraform-state backend/status/drift evidence and IaC reachability stores
- active reducer fact queries and writers for package, image, CI/CD,
  service-catalog, SBOM, and supply-chain domains

This README intentionally avoids a full exported-symbol catalog; use godoc for
the current list.

## Core contracts

- Schema bootstrap is idempotent and ordered. DDL helpers use `IF NOT EXISTS`,
  and tables with foreign keys must appear after referenced tables in
  `BootstrapDefinitions`.
- `graph_schema_applications` stores the graph backend/schema fingerprint after
  `eshu-bootstrap-data-plane` applies graph DDL. Preserved-volume restarts use
  it to skip repeated NornicDB constraint/index checks when the schema is
  unchanged.
- Fact writes deduplicate by `fact_id` before batching. Skipping
  `deduplicateEnvelopes` can trigger `SQLSTATE 21000` in a multi-row
  `ON CONFLICT DO UPDATE`.
- Fact payloads pass through `sanitizeJSONB` before insert so binary or
  non-UTF-8 repository content does not poison Postgres JSONB writes.
- `CommitScopeGeneration` de-dupes against the newest pending or active
  same-scope generation. Failed generations do not satisfy the skip path, so a
  failed first projection remains retryable.
- `ProjectorQueue.Claim` preserves one active source-local generation per
  `scope_id` with `FOR UPDATE SKIP LOCKED`, oldest-ready-row selection,
  expired-lease priority, stale duplicate reclaim, and supersession of older
  same-scope generations.
- `ProjectorQueue.Ack` is a four-step transaction: supersede stale active
  generation, activate target generation, update scope pointer, mark work
  succeeded. It requires a `Beginner` such as `SQLDB` or `InstrumentedDB` around
  `SQLDB`.
- `ReducerQueue.Claim` owns the NornicDB semantic gate. When enabled,
  `semantic_entity_materialization` waits for source-local projection to stop
  competing for graph label indexes.
- `StatusStore` merges `fact_work_items`, pending `shared_projection_intents`,
  and active shared projection leases so `/admin/status` does not report
  healthy before reducer-owned edges become graph-visible.
- Reducer-owned read models use partial indexes for active facts such as
  package correlations, container-image identity, CI/CD run correlations,
  service-catalog correlations, SBOM/attestation attachments, and
  supply-chain impact. Do not add an API/MCP read path that scans all
  `fact_records` rows.
- Workflow, projector, reducer, AWS checkpoint, and AWS scan-status mutations
  are lease- or fencing-token-aware. A rejected fenced write means the caller no
  longer owns the work and must stop.
- Scheduled collector target admission is guarded by
  `CreateRunWithWorkItemsIfNoOpenTargets`. It skips duplicate non-terminal
  targets and uses the deterministic run id plus target tuple as an idempotency
  key during preserved-volume restarts.

## Dependencies

The package depends on facts, scope/workflow models, reducer/projector ports,
query/status contracts, telemetry, and Postgres driver interfaces. Graph Cypher
execution stays in `internal/storage/cypher`.

## Telemetry

Postgres paths use `InstrumentedDB`, queue observer gauges, fact emission spans,
IaC reachability materialization spans, drift evidence spans, AWS drift
evidence spans, queue claim/run metrics, and structured failure logs. Keep
repository paths, fact IDs, cloud IDs, and row payload details out of metric
labels.

## Gotchas / invariants

- Queue claim, heartbeat, ack, fail, replay, supersession, and reclaim paths
  must remain idempotent under retry and partial failure.
- Projector supersession of rows and scope generations must stay atomic.
- Reducer claim-domain filters and semantic-entity claim gates are scheduling
  controls, not alternate truth semantics.
- Fact reads that filter by kind or payload must stay bounded and stable.
- Terraform config-vs-state drift depends on byte-compatible dot-path
  flattening between parser config rows and state rows.
- Schema changes require matching status/recovery/query docs and migration
  proof.

## Focused tests

```bash
cd go
go test ./internal/storage/postgres -run 'Test.*Queue|Test.*Workflow|Test.*Ingestion|Test.*Status|Test.*Drift|Test.*Schema|Test.*Content|Test.*Fact' -count=1
go test ./internal/storage/postgres -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

## Related docs

- `docs/public/architecture.md`
- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `go/internal/reducer/README.md`
- `go/internal/collector/terraformstate/README.md`
