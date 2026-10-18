# 7585 PR3 no-regression note: live-signal telemetry (Refs #7585)

Status: **no-regression by construction, with a live plan/cost measurement
of the unchanged report statement.**

## No-Regression Evidence:

- Baseline: PR2 state (`SelectExpiredGenerations` pure read, no caller, no
  telemetry). After: PR3 wires optional tracer/instruments/logger onto
  `GenerationRetentionStore` and records around the same read. No
  production caller invokes the report yet (verified: only its definition
  and tests reference `SelectExpiredGenerations`), so no new statement
  executes on any runtime path. The report SQL text is byte-identical to
  PR2 — PR3 adds zero SQL, zero deletion behavior, zero migration, and
  zero caller wiring changes.
- The wiring is nil-safe by construction: nil instruments skip the
  metrics, nil tracer skips the span, nil logger skips the logs, and the
  read itself is untouched on all three paths
  (`TestSelectExpiredGenerationsTelemetryNilSafe`). Validation rejections
  (nil database, hard ceiling below the soft window) return before the
  read and record nothing: they are caller bugs, not store failures.
- Telemetry cost per report is bounded and I/O-free: one duration sample,
  at most one counter sample per expired-but-blocked row (capped by the
  report `LIMIT`), one span event and one log line per blocked row, one
  summary log line. No lock, lease, queue, worker, or retry path is
  touched — the concurrency surface is unchanged from PR2's lock-free
  single-snapshot `SELECT`.
- Proof: focused retention suite (`TestEvaluateGenerationRetention*`,
  `TestSelectExpiredGenerations*`, `TestExpiredBlockedReasonLabel*`)
  green in `go/internal/storage/postgres`; full `go/internal/telemetry`
  suite green (new span, new instruments, frozen contract lists);
  `go vet` clean on both packages; `gofmt` clean; telemetry-coverage
  verifier green. The counter-value assertions are mutation-proven (a
  wrong reason label fails the boundary test).
- Live plan/cost of the unchanged statement, measured on a throwaway
  PostgreSQL 18.6 cluster (Homebrew aarch64, trust auth, `/tmp` data
  dir, destroyed after): 200 scopes, 12,200 generations (6,000 past the
  90-day ceiling), 501,200 work items after scaling. The statement was
  extracted programmatically from the shipped Go constant (no hand copy),
  run with cutoff `now() - 90 days`, `LIMIT 100`:
  - `EXPLAIN (ANALYZE, BUFFERS)` x3: execution 62.7–63.5 ms at 1,200
    work items, 81.2 ms at 501,200 work items; planning ~1.1 ms;
    buffers shared hit 448 (~3.6 MB) rising to 1332 (~10.7 MB), all
    reads, no WAL (SELECT only).
  - Expired fetch: Bitmap Index Scan on `(status, superseded_at)`,
    ~0.4 ms for 6,000 expired rows. Live-work `EXISTS` probe: Index
    Only Scan on the `(scope_id, generation_id, status)` prefix,
    ~0.007 ms x 100 loops — the report `LIMIT` bounds the probe count.
    Retrying `EXISTS` probe index-served (~6 ms at this shape).
  - Dominant cost (~48 ms) is the `MATERIALIZED`
    last_activated_full_write CTE: one seq scan plus `DISTINCT ON` sort
    over non-delta activated history. It scales with activated history,
    not with the expired backlog, and takes no lock — the same shape
    PR2 documented as safe.
  - Live result check: 100 rows returned (the `LIMIT`), oldest first,
    all carrying live-work flags at this fixture distribution, as the
    `ORDER BY superseded_at ASC, generation_id ASC` requires.
  - Caveat: the fixture's `(status, superseded_at)` helper index stands
    in for production's serving index; the production parity argument
    remains PR2's term-for-term mirror of the candidate selection
    guards, whose text PR3 does not touch.

## Observability Evidence:

- New: `eshu_dp_generation_retention_expired_blocked_total` (counter by
  closed `reason`: active_current, live_work, retrying, dependency_pin;
  blocked rows only — eligible and scoped-out #7472/#7802 rows emit no
  sample) and
  `eshu_dp_generation_retention_expired_report_duration_seconds`
  (histogram, one sample per successful report, no labels). Failures
  reuse `eshu_dp_generation_retention_failures_total` with reason
  `expired_report_error`.
- New span `generation_retention.expired_report` (report counts as span
  attributes; one `generation_retention.expired_blocked` event per
  blocked generation with scope_id, generation_id, reason, age). New
  logs: per-blocked-generation `generation retention expired generation
  blocked`, per-report `generation retention expired report completed`
  summary, and `generation retention expired report failed` with
  `failure_class=generation_retention_expired_report_error`.
- Scope and generation identifiers appear in span events and logs only,
  never in metric labels (`generation_id` is a hard-banned dimension;
  the cardinality audit and the per-sample identity-label test pin
  this). Operator reference updated in
  `docs/public/reference/telemetry/metrics-reducer-storage.md`; coverage
  row updated in `docs/public/observability/telemetry-coverage.md`.
- Read-only duplicate reconciliation (#7472 uncovered / #7802
  delta-protected) stays scoped out with no blocked reason, as directed:
  scoped-out rows ride the report visibly but emit no counter sample,
  and the dependency_pin slot stays reserved with no live signal.
