# AGENTS.md - internal/collector/awscloud/services/dms/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - DMS SDK pagination and telemetry.
3. `mapping.go` - safe SDK-to-scanner metadata mapping, including the
   engine-specific secret extraction.
4. `exclusion_test.go` - the build-time gate that fails if a data-plane,
   connection-test, schema-refresh, or mutation method reaches the adapter
   interface.
5. `../scanner.go` - scanner-owned DMS fact selection.
6. `../README.md` - DMS scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep DMS SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `apiClient` interface limited to `Describe*` and `List*` reads. The
  exclusion test fails the build if any method is not a Describe or List read or
  matches a data-plane/mutation name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Page every describe stream with the `Marker` token until it is empty.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe instance, subnet group, endpoint, and task metadata plus
  resource tags. Never read or persist migrated rows, secret values, server
  names used as credentials, usernames, passwords, connection attributes,
  external table definitions, SSL key material, task settings, or table-mapping
  bodies.
- Read endpoint data-store and secret references from the populated engine
  settings struct only; record the S3 bucket name, Kinesis stream ARN, and
  Secrets Manager secret id and nothing else from those structs.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new DMS metadata read by extending `Client` and the `apiClient`
  interface with another `Describe*` or `List*` read, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types.
  The exclusion test rejects any non-read addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal a credential, secret value, server name, connection attribute, SSL key,
  external table definition, task setting, or table-mapping body.

## What Not To Change Without An ADR

- Do not test connections, refresh schemas, reload tables, read table
  statistics, start/stop tasks, run assessments, or call any DMS mutation API.
- Do not persist endpoint credentials, secret values, server names, connection
  attributes, SSL key material, external table definitions, task settings, or
  table-mapping bodies.
- Do not infer workload, environment, deployment, or ownership truth from DMS
  names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
