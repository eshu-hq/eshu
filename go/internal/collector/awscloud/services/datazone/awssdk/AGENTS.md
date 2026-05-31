# AGENTS.md - internal/collector/awscloud/services/datazone/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - the `apiClient` read interface, Snapshot orchestration, tag
   reads, and telemetry.
3. `mapper.go` - domain/project/environment pagination and safe metadata
   mapping.
4. `datasource.go` - data source pagination plus GetDataSource backing-store
   extraction.
5. `exclusion_test.go` - the build-time gate that fails if a content-read or
   mutation method reaches the adapter interface.
6. `../scanner.go` - scanner-owned DataZone fact selection.
7. `../README.md` - DataZone scanner contract.
8. `../../../README.md` - AWS cloud envelope contract.
9. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep DataZone SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `apiClient` interface limited to `List*` reads plus the two allowed
  describe reads `GetDomain` and `GetDataSource`. The exclusion test fails the
  build if any method matches a content/mutation name or is a `Get` outside the
  allowed set; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only safe domain/project/environment/data-source metadata plus
  resource tags and the resolvable backing-store names. Never read or persist
  glossary, glossary-term, asset, listing, subscription, time-series, lineage,
  relational filter expression, or access credential content.
- From `GetDataSource`, copy only the parent project id and the backing-store
  names; never the relational filter expressions or credentials.
- Do not copy Redshift Serverless workgroup names into an edge: the published
  workgroup ARN is not synthesizable from the name, so skip rather than dangle.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new DataZone metadata read by extending `Client` and the `apiClient`
  interface with another `List*` read, writing a scanner or adapter test first,
  then mapping the SDK response into scanner-owned types. The exclusion test
  rejects any non-`List` addition outside the allowed describe set.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal governed content.

## What Not To Change Without An ADR

- Do not read glossaries, glossary terms, assets, asset content, listings,
  subscriptions, time-series data, or lineage, and do not call any DataZone
  mutation API.
- Do not widen the allowed `Get` describe set beyond `GetDomain` and
  `GetDataSource` without proving the new read carries no governed content.
- Do not infer workload, environment, deployment, or ownership truth from
  DataZone names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
