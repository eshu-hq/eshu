# AGENTS.md - cmd/collector-aws-cloud

Use `README.md` and `doc.go` for the command contract. This file keeps the
agent-only rules for process startup, config validation, and runtime wiring.

## Read First

1. `README.md`, `config.go`, `service.go`, and `status_committer.go`.
2. `go/internal/collector/awscloud/awsruntime/README.md` for claim runtime,
   credential, scanner registry, and pagination contracts.
3. Service and `awssdk` READMEs under `go/internal/collector/awscloud/services/`
   before changing scanner-specific behavior.
4. `docs/public/services/collector-aws-cloud.md`,
   `docs/public/services/collector-aws-cloud-security.md`, and
   `docs/public/services/collector-aws-cloud-scanners.md`.

## Mandatory Invariants

- This command is process wiring. It does not own AWS service scanner behavior,
  SDK pagination, workflow storage, graph writes, reducer admission, or
  workload ownership inference.
- Reject static AWS credential fields before any claim can run.
- `central_assume_role` requires `role_arn` and `external_id`; the role ARN
  account must match the target account.
- `local_workload_identity` must not set `role_arn` or `external_id`.
- Reject wildcard regions and services. `allowed_services` must name scanner
  families backed by `awsruntime.SupportsServiceKind`.
- Require `ESHU_AWS_REDACTION_KEY` when ECS or Lambda is enabled.
- Keep AWS credentials in `awsruntime`; keep SDK pagination in service
  `awssdk` adapters.
- Scanner-specific forbidden data classes live in scanner docs and service
  READMEs. Do not duplicate or widen them here.
- Keep scanner-side status in `awsruntime` distinct from commit-side status in
  `status_committer.go`.
- Do not log credential values, policy JSON, payloads, resource names, ARNs,
  tags, or raw AWS errors as metric labels.

## Change Routing

- New service: extend target validation, add scanner tests, add a service
  `awssdk` adapter, package docs, and update `awsruntime.DefaultScannerFactory`
  and supported-service tests together.
- New command config: add config tests first, then update public collector and
  environment docs.
- Lease, claim concurrency, batching, pagination, or downstream pressure
  changes require tracked Performance Evidence and Observability Evidence.
- SDK pagination changes belong in the service adapter so spans and AWS API
  counters stay complete.

## Do Not Change Without Architecture-Owner Approval

- Do not bypass workflow claims or claim-aware commits.
- Do not cache cross-account credentials beyond the claim lease.
- Do not make this command write graph truth or reducer-owned rows directly.

## Required Proof

- Run `cd go && go test ./cmd/collector-aws-cloud -count=1`.
- Run `cd go && go test ./internal/collector/awscloud/awsruntime -count=1`
  when runtime validation or scanner registry behavior changes.
- For docs-only edits, run `go run ./cmd/eshu docs verify ../go/cmd/collector-aws-cloud --fail-on contradicted,missing_evidence` from `go/`.
