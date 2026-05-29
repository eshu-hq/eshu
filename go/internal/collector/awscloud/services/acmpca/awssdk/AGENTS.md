# AGENTS.md - internal/collector/awscloud/services/acmpca/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - ACM Private CA SDK pagination, metadata enrichment, mapping,
   and the apiClient interface that intentionally omits forbidden APIs.
3. `client_test.go` - the reflection-based security gate that fails if any
   issuance, body-reading, or mutating API appears on the interface.
4. `telemetry.go` - per-API-call instrumentation and throttle classification.
5. `../scanner.go` - scanner-owned ACM Private CA fact selection.
6. `../README.md` - ACM Private CA scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep ACM Private CA SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- The `apiClient` interface MUST stay restricted to
  `ListCertificateAuthorities`, `DescribeCertificateAuthority`, and `ListTags`.
  Adding `IssueCertificate`, `GetCertificate`, `GetCertificateAuthorityCsr`,
  `GetCertificateAuthorityCertificate`, `RevokeCertificate`, or any CA mutation
  breaks the security gate and the metadata-only contract.
- Do not call any ACM Private CA mutation or body-reading API. The adapter is
  read-only and never reads the certificate chain, CSR, or private key.
- Page with `NextToken` and stop on the empty token; never re-issue the same
  token.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new ACM Private CA metadata read by extending `acmpca.Client`, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not add IssueCertificate, GetCertificate, GetCertificateAuthorityCsr,
  GetCertificateAuthorityCertificate, RevokeCertificate, or any CA mutation to
  the apiClient interface under any circumstances.
- Do not mutate ACM Private CA resources.
- Do not infer workload, environment, deployment, or ownership truth from CA
  names, subjects, or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
