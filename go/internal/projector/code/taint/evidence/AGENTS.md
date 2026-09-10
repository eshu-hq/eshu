# AGENTS.md — taint-evidence projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../../AGENTS.md` and `../../../README.md` for projector-wide invariants.
3. `../../../intent/AGENTS.md` for the neutral intent contract.
4. `../../../scope_generation_intents.go` for root-owned assembly order.
5. `go/internal/reducer/codetaint/` for reducer ownership.

## Invariants

- Import `internal/projector/intent`, never the root projector package.
- `BuildReducerIntent` fires on a `code_taint_evidence` finding, else on the
  `code_dataflow_scanned` marker. The finding outranks the marker regardless of
  input order.
- The marker fallback is required by #2919 so empty scans retract stale
  `CodeTaintEvidence` truth.
- Preserve both reason strings and `code_taint_evidence:<scope>`.
- `SourceSystem` is trimmed `CollectorKind`; do not substitute the two-tier
  `intent.SourceSystem` helper.
- Do not decode payloads or admit schema versions here. The reducer owns typed
  decode and quarantine.
- Do not move lookup construction, queue writes, retries, graph writes, or
  telemetry into this package.

## Verification

Run the package contract tests, root marker cases, ordered fan-out and
probe-count tests, the complete projector tree, package-doc checks, dirgate,
telemetry coverage, and the selected golden-corpus gates.

Do not widen the exported surface beyond `BuildReducerIntent`.
