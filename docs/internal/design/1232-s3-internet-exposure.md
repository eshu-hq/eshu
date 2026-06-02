# S3 Internet Exposure Node-Property Projection

Status: implemented for issue #1232.

Issue: #1232 (`aws/deep: S3 internet-exposure posture derivation`). Parent:
#1147 / #51 AWS deep scanner work.

## Goal

Derive a bounded S3 bucket internet-exposure signal from existing
`s3_bucket_posture` facts and write it onto already-materialized S3
`CloudResource` nodes. This gives graph/API consumers a direct answer to "which
scanned buckets are known public, known blocked, or unknown?" without storing raw
bucket policies, ACL grants, object names, object payloads, or inventing graph
nodes.

This is separate from the #1144 `LOGS_TO` edge slice. `LOGS_TO` projects an edge
between two scanned buckets. Internet exposure is node-property-only on the
source bucket's existing `CloudResource` node.

## Inputs

Loaded fact kinds:

- `aws_resource`: S3 bucket node substrate, filtered to `resource_type =
  aws_s3_bucket`.
- `s3_bucket_posture`: metadata-only derived posture fields:
  `policy_present`, `policy_grants_public`, `block_public_access_all`,
  `ignore_public_acls`, and `restrict_public_buckets`.

The reducer uses the same in-memory S3 bucket-name join index as #1144 LOGS_TO:
`bucket_name`, S3 ARN tail, or `s3://` correlation anchor. Missing source bucket
nodes are counted as `source_unresolved` and produce no write.

## Decision Model

The model is conservative and tri-state:

| State | Boolean property | Reason |
| --- | --- | --- |
| `exposed` | `true` | `policy_grants_public=true` and `restrict_public_buckets=false` |
| `not_exposed` | `false` | public policy exists but is blocked by block-public-access |
| `not_exposed` | `false` | no public policy grant and ACL public access is blocked |
| `unknown` | property absent | public policy grant is unknown |
| `unknown` | property absent | public policy exists but `restrict_public_buckets` is unknown |
| `unknown` | property absent | public-access-block data is partial |

Unknown never becomes false. The graph keeps `s3_internet_exposure_state =
unknown` and removes `s3_internet_exposed` for unknown rows so downstream
queries cannot treat missing evidence as safe.

## Graph Contract

Writer: `storage/cypher.S3InternetExposureNodeWriter`.

Cypher shape:

- `UNWIND $rows AS row`
- `MATCH (resource:CloudResource {uid: row.uid})`
- `SET` reducer-owned properties only

No `MERGE` is used, so the writer cannot fabricate CloudResource nodes.

Reducer-owned properties:

- `s3_internet_exposure_state`
- `s3_internet_exposed`
- `s3_internet_exposure_reason`
- `s3_internet_exposure_scope_id`
- `s3_internet_exposure_generation_id`
- `s3_internet_exposure_evidence_source`
- `s3_internet_exposure_source_fact_id`

Retract removes only those properties where scope and evidence source match.
It never deletes nodes and never touches other CloudResource properties.

## Readiness And Retries

Projector enqueues `s3_internet_exposure_materialization` when a generation
contains any `s3_bucket_posture` fact. Its entity key is
`aws_resource_materialization:<scope>`, matching the CloudResource node phase
published by `DomainAWSResourceMaterialization`.

The durable Postgres claim gate and in-handler gate both require:

- keyspace: `cloud_resource_uid`
- phase: `canonical_nodes_committed`
- same scope, generation, and entity key

A missed phase is retryable. First-generation retractions are skipped when
there is no prior generation; retries and later generations retract before
writing so stale exposure properties are removed.

## Observability

- Span: `reducer.s3_internet_exposure_materialization`.
- Counter: `eshu_dp_s3_internet_exposure_decisions_total`, labels
  `outcome` and `reason`.
- Counter: `eshu_dp_s3_internet_exposure_skipped_total`, label `skip_reason`.
- Completion log: resource fact count, posture fact count, row count, decision
  and reason tallies, skip tally, and load / derive / retract / graph-write /
  total durations.
- `/admin/status` queue blockage reports the same `readiness` conflict-domain
  when `CloudResource` nodes are not committed.

## Verification

Focused proof:

```bash
go test ./internal/projector -run 'S3InternetExposure|S3LogsTo' -count=1
go test ./internal/reducer -run 'S3InternetExposure' -count=1
go test ./internal/storage/cypher -run 'S3InternetExposure' -count=1
go test ./internal/storage/postgres ./internal/telemetry -run 'S3InternetExposure|SpanNames|MetricDimensionKeys' -count=1
go test ./internal/storage/cypher -run '^$' -bench 'BenchmarkS3InternetExposureNodeWriter|BenchmarkS3LogsToEdgeWriter|BenchmarkCloudResourceEdgeWriter' -benchmem -benchtime=50x
```

Benchmark evidence on darwin/arm64 (Apple M4 Pro), 5,000 rows at batch size
500:

- `BenchmarkS3InternetExposureNodeWriter-12`: `1.36 ms/op`, `1.97 MB/op`,
  `25,069 allocs/op`.
- `BenchmarkS3LogsToEdgeWriter-12`: `1.39 ms/op`, `1.97 MB/op`,
  `25,071 allocs/op`.
- `BenchmarkCloudResourceEdgeWriter-12`: `1.90 ms/op`, `3.89 MB/op`,
  `40,100 allocs/op`.
