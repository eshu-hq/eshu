# AWS Cloud Collector Command

## Purpose

`cmd/collector-aws-cloud` builds the `eshu-collector-aws-cloud` process. The
command loads one claim-capable AWS collector instance, opens the shared
Postgres runtime, wires the claim runner, starts the hosted admin/status server,
and exits on `SIGINT` or `SIGTERM`.

## Ownership boundary

This command owns process startup, environment parsing, telemetry registration,
Postgres wiring, and claim runner construction. It does not own AWS service
scanner behavior, AWS SDK pagination, workflow claim storage, graph writes,
reducer admission, or workload ownership inference.

## Exported surface

This `package main` binary exposes the process entrypoint, `--version` / `-v`,
`ESHU_COLLECTOR_INSTANCES_JSON`, AWS collector env, shared Postgres/OTEL/metrics
env, and `ESHU_PPROF_ADDR`. See `doc.go` and config tests for the exact env
contract.

The selected instance must be enabled, use `collector_kind="aws"`, set
`claims_enabled=true`, and authorize concrete `(account, region, service_kind)`
targets through either `central_assume_role` or `local_workload_identity`.

## Dependencies

- `internal/app` for the hosted service and status server.
- `internal/collector` for the claim-aware runner.
- `internal/collector/awscloud/awsruntime` for claim validation, target
  authorization, credentials, scanners, checkpoints, and collected generations.
- `internal/storage/postgres` for workflow claims, ingestion commits, scan
  status, and status reports.
- `internal/telemetry` for metrics, traces, logs, and Prometheus wiring.

## Telemetry

The command registers shared data-plane instruments plus AWS claim, scan,
API-call, throttle, AssumeRole, checkpoint, resource, relationship, and tag
signals. The hosted runtime mounts `/healthz`, `/readyz`, `/metrics`, and
`/admin/status`.

## Gotchas / invariants

- Static AWS access-key fields are rejected before any claim can run.
- `central_assume_role` requires `role_arn` and `external_id`; the role ARN
  account must match the target `account_id`.
- `local_workload_identity` must not set `role_arn` or `external_id`.
- Wildcard regions and services are rejected.
- ECS and Lambda target scopes require `ESHU_AWS_REDACTION_KEY`; IAM and ECR do
  not.
- The acceptance unit ID must be JSON with `account_id`, `region`, and
  `service_kind`.
- `/admin/status` separates scanner status from durable commit status. A
  succeeded scan with failed commit is a persistence problem.
- Scanner-specific data boundaries belong in the public scanner reference and
  service package READMEs, not this command README.

## Focused tests

```bash
cd go
go test ./cmd/collector-aws-cloud -count=1
go test ./internal/collector/awscloud/awsruntime \
  -run 'TestClaimedSourceRecordsEmissionCounters|TestClaimedSourceRecordsScanStatusWithAPICallStats' \
  -count=1 -v
```

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/services/collector-aws-cloud-security.md`
- `docs/public/services/collector-aws-cloud-scanners.md`
- `docs/public/reference/telemetry/index.md`
