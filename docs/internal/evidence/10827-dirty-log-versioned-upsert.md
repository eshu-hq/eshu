# #10827 versioned dirty-log upsert: write-path evidence

Refs #10827. Design ruling: `RULING: CONDITIONAL SHIP on (b)+(c)`
(arbiter, re-shim round): K-ordered versioned upsert with a
writer-count-stratified p95 envelope, plus a follow-up hot-key write
coalescing design issue (#10837, non-blocking). No shipped retry; the
zero-deadlock bar is not weakened per statement (the multi-batch
transaction residual is #10851, ruled 2026-11-17).

## What changed

The impact dirty log was append-only with no unique index, so every new
scope or generation first pass re-marked its findings and OSV batch waves
re-marked 162 hot pairs ~150 times (25,000 rows per 5 minutes against a
~1,200/min drain). Each recompute unit now owns one row: re-marks bump
`dirty_version` under two partial unique indexes (fact five-tuple, scope
pair), and the settle drain deletes only admitted rows whose version still
matches, with every multi-row statement taking row locks in one global
key order. Migrations 303-308 (column, dedup, two CONCURRENTLY index builds,
272/272a trigger upserts). The gate encodes the stratified envelope and
pins the ruled values.

## Rollout: writer quiesce required

Migrations 305/306 build partial UNIQUE indexes CONCURRENTLY over the
dirty log after 304's dedup, and old binaries' plain INSERTs violate the
new indexes until the app rolls. The Helm pre-upgrade hook runs while
outgoing pods still serve, and deployment ordering does not stop
already-running writers, so this deploy is not a standard migration-gated
roll: stop the dirty-log writers first, then apply 303-308, then roll
the app. The writer set is wider than the reducer: scale the reducer and
ingester runtimes to zero per
[service-runtimes-bootstrap.md](../../public/deployment/service-runtimes-bootstrap.md),
disable API repository retirement (its fence and purge write through a
writer login and fire the capture triggers) or scale the API to zero,
stop the workflow-coordinator (its evidence fencer reopens impact
holders on a ticker) and any standalone projector, and let any in-flight
bootstrap-index run finish or stop it. Before the app rolls, widen the
runtime writer pregrant on the dirty log to SELECT, INSERT, UPDATE (the
upsert conflict arm needs SELECT+UPDATE; trigger definers need no caller
grant): an INSERT-only role fails its first re-mark after the roll.

Recovery: if a unique build reports duplicates from concurrent writes,
do not re-run the migration files by hand — their IF NOT EXISTS form
skips an INVALID index a failed CONCURRENTLY build leaves behind, and
every writer then fails for lack of a matching unique index. Under the
same quiesce, execute 304's dedup SQL manually, then re-run the schema
apply (bootstrap), which drops INVALID concurrent indexes and rebuilds
305/306. Confirm both indexes read indisvalid before un-quiescing.

## Performance Evidence:

Writer upsert on local PostgreSQL 18 (disposable database, seeded 1.27M
rows): single-writer p50 ~1.0ms; 2 concurrent writers p95 2.00-2.20ms
(inside the 3.41ms envelope); 4 concurrent writers p95 2.42-3.89ms
(stratified baseline 3.89ms, writers-only control 3.81ms proves hot-row lock
waiting, not drain cost). Tick admit plus compare-and-delete of 5,000 rows
~5.5ms. Full 3M+ mark replays: zero deadlocks, exact conservation (marks
equal drained version sums plus residual), log depth equal to distinct
pending units at every sample and zero between waves, winners table
byte-identical to a full recompute, audit repairs zero. Provenance: the
p50, tick, and 3M+ figures are one-off pre-ship shim measurements (local
PostgreSQL 18, seeded backlog) with no committed harness; the shipped
gates re-measure the writer and settle shapes at gate scale. Unmeasured
hot-path additions beyond the gated capture shape: the retract
statement's sort and count CTEs, trigger ORDER BYs, the Go sort/dedup
before capture, the convert GROUP BY aggregation, and the audit queue
ORDER BY (the last two bounded by the admission window); #10842's bench
scope should name the retract and convert shapes alongside capture.

## No-Regression Evidence:

Full winners live fence green (triggers, retirement, privilege, protocol,
chunked, horizon, audit, parity, rekey, stale suites), plus the deadlock
interleave proof (zero 40P01, EXPLAIN pins LockRows above key-order Sort)
and the survival/collapse proofs (re-mark reuses its row with version+1
and first-mark timestamp preserved; 150 passes collapse to one row). The
deadlock proof is a gate-shape measurement: autocommit single-statement
writers against settle ticks. Production passes batch several ordered
statements per transaction; the cross-batch residual is #10851.

## Observability Evidence:

Duplicate collapse is visible in the settled tally: `sum(dirty_version)`
counts every mark a consumed row stood for, and the NVD trip CTEs sum
versions the same way. Telemetry wording updated to version-sums with no
new metrics. One attribution change ships with the dedup: source is
first-marker-wins, so a trigger re-mark of an already-queued key bumps
the version without changing the row's source, and the maintainer's
per-source counts credit the owner that queued the unit. The convert
path picks MIN(source) when one remainder arrives under two scope-row
sources, so that converted row always credits activation over operator;
totals are conserved. The NVD trip
tally keeps bucketing by first-mark captured_at: a re-mark of a unit
first marked over two hours before its drain adds nothing to recent
buckets, where the append-only log stamped every mark fresh. Accepted:
the trip signal reads newly-dirty inflow, and a long-queued unit's
re-marks are not new inflow. A future hot-key coalescing issue (#10837) tracks reducing
the inherent serialization cost; it does not block this change.
