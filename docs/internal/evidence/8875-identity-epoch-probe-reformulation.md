# #8875 identity epoch probe: grouped reformulation

The epoch probe (`probeIdentityEpochQuery`) digested the DISTINCT active
`(scope_id, generation_id)` pair set with an ordered `string_agg` over the
full filtered row set. On the production-shaped corpus that ordered digest is
about 80% of the roughly 738 ms probe, so the probe reformulation groups
first and digests the group rows instead. The aggregate is a two-level,
byte-identical reformulation (arbiter ruling, option (a)): the inner query
scans the filtered identity facts once, grouped by `(scope_id,
generation_id)` with per-group count and max `observed_at`; the outer query
rolls those up with `SUM`, `MAX`, and the same ordered `string_agg(DISTINCT
...)` over the pair strings. No digest change, no empty-count term,
detection stays on the probe path.

Shim: disposable PostgreSQL 18.6 in Docker on the shared laptop, minimal
schema carrying only the columns the probe touches, `ANALYZE`d before the
runs. Edge rows cover delimiter-hostile ids (leading `:`, leading `|`,
embedded `:`/`|`, empty generation id), tombstones, superseded generations,
and non-identity kinds; the bulk arm adds 200 scopes x 250 facts (50,000
rows, 206 covered pairs with the edge rows). Host load average was above 20
(a peer `make pre-push` ran throughout), so absolute milliseconds carry that
noise; before and after ran back-to-back on the same container and rows.

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` second runs on the
50,008-row fixture, old single-level aggregate vs new grouped aggregate.
Shared buffers are identical (`shared hit=822` both). The old plan sorts the
full 50,008-row string set (`Sort Method: external merge Disk: 2544kB`,
`temp read=318 written=319`, about 31 ms). The new plan hash-aggregates into
206 group rows first and sorts only those (`Sort Method: quicksort Memory:
38kB`, no temp buffers, about 14 ms). The temp sort spill is eliminated; the
digest input shrinks from the row count to the pair count.

Exactness evidence: old vs new triple compared on five states (empty set,
edge rows, 50,008-row bulk, identity-empty activation flip, identity insert
into the new generation) — byte-identical in all five, including count 0,
NULL max, and the SHA-256-of-empty-string fingerprint on the empty set.
`TestIdentityEpochProbeReformulationMatchesOracleLive` proves the new probe
against an independent Go (`crypto/sha256`) oracle over shaped fixtures.
Deployment is epoch-neutral: rollout alone moves no epoch.

No-Observability-Change: the probe call sites are unchanged —
`IdentityEpochCache.get` still records `IdentityCacheProbeDuration` per
probe — and the reformulation adds no metric, span, log, status, or knob.
An operator reads the same probe-duration histogram and the same cache
hit/miss/flight counters as before.
