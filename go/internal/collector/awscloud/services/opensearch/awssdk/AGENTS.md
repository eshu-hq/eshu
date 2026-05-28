# AGENTS.md - internal/collector/awscloud/services/opensearch/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - OpenSearch and OpenSearch Serverless SDK pagination,
   batching, and telemetry.
3. `mapper.go` - safe mapping of AWS SDK responses to scanner-owned types and
   access-policy role-ARN extraction.
4. `../scanner.go` - scanner-owned OpenSearch fact selection.
5. `../README.md` - OpenSearch scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep OpenSearch SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page, batch, or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Never reach the OpenSearch HTTP API (`_search`, `_index`, `_doc`, `_bulk`,
  and similar) and never call `GetIndex` on either service. The `domainAPI`
  and `serverlessAPI` interfaces must carry metadata reads only; the
  reflection test in `contract_test.go` enforces this.
- `DescribeDomains` does not return the master user password; persist only the
  metadata fields modeled on `opensearch.Domain`. Drop the domain `Endpoint`,
  `Endpoints` map, and access policy body. Resolve only access-policy role
  ARNs and never persist the policy body.
- Recognize role ARNs by the `arn:<partition>:iam::<account>:role/` shape
  without synthesizing the aws partition.
- Iterate every `SecurityConfigType` from the SDK enum in
  `ListSecurityConfigs`; do not hardcode the type list.
- Batch `BatchGetCollection` and `BatchGetVpcEndpoint` at the 100-id AWS limit.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new OpenSearch metadata read by extending `Client`, writing a scanner
  or adapter test first, then mapping the SDK response into scanner-owned
  types. Add the new method to the `contract_test.go` want-list so the
  metadata-only proof stays exhaustive.
- Extend mapping only for AWS source data that is metadata and does not reveal
  master passwords, endpoint contents, access policy bodies, package bodies, or
  saved-object bodies.

## What Not To Change Without An ADR

- Do not call mutation, inbound-connection, index/search, or data APIs.
- Do not construct or call the domain HTTP endpoint.
- Do not infer workload, environment, deployment, or ownership truth from
  domain names, collection names, package names, or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
- Do not project the master user password, domain endpoint, access policy body,
  package body, or serverless saved-object body into scanner-owned types.
