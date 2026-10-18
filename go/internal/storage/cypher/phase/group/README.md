# Phase Group Execution Helpers

## Purpose

This package holds the shared helpers every backend phase-group executor
runs: stripping `_eshu_*` diagnostic parameters before statements reach a
driver, detecting all-retract phases (which must run as sequential
auto-commit statements on NornicDB, never one managed transaction), running
profiled sequential statement groups, and classifying file statements for
the opt-in files-phase probe.

## Ownership boundary

The leaf owns the helpers. The parent `cypher` package owns the `Statement`
type, the operation vocabulary, and the statement metadata keys and phase
values the helpers reason about. Backend executors in `cmd/ingester`,
`cmd/bootstrap-index`, and `storage/nornicdb` own transactions and routing.

## Exported surface

- `SanitizeStatementParameters` / `SanitizeStatement` / `SanitizeStatements`
  — strip diagnostic metadata keys, preserving order and the no-alloc path
- `StatementsAllUseOperation` — all-retract phase detector
- `StatementRunner` / `ExecuteProfiledStatementGroup` — ordered profiled
  execution inside the caller-owned boundary
- `CanonicalFileStatementProfile` — closed file-template diagnostic allowlist

See `doc.go` for the package contract.

## Dependencies

- `internal/storage/cypher` (parent) — `Statement`, `Operation`, metadata keys
- `internal/storage/cypher/canonical/node` — file template constants for the
  profile allowlist

## Telemetry

`ExecuteProfiledStatementGroup` logs per-statement attempt timing (index,
count, duration, operation, phase, label, summary, row count) when profiling
is enabled; it never changes transaction ownership. Sanitization itself is
uninstrumented.

## Gotchas / invariants

- Sanitization is load-bearing on NornicDB, not hygiene: an unreferenced
  parameter on a grouped `DETACH DELETE` can make the delete silently no-op.
- The profile allowlist is closed: a new file template without an entry is
  excluded from the probe until classified here.

## Performance Evidence

No-Regression Evidence: pure Pattern-B leaf move from the `cypher` root
(#6694 slice). The helpers carry no Cypher templates; the sanitize and
detector behavior plus the six file-profile classifications are identical to
the pre-move root versions, proven by the RED/GREEN behavior dump diff
recorded in the move PR (empty diff) and the moved contract tests.

No-Observability-Change: profile logging, parameter handling, and execution
ordering are unchanged; this package adds no metric name, label, worker,
queue domain, knob, backend branch, or new graph-write route.

## Related docs

- `docs/public/reference/cypher-performance.md`
- `go/internal/storage/cypher/README.md`
