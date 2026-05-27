# AGENTS.md - internal/collector/awscloud/services/accessanalyzer guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Access Analyzer domain types.
3. `scanner.go` - analyzer, archive-rule, finding-count, unused-access, and
   relationship emission.
4. `awssdk/README.md` - AWS SDK pagination, safe mapping, and telemetry.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep Access Analyzer API access behind `Client`; do not import the AWS SDK
  into this package.
- Never call mutation APIs such as CreateAnalyzer, DeleteAnalyzer,
  CreateArchiveRule, DeleteArchiveRule, UpdateArchiveRule, StartResourceScan,
  ApplyArchiveRule, UpdateFindings, CancelPolicyGeneration, or
  StartPolicyGeneration.
- Never persist external finding bodies: principal maps, action lists,
  conditions, resource policy excerpts, or sources.
- Never persist archive-rule filters unless a future security review adds an
  explicit opt-in contract.
- Never persist policy-generation results.
- Never persist per-action unused-access detail. Use only the per-resource
  last-accessed aggregate timestamp.
- Emit reported evidence only. Do not infer organization ownership, workload,
  repository ownership, or deployable-unit truth from analyzer names, tags, or
  findings.
- Keep analyzer ARNs, resource ARNs, finding IDs, tags, and raw AWS errors out
  of metric labels.

## Common Changes

- Add a new Access Analyzer metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when AWS reports both sides directly or
  the identity can be derived from an AWS-reported ARN without reading sensitive
  finding bodies.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not persist finding bodies, archive filters, policy generation output, or
  unused-action breakdowns.
- Do not resolve analyzer names, tags, findings, or resource identifiers into
  workload ownership here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
