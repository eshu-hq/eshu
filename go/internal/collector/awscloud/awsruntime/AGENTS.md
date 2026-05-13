# AGENTS.md - internal/collector/awscloud/awsruntime guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - target, credential, scanner, and config contracts.
3. `credentials.go` - AWS SDK config, STS AssumeRole, and lease release.
4. `registry.go` - production service scanner registry.
5. `source.go` - claim validation, target authorization, and generation
   construction.
6. `../README.md` - shared AWS fact-envelope contract.
7. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - runtime and
   credential requirements.

## Invariants

- Authorize `(account_id, region, service_kind)` before acquiring credentials.
- Keep static AWS credentials out of this package and out of tests.
- Preserve `aws.RetryModeAdaptive` on every loaded AWS SDK config.
- Pass STS external ID when configured.
- Preserve claim fencing by copying `CurrentFencingToken` into every AWS
  boundary and warning fact.
- Release credential leases even when scanner construction or service scanning
  fails.
- Keep resource ARNs, policy JSON, tags, account names, and raw error payloads
  out of metric labels.

## Common Changes

- Add a new credential mode by extending `CredentialMode`, writing focused
  claim tests, and implementing the provider here.
- Add a new service scanner by adding a service constant in `awscloud`, scanner
  package tests, a service `awssdk` adapter, and a
  `DefaultScannerFactory.Scanner` branch.
- Change claim shape only with coordinator, workflow, and ADR updates in the
  same PR.

## What Not To Change Without An ADR

- Do not bypass workflow claims or claim fencing.
- Do not cache cross-account credentials beyond a claim lease.
- Do not infer environment, workload, or ownership truth in the runtime.
