# Interprocedural-evidence projector intent

## Purpose

This package recognizes cross-function value-flow evidence for one immutable
scope generation and builds the reducer intent that materializes or reconciles
`TAINT_FLOWS_TO` graph truth. The marker fallback from #2919 lets an empty scan
retract stale edges from an earlier generation.

## Ownership

The package owns trigger selection and the reducer-intent value. Root
`internal/projector` owns the fact lookup, family order, lifecycle, queue
writes, retries, and telemetry. The reducer owns payload decode, quarantine,
edge writes, and ledger-anchored stale-edge retraction.

This remains an in-process package boundary. It depends on Eshu's internal
facts, projector-intent, and reducer contracts; the move does not make it
independently extractable.

## Contract

- `BuildReducerIntent` prefers `code_interproc_evidence`; the
  `code_dataflow_scanned` marker is the fallback.
- A finding always wins provenance regardless of cross-kind input order.
- `code_function_summary` does not trigger this domain.
- The entity key, reasons, fact ID, domain, and single-tier trimmed
  `CollectorKind` source label remain byte-for-byte compatible.
- This package does not decode payloads or check schema versions.

The package emits no telemetry directly. Root enqueue and reducer execution,
duration, and quarantine signals remain unchanged.

## Verification

From `go/`, run the three code-leaf contract packages together and then
`go test ./internal/projector/... -count=1`.

Related documentation:

- [Projector architecture](../../../README.md)
- [Intent contract](../../../intent/README.md)
- [Package restructure](../../../../../../docs/internal/design/package-restructure.md)
