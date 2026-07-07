# Secrets/IAM Collector Envelopes

## Purpose

`internal/collector/secretsiam` builds source-fact envelopes for the
`secrets_iam_posture` collector family. It records provider-native IAM identity,
policy, attachment, boundary, instance-profile, optional Access Analyzer, and
Kubernetes ServiceAccount/RBAC/workload-identity plus Vault metadata and
coverage-warning evidence without writing graph truth. The GCP path includes
service-account impersonation trust facts and GKE Workload Identity join
anchors, all represented by fingerprints or digests.

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
  Permission-policy facts accept inline, attached managed, and
  permissions-boundary policy sources; trust policies use the dedicated trust
  envelope.
- `NewGCPPrincipalEnvelope`, `NewGCPTrustPolicyEnvelope`, and
  `NewGCPPermissionPolicyEnvelope` build GCP IAM source facts. Trust facts carry
  target service-account fingerprints/email digests, trusted-member
  fingerprints, bounded impersonation modes, and optional Workload Identity
  subject fingerprints; raw service-account email and member strings stay out of
  payloads.
- `NewKubernetesServiceAccountEnvelope`,
  `NewKubernetesServiceAccountTokenPostureEnvelope`,
  `NewKubernetesRBACRoleEnvelope`, `NewKubernetesRBACBindingEnvelope`,
  `NewKubernetesWorkloadIdentityUseEnvelope`,
  `NewKubernetesGCPWorkloadIdentityBindingEnvelope`,
  `NewEKSIRSAAnnotationEnvelope`,
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
- `WebIdentitySubjectFingerprint` builds the redaction-safe join fingerprint
  used by EKS IRSA source facts and AWS IAM trust-policy source facts. Raw
  `system:serviceaccount:<namespace>:<name>` subjects stay out of payloads.
- `GCPServiceAccountEmailDigest` and
  `GCPWorkloadIdentitySubjectFingerprint` build the redaction-safe join anchors
  shared by GCP IAM trust facts and Kubernetes GKE Workload Identity binding
  facts.

## Dependencies

- `internal/facts` for durable envelope, fact-kind, schema-version, source-ref,
  and stable-id contracts.
- `sdk/go/factschema` for the AWS secrets_iam direct-map payload encoders and
  typed source-contract structs used by the AWS builders.

Provider adapters normalize source data before calling this package. The
package still has no provider SDK dependency.

## Telemetry

This package emits no metrics, spans, or logs. Runtime adapters and hosted
collector sources own API-call metrics, throttling counters, scan duration,
warnings, and status reporting.

Collector Performance Evidence: `go test ./internal/collector/secretsiam ./internal/facts -count=1`
covers deterministic envelope construction, redaction, stable-key generation,
and bounded list normalization without provider calls, graph writes, queue
claims, or network I/O.

Collector Observability Evidence: source-envelope helpers emit durable
`secrets_iam_coverage_warning` facts when callers report hidden, partial,
unsupported, rate-limited, or stale source coverage. Runtime adapters remain
responsible for request spans, API-call counters, retry counters, scan duration,
and status reporting.

No-Observability-Change: this package is not a hosted runtime and adds no
metric instruments, span names, log fields, ServiceMonitor labels, or admin
status endpoints. The Vault source-envelope slice introduces no live Vault
client path; future adapters must add their own runtime telemetry before
claim-driven collection lands.

Collector Deployment Evidence: no Docker Compose service, Helm Deployment,
Service, ServiceMonitor, port, environment variable, or runtime flag changes are
introduced by this package. The Vault slice is source-fact construction only.

## Gotchas / invariants

- `CollectorKind` is always `secrets_iam_posture`; callers must not use the
  AWS cloud collector envelope helpers for these facts.
- Policy payloads carry condition key/operator names only. Raw policy JSON,
  statement bodies, condition values, AWS credentials, and session tokens stay
  outside the fact contract. The only condition-derived value this package permits is the
  deterministic web-identity subject fingerprint required for exact IRSA joins.
- Kubernetes payloads carry fingerprints and bounded metadata only. Raw
  ServiceAccount names, namespaces, RBAC subject names, Secret names,
  resourceVersions, projected tokens, and RBAC resource-name or non-resource-URL
  values stay outside the fact contract. GKE Workload Identity bindings also
  keep raw target GCP service-account email and workload-pool subjects out of
  payloads.
- Vault payloads carry fingerprints, counts, and bounded capability summaries
  only. Raw KV paths, key names, custom metadata keys or values, Vault policy
  bodies, Vault policy names, auth role names, mount accessors, entity IDs,
  alias names, Vault tokens, AppRole secret IDs, private URLs, and warning
  messages stay outside the fact contract. Vault Kubernetes auth roles emit
  `bound_service_account_join_keys` only when both namespace/name selectors are
  exact and the Kubernetes cluster ID is known; wildcard selectors remain broad
  posture evidence.
- Stable keys for principal, policy, trust, attachment, boundary, instance
  profile, OIDC provider, analyzer-finding, Kubernetes ServiceAccount, RBAC,
  workload-identity, IRSA, EKS Pod Identity, Vault mount, Vault auth role, Vault
  policy, Vault identity, and Vault KV metadata facts exclude generation IDs so
  repeated source observations can be compared across generations. Coverage
  warning stable keys include generation IDs because warning state is scoped to
  the scan that observed the missing or partial surface. Conditioned IAM trust
  and permission-policy statement facts include normalized condition key/operator
  names in the stable key; unconditioned statements keep the historical
  identity.
- Builders emit source facts only. Reducers own all graph promotion, trust-chain
  joins, effective RBAC interpretation, and effective-permission decisions.

## Related docs

- `docs/public/guides/collector-authoring.md`
- `docs/public/reference/fact-schema-versioning.md`
