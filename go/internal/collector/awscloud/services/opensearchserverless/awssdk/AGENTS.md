# AGENTS.md - internal/collector/awscloud/services/opensearchserverless/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - OpenSearch Serverless SDK pagination and the read interface.
3. `mapper.go` - safe metadata mapping, encryption-policy-body projection, and
   telemetry.
4. `exclusion_test.go` - the build-time gate that fails if a data-plane or
   mutation method reaches the adapter interface.
5. `../scanner.go` - scanner-owned OpenSearch Serverless fact selection.
6. `../README.md` - OpenSearch Serverless scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.

## Invariants

- Keep OpenSearch Serverless SDK calls here, not in `cmd/collector-aws-cloud` or
  the scanner package.
- Keep the `apiClient` interface limited to `List*`, `BatchGet*`, and `Get*`
  reads. The exclusion test fails the build if any method is not a read or
  matches a data-plane (Index, Search, Bulk, Document, Query) or mutation name;
  do not loosen it.
- Never construct the collection HTTP endpoint or reach the OpenSearch data
  plane.
- Parse the encryption-policy body only for `KmsARN` and collection patterns,
  then discard it. Never return or persist the raw policy document. AWS-owned-key
  policies produce no binding.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only safe collection, policy, and endpoint metadata plus resource tags.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new OpenSearch Serverless metadata read by extending `Client` and the
  `apiClient` interface with another `List*`/`BatchGet*`/`Get*` read, writing a
  scanner or adapter test first, then mapping the SDK response into scanner-owned
  types. The exclusion test rejects any non-read or data-plane/mutation addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal data-plane content or a policy document body.

## What Not To Change Without An ADR

- Do not reach the OpenSearch HTTP data plane, persist policy bodies, or call any
  Serverless mutation API.
- Do not infer workload, environment, deployment, or ownership truth from
  OpenSearch Serverless names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
