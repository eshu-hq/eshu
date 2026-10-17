# 7127 PR-3c: ledger read for changed-since

PR-3c of #7127 answers `get_changed_since` from the link ledger when a link
chain covers the requested window, dark behind
`ESHU_CHANGED_SINCE_LEDGER_READ_ENABLED` (default off). With the flag off
the read path issues no ledger SQL. With it on, the attempt tries one link,
then a chain of links under a 200k stored-delta-row budget, then the legacy
two-generation diff; every attempt is counted on
`eshu_dp_changed_since_ledger_reads_total` and the wire contract never
changes.

Source binding: branch `feat/7127-pr3c-ledger-read`. Measurements below are
local only, on PostgreSQL 18.6 (Ubuntu 18.6-0ubuntu0.26.04.1, aarch64) with a
disposable database per test. No NornicDB and no Neo4j.

## Design as built

- Read statements (`read_sql.go`) are plain SELECTs by the full
  three-column key; no row lock, no advisory lock. The chain walk follows
  `prior_generation_id` pointers; a gap, branch, break, or rebase root is
  `prior_off_chain`.
- The composer (`changed_since_ledger_compose.go`) reduces touches to
  end-to-end entries: only the earliest prior and latest current state of
  each key matter. Unchanged is the final link's live total minus composed
  added and updated (the G1 oracle shape); removed keys go through a
  final-generation tombstone probe that mirrors the legacy tombstone join.
- The attempt runs in one read-only repeatable-read snapshot when the
  queryer opens one (`SQLDB`, `InstrumentedDB`); otherwise the cursor
  recheck and the delta-row completeness check guard statement-by-statement
  reads. A mid-read writer or retention commit can only fall back, never
  skew rows: the retention-interleave live test caught the pre-snapshot
  skew and now passes 10/10 under `-race`.
- Fallback reasons are closed (seven) and every fallback runs the legacy
  statement, so rows are exact served or not.

## No-Regression Evidence:

Flag off is byte-identical by construction: the legacy statement is
untouched and `TestComputeChangedSinceDeltaLedgerReadOffIssuesNoLedgerSQL`
fails if any query touches a ledger table. Focused suites green:
`go/internal/storage/postgres`, `freshness/links`, `telemetry`,
`envregistry`, `cmd/api`, `cmd/mcp-server`, `query/freshness`, plus the
live rows-equal, fallback, locks, and interleave proofs with
`ESHU_POSTGRES_TEST_DSN` on PostgreSQL 18.6.

Local statement inventory and wall time on the G1 accuracy fixture (12
fact rows x 3 generations, warm cache, medians of 20 reads; absolute
microseconds are fixture-small, only the shape transfers):

| window | legacy statements | legacy median | ledger statements | ledger median |
| --- | --- | --- | --- | --- |
| single link | 3 | 593 us | 10 | 406 us |
| chain of two | 3 | 171 us | 11 | 359 us |

The ledger path issues more statements (cursor x2, walk, per-link deltas,
tombstone probe when keys were removed, one unchanged-samples read per
category) but each is a primary-key or narrow indexed read; the legacy
path re-hashes both generations. At 772k keys the banked theory proof
measured 6.78 s legacy against 0.61 ms single-link and 140 ms two-link
chain reads (2026-10-16 comment on #7127, local shim, not re-run here).
Deployed p95 on the 12k-file scope is NOT measured from this host and
stays with the dark-window lane; this PR is blocked on BIN1 #7401 and
does not merge until then.

## Observability Evidence:

- `eshu_dp_changed_since_ledger_reads_total` (label `outcome`:
  `ledger_single`, `ledger_chain`, `fallback_<reason>`), counted once per
  flag-on `ComputeChangedSinceDelta` in `changed_since_ledger_read.go`;
  nil-safe. Registered in `instruments_changed_since.go`, covered by
  `docs/public/observability/telemetry-coverage.md` (changed-since row)
  and `scripts/verify-telemetry-coverage.sh` exit 0.
- A `ledger_error` fallback also logs WARN `changed-since ledger read fell
  back to the legacy diff` with `scope_id`, `current_generation_id`,
  `reason`, and `error`. Ordinary fallbacks (unlinked, off-chain, moved
  cursor, digest, same generation, too large) log nothing; the counter
  carries them.
- Serve-rate follow-up for the dark window: `ledger_single` +
  `ledger_chain` share of all outcomes per scope shape. Under ~95% served,
  PR-3c alone cannot meet the <1 s acceptance and the bounded/paged
  contract goes back to the arbiter (theory-proof stop rule); every
  fallback still costs the legacy 11-18 s on the large scope.
