# Terraform State Collector Operations

Use this page to operate `collector-terraform-state`: metrics, status, and
failure triage. Config lives in
[Terraform State Collector Config](collector-terraform-state-config.md).

## Dashboard Starting Points

| Question | Start with |
| --- | --- |
| Are claims waiting? | `eshu_dp_tfstate_claim_wait_seconds` |
| Are candidates resolving? | `eshu_dp_tfstate_discovery_candidates_total{source}` |
| Are snapshots observed? | `eshu_dp_tfstate_snapshots_observed_total{backend_kind,result}` |
| How large are state sources? | `eshu_dp_tfstate_snapshot_bytes` |
| Is parsing slow? | `eshu_dp_tfstate_parse_duration_seconds` |
| Are facts emitted? | `eshu_dp_tfstate_resources_emitted_total`, `eshu_dp_tfstate_outputs_emitted_total`, `eshu_dp_tfstate_modules_emitted_total` |
| Which warnings fire? | `eshu_dp_tfstate_warnings_emitted_total{warning_kind}` |
| Is redaction changing? | `eshu_dp_tfstate_redactions_applied_total{reason}` |
| Are S3 conditionals working? | `eshu_dp_tfstate_s3_conditional_get_not_modified_total` |
| Is provider-schema coverage loaded? | `eshu_dp_tfstate_schema_resolver_entries` |

Bucket names, S3 keys, absolute paths, work-item IDs, and raw state locators are
excluded from metric labels. Use safe locator hashes, traces, and structured
logs for one-source investigations.

## Admin Status And Spans

The runtime exposes `/healthz`, `/readyz`, `/metrics`, and
`/admin/status?format=json`.

Terraform-state instances appear in `collector_instances`. Snapshot status is
keyed by safe locator hash and includes recent lineage, serial, generation, and
warning data. Recent warnings are capped at 50 rows per safe locator hash;
Postgres remains the source of truth for full history.

Trace spans: `tfstate.collector.claim.process`, `tfstate.discovery.resolve`,
`tfstate.source.open`, `tfstate.parser.stream`, `tfstate.fact.emit_batch`, and
`tfstate.coordinator.complete`.

## Failure Triage

| Symptom | First check |
| --- | --- |
| No candidates resolved | `ESHU_COLLECTOR_INSTANCES_JSON`, workflow coordinator status, discovery candidate metric. |
| S3 access denied | Target trust policy, external ID, bucket policy, allowed region, `tfstate.source.open` span. |
| Missing S3 object | Stale backend declaration or deleted exact seed; inspect warning facts. |
| DynamoDB warning | `dynamodb:GetItem`, table name, or removed lock table. |
| Oversize state | `ESHU_TFSTATE_SOURCE_MAX_BYTES` and snapshot size histogram. |
| Conditional GET never short-circuits | Prior snapshot ETag metadata and conditional-read metric. |
| Redaction surge | Provider-schema coverage and redaction reason metric. |

Fix discovery and credential scope before raising worker counts or widening IAM.
Confirm facts reached Postgres before debugging reducer or query surfaces.

## Related Docs

- [Terraform State Collector](collector-terraform-state.md)
- [Terraform State Collector Config](collector-terraform-state-config.md)
- [Ingestion And Collector Metrics](../reference/telemetry/metrics-ingestion-collectors.md)
- [Runtime Admin API](../reference/runtime-admin-api.md)
