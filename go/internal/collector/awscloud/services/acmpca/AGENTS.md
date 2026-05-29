# AGENTS.md - internal/collector/awscloud/services/acmpca guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned ACM Private CA domain types.
3. `scanner.go` - certificate authority resource and relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep ACM Private CA API access behind `Client`; do not import the AWS SDK into
  this package.
- Never issue or export certificates, never read the CSR body, never read the
  certificate chain body, and never read private key material.
- The CA `resource_id` MUST stay the CA ARN. The App Mesh virtual-node client
  TLS trust edge targets `aws_acmpca_certificate_authority` keyed by that ARN;
  changing the format reopens a dangling edge.
- Emit relationships only when AWS reports a concrete ARN/bucket join key. Never
  synthesize a KMS key ARN, a parent CA ARN, or a bucket name, and never
  hardcode a partition.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from CA subject names or tags.
- Keep CA ARNs, serials, tags, and subject names out of metric labels.

## Common Changes

- Add a new CA metadata field by extending `CertificateAuthority`, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders.
- Add new relationship evidence only when the ACM Private CA API reports both
  sides directly as an ARN or a documented bucket name. Gate the edge on the
  reported value and add the absent-case test.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not call IssueCertificate, GetCertificate, GetCertificateAuthorityCsr,
  GetCertificateAuthorityCertificate, RevokeCertificate, or any CA mutation.
- Do not resolve CA names, tags, or relationships into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
