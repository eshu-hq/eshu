# AGENTS.md - internal/collector/awscloud/services/appmesh guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned App Mesh domain types.
3. `scanner.go` - resource and relationship emission orchestration.
4. `relationships.go` - relationship target-type and join-key rules.
5. `redaction.go` - sensitive HTTP header match value redaction.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep App Mesh API access behind `Client`; do not import the AWS SDK into this
  package.
- NEVER call an App Mesh mutation API: Create/Update/Delete for Mesh,
  VirtualService, VirtualNode, VirtualRouter, Route, VirtualGateway, or
  GatewayRoute. The adapter `apiClient` interface excludes all of them and a
  reflection test asserts the exclusion.
- NEVER persist a client TLS validation certificate body in any form. Client
  TLS validation is reduced to ACM certificate authority ARN references only
  (`VirtualNode.ClientTLSCertificateAuthorityARNs`). File and SDS trust shapes
  (certificate chains, secret names) are intentionally not read.
- Redact sensitive HTTP header match values through the shared redact library.
  Always preserve the header NAME and match type. `Scanner.Scan` fails closed
  when the redaction key is zero.
- Every relationship sets a non-empty `target_type`. App Mesh-internal edges
  key on App Mesh ARNs. The client TLS trust edge keys on the ACM Private CA
  (acm-pca) certificate authority ARN App Mesh reports and targets
  `aws_acmpca_certificate_authority`, not the public ACM scanner's
  `aws_acm_certificate`. There is no ACM Private CA scanner yet; the target type
  is forward-looking so the edge joins once one exists.
- NEVER hardcode `arn:aws:` when synthesizing an ARN. Derive the partition,
  region, account, and mesh name from a known resource ARN (see
  `siblingResourceARN`).
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from resource names or tags.

## Common Changes

- Add a new App Mesh metadata field by extending the relevant type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders.
- Add new relationship evidence only when App Mesh reports both sides directly,
  and always set a non-empty `target_type` with a join key that matches the
  target scanner resource_id.
- Extend SDK pagination and Describe fan-out in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read or persist client TLS certificate bodies, certificate chains, or
  SDS secret names.
- Do not call any App Mesh mutation API.
- Do not stop redacting sensitive header match values or drop the redaction-key
  guard.
- Do not infer workload ownership from resource names or tags.
- Do not add graph writes, reducer logic, or query behavior here.
- Do not add AWS credential loading or STS calls to this package.
