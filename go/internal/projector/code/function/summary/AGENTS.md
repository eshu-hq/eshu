# AGENTS.md — function-summary projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../../AGENTS.md` and `../../../README.md` for projector-wide invariants.
3. `../../../intent/AGENTS.md` for the neutral intent contract.
4. `../../../scope_generation_intents.go` for root-owned assembly order.
5. `go/internal/reducer/code_function_summary_materialization.go` for reducer ownership.

## Invariants

- Import `internal/projector/intent`, never the root projector package.
- `BuildReducerIntent` fires on a `code_function_summary` finding, else on the
  `code_dataflow_scanned` marker. The finding outranks the marker regardless of
  input order.
- When both facts exist and the summary cannot yield a repo ID, fall back to
  the marker repo ID. Keep this logic in `triggerRepoID`.
- Set `full_snapshot` whenever the marker exists, even when the finding supplies
  provenance.
- Preserve the reason strings and `code_function_summary:<scope>` entity key.
- `SourceSystem` is trimmed `CollectorKind`; do not substitute the two-tier
  `intent.SourceSystem` helper.
- Keep `factschema.FactKindCodeFunctionSummary` and
  `factschema.FactKindCodeDataflowScanned` references inside the decode
  functions in `factschema_decode.go`; the payload-usage gate scans them.
- Do not move lookup construction, queue writes, retries, graph writes, or
  telemetry into this package.

## Verification

Run the package contract tests, root fan-out and probe-count tests, the complete
projector test tree, payload-usage verification, package-doc checks, dirgate,
telemetry coverage, and the golden-corpus gates selected by changed paths.

Do not widen the exported surface beyond `BuildReducerIntent`.
