#6814 readiness-supersession anchor — validation record

Both reducer readiness gates bounded their wait on the claimed row's own
repair-cycle anchor (`ReadinessCycleAnchor` /
`awsCloudRuntimeDriftCycleAnchor`). That anchor resets whenever a newer
generation supersedes the waiting row, so with generation turnover faster
than the 30-minute bound and a persisting not-ready condition, the wait
restarted every generation and the bounded fallback never fired.

## Persistence answers (issue ask 1)

- `CheckProducerReadinessBeforeLoad` (`ci_cd_run_correlation`,
  `supply_chain_impact`): YES, can persist. A producer scope that never
  runs, stays failed, or never ingests for a consumer scope keeps every
  new consumer generation deferring; coordinator cadence (30s default)
  turns generations far faster than the 30m bound. Ops-qa
  `/admin/status` shows the turnover is real (3281 superseded
  generations observed).
- AWS runtime-drift state-pending defer: YES, can persist. A
  `state_snapshot` scope wedged mid-ingestion keeps
  `HasPendingStateSnapshotEvidence` true across every AWS generation with
  the same restart effect. Transient pending resolves on its own and is
  not the persistent shape.

## Fix

Both gates now anchor on the #6785 `(scope_id, domain)`
`reducer_readiness_waits` ledger: `CheckProducerReadinessBeforeLoadWithLedger`
plus `ApplyProducerReadinessPostLoad` (`crossscope/readiness_floor_ledger.go`),
and `checkAWSCloudRuntimeDriftReadinessBeforeLoadWithLedger` plus
`applyStatePendingWaitPostLoad` (`awscloud/aws_cloud_runtime_drift_wait.go`).
First-defer anchor with LEAST upsert semantics, settle-once past MaxWait of
the first wait, clear when the condition resolves. Nil ledger preserves the
exact pre-ledger per-row bound. No new SQL, schema, or queue semantics: the
ledger statements and the supersede CTE are live-proven by the #6785 suite
(`TestReadinessWaitSurvivesSupersessionLive`, store fence/clear live tests).

## No-Regression Evidence (#6814):

- Baseline (pre-fix behavior, reproduced): generation N (anchor T0) defers;
  generation N+1 (fresh anchor T0+25m) evaluated at T0+35m with the
  condition persisting DEFERS under the per-row anchor — only 10m elapsed
  on N+1's own row, so the 30m bound never fires. Recorded as failing
  assertions on ledger-ignoring implementations.
- After (post-fix, same input shape): N defers and records the ledger row
  anchored at T0; N+1 at T0+35m settles (proceeds; AWS commits its
  best-available verdict through the writer) and records the settle; a
  later same-set generation settles at once; resolution clears the row.
- Input shape: consumer intents N/N+1 as above; always-not-ready producer
  stub (gate 1) / always-pending state checker with one orphaned
  candidate (gate 2); in-memory ledgers mirroring the store's
  earliest-anchor rule and epoch fence; cmd wiring tests drive the real
  `buildReducerService` with the real ledger SQL strings.
- Terminal counts: 7 new tests (4 crossscope, 3 awscloud), each RED on the
  naive implementation for the intended reason and GREEN after; full
  `go test ./internal/reducer/... ./cmd/reducer/ -count=1` green with all
  pre-existing floor, drift, reopen-grace, and wiring suites unchanged;
  `gofmt`/`go vet`/`git diff --check` clean.
- Backend/version: hermetic unit proof (no live backend required — the
  store half is live-proven by #6785); NornicDB uninvolved (Postgres queue
  path, no graph writes); Go 1.27.1 toolchain.
- Why safe: decision mapping is pure (`DecideWait` unchanged); error
  plumbing only; duplicate same-generation re-evaluation rewrites nothing;
  stragglers fenced by anchor epoch; ledger read/write errors classify
  counting via `factload.ClassifyFactLoadError` (mirroring the probe-error
  path) so a sick store surfaces instead of starving; nil-ledger parity
  pinned by pre-existing tests.

## Observability Evidence (#6814):

- Defer lines keep their message and `elapsed_since_cycle_start`/`max_wait`
  (value now ledger-anchored when wired) plus `outcome=deferred`; gate 1
  gains a settle line with `outcome=abandoned`; failure classes
  (`cross_scope_producer_not_ready`,
  `aws_cloud_runtime_drift_state_pending`) durable on queue rows and
  counted by `eshu_dp_reducer_retry_surge_total{failure_class}`.
- `telemetry-coverage.md` rows for both gates extended with the new stage
  files; `scripts/verify-telemetry-coverage.sh` green.
- Follow-on after deploy: watch for `abandoned`/settle outcomes in the
  defer logs on ops-qa; per-gate live-Postgres supersession remains
  composed (not re-proven) coverage.
