# Open-failure diagnostic (#8486)

## Contract

`OpenFailureSummary`
(`go/internal/runtime/postgres/open_failure_diagnostic.go`) classifies an
`Open` error into redaction-safe tokens for the live-postgres-readiness
diagnostic log. An `Open` failure wraps its driver cause in `privateError`,
whose `Error` stays a fixed public string by design, so the standby
refused/timeout/not-ready distinction survives only inside `Unwrap`. The
summary reports closed tokens derived with `errors.Is` and `errors.As`
(`site`, `class`, four sentinel booleans, unwrap `depth`) plus the failure
class from the existing `readerFailureClass` verdict. It never formats cause
text, hostnames, DSNs, or SQL. The single call site is the live fleet test's
`Open`-failure branch, which logs the summary plus elapsed time against the
qualification budget, per-member attempt outcomes, and a bounded best-effort
standby probe before failing. There is no retry and no behavior change.

## Performance and observability evidence

The `perf-evidence` gate selects this change because
`go/internal/runtime/postgres/open_failure_diagnostic.go` sits under its
hot-file location. The touch is pure observability, so this note records
no-regression plus observability rather than a benchmark shaped like proof.

No-Regression Evidence: the new file adds one exported symbol, a pure
classifier over a local `strings.Builder`. It performs no I/O, takes no
lock, opens no connection, emits no SQL or Cypher, spawns no goroutine,
touches no channel, mutex, batch size, worker claim, lease, queue, or retry
budget, and changes no `Open` semantics: `Open` itself is untouched, and the
only call site runs on the failure path. Every success and steady-state path
compiles and runs the same instructions as before.

Baseline and after are the same program on all of those paths. The input
shape is synthetic and credential-free: `privateFailure`-wrapped sentinel
joins plus one DSN-shaped hostile string, exercised by five
`TestOpenFailureSummary*` unit tests (transient reader-ping, fatal
wrong-topology, depth count, nil, redaction). `go test
./internal/runtime/postgres/ -count=1` PASS (1.484s); the five classifier
tests pass 5/5 standalone and under `-race`; `go vet` on the touched
packages exits 0. No live backend is a variable here because no backend is
touched; there is no terminal queue depth or row count for this change to
move. A before/after latency measurement would be measuring identical
instructions, so none is recorded.

Observability Evidence: `TestOpenFailureSummaryKeepsMetadataPrivate`
proves a DSN-shaped cause surfaces none of its text under `%v`, `%s`,
`%+v`, or `%#v`. The recorded live observation is that the line fired on a
misconfigured fixture and classified it (wrong-topology fatal) with no
secret text. No production metric, span, status field, or telemetry label
is added, removed, or renamed; the existing closed-cardinality
`reader_borrow`, `reader_identity`, `reader_replay`, and `business_query`
signals are unchanged.

Why it is safe: failure-path-only call site, closed token sets with no
cause-text formatting, redaction pinned by unit test, full package suite
and vet green.
