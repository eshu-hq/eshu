# Git Dataflow Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the git collector's value-flow
("dataflow") fact family: `DataflowScanned` (`code_dataflow_scanned`),
`DataflowFunction` (`code_dataflow_function`), `FunctionSummary`
(`code_function_summary`), `FunctionSource` (`code_function_source`),
`TaintEvidence` (`code_taint_evidence`), `InterprocEvidence`
(`code_interproc_evidence`). It must remain independent from Eshu internals.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing any payload struct's fields, run `go generate ./...` from
  the module root and commit the regenerated schema under `../../schema/`
  AND its copy under `../../fixturepack/schema/`
  (`TestFixturePackSchemasMatchCanonical` locks the two).
- Run `go test ./... -count=1` from this directory's module root
  (`sdk/go/factschema`), `gofmt` on changed Go files, and `git diff --check`
  from the repo root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by
  the flat-struct convention required fields are also non-pointer and
  optional fields are pointers/slices/maps carrying `omitempty`. Both the
  schema generator (`../../internal/schemagen`) and the decode seam's
  required-field check (`../../decode.go`) derive that set reflectively from
  the struct's own tags via `../../fields.go` — there is no hand-maintained
  per-kind key list.
- The required set tracks **what the reducer or its postgres loader reads
  for identity/join/attachment**, not what the collector always emits. See
  `README.md`'s required-fields table for the per-kind rationale (`FunctionUID`
  for `TaintEvidence`, `SourceFunctionUID`/`SinkFunctionUID` for
  `InterprocEvidence`, `FunctionID` for `FunctionSummary`, `FunctionID`/`Kind`
  for `FunctionSource`, `RepoID`/`RelativePath`/`FunctionName` for
  `DataflowFunction`, nothing for `DataflowScanned`). Do NOT promote an
  emit-only field the reducer ignores to required — that dead-letters usable
  graph truth, violating Contract System v1's "don't drop right results"
  guarantee.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). A reducer handler or postgres loader receiving it
  must dead-letter the fact (or, for the postgres loaders in this family,
  skip it — see `code_taint_evidence_typed_decode.go`'s doc comment for why
  these particular call sites have no error return to propagate a per-fact
  failure through) rather than proceed with a zero-value struct pretending to
  be valid data.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- `DataflowFunction.CFGBlocks`/`CFGEdges`/`DefUse`/`ControlDependencies` and
  `InterprocEvidence.WhyTrail` keep their inner keys UNTYPED
  (`[]map[string]any`) read-and-forward payloads, mirroring
  `codegraphv1.File.ParsedFileData`'s opacity precedent. Do not add nested
  structs for these without discussing scope first — the query layer forwards
  them verbatim with no reducer field-level consumer. Do NOT retype any of
  these to a fully open `[]any`: the collector conformance validator's
  supported schema subset rejects the unconstrained `"items": true` shape an
  open `[]any` generates (`sdk/go/collector/conformance`); `[]map[string]any`
  is the minimum typed shape the validator accepts for "each element is an
  object, contents unmodeled."
- `FunctionSummary.ParamToSink` (`ParamSink`) and `.ParamToCallArg`
  (`CallArgFlow`) ARE fully typed closed sub-structs, not opaque maps — they
  mirror `go/internal/parser/summary.ParamSink`/`.CallArgFlow` exactly. Keep
  them in lockstep with that package's shape if it ever changes.
- This package defines exactly six fact kinds. A seventh dataflow-family fact
  kind, a nested `WhyTrail`/CFG struct, or a `v2` major is follow-on work
  gated on its own scoped change, not a casual edit here.
- None of these six kinds are registered in
  `specs/fact-kind-registry.v1.yaml` — registration is deferred to issue
  #4752 (it couples to schema-version admission, a pipeline-wide behavioral
  change out of scope for this package). Do not add a registry entry here
  without that issue's scope.
