# Code-taint-evidence projector intents

## Purpose

This package recognizes value-flow taint evidence for one scope generation and
builds the reducer intent that asks the reducer to materialize — or, on the
marker-only path, reconcile and retract — CodeTaintEvidence graph truth. The
marker fallback is the fix for #2919: without it an empty finding set queues no
intent and stale taint evidence from a prior generation leaks.

## Ownership boundary

The package owns only the trigger selection and the reducer-intent value. The
root `internal/projector` package validates scope-generation boundaries,
constructs and owns the immutable fact lookup, preserves family order, and
owns projection lifecycle, queue writes, retries, and telemetry. The reducer's
`DomainCodeTaintEvidence` handler owns typed payload decode, quarantine of
malformed findings, CodeTaintEvidence node and TAINT_FLOWS_TO edge writes, and
stale-evidence retraction; none of that happens here.

## Exported surface

- `BuildCodeTaintEvidenceReducerIntent` builds the `code_taint_evidence`
  intent, anchored to the earliest `code_taint_evidence` finding in original
  input order, else the earliest `code_dataflow_scanned` marker.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup` and
`internal/projector/intent.ReducerIntent`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. It reads `internal/facts` for the two
fact-kind constants and `internal/reducer` for the domain constant. There is
no decode seam: like `packagesource` and `cloudinventory`, this builder reads
only envelope fields and never a payload key.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer handler retains `eshu_dp_reducer_executions_total`,
`eshu_dp_reducer_run_duration_seconds`, and the
`eshu_dp_reducer_input_invalid_facts_total` quarantine counter for the
`code_taint_evidence` domain. Moving the pure builder adds no queue, storage,
graph, span, metric, or log boundary.

## Gotchas / invariants

- A finding always outranks the marker, regardless of which appears earlier in
  the generation. The two kinds are looked up independently through
  `FirstOfKind`; there is no cross-kind original-order merge, unlike the
  families that anchor with `FirstAcrossKinds`.
- The `Reason` strings (`value-flow taint evidence observed` and
  `value-flow gate scanned; reconcile taint evidence`) and the
  `code_taint_evidence:<scope>` entity key are pinned byte-identical by the
  package tests and the root fan-out parity fixture; one intent per scope
  generation either way.
- `SourceSystem` is the trimmed `CollectorKind` alone — a single tier. This
  family never carried a local source-system helper, and the two-tier
  `projectorintent.SourceSystem` is NOT a drop-in: it prefers
  `SourceRef.SourceSystem` when set, which would relabel the intent. The child
  test `TestBuildCodeTaintEvidenceReducerIntentTrimsCollectorKind` pins the
  single-tier behavior against exactly that substitution.
- The payload is never decoded here and no schema version is checked here; the
  reducer handler owns typed decode and quarantine.
- The root dispatcher tests that go through `buildProjection` stay at root in
  `../code_taint_evidence_projection_test.go`, including the #2919 case that
  proves the marker enqueues BOTH the taint and interproc retraction domains.

## Verification

Run the package contract tests, ordered fan-out parity and probe-count tests,
the projector package tree, package-doc and path mirrors, dirgate, telemetry
coverage, and the golden-corpus gates selected by the changed paths.

No-Regression Evidence: this extraction moves one builder without changing
its trigger, value, or fan-out position. The reducer intent domain, entity
key, reason strings, finding-over-marker anchor rule, and source-system
derivation are identical to the base commit, and the dispatcher's ordered
fan-out is unchanged at 44 builder probes with this probe still running
immediately after
`incidentrouting.BuildIncidentRoutingMaterializationReducerIntent` and
immediately before `buildCodeInterprocEvidenceReducerIntent`. The family
carried no private source-system helper: the moved body keeps its original
single-tier `strings.TrimSpace(trigger.CollectorKind)` expression verbatim
rather than substituting the two-tier `projectorintent.SourceSystem`, and the
root `firstOfKind` forwarder it called was a direct delegate to
`projectorintent.FactLookup.FirstOfKind`, so that substitution is
behavior-identical by construction; the forwarder stays at root for its
remaining callers. Focused proof, run from the `go/` module root:
`../scripts/go-test-run-guard.sh 6 'TestBuildCodeTaintEvidenceReducerIntent' -- ./internal/projector/codetaintevidence -count=1`
and `go test ./internal/projector/... -count=1` green, whole-module `go build`
and `go vet` clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
