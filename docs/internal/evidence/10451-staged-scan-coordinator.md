# #10451 staged scan: coordinator proof (L0–L4)

## L0 scope census (QA standby, direct-read role)

Per-scope active-fact counts over the existing partial index completed in
1.8 s: 707 (scope, generation) groups, 397,554 facts total, max 10,742,
p95 2,512, p50 96, zero groups over the 20,000 batch budget. The pruning
premise holds; no giant-scope path is needed.

## L1 worst-batch fetch (QA standby, live collector traffic)

Largest scope (10,742 facts) in final narrow shape with verbatim eligibility:
2.7 s, 2.7 s, 2.7 s across three attempts, zero conflicts, 25 s statement
timeout never neared. EXPLAIN (ANALYZE, BUFFERS) shows index-driven fetches
with index-only classification probes and zero heap fetches there.

## L1 classifier fixture

Live recovery-conflict capture is SQLSTATE 40001 with message `conflict
with recovery`, confirming the classifier contract (both halves required).

## L2 differential (local PG18 fixture, production Go functions)

Legacy single-statement scan: 6 candidates. Staged path (census, batches,
compaction, diff, ident resolution through the shipped functions): the same
6 plus exactly one documented extra, the planted override-only ghost key
with no source winner and no stored row. Zero false negatives; the only
false positive is the designed superset case the writer verification drops.

## Heap gate (local PG18, 200,000 distinct keys in 40 scopes)

Full staged pass: 6 s wall, 115.5 MB retained heap (measured with all maps
kept alive), under the 256 MB gate. A 400,000-distinct-key corpus aborts
cleanly on the 250,000 cap (`distinct keys got 250006, limit 250000`)
instead of growing memory.

## L4 overlap

Two simultaneous full passes produce byte-identical candidate sets; both
exit 0. Ticks share no state (separate maps per tick; the audit lease
single-flights ticks in the engine).

## Concurrency analysis

No new writer lock, lease, or queue edge: the shared writer-side
verification is unchanged, the resweep lock is claimed exactly as before,
and every standby statement is its own short transaction. Overlapping
snapshots across batches are covered by authoritative writer re-verification
plus the periodic repeat. Deferred ticks retry whole next interval;
compaction state is discarded on abort.

## Performance Evidence

Staged standby read replaces a single statement that never completes inside
the 30 s conflict window (cancelled 2/2 at 46 s/64 s on QA) with ~700
batches measured at 2.7 s worst-case. Writer-side verification is the
unchanged shared method, so verify cost is identical for identical
candidates. No-regression scope: legacy writer path and split scan are
byte-intact (golden statement tests green).

## Observability Evidence

Per-tick engine log line carries batches, staged facts, distinct keys,
candidates, unresolvable count, repairs, member, and read seconds; the
deferred path logs its closed reason. In-memory telemetry splits deferrals
by budget/conflict-exhausted/batch-abort and records tick gauges, member
identity with replay lag at both ends, writer-verify discard rate, and the
unresolvable counter. Post-deploy watch: any tick deferred 3x in a row,
max batch duration trending past 15 s, conflict-attributed deferrals above
zero, or a nonzero unresolvable rate each page a look.

## NOT_CHECKED here (owed at L5 on QA)

Three consecutive clean ticks on the standby with writer CPU shed measured;
conflict-counter delta zero post-flush; engine RSS through a tick. The
association-exclusion leg was dropped from the scratch fixture (FK chain
too deep); its predicate is verbatim shipped SQL and the exclusion family
is covered by the classification leg plus lane fixture tests.
