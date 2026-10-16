# Retention Keeps Delta-Active Bases (#7802)

Generation retention pruned superseded generations by age and rank without
reading `scope_generations.is_delta`. For a scope whose active generation is
a delta, the newest activated full generation is the only record of
unchanged files. Past the 90-day hard ceiling it became prunable, and with
it any rebuild from facts lost its base.

The candidate query now flags each candidate with a `delta_base` definition:
the newest activated full generation per delta-active scope
(`DISTINCT ON (scope_id)` over non-delta generations with `activated_at IS
NOT NULL`, newest ingested first, which is the repository's
newest-generation ordering) plus every generation ingested after it (the
delta chain). The activation filter matches the sibling
`last_activated_full_write` CTE. A newer full that is `pending` (until its
projection acks) or `failed` (after a dead letter) never activated, so it
holds no rebuildable truth and cannot displace the real base. Without the
filter, that newer full became the base, and the real base and the chain
before the newer full pruned past the hard ceiling (review F1).

Flagged rows leave the lock set and the prunable leg at any age, including
past the hard ceiling, and return on a lock-free `UNION ALL` report leg
capped at the same batch limit, oldest-first. The store counts them as
`Skipped["delta_base"]`, which the existing cycle log and skipped counter
already carry; no new instrument. The targeted re-lock re-checks the same
definition, so a delta that activates between selection and re-lock still
shields its base at lock time. Its `delta_base` CTE bounds the base
generation, the scope and the active generation by the batch's scope ids
(`$4`), so its cost follows the batch. Residual, same class as #7903: a
delta that commits after the re-lock's snapshot still flips a locked member
before the prune commits; closing that would serialize activation against
retention, which the repo forbids.

The targeted candidate query lives in `generation_retention_policy.go`,
beside the eligibility rules it re-checks at lock time, because the guard
pushed `generation_retention_sql.go` past the 500-line cap and the other
retention files (`generation_retention_relock.go` after the #7981 chunk path
landed) have no room left; no new file, so no dirgate re-pin.

Both candidate statements return ten columns in one order: scope id,
generation id, scope kind, superseded at, observed at, uncovered, the #7981
identity triple (source commit SHA, `is_delta`, freshness hint), then
`delta_protected`. The chunked re-lock (`relockChunkedBatch`) skips a flipped
delta-protected member before it can become the chunk target, so a protected
row never drains a chunk. The selection filters delta-protected rows before
the same-SHA witness runs, so a protected row counts once as `delta_base` and
never also as `same_sha_newest`.

`TestGenerationRetentionDeltaBaseLive` covers four scopes: a delta-active
scope with base plus chain, an overlap scope whose chain delta is both
uncovered and delta-protected, an active-full scope with one old
generation, and a same-SHA scope (active delta, an older full copy inside
the window, the newest activated full as protected base). The seeded bases
are activated, as a real base always is (activation sets `activated_at`
and superseding keeps it). The test asserts `GenerationsPruned == 2`: the
active-full scope's old generation and the same-SHA scope's older copy,
which prunes against the protected base as its witness. Bases and chains
stay, `Skipped["delta_base"] == 3`, and `Skipped["uncovered_writer"] == 1`
reports the overlap row once. Reverting the delta-leg uncovered exclusion
(`AND NOT eligible.uncovered` on `reported_delta_bases`) makes
`TestGenerationRetentionDeltaBaseLive` fail with `Skipped["delta_base"] = 4`,
want 3, proving the overlap arm is load-bearing.

`TestGenerationRetentionDeltaBaseSkipsUnactivatedNewerFullLive` (F1) adds
a newer full in status `failed` and in status `pending`. Before the
activation filter, both cases failed: `GenerationsPruned = 1`, the
activated base was pruned, and `Skipped["delta_base"] = 0`. After the
filter, both pass with the base kept and `Skipped["delta_base"] = 1`.

`TestGenerationRetentionRelockKeepsFlippedDeltaChainLive` (F2) is the
behavioral proof of the re-lock guard. At selection the scope's active
generation is a full, so an old delta after it is prunable. Before the
re-lock, a new delta activates, and the old delta becomes a protected
chain row. With `AND NOT provisional.delta_protected` removed from
`locked_members`, the test fails (exit 1): the chain delta is pruned and
`GenerationsPruned = 2`. Its `newer_pending_full` case also adds a newer
pending full. With the activation filter removed from the targeted
`delta_base` only, that case fails (exit 1, `Skipped["delta_base"] = 0`).

No-Regression Evidence (#7802): baseline is the pre-fix candidate query,
which pruned a 100-day base full and a 95-day chain delta under a 90-day
ceiling (original two-scope `TestGenerationRetentionDeltaBaseLive` RED:
`GenerationsPruned=3`, base and chain gone, `Skipped` empty).

Plan cost at scale (review F3), measured on native PostgreSQL 18.6 with
5,000 scopes x 40 generations (200,001 `scope_generations` rows, 42,000
delta rows, 20% of scopes delta-active), policy soft cutoff now-7d,
min-newer 24, batch limit 100, hard ceiling now-90d. Each figure is the
median of 9 runs of `EXPLAIN (ANALYZE, BUFFERS)` in rolled-back
transactions. The runs interleave the three statement versions and rotate
the first mover. The targeted statement re-locks the 100-row locked set
of the new candidate statement.

| Statement | main 261ca79b85 | PR before F3 (b488772edc) | PR after F3 |
| --- | --- | --- | --- |
| Targeted re-lock | 0.00846 s (8.46 ms) | 0.01900 s (19.00 ms) | 0.00965 s (9.65 ms) |
| General candidate | 1.355 s | 1.419 s | 1.409 s |

Before F3, the targeted `delta_base` was bounded only on the base
generation. The planner seq-scanned `scope_generations AS active`
(42,000 delta rows) and `ingestion_scopes` (5,001 rows), so the cost grew
with the total delta-generation count, not the batch. After F3, `$4` also
bounds `scope` and `active`. The plan reads the batch's scopes through a
bitmap scan of `ingestion_scopes_pkey`, probes `active` through
`scope_generations_pkey` (100 loops), and reads base candidates through
`scope_generations_scope_generation_idx` (21 loops). No seq scan of
`active` remains. The targeted statement still seq-scans `ingestion_scopes`
once in `locked_members`, and main has the same scan. The remaining cost
over main is 1.19 ms (+14%) for the added CTE.

The general candidate statement reads every scope by design. Its
`delta_base` subquery seq-scans `scope_generations AS active` (42,000
rows) and `ingestion_scopes` (5,001 rows) and takes about 25 ms. The full
statement is 54 ms (+4%) over main. The candidate query runs once per
retention pass, not per key, and already reads the table, so this cost is
accepted. `fact_work_items` is still read once in both statements. No new
index is needed.

Observability Evidence (#7802):
`eshu_dp_generation_retention_skipped_total{reason="delta_base"}` counts
every kept base and covered chain row (a both-flags chain row counts once
as `uncovered_writer` instead) and the cycle log carries both reasons in
`skipped_by_reason`; the regression test asserts the `Skipped` map entries
and the metrics reference documents the reason in the same PR. Post-deploy, a nonzero `delta_base` share means retention is
holding delta-active bases; correlate with the count of delta-active scopes.
Like the uncovered-writer leg, the report cap makes the count a floor, not a
census, when protected rows exceed the batch limit.
