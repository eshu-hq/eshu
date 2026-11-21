# Code-import ownership load drive-from-fact rewrite (#11082)

`FactStore.ListActivePackageOwnershipFacts`
(`go/internal/storage/postgres/facts_active_package_ownership.go`) loads the
active, non-tombstoned package-registry identity plus reducer ownership and
publication correlation facts across package-registry scopes: three fact
kinds, no scope or generation equality prefix. The old single statement
folded first and cursor pages behind `$1 IS NULL OR (observed_at, fact_id) >
...`, which stays a Filter under the generic plan the services get through
database/sql (#6154), and the join form invited a nested loop over every
active `(scope, generation)` pair probing `fact_records` per page. Past
corpus scale that is one full-corpus probe set per 500-row page. The QA
readings behind #11082 (2026-11-20, read-only) are the observed cost: one
statement aged 3423s waiting on `IO:DataFileRead` against the 1h handler
bound, with 33 `code_import_repo_edge` items retrying after
`reducer_handler_timeout` and none finishing in four hours.

The rewrite splits first-page and cursor statements (mirroring
`listFactsByKindQuery`/`listFactsByKindCursorQuery`) and drives from
`fact_records` through the partial index
`fact_records_package_ownership_active_idx` (migration 327:
`(observed_at, fact_id)` where the three kinds hold and `is_tombstone` is
false) inside a `MATERIALIZED` CTE with no inner `LIMIT`. The CTE holds every
matching row past the cursor in `(observed_at, fact_id)` order, the outer
join keeps the active generation's rows, and the outer `LIMIT` takes one
page. Honest bound: each page materializes every remaining three-kind
non-tombstone row past the cursor, so per-page cost is O(remaining matching
set), not O(page size); the win over the old form is one index-ordered pass
per page instead of a full-corpus probe set per page. An inner `LIMIT` would be a silent-loss bug, not a tuning choice: the
Go loop stops on a short post-join page, so a pre-join limit drops every
active row past the truncated pre-join set. `MATERIALIZED` is load-bearing:
without it Postgres flattens the CTE back into the generation-driven nested
loop.

## Agent guidance

- Do not fold first-page and cursor keyset pages into one statement. A `$N
  IS NULL OR (observed_at, fact_id) > (...)` predicate demotes the row
  comparison to a filter under the generic plan database/sql prepares, and
  pagination rescans once per page (#6154, #11082). Keep the two statements
  split.
- A `MATERIALIZED` CTE that feeds a short-page-stop loop must not carry an
  inner `LIMIT`; the paging loop stops on a short post-join page and
  silently drops every row past the truncated pre-join set (#11082 arm C:
  204 of 1200 rows before stopping).
- `MATERIALIZED` itself is load-bearing wherever the measured plan flattens
  without it (60.7ms vs 7.5ms per page on the #11082 fixture); pin it in
  the shape test with the measurement cited, per `postgres-rigor`.

## Migration 327

`327_fact_records_package_ownership_active_idx.sql` adds the index above. It
is the only statement, built `CONCURRENTLY` like 099 and 105; the split
first/cursor readers drive from `fact_records` through it. Index size on the
deciding fixture is 128kB for 2700 matching rows (vs 205MB for the
scope+generation keyset index over the same table); write churn is bounded by
the three listed kinds. Deliberately no `ANALYZE` (plain-column btree, same
reasoning as 099; verified by capturing the plans with no `ANALYZE` after
`CREATE INDEX`). Dropping the index is safe: reads return the same rows,
only slower.

## AC2: Why the cost changed between 2026-11-19 and 2026-11-20

Acceptance criterion 2 of #11082 asks why the same statement finished 964
items in four hours on 2026-11-19 and none on 2026-11-20. This record does
not establish that. What it does record is one read-only QA reading that
bounds the shipped design:

- Three-kind non-tombstone row count on the QA replica (the matching set each
  page now materializes), read 2026-11-21: 109 rows, all
  `package_registry.package`, no correlation rows. This fits in one page (page size 500), so the materialized set is read once and the O(remaining matching set) per-page bound in the design
  section costs nothing extra today; the shipped form stands. Revisit when the set reaches the tens of thousands. Replica lag at the read: 0.24s.

No 11-19 versus 11-20 corpus-growth, active (scope, generation) pair-count,
or plan/statistics reading was taken. The cause of the day-over-day change
stays open on #11082 and is not claimed here.

## Performance Evidence (#11082):

Performance Evidence: the four-arm deciding experiment below (disposable
Postgres 18, 1.2M-row fixture, forced generic plan) plus the
timeout-derivation replay; the shipped arm C2 numbers are the after
measurement and arm 0 is the baseline.

Deciding experiment on disposable Postgres 18 (1.2M `fact_records` rows,
1200 generations sharing one `observed_at` each, 1200 active + 1500 inactive
interleaved target-kind rows, tombstones present, `plan_cache_mode =
force_generic_plan`), before/after `EXPLAIN (ANALYZE, BUFFERS)` first + deep
cursor page plus full-drain wall/buffers and row-set md5 per arm:

- Arm 0 (folded statement, no new index): 3 pages, full drain 3573ms; page 1
  528ms/145647 buffers with kind/tombstone/cursor-OR as Filter (Rows Removed
  by Filter: 1798 per probe set).
- Arm A (split statements, no new index): 3 pages, 2135ms; planner keeps the
  generation-driven nested loop (85k-145k buffers/page). Rejected: no access
  path.
- Arm B (split join form + the index): partial index never chosen, probe loop
  stays. Rejected.
- Arm C (MATERIALIZED CTE with inner LIMIT + the index): 1.5ms/page but the
  drain stops after 204 of 1200 rows. Rejected as incorrect: the silent-loss
  hazard, proven on the interleaved fixture.
- Arm C2, shipped (MATERIALIZED, no inner LIMIT, with the index): 3 pages,
  row-set md5-identical to the folded statement including
  inactive-generation exclusion; page 1 7.5ms/4726 buffers, page 2
  4.1ms/2721 buffers, page 3 1.3ms, row comparison as Index Cond, no
  page-scaled Filter removals; without MATERIALIZED the same fixture plans
  the probe loop at 60.7ms/page.

Timeout-derivation replay (4 workers x 5 chained full drains of the shipped
statements, same fixture and plan mode): n=20, min 9.5ms, p50 9.9ms, p95/p99
14.3ms, max 14.3ms. Guarded by
`TestFactStoreListActivePackageOwnershipFactsSplitsFirstAndCursorPages` and
`TestFactStoreListActivePackageOwnershipFactsPagesToExhaustion` (shape +
paging, hermetic) and `TestActivePackageOwnershipDriveFromFactLive`
(generic-plan seek with the row comparison as Index Cond, full-drain buffers
5080 over 3 pages against a 15000 ceiling, row-set differential vs the frozen
legacy statement including inactive-generation exclusion).

## Observability Evidence:

Observability Evidence: the handler completion log and span attributes
below; no new metric family is added.

The code-import repo-edge completion log now carries `file_load` /
`ownership_load` durations and `ownership_rows`, and the handler span carries
the same three attributes (`code_import_repo_edge_handler.go logCompleted`),
so the next slow-but-completing ownership load names its load without pg
archaeology; no new metric family is added. Recorded limitation (arbiter
verdict non-blocking P2, follow-up not in this lane): both the span
attributes and the completion log emit only after both loads succeed (Handle
returns early on either load error), so a future hang inside the ownership
load itself records neither; the handler-duration histogram and the
`reducer_handler_timeout` class still count the burn. Cheap follow-up: set
span attributes incrementally after each load.

## Per-domain bound derivation and deploy step

Deploy step (not a manifest; no file in this repo sets the QA value):

`ESHU_REDUCER_HANDLER_TIMEOUT_OVERRIDES=code_import_repo_edge=2m`

Derivation: v = clamp(5x healthy p99, floor 2m, ceiling 10m). No healthy
whole-handler p99 exists for this domain: the 11-19 histogram was
unavailable, and the replay above times only the two ownership-load
statements (9.5-14.3ms), not the whole `Handle`, which also runs the
scope-local file-fact load (`factload.LoadFactsForKinds`), the decode, and
the transactional `UpsertIntents`. The 2m value is therefore the floor, not a
measured 5x p99. A large repo whose file load plus write exceeds 2m would
time out on every attempt and fail terminally after
`ESHU_REDUCER_MAX_ATTEMPTS`, so reading the whole-handler p99 off
`eshu_dp_reducer_handler_duration_seconds{domain="code_import_repo_edge"}` is
a precondition for applying the override, not only a check afterwards. The
pre-fix statement aged 3423s on QA against the 1h default; a minutes-scale
bound frees a wedged worker in minutes and makes each burn countable under
the existing `reducer_handler_timeout` class (retried under
`ESHU_REDUCER_MAX_ATTEMPTS`) instead of a silent 1h burn. The #10866
write-path audit for this domain is recorded in
`go/internal/reducer/queue-and-runners.md` (single end-of-handler
transactional upsert of deterministic rows, advance-only conflict guards,
lease-fenced queue mutations, no orphaned backend on cancel). Post-deploy
verification (coordinator step): confirm
`eshu_dp_reducer_handler_timeout_seconds{domain="code_import_repo_edge"}`
equals 120 and read a healthy-window p50/p95/p99 off
`eshu_dp_reducer_handler_duration_seconds{domain="code_import_repo_edge"}`
to confirm or adjust the bound; also capture the QA-replica generic plan for
the two shipped statements as the acceptance record.
