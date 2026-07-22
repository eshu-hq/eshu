# Submodule Graph Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload struct for the one `submodule` family fact
kind: `Pin` (fact kind `submodule.pin`). It must remain independent from
Eshu internals.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing the payload struct's fields, run `go generate ./...` from
  the module root and commit the regenerated schema under `../../schema/`
  AND its copy under `../../fixturepack/schema/`
  (`TestFixturePackSchemasMatchCanonical` locks the two).
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofmt` on changed Go files, and `git diff --check` from the repo root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by
  the flat-struct convention required fields are also non-pointer, and
  optional fields are pointers, carrying `omitempty`. Both the schema
  generator (`../../internal/schemagen`) and the decode seam's
  required-field check (`../../decode.go`) derive that set reflectively
  from the struct's own tags via `../../fields.go`, so there is no
  hand-maintained key list to keep in sync.
- `Pin` requires only `parent_repo_id` and `submodule_path` — the join
  identity the reducer's submodule-edge materializer keys off.
  `collector_instance_id`,
  `submodule_url`, `resolved_repo_id`, and `pinned_sha` are OPTIONAL: a
  non-dangling observation can legitimately be missing any one of them (see
  `pin.go`'s doc comment for the exact cases). Do NOT promote any of them to
  required "to match the common case" — a `.gitmodules`-only or
  gitlink-only observation is a valid fact this schema must still accept.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). The reducer must dead-letter the fact on this
  error rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs
  a conversion shim in the parent package's decode seam
  (`decodeLatestMajor` in `../../decode.go`), not a silent edit here.
- This package (issue #5420) defines the CONTRACT for `submodule.pin`: the
  fact-kind constant, the typed struct, its generated schema, and the
  registry entry. The git collector emits this fact kind
  (`go/internal/collector/submodule`) and the reducer decodes and projects
  it (`go/internal/reducer/submodule_pin_materialization.go`) into
  `Repository-[:PINS_SUBMODULE]->Repository` graph edges; both live outside
  this module. Do not add collector emission code, reducer/projector
  consumption code, or a graph read surface here — this directory's scope
  stays the fact-kind constant, the typed struct, its generated schema, and
  the registry entry only.
- `specs/fact-kind-registry.v1.yaml`'s `submodule` family sets
  `read_surface: none` deliberately (the recognized sentinel also used by
  `reducer_internal`): the `PINS_SUBMODULE` submodule edge is queryable
  through the generic graph tools, not a dedicated route. Do not replace it
  with a placeholder route string.
- This package defines exactly one fact kind (`submodule.pin`). A second
  submodule-family fact kind or a `v2` major is follow-on work gated on its
  own scoped change, not a casual edit here.
