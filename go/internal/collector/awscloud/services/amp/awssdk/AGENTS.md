# AGENTS.md - internal/collector/awscloud/services/amp/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - AMP SDK pagination, scraper union decoding, safe metadata
   mapping, and telemetry.
3. `exclusion_test.go` - the build-time gate that fails if a body-read or
   mutation method reaches the adapter interface.
4. `../scanner.go` - scanner-owned AMP fact selection.
5. `../README.md` - AMP scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep AMP SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `apiClient` interface limited to `List*` reads. The exclusion test
  fails the build if any method is not a `List` read or matches a body/mutation
  name; do not loosen it. `ListRuleGroupsNamespaces` returns names only and is
  allowed; `DescribeRuleGroupsNamespace` (the rule body) is banned by the
  `Describe` substring guard.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe workspace, namespace (name), and scraper metadata plus
  resource tags. Never read or persist rule definitions, scrape configuration,
  alert-manager definitions, ingested samples, or query results.
- Decode the scraper `Source` union for EKS cluster ARN and EKS VPC ids only
  from the EKS configuration variant; a non-EKS source yields empty source
  fields. Decode the `Destination` union for the AMP workspace ARN.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new AMP metadata read by extending `Client` and the `apiClient`
  interface with another `List*` read, writing a scanner or adapter test first,
  then mapping the SDK response into scanner-owned types. The exclusion test
  rejects any non-`List` addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal rule, scrape-configuration, alert-manager, or ingested-sample content.

## What Not To Change Without An ADR

- Do not read rule definitions, scrape configuration, alert-manager
  definitions, ingested samples, or query results, and do not call any AMP
  mutation API.
- Do not add `Describe*` or `Get*` body reads to the adapter interface.
- Do not infer workload, environment, deployment, or ownership truth from AMP
  names, aliases, or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
