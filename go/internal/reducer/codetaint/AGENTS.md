# Agent instructions: internal/reducer/codetaint

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The `code_taint_evidence` and `code_interproc_evidence` reducer intent
handlers, their typed-decode + quarantine seam, row/edge projection, and
projected-node/-edge ledgers plus startup backfillers (issue #6061). Moved
out of the reducer root as ONE package, not two siblings, because the two
families are interleaved at the file level — see the README's Purpose
section for the exact symbol usage that makes `code_taint_evidence_typed_decode.go`
need splitting before the families could separate.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/codetaint/README.md`
- `docs/internal/design/package-restructure.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it for `CodeEvidenceHandlers` wiring
  and the two handler constructions, never the reverse.
- **`GraphQueryRunner` and `CodeValueFlowBackfillStateMarker` in
  `graph_ports.go` are deliberately re-declared, not imported from root.**
  Both interfaces are genuinely owned by root (shared with other
  still-in-root families), so importing them would violate the rule above.
  Go's structural typing makes the local declaration safe: do not "fix" this
  by adding a root import, and do not delete the local declaration without
  first hoisting the real one to a shared leaf package both sides import.
- **The ledger record must happen strictly before the graph write**, in
  both `CodeTaintEvidenceMaterializationHandler.Handle` and
  `CodeInterprocEvidenceMaterializationHandler.Handle`. Reordering breaks the
  ledger-is-a-superset-of-graph invariant the anchored-delete retract on the
  next generation depends on (issue #4893).
- **`ExtractCodeInterprocFixpointEvidenceRows` and
  `ExtractCodeInterprocEvidenceRows` use separate uid namespaces on
  purpose.** Unifying them would let a fixpoint-solved edge collide with a
  direct-fact edge in the graph writer's `MERGE`.
- **A malformed required field dead-letters as an input_invalid quarantine,
  never a silent drop or an empty-string row.** `function_uid` for taint;
  `source_function_uid`/`sink_function_uid` for interproc. This is the
  Contract System v1 Wave 4f S2 accuracy guarantee (issue #4754, epic #4566
  §1) — do not swallow a decode error to make a batch "succeed."
- **`DecodeCodeTaintEvidenceInput`/`DecodeCodeInterprocEvidenceInput` are
  exported ONLY because the reducer root's shared
  `codedataflow_bench_test.go` benchmarks them directly** (it also
  benchmarks the unrelated function-summary/source and shell-exec families
  and could not move into this package). Do not treat them as a public
  decode API for new callers — new production code should go through
  `ExtractCode*EvidenceRowsWithQuarantine`.

## Root-side test doubles this package's move required

`go/internal/reducer/codedataflow_evidence_test_helpers_test.go` (root) holds
a SEPARATE, hand-kept-in-sync copy of this package's writer/loader stubs
(`recordingCodeTaintEvidenceWriter`, `stubCodeTaintEvidenceLoader`,
`recordingCodeInterprocEvidenceWriter`, `stubCodeInterprocEvidenceLoader`,
the `sampleCode*Input`/`code*EvidenceEnvelope`/`code*EvidenceIntent`
builders) plus the `fakeBackfillStateMarker`/`splitPipeKey`/
`stringSlicesEqual` cluster this package's own
`code_interproc_projected_edge_backfill_test.go` also defines. Go test files
cannot share unexported symbols across packages, and root's
`defaults_code_taint_evidence_test.go` and the sibling
`projected_source_edge_backfill_test.go` family still need these shapes. The
sibling `valueflow` package keeps its OWN third hand-kept-in-sync copy
(`code_interproc_evidence_test_doubles_test.go`, scoped to just the
`recordingCodeInterprocEvidenceWriter`/`stubCodeInterprocEvidenceLoader`/
`sampleCodeInterprocInput` shapes its `value_flow_fixpoint_evidence_loader_test.go`
needs — it moved out of root in issue #6061 and, being a separate package,
cannot reach either the root or this package's unexported test doubles). If
you change a writer/loader interface's method set or a sample builder's
fields here, update the root copy AND the `valueflow` copy in the same
commit — nothing enforces any of them stay identical.

## Common changes

Adding a new evidence field to either fact kind: extend the `CodeTaintEvidenceInput`/
`CodeInterprocEvidenceInput` struct, the corresponding `Decode*` function,
and the `Extract*Rows` row-building function together, in
`code_taint_evidence_typed_decode.go` and `code_taint_evidence_rows.go` /
`code_interproc_evidence_rows.go`. Then update the matching envelope builder
in the root test-helpers file above if a root test exercises the new field.

## Failure modes to avoid

- Splitting this package into separate `codetaint`/`codeinterproc`
  siblings without first re-verifying the cross-family symbol usage in the
  README's Purpose section no longer holds — it held at move time (issue
  #6061) and is the reason the split did not happen then.
- Letting the root test-helpers copy (see above) silently diverge from this
  package's own test doubles when either side's interface or sample-builder
  shape changes.
- Adding a new caller of `DecodeCodeTaintEvidenceInput`/
  `DecodeCodeInterprocEvidenceInput` outside a benchmark/test context —
  route through `ExtractCode*EvidenceRowsWithQuarantine` instead so the
  quarantine/dead-letter contract stays enforced.

## Do not change without ADR review

- The evidence-source strings returned by `CodeTaintEvidenceSource()` /
  `CodeInterprocEvidenceSource()` / `CodeInterprocFixpointEvidenceSource()`
  — `cmd/reducer` wiring and the root stale-cleanup runner key retraction on
  these exact strings.
- The separate uid namespaces for direct vs. fixpoint interproc evidence.
