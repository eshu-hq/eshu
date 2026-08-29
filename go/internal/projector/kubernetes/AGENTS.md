# AGENTS.md — Kubernetes projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order.

## Invariants

- Import `internal/projector/intent`, never the root projector package.
- Preserve the shared workload-materialization acceptance key for workload
  nodes and `RUNS_IMAGE` edges.
- Select the earliest pod-template fact and keep source-ref precedence with a
  trimmed collector-kind fallback.
- Emit complete empty namespace reconciliation only for a Kubernetes live
  cluster scope with a nonblank cluster ID; partial empty snapshots emit none.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Use TDD. Run focused child and root Kubernetes tests, ordered fan-out parity,
package-doc verification, the projector package tree, and the golden-corpus
gates selected by the changed paths.
