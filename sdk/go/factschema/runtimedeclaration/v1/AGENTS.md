# Runtime Declaration Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload struct for the `runtime_declaration` fact
family's one kind: `Version`. It must remain independent from Eshu internals.

The git collector emits this kind
(`go/internal/collector/repo/runtime/declaration`). The reducer projection
(#8816) and query tool (#8817) are separate changes. Do not add a collector,
reducer wiring or read surface in this directory.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`.
- After changing `Version`'s fields, run `go generate ./...` from the module
  root and commit the regenerated schema under `../../schema/` and its copy
  under `../../fixturepack/schema/`.
- Run `go test ./... -count=1` from `sdk/go/factschema`, `gofmt` on changed
  files, and `git diff --check`.

## Contract Rules

- A field is required exactly when its json tag has no `omitempty`; required
  fields are non-pointer and optional fields are pointers. The schema
  generator and the decode seam derive the set from the struct tags, so there
  is no key list to keep in sync.
- The required set is nine fields. Adding a tenth is a major change.
- Never zero-fill a version part or a range bound. A range, alias or
  unresolved row leaves the version parts nil.
- `SpecKind` is tolerant on read. Do not add a membership check to the decoder
  and do not add an `enum` to the generated schema: either would turn a member
  added later into a rejected fact.
- Removing, renaming or narrowing a field, or changing how the stable key is
  derived, is a major bump and needs a conversion shim in the parent decode
  seam.
- `Version` carries no polymorphic `Attributes` pass-through.
