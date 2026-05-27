# AGENTS.md - internal/collector/awscloud/services/acm guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned ACM domain types.
3. `scanner.go` - certificate resource and in-use-by relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep ACM API access behind `Client`; do not import the AWS SDK into this
  package.
- NEVER call `GetCertificate` and NEVER call `ExportCertificate`. The scanner
  contract excludes both methods because they reveal PEM body or private key
  material.
- NEVER persist certificate body PEM or private key material in any form,
  including `attributes["certificate"]`, `attributes["certificate_body"]`, and
  `attributes["private_key"]`.
- NEVER call ACM mutation APIs: `ImportCertificate`, `DeleteCertificate`,
  `RenewCertificate`, `RequestCertificate`, `UpdateCertificateOptions`,
  `ResendValidationEmail`, `RemoveTagsFromCertificate`.
- ACM Private CA (acm-pca) is out of scope. This package reads only the public
  ACM API.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from certificate names or tags.
- Preserve stable certificate identities across repeated observations in the
  same AWS generation.
- Keep certificate ARNs, domain names, tags, and in-use-by values out of metric
  labels.

## Common Changes

- Add a new ACM metadata field by extending `Certificate`, writing a focused
  scanner or adapter test first, then mapping it through `awscloud` envelope
  builders.
- Add new relationship evidence only when ACM reports both sides directly.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read certificate body PEM or private key material under any
  circumstances.
- Do not call `GetCertificate`, `ExportCertificate`, or any ACM mutation API.
- Do not infer workload ownership from certificate domain names or tags.
- Do not add graph writes, reducer logic, or query behavior here.
- Do not add AWS credential loading or STS calls to this package.
