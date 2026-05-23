# Projector

## Purpose

`internal/projector` owns source-local projection stages. It turns committed
fact envelopes into canonical graph rows and publishes readiness or reducer
intents for shared, cross-source domains.

## Ownership boundary

The projector owns fact filtering, source-local canonical rows, failure
classification, queue retry behavior, readiness publication, and handoff to
Cypher writers. It does not admit cross-source truth; reducer domains own
correlation, materialization, and shared graph projection.

## Exported surface

See `doc.go` for the godoc contract. The main surfaces are `Runtime`,
`Service`, `ProjectionRunner`, work source/sink/heartbeat ports, retry helpers,
stage functions such as `ProjectFileStage`, `ProjectEntityStage`,
`ProjectRelationshipStage`, and `ProjectWorkloadStage`, canonical row types,
failure classification types, `EntityTypeLabel`, and `EntityTypeLabelMap`.

## Dependencies

`internal/facts` supplies durable envelopes and fact kinds.
`internal/storage/cypher` consumes canonical graph rows. `internal/reducer`
consumes published reducer intents and readiness rows. `internal/telemetry`
supplies projector spans, stage metrics, queue metrics, and structured log
keys.

Graph writes route through the `CanonicalWriter` interface. Source-local
evidence such as package-registry source hints, AWS resources, image
references, and service-catalog facts enqueue reducer intents instead of
admitting cross-source truth here.

## Telemetry

Projector code emits `SpanProjectorRun`, `SpanReducerIntentEnqueue`,
`SpanCanonicalWrite`, projector stage duration metrics, canonical projection
metrics, reducer-intent enqueue counters, queue claim/ack/failure signals,
generation processing logs, failure classification, supersession logs, and
large-generation semaphore wait metrics.

## Gotchas / invariants

- Projection must be idempotent across duplicate claims, retries, and partial
  graph writes.
- `ErrWorkSuperseded` means a newer same-scope generation replaced stale local
  polling work; do not report it as corrupt data.
- Source-local projection does not invent cross-source deployment, workload, or
  ownership truth.
- OCI registry projection treats digest-addressed manifests, indexes, and
  descriptors as canonical identity; tags remain weak mutable observations.
- OCI, Git, and AWS image-reference evidence emit
  `container_image_identity` reducer intents for reducer-owned joins.
- AWS resource observations stay source-local until an
  `aws_cloud_runtime_drift` reducer intent is emitted.
- Entity labels must stay aligned with graph schema support.

## Focused tests

```bash
cd go
go test ./internal/projector -count=1
go doc ./internal/projector
go run ./cmd/eshu docs verify ../go/internal/projector --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related docs

- `docs/public/architecture.md`
- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/cypher-performance.md`
- `go/internal/storage/cypher/README.md`
- `go/internal/reducer/README.md`
