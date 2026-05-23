# AGENTS.md - internal/collector/awscloud/awsruntime

Read `README.md`, `doc.go`, `types.go`, `credentials.go`, `registry.go`,
`source.go`, `scan_status.go`, `../checkpoint/README.md`, and relevant service
READMEs before changing claim execution.

## Mandatory Rules

- Authorize the exact `(account_id, region, service_kind)` target before
  acquiring credentials.
- Keep static AWS credentials out of runtime config and production code.
- Preserve `aws.RetryModeAdaptive` on loaded AWS SDK configs.
- Require same-account role routing plus external ID for `central_assume_role`;
  reject AssumeRole fields for local workload identity.
- Copy `CurrentFencingToken` into every AWS boundary and warning fact.
- Expire stale pagination checkpoints before scanner construction.
- Release credential leases even when scanner construction or scanning fails.
- Keep scanner status separate from durable fact commit status.
- Add services through `DefaultScannerFactory`, `SupportedServiceKinds`, command
  validation, scanner packages, SDK adapters, docs, and registry tests together.
- Keep scanner allowlists metadata-only unless the service README and public
  scanner doc approve a sanitized exception.
- Keep ARNs, names, tags, policy JSON, payloads, page tokens, raw AWS errors,
  and credential material out of metric labels and status rows.
- Do not bypass workflow claims, claim fencing, or claim-scoped credentials.
- Do not infer environment, workload, ownership, or deployable-unit truth here.

## Proof

- Run `cd go && go test ./internal/collector/awscloud/awsruntime -count=1`.
- Also run `cd go && go test ./cmd/collector-aws-cloud -count=1` when command
  validation or runtime config changes.
