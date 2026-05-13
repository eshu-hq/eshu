# AGENTS.md - cmd/collector-aws-cloud guidance

## Read First

1. `README.md` - command purpose, configuration, and invariants.
2. `config.go` - collector instance selection and target-scope parsing.
3. `service.go` - claim-aware runner wiring and scanner factory.
4. `credentials.go` - AWS SDK config, STS AssumeRole, and lease release.
5. `iam_client.go` - IAM SDK pagination adapter.
6. `go/internal/collector/awscloud/awsruntime/README.md` - claim runtime
   contract.
7. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - security and
   runtime requirements.

## Invariants

- Do not accept static AWS credential fields.
- Keep AWS SDK calls in this command or provider adapters, not in scanner-owned
  service packages.
- Preserve `aws.RetryModeAdaptive` on every loaded AWS SDK config.
- Pass STS external ID when configured.
- Release credential leases on all scan outcomes.
- Do not log credential values, trust policy JSON, resource ARNs, tags, or raw
  source payloads as metric labels.

## Common Changes

- Add a new AWS service by extending target validation, adding scanner package
  tests, and branching in `scannerFactory.Scanner`.
- Add new command configuration with config tests first.
- Add SDK pagination by wrapping each page in `recordAPICall` so spans and AWS
  API counters stay complete.

## What Not To Change Without An ADR

- Do not bypass workflow claims or claim-aware commits.
- Do not cache cross-account credentials beyond the claim lease.
- Do not make this command write graph truth or reducer-owned rows directly.
