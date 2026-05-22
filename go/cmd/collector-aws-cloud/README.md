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

This is a `package main` binary. Its public contract is the process entrypoint,
`--version` / `-v`, `ESHU_COLLECTOR_INSTANCES_JSON`,
`ESHU_AWS_COLLECTOR_INSTANCE_ID`, `ESHU_AWS_COLLECTOR_OWNER_ID`,
`ESHU_AWS_COLLECTOR_POLL_INTERVAL`, `ESHU_AWS_COLLECTOR_CLAIM_LEASE_TTL`,
`ESHU_AWS_COLLECTOR_HEARTBEAT_INTERVAL`, `ESHU_AWS_REDACTION_KEY`, and shared
Postgres, OTEL, metrics, and `ESHU_PPROF_ADDR` runtime env.

The selected instance must be enabled, use `collector_kind="aws"`, and set
`claims_enabled=true`. Target scopes must name a 12-digit account, concrete
regions, concrete service kinds supported by `awsruntime.SupportsServiceKind`,
and either `central_assume_role` or `local_workload_identity` credentials.

## Dependencies

- `internal/app` for the hosted service and status server.
- `internal/collector` for the claim-aware runner.
- `internal/collector/awscloud/awsruntime` for claim validation, target
  authorization, credentials, scanner registry, pagination checkpoints, and
  collected generations.
- `internal/storage/postgres` for workflow claims, ingestion commits, scan
  status, and status reports.
- `internal/telemetry` for metrics, traces, logs, and Prometheus wiring.

## Telemetry

The command registers shared data-plane instruments plus AWS claim, scan,
API-call, throttle, AssumeRole, pagination-checkpoint, resource, relationship,
and tag-observation signals. The claim runner uses the AWS collector span family
(`aws.collector.claim.process`, `aws.credentials.assume_role`,
`aws.service.scan`, and `aws.service.pagination.page`). The hosted runtime
mounts `/healthz`, `/readyz`, `/metrics`, and `/admin/status`.

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
- Scanner-specific data boundaries belong in
  `docs/public/services/collector-aws-cloud-scanners.md` and the service
  package READMEs, not this command README.

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
