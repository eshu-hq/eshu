# AGENTS.md - internal/collector/awscloud/services/auditmanager/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Audit Manager pagination, the account-status gate, and
   telemetry.
3. `mappers.go` - safe SDK-to-scanner metadata mapping.
4. `errors.go` - throttle, not-registered, and error-class classification.
5. `exclusion_test.go` - the build-time gate that fails if an evidence,
   narrative, report-URL, or mutation method reaches the adapter interface.
6. `../scanner.go` - scanner-owned Audit Manager fact selection.
7. `../README.md` - Audit Manager scanner contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Audit Manager SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `List*`/`Get*` metadata reads. The
  exclusion test fails the build if any method is not a `List`/`Get` read or
  matches an evidence/narrative/report-URL/mutation name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only safe assessment, framework, and control metadata plus resource
  tags and the account settings KMS key. Never read or persist evidence,
  evidence folders, change logs, delegation comments, control narratives, or
  report URLs.
- Gate the scan on `GetAccountStatus`; an unregistered account is a clean empty
  scan with a warning, not a failure.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Audit Manager metadata read by extending `Client` and the
  `apiClient` interface with another `List*`/`Get*` metadata read, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types. The exclusion test rejects any evidence/narrative/
  mutation addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not read evidence, evidence folders, change logs, delegations, insights,
  control narratives, or report URLs, and do not call any Audit Manager mutation
  API.
- Do not infer workload, environment, deployment, or ownership truth from Audit
  Manager names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
