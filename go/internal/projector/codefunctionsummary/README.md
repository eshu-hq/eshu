# Code-function-summary projector intents

## Purpose

This package recognizes value-flow function-summary evidence for one scope
generation and builds the reducer intent that asks the reducer to persist —
or, on the marker-only path, reconcile — durable CodeFunctionSummary graph
truth. The marker fallback lets an empty finding set still queue an intent so
the reducer can replace the repository's summary snapshot and prune summaries
for functions deleted or renamed out of the latest complete scan, instead of
leaving stale summaries in place because no finding arrived this generation.

## Ownership boundary

The package owns trigger selection, the best-effort repo_id derivation, and
the reducer-intent value. The root `internal/projector` package validates
scope-generation boundaries, constructs and owns the immutable fact lookup,
preserves family order, and owns projection lifecycle, queue writes, retries,
and telemetry. The reducer's `DomainCodeFunctionSummary` handler owns typed
payload decode, quarantine of malformed summaries, durable summary/source/
graph-id persistence, and the fixpoint TAINT_FLOWS_TO projection that runs
only after those stores are updated — none of that happens here.
CodeInterprocEvidence and CodeTaintEvidence findings belong to the
`codeinterprocevidence` and `codetaintevidence` sibling families and are not
this family's to reason about, even though all three watch the same
`code_dataflow_scanned` marker.

## Exported surface

- `BuildCodeFunctionSummaryReducerIntent` builds the `code_function_summary`
  intent, anchored to the earliest `code_function_summary` finding in
  original input order, else the earliest `code_dataflow_scanned` marker.

See `doc.go` for the full godoc contract, including the repo_id fallback and
`full_snapshot` payload rules.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup` and
`internal/projector/intent.ReducerIntent`; this package must not import the
root projector package — root already imports this package to dispatch to
it, so the reverse import would cycle. It reads `internal/facts` for the
fact-kind constants and `internal/reducer` for the domain constant. Unlike
`codeinterprocevidence` and `codetaintevidence`, this family carries its own
decode seam: `factschema_decode_codedataflow.go` decodes both
`code_function_summary` and `code_dataflow_scanned` payloads directly against
`sdk/go/factschema` through `internal/factenvelope.FactSchemaFromInternal`,
rather than importing root's decode wrapper (root's copy moved here with
this extraction — it had no other caller left behind).

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer handler retains `eshu_dp_reducer_executions_total`,
`eshu_dp_reducer_run_duration_seconds`, and the
`eshu_dp_reducer_input_invalid_facts_total` quarantine counter for the
`code_function_summary` domain. Moving the pure builder adds no queue,
storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- A `code_function_summary` finding always outranks the `code_dataflow_scanned`
  marker as `FactID`/`Reason` provenance, regardless of which appears earlier
  in the generation. The two kinds are looked up independently through
  `FirstOfKind`; there is no cross-kind original-order merge.
- The payload's `repo_id` is a two-step best effort: try the winning trigger's
  own decode first, and only when that resolves to `""` **and** both facts are
  present, fall back to the marker's `repo_id`. A summary-only generation with
  an unresolvable `function_id` prefix omits `repo_id` entirely rather than
  guessing.
- `full_snapshot` is set whenever the marker is present in the generation, even
  when a summary finding won provenance. It signals a complete repository scan
  ran this generation, independent of which fact anchors the intent.
- `SourceSystem` is the trimmed `CollectorKind` alone — a single tier, matching
  `codeinterprocevidence` and `codetaintevidence`. The two-tier
  `projectorintent.SourceSystem` is NOT a drop-in: it prefers
  `SourceRef.SourceSystem` when set, which would relabel the intent. The
  child test `TestBuildCodeFunctionSummaryReducerIntentTrimsCollectorKind`
  pins the single-tier behavior against exactly that substitution.
- The `Reason` strings (`value-flow function summaries observed` and
  `value-flow gate scanned; reconcile function summaries`) and the
  `code_function_summary:<scope>` entity key are pinned byte-identical by the
  package tests and the root fan-out parity fixture; one intent per scope
  generation either way.
- The root dispatcher tests stay at root: `scope_generation_intents_fanout_test.go`
  and `scope_generation_intents_fanout_parity_test.go` carry the end-to-end
  dispatch-order and payload-parity coverage for this domain.

## Verification

Run the package contract tests, ordered fan-out parity and probe-count tests,
the projector package tree, package-doc and path mirrors, dirgate, telemetry
coverage, and the golden-corpus gates selected by the changed paths.

No-Regression Evidence: this extraction moves one builder and its two-function
decode seam without changing the trigger, repo_id/full_snapshot payload rules,
entity key, reason strings, or fan-out position. The dispatcher's ordered
fan-out is unchanged at 44 builder probes with this probe still running
immediately after `codeinterprocevidence.BuildCodeInterprocEvidenceReducerIntent`
and immediately before `iamcanassume.BuildIAMCanAssumeMaterializationReducerIntent`.
Focused proof, run from the `go/` module root:
`go test ./internal/projector/codefunctionsummary -run TestBuildCodeFunctionSummaryReducerIntent -count=1`
and `go test ./internal/projector/... -count=1` green, whole-module `go build`
and `go vet` clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
