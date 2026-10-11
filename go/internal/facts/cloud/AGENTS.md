# AGENTS.md — internal/facts/cloud

Scoped instructions for this package. Read `README.md` and `doc.go` first;
the root `AGENTS.md` still applies.

## Invariants

- **No file in `package cloud` may import `go/internal/facts`.** The facts
  root already imports this package (its `schemaVersionFamilies` table
  reaches the accessors directly now that #6950 retired the transitional
  compat surface); the reverse import cycles.
  If you need something the facts root defines, it belongs in a leaf package
  instead, or the facts root should forward from here, not the other way
  around.

  The one exception is an *external* test package. `gcp_test.go` is
  `package cloud_test` precisely so it can import both `facts` and
  `facts/cloud` — it asserts GCP kinds against `facts.CoreFactKinds` and
  against the `GCPIAM*` kinds that `secrets_iam.go` still declares at the
  root. Go allows that because `cloud_test` is a separate package, so it is
  not a cycle. Do not "fix" it by moving those assertions into
  `package cloud`; they will not compile.
- **A new fact kind needs three things in the same change**: the kind and
  schema-version constants here, an entry in
  `specs/fact-kind-registry.v1.yaml` (the family's `kinds` list plus a
  `schema_version`/`schema_version_overrides` entry), and a row for the
  owning family in the facts root's `schemaVersionFamilies` table
  (`go/internal/facts/schema_version.go`) so `SchemaVersion`,
  `ClassifySchemaVersion`, and `ValidateSchemaVersion` see it. Missing any
  one leaves the kind either unadmitted or registered without a live
  implementation, which `ValidateFactKindRegistry` and its test are built
  to catch.
- **No package-name stutter.** `cloud.AWSFactKinds`, never
  `cloud.CloudAWSFactKinds`. No filename repeats `cloud/`; `aws.go`,
  `terraform_state.go`, and the rest already follow this.
- **Additive-only schema versions.** A breaking payload change needs a new
  schema-version constant (bump the family/kind version), not a silent
  reinterpretation of the current one. See
  `docs/public/reference/fact-schema-versioning.md`.

## Common changes

- **Add a fact kind to an existing family** — add its constant and schema
  version to that family's file, append it to the family's
  `<family>FactKinds` slice and `<family>SchemaVersions` map, then update
  `specs/fact-kind-registry.v1.yaml` and regenerate
  `fact_kind_registry.generated.go` per the facts root `AGENTS.md`.
- **Add a new cloud-posture family** — new file here (never a name that
  repeats `cloud`), the same `<Family>FactKinds()`/`<Family>SchemaVersion()`
  pair, then wire it into the facts root's `schemaVersionFamilies` table.

## What NOT to change without checking the wiring status

- Renaming or removing any exported constant here breaks the facts root's
  `schemaVersionFamilies` table and every direct `cloud.<Name>` caller (see
  `README.md`'s "Depended on by"). Issue #6950 retired the transitional
  compat surface, so no compat entry exists to update — a rename must
  update the table and the direct callers in the same change.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md`, and `AGENTS.md` present; it checks presence only, not
  content accuracy.
- **`verify-dirgate.sh`** — this directory counts against the repo's
  per-directory file cap; check before adding files.
- **`verify-filename-stutter.sh`** — a new file whose name repeats `cloud`
  fails on Added/Renamed paths.
