# Projector Ack-wait metrics (#6803)

Change: `projector.AckWhenScopeFree` takes the caller's `*telemetry.Instruments`
(nil allowed) and records `eshu_dp_projector_ack_deferrals_total{outcome}` once
per Ack deferred behind a same-scope ingestion commit, and
`eshu_dp_projector_ack_wait_seconds{outcome}` once per Ack that deferred at
least once. The projector service (`processWork`) and the bootstrap-index drain
(`drainProjectorWorkItem`) pass their instruments. The Ack/Heartbeat calls, their
order, the 2 s store lock timeout, and the `DefaultAckWaitMaxRetries` bound
(150) are unchanged; no queue, lease, SQL, or graph write changed.

No-Regression Evidence: Scratch Go benchmark (not committed) of
`AckWhenScopeFree` with an in-memory sink whose Ack succeeds on the first call,
the path every normal Ack takes, on the same local host and Go toolchain.
Baseline origin/main 55117d14f against this branch at eea916ed6 (the final
code; later commits change only this note), 7 runs each: main 188-230 ns/op
(median ~198), branch 219-281 ns/op (median ~237), both 288 B/op and
5 allocs/op. An independent reviewer's 6-run re-measurement on the same pair
gave medians ~279 and ~301 ns/op with the same allocations. The added
`time.Now()`, deferred closure, and string local therefore cost roughly
20-40 ns per Ack and add no allocation. A real Ack is at least one Postgres
transaction round trip, far above the ~0.2-0.3 microseconds measured here. The
deferral path adds one counter `Add` per 2 s lock timeout and one histogram
`Record` per waited Ack. There is no backend, row, or queue count to compare:
the change adds in-process instrumentation only, and the focused tests show the
same Ack, Heartbeat, and error results as before (`go test ./internal/projector
./internal/telemetry ./cmd/bootstrap-index ./cmd/projector ./cmd/ingester
-count=1`, plus `-race -count=3` on the Ack-wait tests).

Observability Evidence: Each deferral increments
`eshu_dp_projector_ack_deferrals_total` with a closed `outcome` label:
`retried` (counted before the lease renewal), `abandoned` (the retry bound ran
out), or `shutdown` (the worker context had ended). Each waited Ack records
`eshu_dp_projector_ack_wait_seconds` (buckets 1-600 s) with its terminal
`outcome`: `succeeded`, `abandoned`, `shutdown`, `superseded`, `claim_lost`, or
`failed`. Acks that never wait record nothing. Points use
`context.WithoutCancel`, so shutdown deferrals are exported. The existing WARN
logs `projector ack waiting for busy scope` and `projector ack abandoned after
waiting for busy scope` are unchanged. Tests in
`go/internal/projector/service_ack_wait_metric_test.go` and
`go/cmd/bootstrap-index/projector_ack_wait_metric_test.go` assert the points;
replacing either caller's instruments with `nil` makes its wiring test fail.
