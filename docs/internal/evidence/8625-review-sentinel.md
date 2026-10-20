# #8625 review sentinel — performance and observability evidence

No-Regression Evidence: named `batchDropUnassignedWorkerID` sentinel
(`go/internal/reducer/service_observability.go`, beside the ack-status
constants) replacing the two inline `workerID: -1` literals at the
pre-handoff (#8623) and mid-handoff (#8625) drop sites in
`go/internal/reducer/service_batch.go`. Source spelling only.

- Baseline: two `workerID: -1` literals with per-site comments.
- After: one untyped `-1` constant used at both sites; grep confirms no
  remaining `workerID: -1` literal in the package; whole
  `go/internal/reducer` package green (`-count=1`); `go vet` and `gofmt`
  clean.
- Backend/version: in-process Go test only (`go1.26.9 linux/arm64`); no
  Neo4j/Postgres backend involved — no runtime path executes differently.
- Input shape: not applicable — no branch, loop, or call target changes.
- Terminal counts: not applicable — emitted `worker_id` values are
  byte-identical (untyped constant into the same int field).
- Why safe: the constant folds to the same `-1` at both sites; the only
  observable change is in source text. No worker count, batch size, poll
  interval, lease, or queue change. No benchmark delta to measure.

No-Observability-Change: the sentinel adds no metric instrument, metric
label, span, log key, route, graph query shape, queue table, worker, lease,
or runtime knob. Drop records keep emitting `worker_id: -1` through the
pre-existing `recordBatchAckOutcome` path. Operators keep diagnosing the
path through the existing reducer run spans, execution counters, and
Postgres query instrumentation.
