# AGENTS.md - internal/collector/awscloud/services/iam/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - IAM SDK pagination, trust policy decoding, OIDC provider
   metadata fingerprinting, and telemetry.
3. `policy_documents.go` - user reads and bounded policy-document fan-out.
4. `policy_normalize.go` - metadata-only policy-statement normalization.
5. `../scanner.go` - scanner-owned IAM fact selection.
6. `../README.md` - IAM scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep IAM SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page and policy-document read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Decode IAM trust policy JSON before returning scanner-owned role records.
- Return OIDC provider URL fingerprints and client ID/thumbprint counts only;
  never return raw OIDC URLs, client IDs, or thumbprints.
- Normalize policy documents to metadata-only `iam.PolicyStatement` values. Keep
  effect, action/resource patterns, statement SID, condition KEYS, and trust
  assume-principals. NEVER return the raw policy JSON body or condition values.
- Bound the per-principal managed policy document fan-out
  (`maxPolicyDocumentsPerPrincipal`); do not loop one `GetPolicy` +
  `GetPolicyVersion` pair per attachment without a cap.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new IAM API read by extending `iam.Client`, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend role mapping in `mapRole` (or user mapping in `mapUser`) when AWS source
  data needs to become scanner-owned evidence.
- Extend policy-statement normalization in `policy_normalize.go`, keeping it
  metadata-only, and keep the per-principal document fan-out bounded in
  `policy_documents.go`.

## What Not To Change Without An ADR

- Do not infer workload, environment, deployment, or ownership truth from IAM
  names, paths, tags, or policy text.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
