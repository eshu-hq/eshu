# secrets_iam VAULT + K8S Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the
secrets_iam family's VAULT and K8S lanes. A reducer handler never reads
`Envelope.Payload["some_key"]` for these kinds directly; it decodes through
the parent `factschema` package's kind-keyed seam (for example
`factschema.DecodeVaultAuthRole`) and receives one of these structs, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

Eight secrets_iam fact kinds decode through this package (Contract System v1
Wave 4d, issue #4566/#4582):

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `vault_auth_role` | `VaultAuthRole` | `factschema.DecodeVaultAuthRole` |
| `vault_acl_policy` | `VaultACLPolicy` | `factschema.DecodeVaultACLPolicy` |
| `vault_kv_metadata` | `VaultKVMetadata` | `factschema.DecodeVaultKVMetadata` |
| `k8s_service_account` | `KubernetesServiceAccount` | `factschema.DecodeKubernetesServiceAccount` |
| `k8s_workload_identity_use` | `KubernetesWorkloadIdentityUse` | `factschema.DecodeKubernetesWorkloadIdentityUse` |
| `eks_irsa_annotation` | `EKSIRSAAnnotation` | `factschema.DecodeEKSIRSAAnnotation` |
| `eks_pod_identity_association` | `EKSPodIdentityAssociation` | `factschema.DecodeEKSPodIdentityAssociation` |
| `k8s_gcp_workload_identity_binding` | `KubernetesGCPWorkloadIdentityBinding` | `factschema.DecodeKubernetesGCPWorkloadIdentityBinding` |

## Lane partition

The secrets_iam family is partitioned into three lanes across separate
migration waves:

- **AWS IAM lane** — already typed in `sdk/go/factschema/iam/v1` (#4568).
  Not this package.
- **VAULT lane + K8S lane** — typed in THIS package (Wave 4d).
- **GCP IAM lane** (`gcp_iam_principal`, `gcp_iam_trust_policy`,
  `gcp_iam_permission_policy`) — deferred to a future wave. The reducer's
  `secrets_iam_trust_chain_gcp.go` reads these three kinds raw via
  `payloadString`, with an explicit `// deferred: gcp_iam lane` comment at
  each read site, even though it reads a K8S-lane kind
  (`k8s_gcp_workload_identity_binding`, typed here) in the same file.

## Ownership boundary

This package owns the Go type definitions for these eight fact kinds'
payloads. It does not own decode dispatch, schema-version routing, or
required-field validation — that lives in the parent `factschema` package
(`decode.go`, `decode_secretsiam.go`). It does not own graph projection or
trust-chain resolution; the secrets_iam trust-chain reducer handler consumes
the decoded structs but lives outside this module.

## Exported surface

`VaultAuthRole`, `VaultACLPolicy` (with nested `VaultACLPolicyRule`),
`VaultKVMetadata`, `KubernetesServiceAccount`, `KubernetesWorkloadIdentityUse`,
`EKSIRSAAnnotation`, `EKSPodIdentityAssociation`, and
`KubernetesGCPWorkloadIdentityBinding`. See each struct's godoc comment for
its full field list.

## Dependencies

Standalone: this package imports nothing beyond the Go standard library and
carries no dependency on `go/internal/...`.

## Required vs. optional fields, per struct

Field mutability encodes the contract, per Contract System v1 §3.1:

| Struct | Required identity fields | Why |
| --- | --- | --- |
| `VaultAuthRole` | `RoleJoinKey` | The collector emitter always derives it; it is the reducer's sole index key for a Vault auth role. |
| `VaultACLPolicy` | `PolicyJoinKey` | The collector emitter always derives it; it is the reducer's sole index key for a Vault ACL policy, joined from `VaultAuthRole.TokenPolicyJoinKeys`. |
| `VaultKVMetadata` | `MountJoinKey`, `KVPathFingerprint` | Both always derived by the collector; together they are the reducer's join key for a Vault KV metadata path. |
| `KubernetesServiceAccount` | `ServiceAccountJoinKey` | The collector emitter always derives it; it is the reducer's sole index key for a Kubernetes ServiceAccount. |
| `KubernetesWorkloadIdentityUse` | `ServiceAccountJoinKey` | Always derived; the reducer's join key from a workload back to its ServiceAccount. |
| `EKSIRSAAnnotation` | `ServiceAccountJoinKey`, `RoleARN` | The emitter rejects an annotation with no role ARN; both anchor the assumed-role identity join. |
| `EKSPodIdentityAssociation` | `ServiceAccountJoinKey`, `RoleARN` | Mirrors `EKSIRSAAnnotation`; the emitter rejects an association with no association ID or role ARN. |
| `KubernetesGCPWorkloadIdentityBinding` | `ServiceAccountJoinKey`, `GCPServiceAccountEmailDigest`, `GCPWorkloadIdentitySubjectFingerprint` | The emitter rejects a binding missing any of the three; all three anchor the GCP exact-chain join. |

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
cp schema/vault_auth_role.v1.schema.json fixturepack/schema/
cp schema/vault_acl_policy.v1.schema.json fixturepack/schema/
cp schema/vault_kv_metadata.v1.schema.json fixturepack/schema/
cp schema/k8s_service_account.v1.schema.json fixturepack/schema/
cp schema/k8s_workload_identity_use.v1.schema.json fixturepack/schema/
cp schema/eks_irsa_annotation.v1.schema.json fixturepack/schema/
cp schema/eks_pod_identity_association.v1.schema.json fixturepack/schema/
cp schema/k8s_gcp_workload_identity_binding.v1.schema.json fixturepack/schema/
```

`schema_gen_test.go`'s `TestSchemasHaveNoDrift` and
`fixturepack_drift_test.go`'s `TestFixturePackSchemasMatchCanonical` both fail
the build on drift.

## Telemetry

None. This package has no runtime, network, queue, graph, or telemetry
emission path.

## Gotchas / invariants

- The GCP IAM lane (`gcp_iam_principal` and siblings) is NOT in this
  package. Do not add it without a design discussion.
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
