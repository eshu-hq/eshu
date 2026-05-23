# cmd/bootstrap-index Agent Rules

These rules apply only inside `go/cmd/bootstrap-index/`. Root `AGENTS.md`
still controls global proof, performance, concurrency, and skill requirements.

## Read First

- `go/cmd/bootstrap-index/README.md`
- `go/cmd/bootstrap-index/doc.go`
- `go/cmd/bootstrap-index/main.go`
- `go/cmd/bootstrap-index/wiring.go`
- `go/cmd/bootstrap-index/nornicdb_wiring.go`
- `go/internal/storage/postgres/ingestion.go`
- `go/internal/storage/postgres/drift_enqueue.go`

## Local Invariants

- MUST preserve the facts-first bootstrap order in `runPipelined`: collect and
  project, backfill relationship evidence, wait for projector drain,
  materialize IaC reachability, reopen deployment mapping, then enqueue
  config-vs-state drift intents.
- MUST NOT call `ReopenDeploymentMappingWorkItems` before projector drain.
  Doing so can leave deployment-mapping work complete before required
  relationship evidence exists.
- MUST keep `SkipRelationshipBackfill=true` on the bootstrap ingestion store.
  Per-commit relationship backfill belongs out of the hot collection path.
- MUST treat `errProjectorDrained` as the normal projector-drain sentinel, not
  as a bootstrap failure.
- MUST treat `projector.ErrWorkSuperseded` as stale-generation control flow.
  Do not ack stale graph truth.
- MUST keep NornicDB grouped writes disabled by default unless conformance and
  performance evidence promote the setting.
- MUST keep `ESHU_DISCOVERY_REPORT` diagnostic-only; it is not runtime truth.

## Change Gates

- New post-collection passes MUST be added to `bootstrapCommitter`, implemented
  on the Postgres ingestion store, wired after projector drain, assigned a
  failure class, and covered by an ordering test.
- Domains that consume `resolved_relationships` MUST have a post-reopen trigger
  or requeue path after deployment mapping is reopened.
- Projection worker or NornicDB tuning changes MUST update the relevant runtime
  and tuning docs named by the README and include same-shape performance proof.
- Signal-handling changes MUST define partial-phase cleanup and retry behavior
  before implementation.

## Focused Verification

```bash
cd go
go test ./cmd/bootstrap-index -count=1
go test ./cmd/bootstrap-index ./cmd/ingester ./internal/storage/postgres -count=1
```
