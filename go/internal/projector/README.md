# Projector

## Purpose

`internal/projector` owns source-local projection stages. It turns committed
fact envelopes into canonical graph rows and publishes readiness or reducer
intents for shared domains.

## Ownership Boundary

The projector owns fact filtering, source-local canonical rows, failure
classification, queue retry behavior, readiness publication, and Cypher writer
handoff. It does not admit cross-source truth; reducer domains own correlation,
materialization, and shared graph projection.

## Exported Surface

See `doc.go` and `go doc ./internal/projector`. Main surfaces include
`Runtime`, `Service`, `ProjectionRunner`, work source/sink/heartbeat ports,
retry helpers, stage functions, canonical row types, failure classification,
`EntityTypeLabel`, and `EntityTypeLabelMap`.

## Telemetry

Projector code emits projector run, reducer-intent enqueue, canonical write,
stage duration, queue, supersession, failure-classification, and large-generation
semaphore signals through `internal/telemetry`.

## Gotchas / Invariants

- Projection must be idempotent across duplicate claims, retries, and partial
  graph writes.
- `ErrWorkSuperseded` means newer same-scope work replaced stale local polling;
  do not treat it as corrupt data.
- Source-local projection must not invent cross-source deployment, workload, or
  ownership truth.
- OCI digest identities are canonical; tags are mutable observations.
- AWS resource observations stay source-local until reducer intent handles
  runtime-drift materialization.

## Focused Tests

```bash
cd go
go test ./internal/projector -count=1
go doc ./internal/projector
go run ./cmd/eshu docs verify ../go/internal/projector --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related Docs

- `docs/public/architecture.md`
- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/cypher-performance.md`
- `go/internal/storage/cypher/README.md`
