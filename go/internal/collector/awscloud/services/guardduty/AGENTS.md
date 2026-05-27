# AGENTS.md - internal/collector/awscloud/services/guardduty guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned GuardDuty domain types.
3. `scanner.go` and `relationships.go` - resource and relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud.md` - AWS collector runtime
   requirements.
6. `docs/public/services/collector-aws-cloud-scanners.md` - scanner coverage
   and data boundary.

## Invariants

- Keep GuardDuty API access behind `Client`; do not import the AWS SDK into
  this package.
- Emit reported evidence only. Do not infer environment, deployment, workload,
  ownership, attacker identity, or deployable-unit truth from GuardDuty names,
  tags, accounts, or finding counts.
- Never persist finding bodies, Service.Action content,
  Resource.InstanceDetails, AccessKeyDetails, S3BucketDetails, network
  interfaces, remote/local IP details, port details, or process trees.
- Never persist filter criteria expressions. Filter facts are name-only.
- Never fetch or persist threat intel set or IP set list contents. Location ARN
  metadata is allowed; list entries are not.
- Preserve stable detector and detector-child identities across repeated
  observations in the same AWS generation.
- Keep detector IDs, account IDs, set IDs, list locations, destination ARNs,
  tags, and finding types out of metric labels.

## Common Changes

- Add a new safe GuardDuty metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when GuardDuty directly reports both sides
  and the value is metadata.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not call GetFindings, ListFindings, GetFilter, mutation APIs, or S3
  APIs that fetch threat intel/IP set contents.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
