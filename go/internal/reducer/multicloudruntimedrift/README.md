# multicloudruntimedrift

## Purpose

Publishes admitted provider-neutral runtime drift findings for GCP and Azure --
orphaned, unmanaged, ambiguous, unknown, image-version drift, and
value-comparison-inconclusive -- as durable `reducer_multi_cloud_runtime_drift_finding`
facts. This is issues #1997, #1998, #5759. This package moved out of the flat
`internal/reducer` root under issue #6061.

## Ownership boundary

This package owns the multi-cloud runtime drift evaluation
(`MultiCloudRuntimeDriftHandler.Handle`), the AWS-row exclusion filter
(`excludeAWSOwnedRows`, #5759), and the durable fact writer
(`PostgresMultiCloudRuntimeDriftWriter`).

It does not own the evidence SQL join (`internal/storage/postgres`'s
`PostgresMultiCloudRuntimeDriftEvidenceLoader`), the correlation rule pack or
candidate model (`internal/correlation/drift/multicloud`, `internal/correlation/
engine`, `internal/correlation/model`), or the shared batched fact-write
mechanics (`reducer/factwrite`). Registration stays in the reducer root
(`defaults_additive_domains_secrets_drift.go`).

## Exported surface

- `MaterializationDomainDefinition` -- the root additive-domain registry
  (`defaults_additive_domains_secrets_drift.go`)
- `MultiCloudRuntimeDriftHandler` -- the root additive-domain registry
- `MultiCloudRuntimeDriftEvidenceLoader`, `MultiCloudRuntimeDriftFindingWriter` --
  `cmd/reducer`; `DefaultHandlers` declares
  `MultiCloudRuntimeDriftEvidenceLoader`/`MultiCloudRuntimeDriftWriter` with
  these types directly -- no root compat file
- `PostgresMultiCloudRuntimeDriftWriter` -- `cmd/reducer`, and
  `internal/replay/costcounting`'s cost-budget tests
- `MultiCloudRuntimeDriftWrite`, `MultiCloudRuntimeDriftWriteResult` -- the
  handler/writer request and result shapes

See `doc.go` for the godoc-rendered contract.

## Dependencies

`reducer/contract`, `reducer/factwrite`, `reducer/payloadcore`,
`internal/correlation/cloudinventory`, `internal/correlation/drift/cloudruntime`,
`internal/correlation/drift/multicloud`, `internal/correlation/engine`,
`internal/correlation/model`, `internal/correlation/rules`, `internal/facts`,
`internal/telemetry`, `internal/truth`, `pkg/log`, `sdk/go/factschema`,
`sdk/go/factschema/reducerderived/v1`. Never `internal/reducer`.

## Telemetry

No metric instrument of its own. `multicloud.RecordEvaluation` records the
shared correlation-engine counters (`eshu_dp_correlation_rule_matches_total`,
`eshu_dp_correlation_orphan_detected_total`,
`eshu_dp_correlation_unmanaged_detected_total`, and related admission/skip
counters) the AWS structural drift path and every other rule-pack consumer
already emit through; `factwrite.BatchInsertVersionedFacts` and the storage
layer's `PostgresMultiCloudRuntimeDriftEvidenceLoader` cover Postgres query
duration. See `docs/public/observability/telemetry-coverage.md` (no row change:
this move is a rename, not a new stage).

No-Regression Evidence: #6061 relocates this family's production logic without
changing it. Every hunk in the moved production files is a package clause, an
import requalification, or an identifier requalification: symbols the reducer
root supplied as one-line forwarders (`Intent`/`Result`/`ResultStatusSucceeded`/
`DomainMultiCloudRuntimeDrift`, `workloadIdentityExecer`, `reducerFactVersionedRow`,
`reducerBatchInsertVersionedFacts`, `reducerWriterNow`, `reducerFactCollectorKind`,
`nonNilStrings`, `nonNilMapSlice`) are now called directly against the leaf
package that already owned them (`reducer/contract`, `reducer/factwrite`,
`reducer/payloadcore`). The previously-unexported domain constructor
(`multiCloudRuntimeDriftDomainDefinition`) is exported and renamed to
`MaterializationDomainDefinition` to match the `iamescalation`/`ec2blockkms`
precedent. Measured on this branch: `go build ./...` exits 0, `go vet ./...`
exits 0, `go test ./internal/reducer/multicloudruntimedrift -count=1` passes,
and `go test ./internal/reducer/... ./cmd/reducer ./internal/storage/postgres
./internal/replay/costcounting -count=1` passes.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph, or
storage contract. The metrics and log lines are the same before and after the
move.

## Gotchas / invariants

- `excludeAWSOwnedRows` MUST run before candidate construction. The shared
  evidence loader's SQL deliberately joins AWS, GCP, and Azure inventory facts
  into one `cloud_resource_uid` keyspace for implementation reuse (see
  `PostgresMultiCloudRuntimeDriftEvidenceLoader` in
  `internal/storage/postgres`), so a scope carrying AWS facts alongside GCP/
  Azure facts returns AWS rows too. Without the filter this domain would
  silently republish every AWS finding a second time under
  `reducer_multi_cloud_runtime_drift_finding`, duplicating what
  `DomainAWSCloudRuntimeDrift` already reports for the same resource.
- The handler never emits telemetry or a durable write before the write
  succeeds (`TestMultiCloudRuntimeDriftHandlerDoesNotEmitBeforeDurableWrite`).
- Admitted-finding logs are redacted through `telemetry.SafeResourceLogAttrs`
  and carry only the bounded `drift.pack`/`drift.kind`/`drift.provider`
  attributes -- never the raw cloud identity string.
- The fact identity (scope, generation, finding kind, canonical uid) MUST stay
  stable across replays; `TestPostgresMultiCloudRuntimeDriftWriterIsIdempotentAcrossReplays`
  guards this.
- Test helpers (`fakeMultiCloudRuntimeDriftExecer`,
  `decodeBatchedVersionedFactCalls`, `reducerCounterValue`, `counterTotal`,
  `hasAttrs`, `expectedBatchedExecCount`) are duplicated from the reducer root
  by design; Go test files cannot share unexported symbols across a package
  boundary.

## Related docs

- `go/internal/reducer/README.md`
- `go/internal/reducer/multi-cloud-runtime-drift.md`
- `go/internal/reducer/factwrite/README.md`
- `go/internal/reducer/payloadcore/README.md`
- `docs/public/observability/telemetry-coverage.md`
