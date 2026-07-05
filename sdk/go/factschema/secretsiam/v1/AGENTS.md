# secrets_iam VAULT + K8S Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the secrets_iam family's VAULT lane
(`VaultAuthRole`, `VaultACLPolicy`, `VaultKVMetadata`) and K8S lane
(`KubernetesServiceAccount`, `KubernetesWorkloadIdentityUse`,
`EKSIRSAAnnotation`, `EKSPodIdentityAssociation`,
`KubernetesGCPWorkloadIdentityBinding`). It must remain independent from Eshu
internals.

## Scope (Wave 4d, Contract System v1 #4566/#4582)

This package is deliberately partial. The secrets_iam family has three lanes:

- **AWS IAM lane** (`aws_iam_principal`, `aws_iam_trust_policy`,
  `aws_iam_permission_policy`): already migrated in #4568. Its structs live in
  `sdk/go/factschema/iam/v1`, NOT here. Do not move or duplicate them.
- **VAULT lane** and **K8S lane**: typed here in this wave.
- **GCP IAM lane** (`gcp_iam_principal`, `gcp_iam_trust_policy`,
  `gcp_iam_permission_policy`): deferred to a future wave. Do not add these
  kinds here without a design discussion — the reducer's
  `secrets_iam_trust_chain_gcp.go` continues reading them raw with an explicit
  "deferred: gcp_iam lane" comment at each read site.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing any payload struct's fields, run `go generate ./...` from
  the module root and commit the regenerated schema under `../../schema/`,
  AND refresh the byte-identical copy under
  `../../fixturepack/schema/` (`TestFixturePackSchemasMatchCanonical` locks
  the two).
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofmt` on changed Go files, and `git diff --check` from the repo root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by
  the flat-struct convention required fields are also non-pointer, and
  optional fields are pointers or slices, carrying `omitempty`. Both the
  schema generator (`../../internal/schemagen`) and the decode seam's
  required-field check (`../../decode.go`) derive that set reflectively from
  the struct's own tags via `../../fields.go`.
- `VaultACLPolicy.Rules` is a typed `[]VaultACLPolicyRule`, not a
  `map[string]any` pass-through. The collector emitter
  (`secretsiam.vaultPolicyRulePayloads`) always emits a well-known
  `{path_fingerprint, path_depth, capabilities}` shape per rule; keep the
  nested struct fully typed rather than reverting to a raw map.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). A reducer handler receiving it must dead-letter
  the fact rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs
  a conversion shim in the parent package's decode seam
  (`decodeLatestMajor` in `../../decode.go`), not a silent edit here.
- Every fact-kind wire string here is UNDERSCORE-separated
  (`go/internal/facts.VaultAuthRoleFactKind` and siblings), matching the
  aws/gcp/azure convention, NOT the dotted incident/kubernetes_live
  convention. Never invent a dot this family's wire kinds do not already
  carry.

## Adding the GCP IAM lane later

When the GCP IAM lane's own wave lands, add its structs to a sibling file in
this package (or a design-reviewed alternative), add its `FactKind*`
constants and `Decode*`/`Encode*` seam functions to the parent module's
`decode.go`/`decode_secretsiam.go`, and update
`go/internal/reducer/secrets_iam_trust_chain_gcp.go`'s "deferred" comments to
point at the real decode call. Do not do this opportunistically inside an
unrelated change.
