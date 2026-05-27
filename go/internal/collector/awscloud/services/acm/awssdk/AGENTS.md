# AGENTS.md - internal/collector/awscloud/services/acm/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - ACM SDK pagination, metadata enrichment, mapping, and the
   apiClient interface that intentionally omits forbidden APIs.
3. `client_test.go` - the reflection-based security gate that fails if
   `GetCertificate` or `ExportCertificate` ever appears on the interface.
4. `telemetry.go` - per-API-call instrumentation and throttle classification.
5. `../scanner.go` - scanner-owned ACM fact selection.
6. `../README.md` - ACM scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep ACM SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- The `apiClient` interface MUST stay restricted to `ListCertificates`,
  `DescribeCertificate`, and `ListTagsForCertificate`. Adding `GetCertificate`
  or `ExportCertificate` breaks the security gate and the metadata-only
  contract.
- Do not call any ACM mutation API. The adapter is read-only.
- ACM Private CA (acm-pca) APIs are out of scope.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new ACM metadata read by extending `acm.Client`, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not add `GetCertificate` or `ExportCertificate` to the apiClient interface
  under any circumstances.
- Do not mutate ACM resources.
- Do not infer workload, environment, deployment, or ownership truth from
  certificate names, domains, tags, or in-use-by ARNs.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
