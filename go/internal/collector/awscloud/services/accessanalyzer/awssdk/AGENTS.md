# AGENTS.md - internal/collector/awscloud/services/accessanalyzer/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Access Analyzer SDK pagination, safe mapping, and telemetry.
3. `../scanner.go` - scanner-owned Access Analyzer fact selection.
4. `../README.md` - Access Analyzer scanner contract.
5. `../../../README.md` - AWS cloud envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep Access Analyzer SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap every AWS page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only analyzer metadata, archive-rule names, analyzer bindings,
  aggregate finding counts, and per-resource unused-access last-accessed
  timestamps.
- Do not persist external finding bodies, archive-rule filter criteria,
  policy-generation output, or unused-action breakdowns.
- Do not call GetFinding for external finding bodies.
- Do not call mutation APIs such as CreateAnalyzer, DeleteAnalyzer,
  CreateArchiveRule, DeleteArchiveRule, UpdateArchiveRule, StartResourceScan,
  ApplyArchiveRule, UpdateFindings, CancelPolicyGeneration, or
  StartPolicyGeneration.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Access Analyzer metadata read by extending `Client`, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend finding mapping only for AWS source data that is aggregate metadata and
  does not reveal principals, actions, policy excerpts, archive criteria, or
  per-action unused access.

## What Not To Change Without An ADR

- Do not mutate analyzers, archive rules, findings, resource scans, or policy
  generation jobs.
- Do not infer workload, environment, deployment, or ownership truth from
  analyzer names, tags, findings, or last-accessed timestamps.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
