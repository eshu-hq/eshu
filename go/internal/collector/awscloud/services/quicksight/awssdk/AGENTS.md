# AGENTS.md - internal/collector/awscloud/services/quicksight/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - QuickSight SDK pagination, account-id threading, and telemetry.
3. `mappers.go` - safe metadata mapping and backing-store/edge extraction.
4. `errors.go` - throttle, not-subscribed, and access-denied classification.
5. `exclusion_test.go` - the build-time gate that fails if a mutation, secret,
   credential, permission, embed, or ingestion method reaches the adapter.
6. `../scanner.go` - scanner-owned QuickSight fact selection.
7. `../README.md` - QuickSight scanner contract.
8. `../../../README.md` - AWS cloud envelope contract.
9. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep QuickSight SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `List*` and `Describe*` reads. The
  exclusion test fails the build if any method is not a List/Describe read or
  matches a mutation, credential, secret, permission, embed, or ingestion name;
  do not loosen it.
- Thread `boundary.AccountID` into every account-scoped QuickSight call. Fail
  fast when it is empty rather than calling AWS with a nil account id.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only safe resource metadata plus resource tags. Never read or persist
  credentials, connection secrets, custom-SQL query bodies, dataset column data,
  row-level security values, or visual definitions. `backingStore` reads only
  bare identifiers and the S3 manifest bucket name.
- Map only the documented not-subscribed message to an empty snapshot; surface
  every other authorization failure.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new QuickSight metadata read by extending `Client` and the `apiClient`
  interface with another `List*`/`Describe*` read, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types. The
  exclusion test rejects any mutation/secret/permission addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal credential, secret, SQL, or visual-definition content.

## What Not To Change Without An ADR

- Do not read credentials, secrets, SQL bodies, visual definitions, or
  permissions, and do not call any QuickSight mutation, ingestion, refresh, or
  embed API.
- Do not infer workload, environment, deployment, or ownership truth from
  QuickSight names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
