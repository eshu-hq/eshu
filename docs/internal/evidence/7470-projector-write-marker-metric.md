# Projector write-marker wait metrics (#7470)

Change: `projector.MarkProjectionWriteStarted` takes the caller's `*telemetry.Instruments`
(nil allowed) and records `eshu_dp_projector_write_marker_deferrals_total{outcome}` once
per marker attempt deferred behind a busy generation row, and
`eshu_dp_projector_write_marker_wait_seconds{outcome}` once per marker that deferred at
least once. The projector service (`markProjectionWriteStarted`) and the bootstrap-index
drain (`markBootstrapProjectionWriteStarted`) pass their instruments. The marker calls,
their order, the 2 s store lock timeout, and the `DefaultWriteMarkerMaxAttempts` bound
(150) are unchanged; no queue, lease, SQL, or graph write changed.

No-Regression Evidence: Scratch Go benchmark (not committed) of
`MarkProjectionWriteStarted` with an in-memory marker that succeeds on the first call,
the path every uncontended marker takes, on the same local host and Go toolchain.
Baseline origin/main 1277f0429 against this branch at 1c47ffbb7 (the final code;
later commits change only this note and the bootstrap wiring test), 7 runs each at
2M iterations per run: main 11.77-11.83 ns/op (median ~11.8), branch 61.55-61.64
ns/op (median ~61.6), both 0 B/op and 0 allocs/op. The added `time.Now()`, deferred
closure, and string local therefore cost roughly 50 ns per marker call and add no
allocation. A real marker call is at least one Postgres transaction round trip with
a 2 s lock timeout, far above the ~0.06 microseconds measured here. The deferral
path adds one counter `Add` per 2 s lock timeout and one histogram `Record` per
waited marker. There is no backend, row, or queue count to compare: the change adds
in-process instrumentation only, and the focused tests show the same marker,
supersede, claim-lost, and error results as before (`go test ./internal/projector
./internal/telemetry ./cmd/bootstrap-index -count=1`).

Observability Evidence: Each deferral increments
`eshu_dp_projector_write_marker_deferrals_total` with a closed `outcome` label:
`retried`, `gave_up` (the retry bound ran out), or `shutdown` (the worker context
had ended). Each waited marker records `eshu_dp_projector_write_marker_wait_seconds`
(buckets 1-600 s) with its terminal `outcome`: `written`, `gave_up`, `shutdown`,
`superseded`, `claim_lost`, or `failed`. Markers that never wait record nothing.
Points use `context.WithoutCancel`, so shutdown deferrals are exported. The existing
WARN log `projector write marker waiting for busy generation row` is unchanged. Tests
in `go/internal/projector/write_marker_metric_test.go` and
`go/cmd/bootstrap-index/projector_write_marker_metric_test.go` assert the points;
replacing either caller's instruments with `nil` makes its wiring test fail.
