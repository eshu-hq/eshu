# AGENTS.md - internal/collector/awscloud/services/docdbelastic/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - DocumentDB Elastic SDK pagination, safe metadata mapping, and
   telemetry.
3. `exclusion_test.go` - the build-time gate that fails if a document-read,
   credential-read, or mutation method reaches the adapter interface.
4. `../scanner.go` - scanner-owned DocumentDB Elastic fact selection.
5. `../README.md` - DocumentDB Elastic scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep DocumentDB Elastic SDK calls here, not in `cmd/collector-aws-cloud` or
  the scanner package.
- Keep the `apiClient` interface limited to `List*` and `Get*` reads. The
  exclusion test fails the build if any method is not a `List`/`Get` read or
  matches a document/credential/mutation name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe cluster metadata plus resource tags. Never read or persist
  document, collection, index, query-result, endpoint, admin-user-name, or
  password content.
- Capture the admin-secret ARN only for `SECRET_ARN` auth and only when it is
  ARN-shaped. Drop the admin user name under `PLAIN_TEXT` auth.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new DocumentDB Elastic metadata read by extending `Client` and the
  `apiClient` interface with another `List*`/`Get*` read, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types.
  The exclusion test rejects any non-`List`/`Get` addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal document, credential, endpoint, or password content.

## What Not To Change Without An ADR

- Do not read documents, snapshots, or query results, read the admin password
  or user name, read the cluster endpoint connection string, or call any
  DocumentDB Elastic mutation API.
- Do not infer workload, environment, deployment, or ownership truth from
  DocumentDB Elastic names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
