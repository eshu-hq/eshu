# #6695 parity leaf: parity → conformance/parity move evidence

Rename-only nest per #6695 (`collector/parity` becomes
`collector/conformance/parity`, package `parity` unchanged) with a
documentation-only `conformance/` parent trio (future `contract` child
noted). Settlement from the issue body: zero non-test importers (only
its own `parity_test`), but a live self-contained conformance harness
driving the real `ClaimedService` path — move, not delete. `contracttest`
stays for its own leaf. Only self-references repointed (test import,
README test path, AGENTS.md title); the storage-collector-tree design
record already prescribes this destination.

No-Regression Evidence: `go test -count=1` on the harness package,
baseline origin/main `ee79771f3` vs leaf head, toolchain go1.27.1
darwin/arm64, same input shape (in-memory fixture suite, no credentials
or backend): baseline `parity` ok (0.576s); after move
`conformance/parity` ok (0.265s). `go test -list` discovers 15 tests on
both sides (identical set modulo the package path). `go vet` clean
recursive. Safe because the move is byte-identical except the import
path and doc-trio pointers; no logic, fixture, or contract change.

No-Observability-Change: the harness emits no facts, metrics, spans,
logs, status rows, or graph writes outside its in-memory readback model;
no telemetry-coverage row names this package.
