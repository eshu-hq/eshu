# Function-summary projector intent

## Purpose

This package recognizes function-summary evidence for one immutable scope
generation and builds the reducer intent that persists or reconciles durable
`CodeFunctionSummary` graph truth. The scan-marker fallback lets an empty
finding set prune summaries for functions removed by the latest complete scan.

## Ownership

The package owns trigger selection, trigger-time typed decode for best-effort
repo-ID derivation, and the reducer-intent value. It does not admit schema
versions or quarantine malformed facts. Root `internal/projector` owns the fact
lookup, family order, lifecycle, queue writes, retries, and telemetry. The
reducer owns materialization-time decode, quarantine, persistence, and fixpoint
`TAINT_FLOWS_TO` projection.

This remains an in-process package boundary. It depends on Eshu's internal
facts, projector-intent, and reducer contracts, plus the public factschema SDK;
the move does not make it independently extractable.

## Contract

- `BuildReducerIntent` prefers the earliest `code_function_summary` finding;
  the earliest `code_dataflow_scanned` marker is the fallback.
- A finding always wins provenance regardless of cross-kind input order.
- `repo_id` comes from the winning fact, then falls back to the marker when
  both facts exist and the summary repo ID cannot be resolved.
- `full_snapshot` is true whenever the marker is present.
- Malformed payloads omit `repo_id`; they do not suppress the intent.
- `SourceSystem` is trimmed `CollectorKind` only.

The package emits no telemetry directly. Root enqueue and reducer execution,
duration, and quarantine signals remain unchanged.

## Verification

From `go/`, run:

```bash
../scripts/go-test-run-guard.sh 20 '^TestBuildReducerIntent' -- \
  ./internal/projector/code/function/summary \
  ./internal/projector/code/interproc/evidence \
  ./internal/projector/code/taint/evidence -count=1
go test ./internal/projector/... -count=1
```

Related documentation:

- [Projector architecture](../../../README.md)
- [Intent contract](../../../intent/README.md)
- [Package restructure](../../../../../../docs/internal/design/package-restructure.md)
