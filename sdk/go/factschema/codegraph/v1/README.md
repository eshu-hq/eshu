# Code Graph Core Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the `code`
fact family's two git-collector fact kinds. A reducer handler never reads
`Envelope.Payload["some_key"]` for `file`/`repository` identity directly; it
decodes through the parent `factschema` package's kind-keyed seam (for example
`factschema.DecodeCodegraphFile`) and receives one of these structs, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `file` | `File` | `factschema.DecodeCodegraphFile` |
| `repository` | `Repository` | `factschema.DecodeCodegraphRepository` |

Both kinds are emitted once per source file / once per generation by the git
collector (`go/internal/collector/git_fact_builder.go` `fileFactEnvelope`,
`repositoryFactEnvelope`). This package types only the OUTER envelope identity
the code-graph-core reducer handlers (code-call extraction, code-import
repo-dependency edges) READ to attribute rows to a repository and file. The
required set tracks what the reducer reads for identity, not the full wire
shape (see the required/optional table below).

`File.ParsedFileData` stays an OPEN `map[string]any` container — it is never
narrowed to a closed struct (that would drop unmodeled inner keys, force a
schema major bump, and break the parsed_file_data wire byte-identity). Its
inner keys are typed INCREMENTALLY, key by key, as reducer read sites migrate
off raw map lookups (issue #4750): the typed inner structs live in
`parsed_file_data.go` and are decoded on demand through the parent module's
`DecodeParsedFileData*` accessors (`decode_parsed_file_data.go`). S1 types the
five closed-shape, single-producer keys — `gomod_state`, `function_calls_scip`,
`dockerfile_stages`, `pipeline_calls`, `dead_code_file_root_kinds`. The wide
per-language AST buckets (`imports`, `functions`, `function_calls`, `classes`,
`variables`, `framework_semantics`) are still read raw until their own #4750
increment. Only the container's identity fields and object-ness are validated at
the envelope level.

## Ownership boundary

This package owns the Go type definitions for these two fact kinds' outer
envelopes. It does not own decode dispatch, schema-version routing, or
required-field validation — that lives in the parent `factschema` package
(`decode.go`, `decode_codegraph.go`). It does not own graph projection or
code-call/import-edge extraction; reducer handlers under `go/internal/reducer`
(`code_call_materialization_extract.go`, `code_import_repo_edge.go`,
`code_import_repo_edge_retract.go`) consume the decoded structs but live
outside this module. It does not own the git collector emitters that build
these payloads (`go/internal/collector/git_fact_builder.go`,
`go/internal/collector/git_refs.go`), which also live outside this module.

## Exported surface

`File`, `Repository`, `GitRef`. See each struct's godoc comment for its full
field list; the required/optional split below is the contract most callers
need first.

## Dependencies

Standalone: this package has no imports beyond the standard library implied by
its struct tags — no custom JSON codec, no polymorphic `Attributes`
pass-through. It carries no dependency on `go/internal/...` — see the module
`AGENTS.md` for the rule.

## Required vs. optional fields, per struct

Field mutability encodes the contract, per Contract System v1 §3.1
(`docs/internal/design/contract-system-v1.md`):

- **Required**: a non-pointer field with no `omitempty` tag. The decode seam
  rejects a payload that omits a required field, or supplies an explicit JSON
  null for one, with a classified `input_invalid` error naming the field,
  never a zero-value struct.
- **Optional**: a pointer field, or a slice/map carrying `omitempty`. An
  absent optional field decodes to nil, not a defaulted zero value.

The required set tracks **what the reducer reads for identity/extraction**, not
what the collector always emits. Requiring an emit-only field the reducer
ignores would dead-letter usable graph truth — the wrong contract under Contract
System v1's "don't drop right results" accuracy guarantee.

| Struct | Required fields | Why |
| --- | --- | --- |
| `File` | `RepoID`, `RelativePath`, `ParsedFileData` | `RepoID`/`RelativePath` are the accuracy hole issue #4749 exists to close (a fact missing either used to join under an empty-string graph identity); the code-graph-core handlers reach into `ParsedFileData` for every edge. `ParsedFileData` is required-present and must decode as a JSON object; its container stays open while inner keys are typed incrementally through the `DecodeParsedFileData*` accessors (issue #4750). |
| `File` optional fields | `GraphID`, `GraphKind`, `IsDependency`, `Language` | `GraphID`/`GraphKind`/`IsDependency` are unconditionally emitted but no reducer read site consumes them (`GraphID` is a redundant `RepoID:RelativePath` derivation, `GraphKind` a constant discriminator, `IsDependency` unread by any code-graph-core handler). `Language` is written only when the parser reported one. |
| `Repository` | `RepoID` | The only field a code-graph-core reducer read site requires (`buildCodeCallProjectionContexts`, `buildCodeCallDeltaFileScopesByRepoID`). |
| `Repository` optional fields | `GraphID`, `GraphKind`, `Name`, `ParsedFileCount`, `IsDependency`, `RepoSlug`, `RemoteURL`, `LocalPath`, `DefaultBranch`, `GitRefs`, `DeltaGeneration`, `ReconciliationGeneration`, `DeltaRelativePaths`, `DeltaDeletedRelativePaths`, `SourceRunID` | `Name`/`ParsedFileCount`/`GraphID`/`GraphKind`/`IsDependency` are unconditionally emitted but unread by any code-graph-core reducer read site. The reducer reads only `SourceRunID`, `LocalPath`, `DeltaRelativePaths`, and `DeltaDeletedRelativePaths` (all optional) beyond `RepoID`. `ParsedFileCount` is a STRING on the wire (`fmt.Sprintf("%d", ...)`) — do not retype it numeric. |

`imports_map` (the repository import graph, `map[string][]string` on the wire)
is deliberately NOT a modeled field. Its array-valued `additionalProperties`
shape is rejected by the collector conformance validator's supported schema
subset (`sdk/go/collector/conformance/payload_schema.go`), and no reducer read
site consumes it, so it passes through the open top-level object
(`additionalProperties: true`) untyped, like an `aws_resource` unmodeled key.
See the struct comment in `repository.go`.

`GitRef` (the `git_refs` element shape) has zero optional fields:
`repositoryFactGitRefsPayload` only emits a ref entry once `name` and
`head_sha` are both non-blank, and always writes `kind` (defaulted to
`"branch"`) and `is_default`.

## Why `DefaultBranch`/`GitRefs` are schema-declared but reducer-unread

Only the projector reads `default_branch`/`git_refs`
(`go/internal/projector/canonical_*`), not the reducer. They are declared in
this schema anyway because the #4573 payload-usage manifest gate only scans
reducer decode calls — leaving a projector-only field undeclared would silently
break projector reads on a future schema change with no gate to catch it. This
mirrors the incident family's SQL-loader-only field precedent documented in
the parent module's `AGENTS.md`.

## `parsed_file_data` is an open container, typed key by key (issue #4750)

`File.ParsedFileData` is an OPEN `map[string]any` — the container is never
narrowed to a closed struct. Its inner keys are typed INCREMENTALLY as reducer
read sites migrate off raw map lookups: the typed inner structs live in
`parsed_file_data.go` (`GomodState`, `SCIPFunctionCall`, `DockerfileStage`, ...)
and are decoded on demand through the parent module's `DecodeParsedFileData*`
accessors (`decode_parsed_file_data.go`), each of which reads ONE inner key.
This follows the shipped `aws_resource.Attributes` open-object precedent: type
what a consumer joins on, leave the container open so an un-typed key is still
read raw and no producer field is dropped.

S1 (issue #4750) types the five closed-shape, single-producer keys —
`gomod_state`, `function_calls_scip`, `dockerfile_stages`, `pipeline_calls`,
`dead_code_file_root_kinds`. Each typed inner struct that carries producer
fields no consumer reads uses an open `Attributes map[string]any` pass-through
so the accessor drops nothing. Do NOT add typed structs for the wide
per-language AST buckets (`imports`, `functions`, `function_calls`, `classes`,
`variables`, `framework_semantics`) here yet — their element shape is a union of
many independently evolving per-language field sets, deferred to later #4750
increments that will follow the same read-set-plus-open-passthrough shape.

Issue #5440 later typed a sixth key, `image_overrides` (`ImageOverride`), the
same way: closed shape, two single-producer parsers
(`go/internal/parser/yaml/image_overrides.go`), every field named with no
`Attributes` pass-through since no third producer can add an unlisted field.
Unlike the S1 batch, it was typed ahead of any reducer read site — it has no
consumer yet (round-4 review corrected an earlier, inaccurate #5441 citation
here and at seven other sites -- #5441 is "iac: persist relationship Details
and Terraform attributes at the graph write" and has nothing to do with
image_overrides). Issue #5445 ("contract the extraction surface: registry
entries + typed accessors") governs the typed-accessor contract it follows.
Issue #5469 ("vuln: tiered deployed-version resolution") aims to judge a
vulnerability finding's version from the strongest available tier,
including branch-resolved manifest evidence, which this bucket's declared
Helm/Kustomize tag/digest is the kind of evidence for -- though #5469 does
not yet name this bucket explicitly.

Issue #5445 slice 1 typed eight more keys the same way, split across two
sibling files by source family: `parsed_file_data_terraform.go`
(`TerraformModule`, `TerragruntDependency`, `TerragruntConfig`, from
`go/internal/parser/hcl/parser.go`) and `parsed_file_data_gitops.go`
(`HelmChart`, `HelmValues`, `ArgoCDApplication`, `ArgoCDApplicationSet`,
`FluxGitRepository`, from `go/internal/parser/yaml`). These keys are NOT
consumed by the code-graph-core reducer — their consumer is
`go/internal/relationships` (IaC deploy/dependency evidence discovery:
`terraform_evidence.go`, `terragrunt_helper_evidence.go`,
`structured_family_evidence.go`, `argocd_generator_config.go`,
`flux_evidence.go`), a different read site than the #4750 S1 batch's
code-call-materialization reducer handlers, but the same open-container,
typed-inner-key contract applies regardless of which package decodes the
key. Every multi-value field in this batch (`dependencies`, `source_repos`,
`generator_source_repos`, ...) is a comma-joined CSV string on the wire, not
a JSON array — both the HCL and YAML parsers always emit these fields
pre-joined, and the CSV split (`csvValues`/`tupleCSVValues`) stays business
logic in `go/internal/relationships`, not the payload contract, so the
typed field is a plain `string`, matching the wire shape exactly.

Typing an inner key here adds a struct + accessor, NOT a new fact kind: these
structs have no `payloadContracts` row, no `schema/` artifact, and no schemagen
entry (they are not envelopes), so they do not change the `file.v1.schema.json`
wire schema — that is the whole point of keeping the container open.

## Changing a struct

Any field change here is a payload-schema change.

- **Additive optional field** (new pointer/`omitempty` field): a minor schema
  bump. Add the field, regenerate, and commit the schema in the same change.
- **Remove, rename, or narrow a field**: a major schema bump. It needs a
  conversion shim in the parent package's decode seam (`decode.go`,
  `decodeLatestMajor`) — see the module `README.md` — never a silent edit
  here.

Regenerate after any struct change:

```bash
cd sdk/go/factschema
go generate ./...
```

`schema_gen_test.go`'s `TestSchemasHaveNoDrift` fails the build on drift. The
decode seam derives its required-field set reflectively from each struct's
tags (`../../fields.go`), so there is no separate map to update;
`TestDerivedKeySetsMatchGeneratedSchemas` fails if that reflective set ever
diverges from the generated schema, and `TestPayloadStructShapeConvention`
rejects a field shape that would make "required" ambiguous. Any fixture pack
copy under `../../fixturepack/schema/` must be refreshed in the same change
(`TestFixturePackSchemasMatchCanonical` locks the two).

## Telemetry

None. This package has no runtime, network, queue, graph, or telemetry
emission path — see the module `README.md`'s no-observability-change note.

## Gotchas / invariants

- `Repository.ParsedFileCount` is a STRING on the wire. Do not retype it as an
  int; the collector's `fmt.Sprintf("%d", parsedFileCount)` is the contract.
- `File.ParsedFileData` must decode as a JSON object. A non-object payload
  value (a string, number, or array) fails the parent module's `decodeMapInto`
  assignment and surfaces as a classified decode error — no extra validation
  code is needed here for the "must be a map" guarantee.
- Type `ParsedFileData`'s inner keys INCREMENTALLY via `parsed_file_data.go`
  structs + `DecodeParsedFileData*` accessors (issue #4750), keeping the
  container open. Do not narrow `ParsedFileData` itself, and do not type the
  wide per-language AST buckets ahead of their increment — see the
  `parsed_file_data` section above.
- The reducer decodes only the latest struct per fact kind. Older-schema-major
  shims live in the parent package's `decodeLatestMajor`, never here.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning, §7 migration plan.
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` (`sdk/go/factschema/README.md`) — decode seam,
  classified errors, schema generation.
- `go/internal/collector/git_fact_builder.go`,
  `go/internal/collector/git_refs.go` — the collector-side emitters for these
  two fact kinds.
