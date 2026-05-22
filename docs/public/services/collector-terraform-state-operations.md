# Terraform State Collector Operations

Use this page to operate `collector-terraform-state`: metrics, status, and
failure triage.

## Dashboard Starting Points

| Question | Start with |
| --- | --- |
| Is work backing up before claims start? | `eshu_dp_tfstate_claim_wait_seconds` |
| Are discovery candidates being resolved? | `eshu_dp_tfstate_discovery_candidates_total{source}` |
| Are state snapshots being observed? | `eshu_dp_tfstate_snapshots_observed_total{backend_kind,result}` |
| How large are state sources? | `eshu_dp_tfstate_snapshot_bytes` |
| Is parsing slow? | `eshu_dp_tfstate_parse_duration_seconds` |
| Are facts reaching the boundary? | `eshu_dp_tfstate_resources_emitted_total`, `eshu_dp_tfstate_outputs_emitted_total`, `eshu_dp_tfstate_modules_emitted_total` |
| Which warnings are firing? | `eshu_dp_tfstate_warnings_emitted_total{warning_kind}` |
| Are redaction paths changing? | `eshu_dp_tfstate_redactions_applied_total{reason}` |
| Are S3 conditional reads avoiding work? | `eshu_dp_tfstate_s3_conditional_get_not_modified_total` |
| Is provider-schema coverage loaded? | `eshu_dp_tfstate_schema_resolver_entries` |

Metric labels stay bounded. Bucket names, S3 keys, absolute paths, work-item
IDs, and raw state locators are deliberately excluded from metric labels. Use
safe locator hashes, traces, and structured logs when investigating one source.

## Trace Spans

The Terraform-state span family is named in `go/internal/telemetry/contract.go`:

- `tfstate.collector.claim.process`
- `tfstate.discovery.resolve`
- `tfstate.source.open`
- `tfstate.parser.stream`
- `tfstate.fact.emit_batch`
- `tfstate.coordinator.complete`

## Admin Status

The runtime uses the shared admin surface:

- `/healthz`
- `/readyz`
- `/metrics`
- `/admin/status?format=json`

Terraform-state instances appear in `collector_instances`. Terraform-state
snapshot status is keyed by safe locator hash and includes recent lineage,
serial, generation, and warning data. Recent warnings are capped at 50 rows per
safe locator hash. Postgres remains the source of truth for full history.

## Failure Triage

| Symptom | Likely cause | First check |
| --- | --- | --- |
| No candidates resolved | Missing claim-capable instance, graph discovery waiting on Git readiness, or filters that match no indexed backend facts. | `ESHU_COLLECTOR_INSTANCES_JSON`, workflow coordinator status, and `eshu_dp_tfstate_discovery_candidates_total`. |
| S3 access denied | Target-scope trust policy, external ID, bucket policy, or allowed-region mismatch. | `tfstate.source.open` span and structured logs with `failure_class`. |
| Missing S3 object warning | Stale graph-discovered backend declaration or deleted exact seed. | `terraform_state_warning{warning_kind=state_missing}` facts and discovery source. |
| DynamoDB lock read warning | Missing `dynamodb:GetItem`, wrong table name, or removed lock table. | Seed/backend `dynamodb_table` and warning facts. |
| Oversize state warning | State object exceeds `ESHU_TFSTATE_SOURCE_MAX_BYTES`. | `eshu_dp_tfstate_snapshots_observed_total{result="state_too_large"}` and state size histogram. |
| Conditional GET never short-circuits | Missing or stale prior ETag metadata. | Prior `terraform_state_snapshot` metadata and `eshu_dp_tfstate_s3_conditional_get_not_modified_total`. |
| Redaction reason surge | New provider shape or unsupported composite path. | `eshu_dp_tfstate_redactions_applied_total{reason}` and provider-schema coverage. |

## Operator Rules

- Fix discovery and credential scope before raising worker counts or widening
  IAM permissions.
- Treat `state_missing` as stale-source evidence, not as a retryable transport
  failure.
- Raise `ESHU_TFSTATE_SOURCE_MAX_BYTES` only after confirming the state size is
  expected.
- Investigate provider-schema gaps before assuming redaction is a parser bug.
- Confirm facts reached Postgres before debugging reducer or query surfaces.

## Related Docs

- [Terraform State Collector](collector-terraform-state.md)
- [Terraform State Collector Config](collector-terraform-state-config.md)
- [Ingestion And Collector Metrics](../reference/telemetry/metrics-ingestion-collectors.md)
- [Runtime Admin API](../reference/runtime-admin-api.md)
- [Collector And Reducer Readiness](../reference/collector-reducer-readiness.md)
