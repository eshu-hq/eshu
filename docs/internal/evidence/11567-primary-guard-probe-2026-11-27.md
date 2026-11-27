# #11567 primary-side guard probe: CVE-2026-26996 vs the 10 s production guard

Refs #11567 (Part of #11563). Date: 2026-11-27. Context: the #11563 QA
contention-cut
([results](11563-qa-contention-cut-results-2026-11-26.md),
[manifest](11563-qa-contention-cut-manifest-2026-11-26.md)).

## Summary

The QA cut found that id `CVE-2026-26996` (planner MCV #1, ~286k estimated
rows) never completes serially on the QA read replica (3/3 > 15 s). This
probe asked whether the primary serves it inside the production 10 s guard:
it does not — one serial exec against the primary fired the 10 s
statement timeout at 10.051 s. The exclusion is not a replica artifact, so
the follow-up is a planner/cost fix for the shipped holder statement, not
further replica work.

## Question

Does the PRIMARY serve id `CVE-2026-26996` inside the production 10 s guard
(`supplyChainImpactFanOutReadTimeout`, `go/internal/storage/postgres/supply_chain_impact_fanout.go`)?

## Method (read-only; no deploy, no mutation, no knob moves)

- Target: pod `eshu-postgresql-0` (`ns eshu`) via
  `kubectl -n eshu port-forward pod/eshu-postgresql-0 54400:5432`.
  Identity check `SELECT pg_is_in_recovery()` returned `f`: this is the primary.
- Credentials: `kubectl -n eshu get secret eshu-postgresql-auth` (key names
  only recorded; value never recorded), `eshu` role DSN repointed at the
  local forward. Belt and suspenders on the cap: `PGOPTIONS='-c
  statement_timeout=10s'` on the client AND the production in-txn
  `SELECT set_config('statement_timeout', '10000ms', true)`.
- Envelope mirrors `readScopeIDsIn` exactly: `BEGIN`, `SET TRANSACTION
  ISOLATION LEVEL REPEATABLE READ, READ ONLY` (=
  `setHolderKeyRangeSnapshotQuery`, the first statement of every supports
  read), in-txn 10 s guard, server-side `PREPARE holder_probe(text[], text)`
  carrying the byte-identical `FindSupplyChainImpactHoldersQuery`, then
  `EXECUTE holder_probe('{CVE-2026-26996}', 'scope:none')` (`scope:none` is
  the production dummy own-scope used by the live contention tests), then
  `ROLLBACK`. Rows discarded client-side (`-o` file); the server still
  produces the full answer set, as production does.
- Minimal attempts: one serial exec; on a guard breach record and stop
  (the shared primary is already near request saturation — no retry storm).

## Measurement

Attempt 1 (serial, cold w.r.t. this session; the ONLY exec attempt):

- `SELECT pg_is_in_recovery();` -> `f`, exit 0 (primary confirmed).
- `EXECUTE holder_probe(...)` -> `ERROR: canceling statement due to
  statement timeout`, server-side time **10051.272 ms (00:10.051)**,
  `probe_exit=3` (`ON_ERROR_STOP`). The id does NOT complete inside the
  10 s production guard on the primary either.

No p50/p95 exists: a single timed sample that fires the guard is the whole
exec record, by design (see Minimal attempts above).

## Plan shape (EXPLAIN without ANALYZE — planning only, executes nothing)

`EXPLAIN (COSTS ON) EXECUTE holder_probe('{CVE-2026-26996}', 'scope:none')`,
same read-only envelope, exit 0, ~39 ms planning:

- `CTE candidates` MATERIALIZED: `HashAggregate` over `Append`, est
  **276,647 rows** — matches the QA ~286k estimate (188k cve-arm + 49k
  advisory + 49k member).
- Arm 1 (cve): `Bitmap Heap Scan`, est 143,579 wide rows (`width=899`).
- Arm 2 (advisory): `Index Scan`, est 66,465 rows.
- Arm 3 (members GIN `?|`): `Bitmap Heap Scan`, est 66,603 rows.
- Top `Unique` est 16 rows after dedup + generation join.

Read: the cost sits in bitmap-heap materialization of ~210k 899-byte rows
(arms 1+3) plus the HashAggregate over ~277k pairs. The planner shape is the
shipped one (all three arms on their indexes); the row volume for this MCV
id simply does not fit the 10 s guard, on either host.

## Verdict

**Guard-unsafe on primary**: 1/1 serial exec breaches the production 10 s
guard (10.051 s to statement-timeout), consistent with the replica 3/3 > 15 s.
The QA exclusion is not a replica artifact; the shipped single statement
cannot serve this id anywhere under the current guard.

## Disposition

Per #11567 acceptance criteria, second arm: file the planner/cost follow-up
the measurement calls for (existing label, `Part of #11567`, worked in this
lane). The fix surface is a design decision (cost-based statement choice /
always-key-range routing for above-gate masses / plan or index change on the
members arm — note the sibling arm-3 heap pathology already tracked for the
mass probe in #10681), so it goes in a follow-up issue, not in this probe.

## Commands and exits

- `kubectl -n eshu port-forward pod/eshu-postgresql-0 54400:5432` -> forwarding
  `127.0.0.1:54400 -> 5432` (held for the probe, terminated after).
- Identity `SELECT pg_is_in_recovery();` -> `f`, exit 0.
- Exec probe (scratch SQL file, not committed) -> guard fire at 10.051 s,
  exit 3. Exactly one exec ran against the primary.
- Plan probe (scratch SQL file, not committed) -> exit 0.
- No writes, no deploys, no cluster mutations, no knob moves. Drive/10728*,
  drive/10972*, drive/11563*, h2-shim, and the main checkout untouched; this
  note is the only file owned by this lane.
