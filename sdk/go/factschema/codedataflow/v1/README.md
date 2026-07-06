# Git Dataflow Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the git
collector's value-flow ("dataflow") fact family. A reducer handler or its
postgres loader never reads `Envelope.Payload["some_key"]` for one of these
six fact kinds directly; it decodes through the parent `factschema` package's
kind-keyed seam (for example `factschema.DecodeCodeTaintEvidence`) and
receives one of these structs, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/codedataflow/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `code_dataflow_scanned` | `DataflowScanned` | `factschema.DecodeCodeDataflowScanned` |
| `code_dataflow_function` | `DataflowFunction` | `factschema.DecodeCodeDataflowFunction` |
| `code_function_summary` | `FunctionSummary` | `factschema.DecodeCodeFunctionSummary` |
| `code_function_source` | `FunctionSource` | `factschema.DecodeCodeFunctionSource` |
| `code_taint_evidence` | `TaintEvidence` | `factschema.DecodeCodeTaintEvidence` |
| `code_interproc_evidence` | `InterprocEvidence` | `factschema.DecodeCodeInterprocEvidence` |

All six kinds are emitted only when the value-flow gate (`ESHU_EMIT_DATAFLOW`)
is on (`go/internal/collector/git_snapshot_dataflow_function.go`,
`git_snapshot_function_summary.go`, `git_snapshot_function_source.go`,
`git_snapshot_taint_evidence.go`, `git_snapshot_interproc_evidence.go`,
`git_followup_facts.go`). None are registered in
`specs/fact-kind-registry.v1.yaml` (deferred to issue #4752): they are
version-less on the wire, so the Postgres persist layer stamps
`SchemaVersion="0.0.0"` for every one of them
(`go/internal/storage/postgres/facts_streaming.go`
`emptyToDefault(SchemaVersion, "0.0.0")`), which the reducer's
`factschemaEnvelope` adapter normalizes to the latest major exactly like the
codegraph family's `file`/`repository` kinds.

## Ownership boundary

This package owns the Go type definitions for these six fact kinds. It does
not own decode dispatch, schema-version routing, or required-field
validation — that lives in the parent `factschema` package (`decode.go`,
`decode_codedataflow.go`). It does not own graph projection, evidence-row
extraction, or postgres loading; `go/internal/reducer`
(`factschema_decode_codedataflow.go`, `code_taint_evidence_typed_decode.go`,
`code_function_summary_typed_decode.go`,
`code_taint_evidence_materialization.go`,
`code_interproc_evidence_materialization.go`,
`code_function_summary_materialization.go`) and
`go/internal/storage/postgres` (the `Load*` loader files) consume the decoded
structs but live outside this module. It does not own the git collector
emitters that build these payloads
(`go/internal/collector/git_snapshot_*.go`, `git_followup_facts.go`), which
also live outside this module.

## Exported surface

`DataflowScanned`, `DataflowFunction`, `FunctionSummary`, `ParamSink`,
`CallArgFlow`, `FunctionSource`, `TaintEvidence`, `InterprocEvidence`. See each
struct's godoc comment for its full field list; the required/optional split
below is the contract most callers need first.

## Dependencies

Standalone: no imports beyond the standard library implied by struct tags — no
custom JSON codec, no polymorphic `Attributes` pass-through. No dependency on
`go/internal/...`.

## Required vs. optional fields, per struct

Field mutability encodes the contract, per Contract System v1 §3.1
(`docs/internal/design/contract-system-v1.md`):

- **Required**: a non-pointer field with no `omitempty` tag. The decode seam
  rejects a payload that omits a required field, or supplies an explicit JSON
  null for one, with a classified `input_invalid` error naming the field,
  never a zero-value struct.
- **Optional**: a pointer field, or a slice/map carrying `omitempty`. An
  absent optional field decodes to nil, not a defaulted zero value.

The required set tracks **what the reducer or its postgres loader reads for
identity/join/attachment**, not what the collector always emits.

| Struct | Required fields | Why |
| --- | --- | --- |
| `DataflowScanned` | none | The marker's only job is signaling "the gate ran"; the projector's trigger read already tolerates an absent `repo_id`. |
| `DataflowFunction` | `RepoID`, `RelativePath`, `FunctionName` | The join identity for a per-function record; no reducer materialization handler decodes this kind today (query-layer-only consumer), but the identity contract is declared for a future reducer consumer. |
| `FunctionSummary` | `FunctionID` | The durable, generation-independent map key every reducer read site (summary store, `durableFunctionRepo` repo-prefix parse, graph-id store) keys on. |
| `FunctionSource` | `FunctionID`, `Kind` | `LoadCodeFunctionSources`'s pre-existing drop guard for a missing id or kind, made explicit and dead-lettering. |
| `TaintEvidence` | `FunctionUID` | The graph Function node this finding attaches to; a finding whose function did not resolve is never emitted with an empty uid collector-side, but making it required here dead-letters any payload that still arrives malformed. |
| `InterprocEvidence` | `SourceFunctionUID`, `SinkFunctionUID` | The TAINT_FLOWS_TO edge's two endpoints; `ExtractCodeInterprocEvidenceRows` already drops any row missing either, made explicit and dead-lettering here. |

Every other field on every struct is optional: written conditionally by the
collector (only a non-empty/non-zero/true value gets a payload key), so a
present-but-absent optional field is a legitimate observation, not a malformed
payload.

## Untyped nested shapes

Several fields keep their inner keys UNTYPED (`[]map[string]any`), mirroring
`codegraphv1.File.ParsedFileData`'s opacity precedent: `DataflowFunction`'s
`CFGBlocks`/`CFGEdges`/`DefUse`/`ControlDependencies` and
`InterprocEvidence.WhyTrail`. These are read-and-forward payloads the query
layer (`go/internal/query/code_flow_postgres.go`) forwards to API/MCP callers
verbatim, with no reducer field-level consumer; per-nested-shape typing is out
of scope for this migration. All five are typed as `[]map[string]any`, never a
fully open `[]any`: an open `[]any` generates an unconstrained `"items": true`
JSON Schema, a construct the collector conformance validator's supported
schema subset rejects (`sdk/go/collector/conformance`) — `[]map[string]any`
generates a constrained `"items": {"type": "object"}` shape instead, which the
validator accepts.

`FunctionSummary.ParamToSink` (`ParamSink`) and `.ParamToCallArg`
(`CallArgFlow`) ARE fully typed closed sub-structs (not opaque maps): the
postgres loader reconstructs `summary.Effects` from them field-by-field
(`go/internal/parser/summary.ParamSink`, `.CallArgFlow`), so their shape is
small and fully known, unlike the CFG/why-trail shapes above.

## Changing a struct

Any field change here is a payload-schema change.

- **Additive optional field**: a minor schema bump. Add the field, regenerate,
  and commit the schema in the same change.
- **Remove, rename, or narrow a field**: a major schema bump, needing a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `decode.go`), never a silent edit here.

Regenerate after any struct change:

```bash
cd sdk/go/factschema
go generate ./...
```

`schema_gen_test.go`'s `TestSchemasHaveNoDrift` fails the build on drift. Any
fixture pack copy under `../../fixturepack/schema/` must be refreshed in the
same change (`TestFixturePackSchemasMatchCanonical` locks the two).

## Telemetry

None. This package has no runtime, network, queue, graph, or telemetry
emission path.

## Gotchas / invariants

- The reducer decodes only the latest struct per fact kind. Older-schema-major
  shims live in the parent package's `decodeLatestMajor`, never here.
- `DataflowFunction` has no reducer materialization handler consuming it
  today — do not assume adding one is in scope for an unrelated change; see
  `doc.go` for the family's scope.
- These six kinds are deliberately NOT registered in
  `specs/fact-kind-registry.v1.yaml` (issue #4752 tracks that follow-up);
  registration couples to schema-version admission, a pipeline-wide change out
  of scope here.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning, §7 migration plan.
- Parent module `README.md` (`sdk/go/factschema/README.md`).
- `sdk/go/factschema/codegraph/v1/README.md` — the sibling family this
  package's precedent (opaque nested shapes, bare wire kinds) mirrors.
- `go/internal/collector/git_snapshot_dataflow_function.go`,
  `git_snapshot_function_summary.go`, `git_snapshot_function_source.go`,
  `git_snapshot_taint_evidence.go`, `git_snapshot_interproc_evidence.go`,
  `git_followup_facts.go` — the collector-side emitters for these six fact
  kinds.
