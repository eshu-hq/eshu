# AGENTS.md - cmd/collector-aws-cloud

Read `README.md`, `doc.go`, `config.go`, `service.go`,
`status_committer.go`, `go/internal/collector/awscloud/awsruntime/README.md`,
and the public AWS collector docs before editing this command.

## Mandatory Rules

- Keep this package limited to process startup, config validation, hosted
  runtime wiring, status commit wrapping, and claim-runner construction.
- Do not move scanner behavior, SDK pagination, workflow storage, graph writes,
  reducer admission, credentials, or workload inference into this command.
- Reject static AWS credential fields, wildcard regions, wildcard services, and
  `allowed_services` not backed by `awsruntime.SupportsServiceKind`.
- Require `role_arn` plus `external_id` for `central_assume_role`; require the
  role account to match the target account.
- Reject `role_arn` and `external_id` for `local_workload_identity`.
- Require `ESHU_AWS_REDACTION_KEY` when an enabled target includes ECS or
  Lambda.
- Keep scanner status from `awsruntime` separate from commit status in
  `status_committer.go`.
- Keep credential values, policy JSON, payloads, resource names, ARNs, tags,
  raw AWS errors, and page tokens out of metric labels and logs.
- Add config tests before changing env parsing. Add registry, scanner, adapter,
  docs, and supported-service tests together for new service kinds.

## Proof

- Run `cd go && go test ./cmd/collector-aws-cloud -count=1` for command edits.
- Also run `cd go && go test ./internal/collector/awscloud/awsruntime -count=1`
  when validation, claim runtime, or scanner registry behavior changes.
