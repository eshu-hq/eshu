# Codeowners Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload struct for the `codeowners` fact family's one
kind: `Ownership`. It must remain independent from Eshu internals.

This package is the `codeowners.ownership` payload contract for issue #5419's
branch-aware CODEOWNERS ingestion (epic #5415). The collector
(`go/internal/collector`), reducer materializer (`go/internal/reducer`), and
query/MCP read surface (`go/internal/query`, `go/internal/mcp`) now emit,
project, and serve this fact kind — but that runtime wiring lives in those
`go/internal/...` packages, never here. This SDK package stays the standalone
payload contract only: do NOT add a collector, reducer wiring, or read surface
in this directory (it would also break the module-independence rule below).

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing `Ownership`'s fields, run `go generate ./...` from the
  module root and commit the regenerated schema under `../../schema/` AND its
  copy under `../../fixturepack/schema/`
  (`TestFixturePackSchemasMatchCanonical` locks the two).
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofmt` on changed Go files, and `git diff --check` from the repo root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by
  the flat-struct convention required fields are also non-pointer, and
  optional fields are pointers carrying `omitempty`. Both the schema
  generator (`../../internal/schemagen`) and the decode seam's required-field
  check (`../../decode.go`) derive that set reflectively from the struct's
  own tags via `../../fields.go`, so there is no hand-maintained key list to
  keep in sync. `TestDerivedKeySetsMatchGeneratedSchemas` locks the two
  derivations to the generated schema, `TestPayloadStructShapeConvention`
  enforces the flat-struct convention, and `TestSchemasHaveNoDrift` keeps the
  checked-in schema in lockstep with the struct.
- `Owners` is an intentionally required (`omitempty`-free) slice: a
  CODEOWNERS pattern line with zero owners carries no ownership claim, so the
  collector never emits one. It is allow-listed in
  `intentionalRequiredCollections` (`../../decode_gcp_test.go`), which
  `TestPayloadStructShapeConvention` reads.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). A future reducer or query handler receiving it must
  dead-letter the fact rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- `Ownership` carries no polymorphic `Attributes map[string]any`
  pass-through — it is flat and fully closed. Do not add one without
  discussing scope.
- This package defines one fact kind. Adding a second `codeowners` kind or a
  `v2` major is follow-on work, not a casual edit.
