# Package Registry Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the `package_registry` fact family:
`Package`, `PackageVersion`, `PackageDependency`, `SourceHint`,
`PackageArtifact`, `VulnerabilityHint`, `RegistryEvent`, `RepositoryHosting`,
and `Warning`. It must remain independent from Eshu internals.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing any payload struct's fields, run `go generate ./...` from the
  module root and commit the regenerated schema under `../../schema/` AND the
  embedded fixture-pack copy under `../../fixturepack/schema/` (the drift-lock
  test compares the two).
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofumpt` on changed Go files, and `git diff --check` from the repo root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by the
  flat-struct convention required fields are also non-pointer, and optional
  fields are pointers or slices/maps carrying `omitempty`. Both the schema
  generator (`../../internal/schemagen`) and the decode seam's required-field
  check (`../../decode.go`) derive that set reflectively from the struct's own
  tags via `../../fields.go`, so there is no hand-maintained key list to keep
  in sync. `TestDerivedKeySetsMatchGeneratedSchemas`,
  `TestPayloadStructShapeConvention`, and `TestSchemasHaveNoDrift` lock the
  derivations together.
- **Required set = today's identity/join gate only.** Mark a field required
  ONLY when its ABSENCE produces a broken or empty graph identity in the
  projector's current read path
  (`go/internal/projector/package_registry_canonical.go`). A field the
  projector tolerates empty must stay OPTIONAL. Flipping a present-but-empty
  value into a dead-letter is an ACCURACY REGRESSION the contract forbids:
  only an ABSENT key (or explicit null) dead-letters; a present-but-empty
  value is a valid decode. See `doc.go` for the per-kind required set and its
  justification.
- `IsYanked`/`IsUnlisted`/`IsDeprecated`/`IsRetracted` on `PackageVersion` and
  `Optional`/`Excluded` on `PackageDependency` are OPTIONAL `*bool` with
  `omitempty`, NOT required identity keys: they are descriptive status flags,
  and the projector re-decodes STORED facts on every re-projection
  (`go/internal/projector/runtime.go`), so a persisted, older, or out-of-tree
  fact that omits one must still decode and project its version/dependency node
  (the row builder derefs nil to false) rather than quarantine the whole node
  on a missing descriptive flag. This matches the ociregistry `Mutated *bool`
  and terraformstate `Sensitive *bool` convention. Never type a descriptive
  non-identity flag as a required non-pointer bool.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). A consumer receiving it must dead-letter the fact
  rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- These structs are fully typed (no `Attributes map[string]any` pass-through).
  Do not add a bare optional `any` field — `TestPayloadStructShapeConvention`
  bans it.

## Consumed vs deferred

- **Consumed today** (decode through the seam on the projector read path):
  `Package`, `PackageVersion`, `PackageDependency`
  (`go/internal/projector/package_registry_canonical.go`).
- **Typed-but-not-yet-consumed**: `SourceHint`, `PackageArtifact`,
  `VulnerabilityHint`, `RegistryEvent`, `RepositoryHosting`, and `Warning` have
  no decode-seam read consumer in the current codebase. `SourceHint` IS read
  by the reducer's `package_source_correlation` domain
  (`go/internal/reducer/package_source_correlation.go`), a separate reducer
  family this wave does not convert — do not add a projector decode site for
  it here; that conversion belongs to the reducer family's own migration.
  `VulnerabilityHint.PackageID` and `Warning.Ecosystem`/`Warning.WarningCode`
  are read by raw-SQL-JSONB loaders in `go/internal/storage/postgres`
  (`facts_active_supply_chain_impact.go`, `status_registry.go`); those fields
  MUST stay declared here even though no decode site exists —
  `go/internal/storage/postgres/package_registry_sql_schema_lockstep_test.go`
  locks that coverage. The six kinds ship a struct, schema, and fixture pack so
  the contract is ready, but their decode-site conversion, regression test, and
  benchmark land in the change that first reads each kind through the typed
  seam — matching how the terraform_state family typed
  `Candidate`/`ProviderBinding`/`Warning` ahead of their consumer.

## Family boundary

- This package defines nine fact kinds. Adding a tenth kind or a `v2` major is
  follow-on epic work, not a casual edit. The kinds and their wire strings are
  the `facts.PackageRegistry*FactKind` constants in
  `go/internal/facts/package_registry.go`; the parent package's `FactKind*`
  constants MUST stay byte-equal to them (the reducer-side drift lock
  `TestFactSchemaKindsMatchWireFactKinds` asserts it).
