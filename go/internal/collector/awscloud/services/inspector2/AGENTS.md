# AGENTS.md - internal/collector/awscloud/services/inspector2 guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Inspector v2 domain types.
3. `scanner.go` and `relationships.go` - resource and relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud.md` - AWS collector runtime
   requirements.
6. `docs/public/services/collector-aws-cloud-scanners.md` - scanner coverage
   and data boundary.

## Invariants

- Keep Inspector v2 API access behind `Client`; do not import the AWS SDK into
  this package.
- Emit reported evidence only. Do not infer environment, deployment, workload,
  ownership, attacker identity, or defender posture truth from Inspector v2
  names, tags, accounts, or feature status.
- Never persist finding details. A CVE plus package version plus affected host
  ARN reveals exploitation surface. The scanner makes no finding-listing or
  finding-aggregation call at all.
- Never persist filter criteria expressions, descriptions, or reasons. Filter
  facts are name-only.
- CIS scan configuration facts are metadata only. Do not persist scan results
  or per-check details. The target account set is allowed as relationship
  evidence.
- A standalone (non-administrator) account emits no member relationships.
- Preserve stable account, member, filter, and CIS configuration identities
  across repeated observations in the same AWS generation.
- Keep account IDs, filter ARNs, CIS configuration ARNs, and tags out of metric
  labels.

## Common Changes

- Add a new safe Inspector v2 metadata field by extending the scanner-owned
  type, writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when Inspector v2 directly reports both
  sides and the value is metadata.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not call ListFindings, GetFindings, ListFindingAggregations,
  BatchGetFindingDetails, BatchGetCodeSnippet, GetSbomExport, GetFilter, CIS
  scan-result reads, or any mutation API.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
