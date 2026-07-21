# Code Graph Core Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the two `code` family fact kinds:
`File` (fact kind `file`) and `Repository` (fact kind `repository`). It must
remain independent from Eshu internals.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing any payload struct's fields, run `go generate ./...` from
  the module root and commit the regenerated schema under `../../schema/`
  AND its copy under `../../fixturepack/schema/`
  (`TestFixturePackSchemasMatchCanonical` locks the two).
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofmt` on changed Go files, and `git diff --check` from the repo root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by the
  flat-struct convention required fields are also non-pointer, and optional
  fields are pointers or slices/maps, carrying `omitempty`. Both the schema
  generator (`../../internal/schemagen`) and the decode seam's required-field
  check (`../../decode.go`) derive that set reflectively from the struct's own
  tags via `../../fields.go`, so there is no hand-maintained key list to keep
  in sync.
- The required set tracks **what the reducer READS for identity/extraction**,
  not what the collector always emits. `File` requires `repo_id`,
  `relative_path`, and `parsed_file_data`; `Repository` requires only
  `repo_id`. Fields the collector unconditionally emits but no reducer read
  site consumes (`graph_id`, `graph_kind`, `is_dependency`, `name`,
  `parsed_file_count`) are OPTIONAL. Do NOT promote one to required to "match
  the wire shape": requiring an emit-only field the reducer ignores would
  dead-letter usable graph truth, violating Contract System v1's "don't drop
  right results" accuracy guarantee. Add a field to the required set only when
  a real reducer read site depends on it for identity/extraction.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). A reducer handler receiving it must dead-letter the
  fact rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- `File.ParsedFileData` is a REQUIRED `map[string]any` field, not the
  polymorphic `Attributes` catch-all pattern `awsv1.Resource` uses. It has no
  custom `MarshalJSON`/`UnmarshalJSON` — the parent module's `decodeMapInto`
  already assigns any payload map value directly onto a `map[string]any`
  field regardless of its Go field name (`decode_map.go`), and a non-object
  payload value fails that assignment with a classified decode error. Do NOT
  add a custom codec to this field; it is unnecessary.
- `ParsedFileData` MUST stay opaque (`map[string]any`, no nested struct for the
  container itself). Its SPECIFIC inner keys are typed incrementally,
  key-by-key, as a consuming read site migrates off a raw map lookup (issue
  #4750 S1, issue #5440, issue #5445 slice 1): the typed inner structs live in
  `parsed_file_data*.go` and are decoded on demand through the parent module's
  `DecodeParsedFileData*` accessors, per key, per the pattern documented in
  `README.md`'s "parsed_file_data is an open container" section. Do not add a
  typed struct for the WIDE per-language AST buckets (`imports`, `functions`,
  `function_calls`, `classes`, `variables`, `framework_semantics`) — their
  element shape is a union of many independently evolving per-language field
  sets, deferred to a later increment — without discussing scope first.
- `File.ParsedFileData` is a required, non-`omitempty` `map[string]any` field.
  `TestPayloadStructShapeConvention` (parent module `decode_test.go`) requires
  every such field to be explicitly allow-listed in
  `intentionalRequiredCollections` (`decode_gcp_test.go`) with a justification;
  the entry for `{FactKindCodegraphFile, "parsed_file_data"}` cites
  `fileFactEnvelope`'s unconditional emission. Do not remove that allow-list
  entry without also relaxing this field to optional.
- `Repository.ParsedFileCount` is a STRING (`json:"parsed_file_count,omitempty"`),
  not a number — the collector formats it with `fmt.Sprintf("%d", parsedFileCount)`
  before emission (`repositoryFactEnvelope`). It is OPTIONAL (no reducer read
  site consumes it). Do not retype this field to a number, and do not promote
  it to required.
- `Repository.DefaultBranch` and `Repository.GitRefs` are schema-declared even
  though only the projector reads them
  (`go/internal/projector/canonical_*.go`), not the reducer. This mirrors the
  incident family's SQL-loader-only field precedent
  (`sdk/go/factschema/AGENTS.md`): the #4573 payload-usage manifest gate only
  scans reducer decode calls, so a projector-only field must still be
  schema-declared or a future schema change can silently break the projector
  read with no gate to catch it.
- `GitRef` is a closed sub-struct with no optional fields —
  `repositoryFactGitRefsPayload` (`go/internal/collector/git_refs.go`) only
  emits a ref entry once `name` and `head_sha` are both non-blank, and always
  writes `kind` (defaulted to `"branch"`) and `is_default`.
- This package defines exactly two fact kinds (`file`, `repository`). A third
  code-graph fact kind, a nested `ParsedFileData` struct, or a `v2` major is
  follow-on work gated on its own scoped change, not a casual edit here.
