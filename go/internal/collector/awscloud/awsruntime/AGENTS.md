# AGENTS.md - internal/collector/awscloud/awsruntime guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - target, credential, scanner, and config contracts.
3. `credentials.go` - AWS SDK config, STS AssumeRole, and lease release.
4. `registry.go` - production service scanner registry.
5. `source.go` - claim validation, target authorization, checkpoint expiry, and generation
   construction.
6. `scan_status.go` - scanner-side durable status projection.
7. `../checkpoint/README.md` - durable pagination checkpoint contract.
8. `../README.md` - shared AWS fact-envelope contract.
9. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - runtime and
   credential requirements.

## Invariants

- Authorize `(account_id, region, service_kind)` before acquiring credentials.
- Keep static AWS credentials out of this package and out of tests.
- Preserve `aws.RetryModeAdaptive` on every loaded AWS SDK config.
- Pass STS external ID when configured.
- Preserve claim fencing by copying `CurrentFencingToken` into every AWS
  boundary and warning fact.
- Expire pagination checkpoints for prior generations before building service
  scanners.
- Record AWS scan status after claim start and after scanner completion when a
  scan-status store is configured. Scanner status is not the same as durable
  fact commit status.
- Release credential leases even when scanner construction or service scanning
  fails.
- Keep resource ARNs, policy JSON, tags, account names, and raw error payloads
  out of metric labels.
- Keep ECR lifecycle policy JSON and image digests out of metric labels.
- Keep ECS task-definition environment values out of persisted payloads unless
  they are replaced by `internal/redact` markers.
- Keep ELBv2 target health out of service scans; it is live/noisy status, not
  stable routing topology.
- Keep Route 53 DNS names, hosted-zone IDs, and record values out of metric
  labels.
- Keep EC2 instance inventory out of EC2 service scans; collect ENI attachment
  metadata only.

## Common Changes

- Add a new credential mode by extending `CredentialMode`, writing focused
  claim tests, and implementing the provider here.
- Add a new service scanner by adding a service constant in `awscloud`, scanner
  package tests, a service `awssdk` adapter, package docs, and a
  `DefaultScannerFactory.Scanner` branch.
- Change claim shape only with coordinator, workflow, and ADR updates in the
  same PR.

## What Not To Change Without An ADR

- Do not bypass workflow claims or claim fencing.
- Do not cache cross-account credentials beyond a claim lease.
- Do not infer environment, workload, or ownership truth in the runtime.
