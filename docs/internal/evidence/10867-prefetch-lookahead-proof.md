# Bounded lookahead prefetch: local proof record (#10867)

Refs #10867. Branch `drive/10867-prefetch-ahead`
(worktree `eshu-worktrees/drive/10867-prefetch-ahead`).

## What changed

The batch claimer parks up to free-buffer-slots items in a hold-heartbeated
FIFO buffer (`go/internal/reducer/service_batch_prefetch.go`) instead of
handing each claim only to an idle worker. Pop order is stop-hold then worker
heartbeat (no overlap); dead holds evict without execute/ack/Fail. Depth 0
preserves the rendezvous path; `ESHU_REDUCER_BATCH_PREFETCH_DEPTH` defaults to
workers capped by claim size (explicit 0 disables). One deliberate deviation
from the design ruling: the claim bound uses free slots alone, not
idle-plus-free — a staged test proved the idle credit double-counts when a
signaled worker pops before the claimer drains its signal (claimed 5, want 4).

## No-Regression Evidence:

Local behavioral proof, worktree at `b9267c2336` (= `origin/main` at build
time), Go toolchain in the worktree:

- `go test ./internal/reducer/ -count=1` — ok, 3 consecutive runs (7.2s each).
  Covers the 14 new T1–T11 contract tests and every pre-existing batch test
  unmodified (acker extraction into `runBatchAckLoop` and the `executeAndReport`
  pre-start fence are proven by the existing ack/claim/observability suites).
- `go test -race ./internal/reducer/ -count=1` — ok (9.4s).
- `go test -race ./internal/reducer/ -run TestServiceRunBatch` — ok.
- `go test ./internal/reducer/contract/`, `go test ./internal/storage/postgres/
  -run 'TestClaimBatch|TestReducerQueueBatch'`, `go test ./cmd/reducer/
  -run TestLoadReducerBatch` (incl. 6 new loader tests), `go test
  ./internal/telemetry/`, `go test ./internal/envregistry/` (after regenerating
  the env-registry doc) — all ok.
- `golangci-lint run` on the four touched Go packages — 0 issues (after
  building `tools/golangci-lint-filelength/filelength.so` per checkout and
  splitting `runBatchConcurrentPrefetch` past the `funlen` gate).
- `git diff --check` clean; `service_batch*.go` and the new files all under
  the 500-line cap; `metrics-reducer-storage.md` held at exactly 500 lines by
  reflowing three wrapped prose lines (whitespace-only, prior-art d3f5c3b109).

No performance speedup is claimed. The before-measure is the #10862
baseline (busy-share 30.1%, claim mean ~3s, 0.86 items/s; see
`10862-qa-reducer-baseline-2026-11-18.md` in this directory). The
after-measure — busy-share, claim mean, throughput against that baseline —
is NOT_CHECKED here: it needs the deployed build and belongs to the
coordinator's live re-measure step, recorded as required proof before any
perf claim in the PR.

## Observability Evidence:

New instruments (all nil-guarded, all covered by emission tests):

- `eshu_dp_reducer_batch_buffer_depth` (UpDownCounter, `queue="reducer"`;
  +1 insert / -1 pop-or-drain; trajectory test pins 0 → B → 0).
- `eshu_dp_reducer_batch_buffer_dwell_seconds` (histogram, buckets
  0.01–60s; exactly one observation per executed buffered item, none for
  dropped).
- `eshu_dp_reducer_batch_buffer_hold_heartbeat_failed_total` (counter, by
  `domain`; +1 per eviction; evictions also record `ack_outcome_unknown`).
- `eshu_dp_reducer_batch_claim_size` and `eshu_dp_queue_claim_duration_seconds`
  emission unchanged (one observation per `ClaimBatch`, proven for the
  prefetch path by `TestServiceRunBatchPrefetchKeepsClaimSizeEmission`).

Registered in `go/internal/telemetry/instruments.go` (+ `instruments_test.go`
and `bucket_audit_test.go` entries), documented in
`docs/public/reference/telemetry/metrics-reducer-storage.md` and
`docs/public/observability/telemetry-coverage-reducer-core.md`, and surfaced
to operators via `ESHU_REDUCER_BATCH_PREFETCH_DEPTH` (`entries.go` +
generated env-registry page + `environment-variables.md`).

## No-Regression Evidence (follow-up comment-only fixes):

Baseline and after: `go test ./internal/reducer/ -run
'TestServiceRunBatchPrefetch' -count=1` green on both sides of the delta
(5.0s); `gofmt` clean on touched packages; `git diff --check` clean.
Backend: none touched (no query, plan, or lease change); input shape
unchanged; terminal queue and row counts untouched. Safe because the
delta holds zero non-comment lines, so compiled behavior is identical.

## No-Observability-Change (follow-up comment-only fixes):

No instruments, logs, spans, or dashboards touched; comment text only.
The three new instruments above are unchanged by the follow-up.
