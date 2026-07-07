# secrets_iam Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the secrets_iam family, except for
the legacy `aws_iam_principal` struct in `sdk/go/factschema/iam/v1`. It must
remain independent from Eshu internals.

## Scope

This package covers AWS IAM source-detail facts, GCP IAM facts, Kubernetes
identity and RBAC facts, Vault identity and mount facts, and
`secrets_iam_coverage_warning`. Keep `aws_iam_principal` in `iam/v1` unless a
separate design moves that legacy boundary. W2c (#4796) owns loader-side
consumer decode changes; do not add raw JSONB consumer rewrites here unless the
issue scope says so.

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

## Adding consumer decode later

The source-contract structs and parent decode/encode seams are present for the
full family. Loader/query consumers that still read raw payload JSONB must move
through their own scoped issue so field-use evidence, no-regression proof, and
dead-letter behavior stay reviewable.
