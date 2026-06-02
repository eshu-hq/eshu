# Secrets/IAM Collector Envelopes

## Purpose

`internal/collector/secretsiam` builds source-fact envelopes for the
`secrets_iam_posture` collector family. It records provider-native IAM identity,
policy, attachment, boundary, instance-profile, optional Access Analyzer, and
Kubernetes ServiceAccount/RBAC/workload-identity plus Vault metadata evidence
without writing graph truth.

## Ownership boundary

This package owns source-fact shape, stable identity, collector kind, and
redaction-safe payload construction for secrets/IAM posture facts. It does not
call AWS, Kubernetes, Vault, Jira, PagerDuty, Postgres, or graph backends, and it
does not decide effective permissions, effective RBAC, or trust-chain posture.

## Exported surface

See `doc.go` for the godoc contract.

- `EnvelopeContext` carries source scope, generation, claim, and observation
  time fields.
- `NewPrincipalEnvelope`, `NewTrustPolicyEnvelope`,
  `NewPermissionPolicyEnvelope`, `NewPolicyAttachmentEnvelope`,
  `NewPermissionBoundaryEnvelope`, `NewInstanceProfileEnvelope`,
  `NewAccessAnalyzerFindingEnvelope`, and `NewCoverageWarningEnvelope` build
  AWS IAM source facts in the `facts.SecretsIAMSchemaVersionV1` schema.
- `NewKubernetesServiceAccountEnvelope`,
  `NewKubernetesServiceAccountTokenPostureEnvelope`,
  `NewKubernetesRBACRoleEnvelope`, `NewKubernetesRBACBindingEnvelope`,
  `NewKubernetesWorkloadIdentityUseEnvelope`, `NewEKSIRSAAnnotationEnvelope`,
  `NewEKSPodIdentityAssociationEnvelope`, and
  `NewKubernetesCoverageWarningEnvelope` build Kubernetes source facts in the
  same schema.
- `NewVaultAuthMountEnvelope`, `NewVaultAuthRoleEnvelope`,
  `NewVaultACLPolicyEnvelope`, `NewVaultIdentityEntityEnvelope`,
  `NewVaultIdentityAliasEnvelope`, `NewVaultKVMetadataEnvelope`,
  `NewVaultSecretEngineMountEnvelope`, and `NewVaultCoverageWarningEnvelope`
  build Vault metadata source facts in the same schema.
- Observation structs define the metadata-only inputs accepted by those
  builders.

## Dependencies

- `internal/facts` for durable envelope, fact-kind, schema-version, source-ref,
  and stable-id contracts.

This package has no SDK dependency. Provider adapters normalize source data
before calling it.

## Telemetry

This package emits no metrics, spans, or logs. Runtime adapters and hosted
collector sources own API-call metrics, throttling counters, scan duration,
warnings, and status reporting.

## Gotchas / invariants

- `CollectorKind` is always `secrets_iam_posture`; callers must not use the
  AWS cloud collector envelope helpers for these facts.
- Policy payloads carry condition keys only. Raw policy JSON, statement bodies,
  condition values, AWS credentials, and session tokens stay outside the fact
  contract.
- Kubernetes payloads carry fingerprints and bounded metadata only. Raw
  ServiceAccount names, namespaces, RBAC subject names, Secret names,
  resourceVersions, projected tokens, and RBAC resource-name or non-resource-URL
  values stay outside the fact contract.
- Vault payloads carry fingerprints, counts, and bounded capability summaries
  only. Raw KV paths, key names, custom metadata keys or values, policy bodies,
  policy names, auth role names, mount accessors, entity IDs, alias names, Vault
  tokens, AppRole secret IDs, private URLs, and warning messages stay outside
  the fact contract.
- Stable keys for principal, policy, trust, attachment, boundary, instance
  profile, OIDC provider, analyzer-finding, Kubernetes ServiceAccount, RBAC,
  workload-identity, IRSA, EKS Pod Identity, Vault mount, Vault auth role, Vault
  policy, Vault identity, and Vault KV metadata facts exclude generation IDs so
  repeated source observations can be compared across generations. Coverage
  warning stable keys include generation IDs because warning state is scoped to
  the scan that observed the missing or partial surface.
- Builders emit source facts only. Reducers own all graph promotion, trust-chain
  joins, effective RBAC interpretation, and effective-permission decisions.

## Related docs

- `docs/public/guides/collector-authoring.md`
- `docs/public/reference/fact-schema-versioning.md`
