# Grouped Impact Inventory Store Evidence (#9873)

`PostgresGroupedInventoryStore` serves `group_by=service_id`, `workload_id`, and
`environment` on `GET /api/v0/supply-chain/impact/inventory`. It reads the
maintained `supply_chain_impact_canonical_winners` table and resolves each
finding's service, workload, or environment at read time from current runtime
facts. The code lives in
`go/internal/query/supply/chain/impact/grouped_inventory_postgres.go` and
`grouped_inventory_sql.go`.

## Why the winners table

The arbiter ruling on #9873 required a bounded shim run with
`operator_overrides AS MATERIALIZED` before the source was chosen. The
read-time CTE source failed the threshold (p95 at most 1.5 s at 5x) by about an
order of magnitude. The cause was a nested loop that rescans the
operator-override CTE and the repository map once per finding, because the
planner estimates one row for each. The issue comments hold the shim tables.
This store therefore reads the winners table only. It never joins the operator
facts per finding: the winners row already carries the overlaid
`suppression_state` and `suppression_expires_at`.

## Read contract

- **Finding set.** One row per canonical key from the winners table. It uses
  the static-winner predicate (protected-reads marker substituted closed, as
  the aggregate does) and every aggregate filter on the same argument order
  (`$1..$17`). The suppression filters use the effective decision state at
  `$20`, the single UTC read clock. The grant (`$18`, `$19`) applies to
  `repository_id` and `winner_scope_id`, so counts, breakdowns,
  `max_priority_score`, and `distinct_findings` read only granted and
  filtered findings. A scoped request (either grant array non-empty) uses a
  separate query variant that differs only in its partial signal: a
  `grouped_hidden_repositories` CTE lists every repository with at least one
  static winner the grant does not admit. It reads the winners with only the
  static-winner predicate and the grant, never a request filter. If request
  filters fed it, a caller could vary `cve_id` or `min_priority_score` and
  watch a bucket flip between complete and partial, an oracle on hidden
  findings (an automated security review caught this in the first version).
  Partial therefore over-reports for narrow filters and discloses nothing
  about individual findings. The unscoped SQL text is byte-identical to the
  version before the scoped variant.
- **Composition.** The shared runtime-filter CTE and predicate are included,
  so `group_by=environment&service_id=X` keeps AND semantics.
- **Repository-to-group map.** It uses the same fact kinds, active-generation
  joins, outcome guards, repository decoder, and BTRIM rule that the
  `service_id`, `workload_id`, and `environment` filters use. The workload map
  unions identity `workload_id`, identity `entity_keys` (only `workload:`
  keys, array or bare string), and catalog `workload_id`. The `entity_keys`
  leg is required: the `workload_id` filter matches through it, and the shim
  map had left it out (the mutation check below proves the live test catches
  its removal). Each map row carries a `granted` flag computed with the
  runtime-filter grant shape on the runtime fact's repository and scope.
- **Fan-out.** A finding joins every granted map row of its repository, so it
  counts in each group. A finding with no granted mapping counts in the `""`
  unattributed bucket.
- **Completeness.** These are the store's rules. The handler also demotes
  `complete` to `partial` on a truncated page.

  | Case | Completeness | `missing_evidence` |
  | --- | --- | --- |
  | Scoped: the grant hides one of the bucket's map rows, or any static winner on a repository the bucket maps to (independent of the request filters) | `partial` | none (the reason is not disclosed) |
  | Watermark older than 2 minutes, or the probe failed | `read_model_unavailable` | `readiness_unavailable` |
  | Unattributed bucket | `unattributed`, always | the dimension's mapping reason |
  | Otherwise | `complete` | none |

  The 2-minute window is `impact.WinnersFreshnessWindow`, which the findings
  handler's `supplyChainImpactWinnersFreshnessWindow` aliases, and a test pins
  the two equal. An absent watermark answers
  `ErrGroupedInventoryReadModelUnavailable` (the handler's 503). A table that
  was never built would otherwise read as zero findings. The coordinator
  approved this choice.
- **DistinctFindings** is `COUNT(*)` over the whole filtered finding set,
  independent of limit and offset, so it equals the count route's
  `total_findings` for the same filters.
- **Ordering** is `max_priority_score DESC, count DESC, value ASC`, with
  `LIMIT $21 OFFSET $22`. The handler asks for limit+1 to detect truncation.
- **Gate.** `ReadFromWinners` comes from `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ`
  in `newSupplyChainHandler`. When it is off the store answers
  `ErrGroupedInventoryReadModelUnavailable` without running a query. There is
  no CTE fallback.
- **Empty grant.** The store cannot detect an empty scoped grant, because
  empty arrays mean unrestricted. It relies on the handler's `access.Empty()`
  guard, as `PostgresAggregateStore` does.
- **NULL grant arrays.** The map's `granted` flag is wrapped in
  `COALESCE(..., FALSE)`. An empty grant array can bind as NULL, and
  `scope_id = ANY(NULL)` would otherwise make `BOOL_OR` NULL, so a hidden
  repository would never mark its bucket partial. The first live run failed
  on exactly this case.

## Known divergence: scope-id grants

With a scope-id grant (`AllowedScopeIDs`) the winners table has already picked
one winner per canonical key across every scope before the grant applies. The
aggregate CTE applies the grant first. Take a key whose two copies tie on
priority: the copy with the lower `fact_id` wins the materialized ranking. If
that copy sits in an ungranted scope, the key is hidden here, while the count
route counts the granted copy. Measured at 1x on the shim seed: distinct 429
against the count route's 431 (0.46 percent), with one sampled bucket per
dimension off by one or two. Unscoped reads and repository-id grants matched
exactly (below). The winners list SQL has the same property, but the list
store routes scoped calls to the CTE. This store must not fall back, so the
divergence stays, as the ruling directs.

Two more cases have the same root, one chosen copy per canonical key:

- **Stale winners.** While buckets read `read_model_unavailable`, the grouped
  counts reflect the last resweep and the count route reads live facts.
- **Fields outside the key.** Copies that share a canonical key can differ on
  `image_ref`, `detection_profile`, or `priority_bucket`. A filter on such a
  field tests only the chosen winner here. The count route tests every copy
  before choosing.

The public inventory page and the OpenAPI and MCP descriptions name all
three cases.

## Correctness evidence

The live proof is
`grouped_inventory_postgres_live_test.go` (`TestGroupedInventoryStoreWinnersLive`,
enrolled in live-postgres-readiness). It runs on PostgreSQL 18.6 with the
production bootstrap and winners from `RebuildAllWinners`. It covers:

- a two-service finding in both buckets;
- the unattributed bucket, including a superseded-generation mapping that is
  ignored and an ambiguous catalog row that is rejected;
- each bucket equal to the count route for `service_id=X`, `workload_id=X`
  (including `workload:w2`, reachable only through `entity_keys`), and
  `environment=X` (including a `" qa "` value matched as `qa`);
- composition by service;
- a repository grant hiding the ungranted-only `service:c`, and marking
  `service:a` partial because `repository:r6` is outside the grant;
- the same with a scope-id-only grant, with every bucket equal to the count
  route;
- a grant on a central catalog scope that maps `repository:r7` and
  `repository:r8` to `service:e`, with r8's finding in an ungranted scope.
  The bucket counts only r7's finding (equal to the count route) and reads
  `partial`. It still reads `partial` when `cve_id`, `min_priority_score` (65,
  above the hidden finding's 64), or `severity` narrow the request. A grant on
  the catalog scope alone yields no bucket at all, because no granted finding
  maps to `service:e`. Before the scoped variant this case read `complete`.
  Feeding `min_priority_score` into the hidden CTE fails both this subtest and
  the unit test that bans request parameters there;
- read-time expiry of an operator suppression, with `IncludeSuppressed` and a
  later clock, matched by the count route on the same clock;
- ordering and truncation, with whole-set `DistinctFindings` on a truncated
  page;
- gate off and absent watermark both returning the typed error.

The mutation check removed the `entity_keys` leg. The workload subtest failed
(`workload:w2` missing). Restoring the leg made it pass again.

Seeded-corpus parity, at 1x of the shim seed (20,000 findings, 400
repositories). Every seventh named bucket was compared to the count route for
the same filter.

| Grant | Dimension | Buckets | Checked | Mismatches | distinct / count total |
| --- | --- | --- | --- | --- | --- |
| none | service_id | 148 | 21 | 0 | 8,827 / 8,827 |
| none | workload_id | 61 | 8 | 0 | 8,827 / 8,827 |
| none | environment | 7 | 1 | 0 | 8,827 / 8,827 |
| repository ids | service_id | 22 | 3 | 0 | 504 / 504 |
| repository ids | workload_id | 4 | 1 | 0 | 504 / 504 |
| repository ids | environment | 7 | 1 | 0 | 504 / 504 |
| scope ids | service_id | 19 | 3 | 1 | 429 / 431 |
| scope ids | workload_id | 4 | 1 | 1 | 429 / 431 |
| scope ids | environment | 7 | 1 | 1 | 429 / 431 |

## Performance evidence

Performance Evidence: the production store call (watermark probe plus the
grouped statement, through `database/sql` with bound parameters). Setup:
disposable PostgreSQL 18.6 (`postgres:18` container on the shared host), the
shim seed (`seed.sql`) at 1x (20,000 findings, 400 repositories, 160,000
background facts) and 5x (100,000 findings, 2,000 repositories, 800,000
background facts), winners rebuilt with `RebuildAllWinners`, then
`VACUUM (ANALYZE)`. Each case ran 3 times warm with limit 101. The max stands
in for p95. Scoped cases use the shim's grant: repositories with index mod 20
= 3 in `$18`, and scopes with index mod 20 = 7 in `$19`. The host was not
quiet (load average 8 to 40 on 12 CPUs), so compare shapes and treat absolute
milliseconds as upper bounds.

The "before" figures are the read-time CTE source, from the #9873 shim comment
(5x, precise, unscoped): service 12,016 ms p50 / 13,974 ms max, workload
14,677 / 27,243 ms, environment 41,193 / 41,856 ms. Comprehensive service was
44.7 s p50.

Results at 5x (milliseconds):

| Case | p50 | max | EXPLAIN ANALYZE | buckets | distinct |
| --- | --- | --- | --- | --- | --- |
| service, precise, unscoped | 269 | 275 | 270 | 101 | 43,664 |
| service, precise, scoped | 174 | 183 | 115 | 101 | 4,476 |
| service, comprehensive, unscoped | 370 | 382 | 381 | 101 | 90,981 |
| service, comprehensive, scoped | 151 | 155 | 126 | 101 | 9,337 |
| workload, precise, unscoped | 394 | 400 | 472 | 101 | 43,664 |
| workload, precise, scoped | 271 | 344 | 229 | 31 | 4,476 |
| workload, comprehensive, unscoped | 503 | 510 | 532 | 101 | 90,981 |
| workload, comprehensive, scoped | 275 | 284 | 227 | 31 | 9,337 |
| environment, precise, unscoped | 505 | 515 | 601 | 7 | 43,664 |
| environment, precise, scoped | 392 | 438 | 366 | 7 | 4,476 |
| environment, comprehensive, unscoped | 648 | 672 | 706 | 7 | 90,981 |
| environment, comprehensive, scoped | 392 | 399 | 387 | 7 | 9,337 |
| environment with service_id, precise | 391 | 394 | 384 | 6 | 39 |
| environment with service_id, comprehensive | 406 | 451 | 388 | 6 | 93 |

At 1x every case was at most 132 ms max. The range from the lowest p50 to the
highest max was 30 to 93 ms for service, 73 to 123 ms for workload, 83 to
132 ms for environment, and 53 to 57 ms for environment composed with
service_id.

The worst 5x case is 672 ms max, under the 1.5 s threshold for both profiles.
At 5x, precise and unscoped, the p50 is about 45 times faster than the CTE
source for service (12,016 to 269 ms), 37 times for workload (14,677 to
394 ms), and 82 times for environment (41,193 to 505 ms).

Plan shape (`EXPLAIN (ANALYZE, BUFFERS)`, 28 plans across 1x and 5x, kept in the
lane scratchpad, not committed):

- The winners table is read by one sequential scan with a hash anti join on
  `scanner_image_result_fact_associations`.
- The fan-out join is a Hash Left Join in every plan. The map CTEs are
  estimated at 1 row but hold 5,547 rows for the 5x environment case. The
  join has not flipped to a nested loop, but that estimate is the residual
  plan risk to watch at larger scale.
- The only nested loop over CTEs is the one-row totals row joined to the page.
- The map legs spend most of their time in the repository decoder lateral
  (253 ms for 49,923 CI/CD rows at 5x).
- Small temp spills (about 8 MB written) come from the CTE tuplestores and
  hash aggregate exceeding the stock 4 MB `work_mem`. No sort spilled.

The 5x `EXPLAIN ANALYZE` of the slowest case (environment, comprehensive,
unscoped) took 706 ms, with shared hit 22,716, read 2,509, and temp written
1,072 pages.

Scoped path with the filter-independent partial signal (third run, same 5x
seed and host, load average 12 to 27). The shim grant mixes repository ids and
scope ids. The distinct counts equal the first run's (4,476 precise, 9,337
comprehensive), so the granted counts did not move.

| Scoped case | p50 | max | EXPLAIN ANALYZE | buckets | distinct |
| --- | --- | --- | --- | --- | --- |
| service, precise | 106 | 108 | 143 | 101 | 4,476 |
| service, comprehensive | 138 | 146 | 107 | 101 | 9,337 |
| workload, precise | 174 | 177 | 136 | 31 | 4,476 |
| workload, comprehensive | 171 | 223 | 147 | 31 | 9,337 |
| environment, precise | 200 | 201 | 233 | 7 | 4,476 |
| environment, comprehensive | 234 | 259 | 201 | 7 | 9,337 |

The extra cost is the unfiltered `grouped_hidden_repositories` scan: a hash
aggregate over every ungranted static winner, 21 ms for 1,824 repositories in
the slowest scoped plan (shared hit 31,549, temp written 433 pages). The
worst scoped case is 259 ms max, under the 1.5 s threshold, and the fan-out
join stays a Hash Left Join in every scoped plan. An intermediate version that
kept the whole pre-grant filtered set to compute the same signal measured up
to 412 ms max and was replaced because its signal depended on the request
filters.

Not checked: 10x, cold cache, `__PROTECTED_READS__` mode (static winners only,
so the marker is substituted closed), and real QA volumes.

## Concurrency

The store runs two read-only statements (the watermark probe and the grouped
query) on the guarded read queryer. It opens no transaction and takes no lock
beyond the ACCESS SHARE every SELECT takes. It has no mutable shared state. The
winners maintainer writes under its own lease. A read that overlaps a resweep
sees either the old or the new committed winners, per statement snapshot. The
watermark is read in a separate statement, so freshness can lag the data by
one resweep. It is a hint, not a fence.

## Observability

No-Observability-Change: the grouped read reuses the handler's
`impact_aggregate_grouped_inventory` stage on the
`supply_chain_impact_aggregate_read` operation (stage_started, stage
completion, and the stage_failed event). Winners freshness reuses
`eshu_dp_supply_chain_impact_winners_watermark_age_seconds` plus the
per-response `freshness.state` and per-bucket `read_model_unavailable`. This
change adds no metric, span, or log key.
