# AGENTS.md - cmd/collector-aws-cloud guidance

## Read First

1. `README.md` - command purpose, configuration, and invariants.
2. `config.go` - collector instance selection and target-scope parsing.
3. `service.go` - claim-aware runner and runtime wiring.
4. `go/internal/collector/awscloud/awsruntime/README.md` - claim runtime
   contract.
5. `go/internal/collector/awscloud/services/iam/awssdk/README.md` - IAM SDK
   adapter contract.
6. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - security and
   runtime requirements.

## Invariants

- Do not accept static AWS credential fields.
- Keep this command process-only. AWS credentials belong in `awsruntime`; AWS
  service pagination belongs in service `awssdk` adapters.
- Do not log credential values, trust policy JSON, resource ARNs, tags, or raw
  source payloads as metric labels.

## Common Changes

- Add a new AWS service by extending target validation, adding scanner package
  tests, adding a service `awssdk` adapter, and branching in
  `awsruntime.DefaultScannerFactory.Scanner`.
- Add new command configuration with config tests first.
- Add SDK pagination in the service adapter so spans and AWS API counters stay
  complete.

## What Not To Change Without An ADR

- Do not bypass workflow claims or claim-aware commits.
- Do not cache cross-account credentials beyond the claim lease.
- Do not make this command write graph truth or reducer-owned rows directly.
