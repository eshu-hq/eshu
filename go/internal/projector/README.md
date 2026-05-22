# Projector

## Purpose

`internal/projector` owns source-local projection stages. It turns committed
fact envelopes into canonical graph rows and publishes readiness or reducer
intents for shared, cross-source domains.

## Ownership boundary

The projector owns fact filtering, source-local canonical rows, failure
classification, queue retry behavior, readiness publication, and handoff to the
Cypher writers. It does not admit cross-source truth; reducer domains own
correlation, materialization, and shared graph projection.

## Exported surface

Use `go doc ./internal/projector` for the full contract. The main surface is:

- `Runtime`, `Service`, `ProjectionRunner`, work source/sink/heartbeat ports,
  and retry helpers.
- Stage functions such as `ProjectFileStage`, `ProjectEntityStage`,
  `ProjectRelationshipStage`, and `ProjectWorkloadStage`.
- Canonical row types for repositories, directories, files, entities, workloads,
  Terraform state, OCI registry, package registry, and reducer intents.
- `ClassifyFailure`, `FailureClass`, `RetryDisposition`,
  `InputValidationError`, and `ResourceExhaustedError`.
- `EntityTypeLabel` and `EntityTypeLabelMap` for parser/content label mapping.

## Dependencies

- `internal/facts` supplies durable envelopes and fact kinds.
- `internal/storage/cypher` consumes canonical graph rows.
- `internal/reducer` consumes published reducer intents and readiness rows.
- `internal/telemetry` supplies projector spans, stage metrics, queue metrics,
  and structured log keys.

Graph writes route through the `CanonicalWriter` interface. The projector keeps
source-local evidence local: package-registry source hints, AWS resources, image
references, and service-catalog facts enqueue reducer intents for the owning
domain instead of admitting cross-source truth here.

## Telemetry

Projector code emits stage duration metrics, queue claim/ack/failure status,
generation processing logs, failure classification, and source-local projection
timings. Runtime-affecting changes must keep the operator path clear: whether
work is stuck, slow, failing, retrying, superseded, or complete.

## Gotchas / invariants

- Projection must be idempotent across duplicate claims, retries, and partial
  graph writes.
- `ErrWorkSuperseded` means a newer same-scope generation replaced stale local
  polling work; do not report it as corrupt data.
- Source-local projection does not invent cross-source deployment, workload, or
  ownership truth.
- OCI registry projection treats digest-addressed manifests, indexes, and
  descriptors as canonical identity; tags remain weak mutable observations.
- OCI, Git, and AWS image-reference evidence emit one
  `container_image_identity` reducer intent per scope generation.
- AWS resource observations stay source-local until an
  `aws_cloud_runtime_drift` reducer intent is emitted; reducer code owns ARN
  joins and unmanaged/orphan admission.
- Service-catalog facts emit one `service_catalog_correlation` reducer intent
  per scope generation after schema validation; reducer code owns repository
  evidence gating.
- Entity labels must stay aligned with graph schema support, including
  Terraform backend/import/refactor/check and lockfile-provider labels.

## Focused tests

```bash
go test ./internal/projector -count=1
go doc ./internal/projector
```

## Related docs

- `docs/public/architecture.md`
- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/cypher-performance.md`
- `go/internal/storage/cypher/README.md`
- `go/internal/reducer/README.md`
