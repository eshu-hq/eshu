# #8948: redrive event_name attrs — no-regression and observability evidence

Issue #8948 follow-up: the five existing redrive-path slog calls had no
`event_name` key, so the failure/progress signals were not queryable by the
log pipeline convention (`telemetry.EventAttr`, cf. `cmd/api/wiring.go`).
The change adds one `telemetry.EventAttr(...)` argument to each of the five
calls and one import. No control flow, SQL, lock, lease, queue, metric, or
dashboard change.

## Classification

Observability label on existing emissions, not a performance change. Every
path that logged before executes identical code plus one `slog.Attr`
construction on a log call that already allocates its args. The five sites
are per-sweep or per-failure emissions (live-hook failure, catch-up
tick failed/completed, edge-confirmation failure, ledger-record failure),
never a per-row hot loop: the per-target fan-out emits only through the
unchanged `recordSweep`/`recordTargetsAndPages` counters.

## No-Regression Evidence: unchanged emission behavior on the proof head

Backend: local PostgreSQL 16, disposable database, single-connection proof
harness (`crossplaneRedriveProofDB`).

| Run | Result |
| --- | --- |
| `go test -tags integration -race ./internal/storage/postgres/crossplane/ -count=1` (9-subtest parent incl. 2 new proofs) | ok, 84.984 s, zero failures, zero data races |
| `go test -count=1 ./internal/reducer/crossplane/` | ok |
| `go vet` on `crossplane`, `reducer/crossplane`, `cmd/projector` | clean |
| `gofmt -l` on all touched files | clean |
| `scripts/verify-telemetry-coverage.sh` (`ESHU_TELEMETRY_COVERAGE_BASE=origin/main`) | agree, no new untracked stages |

Why safe: the diff is +6 lines (five attr args, one import). No new
emission point, no label cardinality change (all values are constants),
no hot-path allocation beyond the pre-existing log args.

## Observability Evidence: queryable redrive signals

- `crossplane.redrive.sweep_failed` (`projector_queue_ack.go`,
  `runCrossplaneRedriveHook`) — live-hook failure, pairs with the
  existing `eshu_dp_crossplane_redrive_sweeps_total{outcome=sweep_error}`.
- `crossplane.redrive.catchup_failed` / `crossplane.redrive.catchup_completed`
  (`cmd/projector/crossplane_redrive_catchup.go`) — catch-up tick outcome.
- `crossplane.redrive.ledger_record_failed` and
  `crossplane.satisfied_by.edge_confirmation_failed`
  (`satisfied_by_materialization.go`) — handler-side failure gates.
- Existing counters and state rows already covered failure and progress;
  these keys only make the same emissions filterable. No dashboard change
  per the standing operator-dashboard decision in the crossplane
  `AGENTS.md`.
