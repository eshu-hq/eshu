# secrets_iam Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the
secrets_iam family's AWS source details, GCP IAM lane, Kubernetes lane, Vault
lane, and coverage warnings. The legacy AWS principal payload remains in
`sdk/go/factschema/iam/v1` because that struct predated this package, but the
rest of the secrets_iam source-contract matrix lives here.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

These secrets_iam fact kinds decode through this package:

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `aws_iam_trust_policy` | `AWSIAMTrustPolicy` | `factschema.DecodeAWSIAMTrustPolicy` |
| `aws_iam_permission_policy` | `AWSIAMPermissionPolicy` | `factschema.DecodeAWSIAMPermissionPolicy` |
| `aws_iam_policy_attachment` | `AWSIAMPolicyAttachment` | `factschema.DecodeAWSIAMPolicyAttachment` |
| `aws_iam_permission_boundary` | `AWSIAMPermissionBoundary` | `factschema.DecodeAWSIAMPermissionBoundary` |
| `aws_iam_instance_profile` | `AWSIAMInstanceProfile` | `factschema.DecodeAWSIAMInstanceProfile` |
| `aws_iam_access_analyzer_finding` | `AWSIAMAccessAnalyzerFinding` | `factschema.DecodeAWSIAMAccessAnalyzerFinding` |
| `gcp_iam_principal` | `GCPIAMPrincipal` | `factschema.DecodeGCPIAMPrincipal` |
| `gcp_iam_trust_policy` | `GCPIAMTrustPolicy` | `factschema.DecodeGCPIAMTrustPolicy` |
| `gcp_iam_permission_policy` | `GCPIAMPermissionPolicy` | `factschema.DecodeGCPIAMPermissionPolicy` |
| `vault_auth_role` | `VaultAuthRole` | `factschema.DecodeVaultAuthRole` |
| `vault_acl_policy` | `VaultACLPolicy` | `factschema.DecodeVaultACLPolicy` |
| `vault_kv_metadata` | `VaultKVMetadata` | `factschema.DecodeVaultKVMetadata` |
| `vault_auth_mount` | `VaultAuthMount` | `factschema.DecodeVaultAuthMount` |
| `vault_identity_entity` | `VaultIdentityEntity` | `factschema.DecodeVaultIdentityEntity` |
| `vault_identity_alias` | `VaultIdentityAlias` | `factschema.DecodeVaultIdentityAlias` |
| `vault_secret_engine_mount` | `VaultSecretEngineMount` | `factschema.DecodeVaultSecretEngineMount` |
| `k8s_service_account` | `KubernetesServiceAccount` | `factschema.DecodeKubernetesServiceAccount` |
| `k8s_workload_identity_use` | `KubernetesWorkloadIdentityUse` | `factschema.DecodeKubernetesWorkloadIdentityUse` |
| `eks_irsa_annotation` | `EKSIRSAAnnotation` | `factschema.DecodeEKSIRSAAnnotation` |
| `eks_pod_identity_association` | `EKSPodIdentityAssociation` | `factschema.DecodeEKSPodIdentityAssociation` |
| `k8s_gcp_workload_identity_binding` | `KubernetesGCPWorkloadIdentityBinding` | `factschema.DecodeKubernetesGCPWorkloadIdentityBinding` |
| `k8s_rbac_role` | `KubernetesRBACRole` | `factschema.DecodeKubernetesRBACRole` |
| `k8s_rbac_binding` | `KubernetesRBACBinding` | `factschema.DecodeKubernetesRBACBinding` |
| `k8s_service_account_token_posture` | `KubernetesServiceAccountTokenPosture` | `factschema.DecodeKubernetesServiceAccountTokenPosture` |
| `secrets_iam_coverage_warning` | `CoverageWarning` | `factschema.DecodeSecretsIAMCoverageWarning` |

## Lane partition

The secrets_iam family keeps one historical split: `aws_iam_principal` uses
`iam/v1.Principal`, while the other AWS IAM source-detail structs live here.
W2c (#4796) owns the loader-side second-decode path for consumers that still
read raw JSONB; this package only defines the typed payload contract and the
parent `factschema` decode/encode seam.

## Ownership boundary

This package owns the Go type definitions for these fact kinds' payloads. It
does not own decode dispatch, schema-version routing, or
required-field validation — that lives in the parent `factschema` package
(`decode.go`, `decode_secretsiam.go`). It does not own graph projection or
trust-chain resolution; the secrets_iam trust-chain reducer handler consumes
the decoded structs but lives outside this module.

## Exported surface

`AWSIAMTrustPolicy`, `AWSIAMPermissionPolicy`, `AWSIAMPolicyAttachment`,
`AWSIAMPermissionBoundary`, `AWSIAMInstanceProfile`,
`AWSIAMAccessAnalyzerFinding`, `GCPIAMPrincipal`, `GCPIAMTrustPolicy`,
`GCPIAMPermissionPolicy`, `VaultAuthRole`, `VaultACLPolicy` (with nested
`VaultACLPolicyRule`), `VaultKVMetadata`, `VaultAuthMount`,
`VaultIdentityEntity`, `VaultIdentityAlias`, `VaultSecretEngineMount`,
`KubernetesServiceAccount`, `KubernetesWorkloadIdentityUse`,
`EKSIRSAAnnotation`, `EKSPodIdentityAssociation`,
`KubernetesGCPWorkloadIdentityBinding`, `KubernetesRBACRole`,
`KubernetesRBACBinding`, `KubernetesServiceAccountTokenPosture`, and
`CoverageWarning`. See each struct's godoc comment for its full field list.

## Dependencies

Standalone: this package imports nothing beyond the Go standard library and
carries no dependency on `go/internal/...`.

## Required vs. optional fields, per struct

Field mutability encodes the contract, per Contract System v1 §3.1:

| Struct | Required identity fields | Why |
| --- | --- | --- |
| `AWSIAMTrustPolicy` | `AccountID`, `Region`, `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `RoleARN`, `PolicySource`, `Effect` | The AWS collector validates the role/effect identity and always stamps source context. |
| `AWSIAMPermissionPolicy` | `AccountID`, `Region`, `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `PrincipalARN`, `PolicySource`, `Effect` | The AWS collector validates the principal, source, and effect that anchor one normalized statement. |
| `AWSIAMPolicyAttachment` | `AccountID`, `Region`, `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `PrincipalARN`, `PolicyARN` | Attachment facts join one principal to one managed policy ARN. |
| `AWSIAMPermissionBoundary` | `AccountID`, `Region`, `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `PrincipalARN`, `BoundaryPolicyARN` | Boundary facts join one principal to one boundary policy ARN. |
| `AWSIAMInstanceProfile` | `AccountID`, `Region`, `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `ProfileARN` | Instance-profile facts are keyed by the observed profile ARN. |
| `AWSIAMAccessAnalyzerFinding` | `AccountID`, `Region`, `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion` | Finding identity fields are optional, but every emitted finding carries source context. |
| `GCPIAMPrincipal` | `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `PrincipalFingerprint`, `PrincipalType` | The GCP collector emits redacted principal identity only. |
| `GCPIAMTrustPolicy` | `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `TargetPrincipalFingerprint`, `TargetServiceAccountEmailDigest`, `Role`, `ImpersonationMode` | Trust facts join a redacted target service account, role, and impersonation mode. |
| `GCPIAMPermissionPolicy` | `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `PrincipalFingerprint`, `PrincipalType`, `Role`, `ResourceFullName` | Permission facts join a redacted principal, role, and resource. |
| `VaultAuthRole` | `RoleJoinKey` | The collector emitter always derives it; it is the reducer's sole index key for a Vault auth role. |
| `VaultACLPolicy` | `PolicyJoinKey` | The collector emitter always derives it; it is the reducer's sole index key for a Vault ACL policy, joined from `VaultAuthRole.TokenPolicyJoinKeys`. |
| `VaultKVMetadata` | `MountJoinKey`, `KVPathFingerprint` | Both always derived by the collector; together they are the reducer's join key for a Vault KV metadata path. |
| `VaultAuthMount` | `VaultClusterID`, `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `AuthMethod`, `MountJoinKey` | Auth-mount facts are keyed by the redacted mount join key and method. |
| `VaultIdentityEntity` | `VaultClusterID`, `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `EntityJoinKey` | Entity facts use the redacted entity join key. |
| `VaultIdentityAlias` | `VaultClusterID`, `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `AliasIDFingerprint`, `EntityJoinKey`, `MountJoinKey` | Alias facts join a redacted alias to an entity and auth mount. |
| `VaultSecretEngineMount` | `VaultClusterID`, `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `MountJoinKey`, `MountType` | Secret-engine facts are keyed by mount join key and mount type. |
| `KubernetesServiceAccount` | `ServiceAccountJoinKey` | The collector emitter always derives it; it is the reducer's sole index key for a Kubernetes ServiceAccount. |
| `KubernetesWorkloadIdentityUse` | `ServiceAccountJoinKey` | Always derived; the reducer's join key from a workload back to its ServiceAccount. |
| `EKSIRSAAnnotation` | `ServiceAccountJoinKey`, `RoleARN` | The emitter rejects an annotation with no role ARN; both anchor the assumed-role identity join. |
| `EKSPodIdentityAssociation` | `ServiceAccountJoinKey`, `RoleARN` | Mirrors `EKSIRSAAnnotation`; the emitter rejects an association with no association ID or role ARN. |
| `KubernetesGCPWorkloadIdentityBinding` | `ServiceAccountJoinKey`, `GCPServiceAccountEmailDigest`, `GCPWorkloadIdentitySubjectFingerprint` | The emitter rejects a binding missing any of the three; all three anchor the GCP exact-chain join. |
| `KubernetesRBACRole` | `ClusterID`, `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `RoleKind`, `RoleScope`, `RoleJoinKey` | RBAC role facts are keyed by redacted role identity and scope. |
| `KubernetesRBACBinding` | `ClusterID`, `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `BindingKind`, `BindingScope`, `RoleRefKind`, `RoleRefJoinKey` | RBAC binding facts join redacted subjects to a redacted role reference. |
| `KubernetesServiceAccountTokenPosture` | `ClusterID`, `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `ServiceAccountJoinKey` | Token posture facts attach to one redacted ServiceAccount join key. |
| `CoverageWarning` | `Provider`, `CollectorInstanceID`, `RedactionPolicyVersion`, `WarningKind`, `SourceState` | Coverage warnings carry source-local state and optional scoped metadata. |

Missing a required identity field dead-letters as `input_invalid` rather than
forming an empty-string graph identity, a dropped index entry, or a
malformed edge.

## Nested typed struct: `VaultACLPolicyRule`

Unlike `aws/v1.Resource`, `VaultACLPolicy.Rules` is a fully typed
`[]VaultACLPolicyRule`, not a `map[string]any` pass-through: the collector
emitter (`secretsiam.vaultPolicyRulePayloads`) always emits exactly
`{path_fingerprint, path_depth, capabilities}` per rule, a closed, well-known
shape. Every field on `VaultACLPolicyRule` is optional (pointer/slice with
`omitempty`) — a malformed rule entry in a heterogeneous array degrades to
"this rule matches nothing" on the reducer's read side rather than failing
the whole `VaultACLPolicy` decode.

## Changing a struct

Any field change here is a payload-schema change.

- **Additive optional field** (new pointer/`omitempty` field): a minor schema
  bump. Add the field, regenerate, and commit the schema (both `../../schema/`
  and `../../fixturepack/schema/`) in the same change.
- **Remove, rename, or narrow a field**: a major schema bump. It needs a
  conversion shim in the parent package's decode seam (`decode.go`,
  `decodeLatestMajor`) — never a silent edit here.

Regenerate after any struct change:

```bash
cd sdk/go/factschema
go generate ./...
cp schema/<changed-kind>.v1.schema.json fixturepack/schema/
```

`schema_gen_test.go`'s `TestSchemasHaveNoDrift` and
`fixturepack_drift_test.go`'s `TestFixturePackSchemasMatchCanonical` both fail
the build on drift.

## Telemetry

None. This package has no runtime, network, queue, graph, or telemetry
emission path.

## Gotchas / invariants

- `aws_iam_principal` is still typed in `iam/v1.Principal`; keep that legacy
  placement unless a separate design changes the package boundary.
- The reducer decodes only the latest struct per fact kind. Older-schema-major
  shims live in the parent package's `decodeLatestMajor`, never here.
- `VaultACLPolicyRule` stays a nested typed struct, never a raw map, even
  though every one of its fields is optional.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning, §7 migration plan.
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` (`sdk/go/factschema/README.md`) — decode seam,
  classified errors, schema generation.
- `sdk/go/factschema/iam/v1/README.md` — the AWS IAM lane, already migrated.
