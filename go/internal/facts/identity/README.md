# Identity

## Purpose

The identity package owns secrets and identity-posture source fact kinds:
AWS IAM, GCP IAM, Kubernetes service accounts and RBAC, and Vault. It
exists so the facts root stays a registry, not a family container (issue
#6951).

## Ownership boundary

Source-evidence declarations only. The package owns kind constants, family
kind lists, schema-version lookups, and the `contract.Declare` family
declaration. It does not own posture evaluation or trust chains — the
reducer secretsiam domain and projectors consume these kinds downstream.

## Exported surface

- `SecretsIAMFactKinds`, `SecretsIAMSchemaVersion` — IAM source kinds
- `AWSIAM*`, `GCPIAM*`, `Kubernetes*`, `Vault*` kind constants — join identities
- `SecretsIAMFamilyDeclaration` — registry declaration

See `doc.go` for the full contract.

## Dependencies

- `internal/facts/contract` — family declaration type
- Standard library only otherwise. Must not import `internal/facts`.

## Telemetry

None of its own. Kind declaration is compile-time data.

## Gotchas / invariants

- Kind strings are contracts: collectors emit them and the registry,
  schema-version table, loaders, and trust-chain builders match on them.
  Renaming a kind is a contract change, not a refactor.
- The schema-version table in the parent `schema_version.go` must list the
  family, or the classifier and drift guard go blind to IAM kinds.
- The flat layout (`identity/iam.go`, not `identity/secrets_iam/iam.go`)
  is deliberate: the whole file is the IAM family.

## Related docs

- `docs/internal/naming.md` (nesting rules behind the `identity/` split)
