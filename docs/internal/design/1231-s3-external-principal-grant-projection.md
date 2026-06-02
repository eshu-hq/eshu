# S3 External Principal Grant Projection

Issue: #1231

## Contract

`s3_external_principal_grant` facts are metadata-only AWS evidence derived from
bucket policy parsing. The reducer projects exact, graph-safe grant identities
into:

- `(:CloudResource)-[:GRANTS_ACCESS_TO]->(:ExternalPrincipal)`

The source `CloudResource` must already exist from AWS resource materialization.
The reducer never creates S3 bucket nodes. A missing source bucket produces a
bounded skip outcome.

`ExternalPrincipal` is keyed by:

- `uid = StableID("ExternalPrincipal", {"principal_kind": kind, "principal_value": value})`

The node stores `principal_kind`, `principal_value`,
`principal_account_id`, `principal_partition`, `principal_service`,
`scope_id`, `generation_id`, and `evidence_source`. The identity key excludes
optional partition/account/service fields so one principal does not split when
an upstream source omits secondary metadata. Optional metadata enriches an
existing node only when the incoming row carries a non-empty value, so a later
partial observation cannot clear prior bounded metadata for the same principal.

The edge stores `grant_outcome`, `is_public`, `is_cross_account`,
`is_service_principal`, `resolution_mode`, `scope_id`, `generation_id`, and
`evidence_source`.

Unsupported or incomplete principal identities do not create
`ExternalPrincipal` nodes. They are counted and logged as skipped outcomes.

## Privacy Boundary

The projection must not read, persist, or write raw policy JSON, statement
bodies, actions, resource lists, condition payloads, ACL grants, object keys, or
object data. Those values stay adapter-local and are discarded before durable
facts. Reducer rows and graph writes use only the bounded fact fields listed in
the contract above.

## Readiness

The reducer intent uses entity key `aws_resource_materialization:<scope_id>` and
gates on `cloud_resource_uid / canonical_nodes_committed`. This is the same
source-bucket readiness slice used by S3 `LOGS_TO`, RDS posture, and S3
internet exposure.

## Performance Evidence

No-Regression Evidence: focused package tests cover projector intent emission,
source-bucket readiness, reducer extraction skips, raw-policy redaction at the
row and writer boundaries, static relationship-token validation, scoped retract,
schema DDL, and telemetry span registration:

```bash
go test ./internal/projector -run 'S3ExternalPrincipalGrant' -count=1
go test ./internal/reducer -run 'S3ExternalPrincipalGrant' -count=1
go test ./internal/storage/cypher -run 'S3ExternalPrincipalGrant' -count=1
go test ./internal/graph -run 'ExternalPrincipal' -count=1
go test ./internal/storage/postgres -run 'S3ExternalPrincipalGrant' -count=1
go test ./internal/telemetry -run 'SpanNames' -count=1
```

Benchmark Evidence: on darwin/arm64 Apple M4 Pro with a no-op group executor,
`go test ./internal/storage/cypher -run '^$' -bench
'BenchmarkS3ExternalPrincipalGrantWriter|BenchmarkS3LogsToEdgeWriter|BenchmarkCloudResourceEdgeWriter|BenchmarkCloudResourceNodeWriter'
-benchmem -benchtime=100x` shaped 5,000 rows at batch size 500:

- `BenchmarkS3ExternalPrincipalGrantWriter-12`: `3.28 ms/op`, `6.49 MB/op`,
  `35,072 allocs/op`.
- `BenchmarkCloudResourceNodeWriter-12`: `2.90 ms/op`, `6.33 MB/op`,
  `25,068 allocs/op`.
- `BenchmarkCloudResourceEdgeWriter-12`: `1.83 ms/op`, `3.89 MB/op`,
  `40,100 allocs/op`.
- `BenchmarkS3LogsToEdgeWriter-12`: `1.39 ms/op`, `1.97 MB/op`,
  `25,071 allocs/op`.

The #1231 writer is expectedly heavier than edge-only writers because it
MERGEs an `ExternalPrincipal` node and the `GRANTS_ACCESS_TO` relationship in
one batched statement. It remains bounded by `ceil(rows/batch_size)` statements
with no per-row graph round trip, and both graph anchors use uid-indexed labels.

Observability Evidence: the reducer emits
`reducer.s3_external_principal_grant_materialization` and a structured
completion log with resource fact count, grant fact count, edge count, resolved
outcome tally, skipped-reason tally, first-generation retract decision, and
load/extract/retract/write/total durations. The existing reducer execution
counter remains labeled by domain, and graph statements carry
`phase=s3_external_principal_grant` plus `label=ExternalPrincipal` metadata for
the instrumented graph executor.
