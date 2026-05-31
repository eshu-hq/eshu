# OpenSearch Serverless SDK Adapter

## Purpose

`internal/collector/awscloud/services/opensearchserverless/awssdk` adapts AWS SDK
for Go v2 OpenSearch Serverless (aoss) control-plane calls into the scanner-owned
metadata model. It paginates `ListCollections`, `ListSecurityPolicies`, and
`ListVpcEndpoints`, hydrates details through `BatchGetCollection`,
`GetSecurityPolicy`, and `BatchGetVpcEndpoint`, reads resource tags through
`ListTagsForResource`, and records API-call, throttle, and pagination telemetry.

## Ownership boundary

This package owns OpenSearch Serverless SDK pagination, safe metadata mapping,
encryption-policy-body projection, and adapter telemetry. It does not own scanner
fact selection, envelope construction, graph writes, or reducer admission.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - SDK-backed implementation of the scanner's `Client` interface.
- `NewClient` - constructs the adapter for one claimed AWS boundary.

## Telemetry

Each paginator page and point read is wrapped in `recordAPICall`, which starts
the shared `aws.service.pagination.page` span and records the shared AWS API-call
and throttle counters. Metric labels stay bounded to service, account, region,
operation, and result.

## Gotchas / invariants

- Keep OpenSearch Serverless SDK calls here, not in the scanner package or the
  collector command.
- Keep the `apiClient` interface limited to `List*`, `BatchGet*`, and `Get*`
  reads. The exclusion test fails the build if any method is not a read or
  matches a data-plane (Index, Search, Bulk, Document, Query) or mutation name;
  do not loosen it.
- The OpenSearch HTTP data plane (index, search, bulk, document APIs) lives on
  the collection endpoint. This adapter never constructs that endpoint and the
  SDK interface carries no such method.
- The encryption-policy body is parsed only to extract the customer-managed
  `KmsARN` and collection resource patterns, then discarded. The raw policy
  document never leaves `parseEncryptionPolicy` and is never persisted. AWS-owned
  -key policies produce no key binding.
- OpenSearch Serverless reports created/last-modified timestamps as epoch
  milliseconds; `epochMillis` converts them to UTC and treats zero as absent.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Evidence

No-Regression Evidence: metadata-only control-plane adapter; new read path, no change to existing hot paths. `go test ./internal/collector/awscloud/services/opensearchserverless/...` green.

No-Observability-Change: reuses shared AWS pagination span + API-call/throttle counters; no telemetry contract change.

## Related docs

- `../README.md` for the OpenSearch Serverless scanner contract.
- `../../../README.md` for the AWS cloud envelope contract.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
