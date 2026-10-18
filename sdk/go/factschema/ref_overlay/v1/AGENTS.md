# Ref Overlay Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the two `ref_overlay` family fact
kinds: `BranchObservation` (fact kind `ref_overlay.branch_observation`) and
`ConfigKeyDivergence` (fact kind `config_key_divergence`). It must remain
independent from Eshu internals.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing a payload struct's fields, run `go generate ./...` from
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
- `BranchObservation` requires `repo_id`, `ref`, `ref_kind`, and
  `resolvability` — the overlay identity plus the resolution state. Do NOT
  promote `head_sha` to required "to match the common case" — an
  unresolved or unfetchable ref is a valid observation that must still
  decode (it reads unknown, never the default branch's SHA).
- `ConfigKeyDivergence` requires `repo_id`, `ref`, `config_key`, `env`, and
  `diverged`. It carries keys and booleans only:
  `TestConfigKeyDivergenceCarriesNoValuesOrHashes` pins the exact field set,
  so a value-, hash-, or secret-shaped field fails there first. Do NOT add
  one — a reader that needs differing values names the ref and reads the
  content overlay, it does not read them here.
- A URL-shaped `value_kind` classification MUST be computed from the
  userinfo-stripped form (`StripURLUserinfo`): credentials must never
  influence the classification or leak into the fact.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). The reducer must dead-letter the fact on this
  error rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs
  a conversion shim in the parent package's decode seam
  (`decodeLatestMajor` in `../../decode.go`), not a silent edit here.
- This package (epic #5393, Phase 1) defines the CONTRACT for these two
  kinds: the fact-kind constants, the typed structs, their generated
  schemas, and the registry entry. The ref-extraction emitter, the config
  comparison, reducer/query consumption, and the divergence read surface all
  live outside this module in sibling Phase 1 leaves. Do not add collector
  emission code, reducer/projector consumption code, or a graph read surface
  here — this directory's scope stays the fact-kind constants, the typed
  structs, their generated schemas, and the registry entry only.
- `specs/fact-kind-registry.v1.yaml`'s `ref_overlay` family sets
  `read_surface: none` deliberately (the recognized sentinel also used by
  `reducer_internal` and `submodule`): the divergence surface lands in a
  sibling Phase 1 leaf. Do not replace it with a placeholder route string.
- A third ref_overlay-family fact kind or a `v2` major is follow-on work
  gated on its own scoped change, not a casual edit here.
