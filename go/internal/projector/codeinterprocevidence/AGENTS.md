# AGENTS.md — code-interproc-evidence projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants, including
   the rule that the projector never makes cross-source admission decisions.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after the code-taint-evidence probe and before the
   code-function-summary probe.
5. `go/internal/reducer/codetaint/` for what the reducer does with the intent
   this package enqueues, including typed decode, quarantine, the
   TAINT_FLOWS_TO edge writes between Function nodes, and the ledger-anchored
   stale-edge retraction the marker path exists for.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildCodeInterprocEvidenceReducerIntent` fires on a
  `code_interproc_evidence` finding, else on the `code_dataflow_scanned`
  marker. A finding always outranks the marker regardless of input order — the
  two kinds are looked up independently via `FirstOfKind`, and there is
  deliberately no cross-kind original-order merge. Do not "fix" this to
  `FirstAcrossKinds`: it changes `FactID` provenance for generations carrying
  both kinds.
- The marker fallback is load-bearing (#2919). Removing it means an empty
  finding set queues no intent, so stale TAINT_FLOWS_TO edges from a prior
  generation are never retracted.
- Keep the two reason strings and the `code_interproc_evidence:<scope>` entity
  key byte-identical. The reducer claims one intent per scope generation and
  reloads the generation's facts itself; the root fan-out parity fixture pins
  the marker-path values.
- `SourceSystem` is the trimmed `CollectorKind` alone — a single tier. Do NOT
  substitute the two-tier `projectorintent.SourceSystem`: it prefers
  `SourceRef.SourceSystem` when set and silently relabels the intent. The
  package test `TestBuildCodeInterprocEvidenceReducerIntentTrimsCollectorKind`
  fails on exactly that substitution.
- A `code_function_summary` fact must not trigger this family. Summaries go to
  the summary-persistence domain, and summary-driven fixpoint projection runs
  only after that handler persists durable summaries, sources, and graph ids.
- Do not decode a payload or check a schema version here. This builder reads
  only `FactKind`, `FactID`, and `CollectorKind`; the reducer handler owns
  typed decode and quarantine.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Changing a reason string or the entity key.** Both are asserted verbatim by
  the package tests and by the root fan-out parity fixture
  (`../scope_generation_intents_fanout_parity_test.go`); change them together.
- **Adding a trigger kind.** Decide explicitly whether it joins the finding
  tier or the marker tier, keep the finding-over-marker rule, and update the
  child tests plus the root dispatcher tests in
  `../code_interproc_evidence_projection_test.go` and
  `../code_taint_evidence_projection_test.go`.

## Failure modes

- **Route-serves-data registry path citations.** The registry in
  `go/internal/mcp/route_serves_data_registry_routes*.go` cites projector
  source files by path and greps them for a marker, so a rename breaks a test
  two packages away with a `read ...: no such file` error that never names
  this package. No registry entry cites this package today (verified with a
  positive control against the cloudinventory citations); if a route ever
  serves the interproc domain, its evidence entry will create that coupling
  here. The `sdk/go/factschema/codedataflow/v1/dataflow_scanned.go` doc
  comment DOES cite this package's `evidence_intents.go` by full path — keep
  that citation resolving on any rename.
- **A trigger fact with no collector kind.** The single-tier label yields an
  empty `SourceSystem` rather than falling back to `SourceRef.SourceSystem`
  or a literal default. That is the preserved pre-extraction behavior, not a
  bug to patch in passing.
- **Root dispatcher tests live outside this directory.** The wiring case is at
  root in `../code_interproc_evidence_projection_test.go`, and the #2919
  both-domains marker case is in `../code_taint_evidence_projection_test.go`.
  A change here can break them without touching any file in this directory.

## Anti-patterns

- Do not add a package-local source-system helper that forwards to
  `projectorintent.SourceSystem`; the two-tier fallback is a behavior change
  here, not a cleanup.
- Do not import the root `projector` package. Root imports this package to
  dispatch, and the reverse direction is an import cycle.
- Do not widen the export surface past
  `BuildCodeInterprocEvidenceReducerIntent`. Every sibling family in this
  series exports exactly one builder and no types.

## Changes needing ADR review

- Adding a decode seam. This family triggers on fact presence alone; families
  that need one keep a local decode call against `sdk/go/factschema` rather
  than importing root's wrapper, and that split is a design decision rather
  than a local call.
- Changing `reducer.DomainCodeInterprocEvidence`, the finding-over-marker
  rule, or the single-tier source-system label. All three are contract surface
  the reducer handler and the fan-out parity fixture assert against.

## Verification

Use TDD. Run the focused child tests, the root dispatcher tests in
`../code_interproc_evidence_projection_test.go` and
`../code_taint_evidence_projection_test.go`, the root ordered fan-out parity
and probe-count tests, package-doc verification, the projector package tree,
and the golden-corpus gates selected by the changed paths.
