# #10827 versioned dirty-log upsert: write-path evidence

Refs #10827. Design ruling: `RULING: CONDITIONAL SHIP on (b)+(c)`
(arbiter, re-shim round): K-ordered versioned upsert with a
writer-count-stratified p95 envelope, plus a follow-up hot-key write
coalescing design issue (#10837, non-blocking). No shipped retry; the
zero-deadlock bar is not weakened.

## What changed

The impact dirty log was append-only with no unique index, so every new
scope or generation first pass re-marked its findings and OSV batch waves
re-marked 162 hot pairs ~150 times (25,000 rows per 5 minutes against a
~1,200/min drain). Each recompute unit now owns one row: re-marks bump
`dirty_version` under two partial unique indexes (fact five-tuple, scope
pair), and the settle drain deletes only admitted rows whose version still
matches, with all multi-row accessors taking row locks in one global key
order. Migrations 303-308 (column, dedup, two CONCURRENTLY index builds,
272/272a trigger upserts). The gate encodes the stratified envelope and
pins the ruled values.

## Performance Evidence:

Writer upsert on local PostgreSQL 18 (disposable database, seeded 1.27M
rows): single-writer p50 ~1.0ms; 2 concurrent writers p95 2.00-2.20ms
(inside the 3.41ms envelope); 4 concurrent writers p95 2.42-3.89ms
(stratified baseline 3.89ms, writers-only control 3.81ms proves hot-row lock
waiting, not drain cost). Tick admit plus compare-and-delete of 5,000 rows
~5.5ms. Full 3M+ mark replays: zero deadlocks, exact conservation (marks
equal drained version sums plus residual), log depth equal to distinct
pending units at every sample and zero between waves, winners table
byte-identical to a full recompute, audit repairs zero.

## No-Regression Evidence:

Full winners live fence green (triggers, retirement, privilege, protocol,
chunked, horizon, audit, parity, rekey, stale suites), plus the deadlock
interleave proof (zero 40P01, EXPLAIN pins LockRows above key-order Sort)
and the survival/collapse proofs (re-mark reuses its row with version+1
and first-mark timestamp preserved; 150 passes collapse to one row).

## Observability Evidence:

Duplicate collapse is visible in the settled tally: `sum(dirty_version)`
counts every mark a consumed row stood for, and the NVD trip CTEs sum
versions the same way. Telemetry wording updated to version-sums with no
new metrics. A future hot-key coalescing issue (#10837) tracks reducing
the inherent serialization cost; it does not block this change.
