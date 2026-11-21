# #10643 holder read: key ranges for a hot singleton id

Refs #10643, epic #10860, serving #11147. Ruling records:
[11147a decision 2](../arbiter/10643-holder-read-bound-rulings-2026-11-21.md#11147a-decision-2)
and [10643b](../arbiter/10643-holder-read-bound-rulings-2026-11-21.md#10643b-the-member-arm).

This change does not close #10643. The cve_id and advisory_id arms now meet
the bar. The advisory-group member arm does not for member-heavy ids: its
statement ran 15.329 s cold in a 4-way burst on QA. That arm is the second
PR, designed and not yet proven (see Residual).

## What changed

A hot singleton lookup id is one whose measured mass is over 10,000 rows
(`fanOutHolderHotMassRows`). On a port with `HolderKeyRangeReader` it no
longer runs as one holder statement over all of its rows. It runs as:

- 16 statements, one per fact_id key range (`HolderKeyRanges`), each reading
  the shipped cve_id and advisory_id arms with `fact_id >= lower AND fact_id
  < upper` added. Each statement reads the existing partial lookup index
  with fact_id in the index condition. No new index.
- one member-arm statement, the shipped GIN arm alone.

All 17 run in one read-only REPEATABLE READ transaction under
`plan_cache_mode = force_custom_plan`. Each keeps the 10 s statement guard
and the typed timeout error. Cold ids and the packing rules are unchanged.

The ranges partition the whole text key space. The first range is unbounded
below, the last unbounded above, and each upper bound is the next lower
bound, so any fact_id falls in exactly one range whatever its format or
collation. Finding fact ids are the fact kind, a colon, and a hex digest;
on QA the first digest digit splits 13,726 rows of one id 819 to 907 per
digit, so the format only keeps ranges balanced.

## Why not the other candidates

- Ordered keyset pages, by the index's own `(impact_status, fact_id)`
  order: measured on the QA replica and rejected. Each row is a separate
  random heap fetch. Cold, one 13-20k-row id took 5.914 s across 9 pages,
  against 0.41-0.47 s for the shipped read on comparable ids. The pages met
  the per-statement bar, but total work grew about 10x.
- A covering index on `((payload->>'cve_id'), scope_id, generation_id)`:
  still no index-only path on PostgreSQL 18.6. A query that filters on an
  indexed expression must visit the heap
  (`evidence-holder-covering-index-h3.md`). Re-run on a PG 18.6 container:
  plain Index Scan on the covering index, no Index Only Scan.
- A skip scan over distinct generation_id with new narrow indexes: not
  built, because the key-range variant met the bar for the btree arms with
  no new index (11147a decision 2: take the one with no new index).

## Performance Evidence

QA replica, read-only, 30 s statement timeout, replica lag 0.02-0.64 s.

Causes, measured first (M15). The 20 most recent fan-out failures carry
6-70 lookup ids each, 376 distinct. 349 of them name more than 1,000 rows.
Exact cve-arm rows: CVE-2025-27789 83,642 (263 pairs); CVE-2021-23364 47,515;
CVE-2026-48038 22,626, with 21,342 member-arm rows.

Bar (11147a decision 2): per statement, p95 under 2 s, max under 5 s,
zero 57014.

| Shape | Statement | n | p50 | p95 | max | 57014 |
|---|---|---|---|---|---|---|
| single reader, cold | key range | 32 | 254 ms | 992 ms | 1.698 s | 0 |
| single reader, cold | member arm | 2 | 7 ms | 7 ms | 16 ms | 0 |
| single reader, cold | shipped | 2 | 3.101 s | 3.101 s | 3.917 s | 0 |
| 4-way burst, cold | key range | 64 | 201 ms | 581 ms | 1.01 s | 0 |
| 4-way burst, cold | member arm | 4 | 11 ms | 4.917 s | 15.329 s | 0 (30 s limit) |
| 4-way burst, cold | shipped | 4 | 2.85 s | 5.672 s | 13.47 s | 0 (30 s limit) |
| 4-way burst, warm | key range | 64 | 56 ms | 75 ms | 128 ms | 0 |
| 4-way burst, warm | member arm | 4 | 12 ms | 785 ms | 2.439 s | 0 |
| 4-way burst, warm | shipped | 4 | 315 ms | 596 ms | 1.899 s | 0 |

Cold ids: single reader CVE-2026-26996 and CVE-2022-0536 (key ranges),
CVE-2023-26159 and CVE-2024-52011 (shipped); 4-way, one id per reader,
CVE-2025-32395, CVE-2025-62522, CVE-2026-109932 and CVE-2026-14257 (key
ranges), CVE-2026-112944, CVE-2026-40895, CVE-2026-44289 and CVE-2026-45149
(shipped). Cve-arm rows: 23,430 to at least 60,001. The 13.47 s shipped max
reproduces the production timeout under the 10 s guard. So does a separate
shipped read of CVE-2025-27789, which took 11.42 s.

Total work per hot id is unchanged, since each row is still fetched once.
Whole read per hot id, cold: 3.1-9.2 s single reader, up to 21.7 s in the
4-way burst. Warm: 391-891 ms, against 252-628 ms for the shipped read. A
70-id owed recovery pass at the existing parallelism of 4 needs about 90 s at
5 s per hot id. That is far under the reducer handler bound.

Snapshot hold. The read runs on the primary: the reducer's
`SupplyChainImpactFanOutStore` uses its own database. One key-range read
holds xmin for its whole duration, worst case 17 statement guards (170 s);
measured 3.1-21.7 s cold. `kind=holders_key_range_read` on the read-duration
histogram records each whole read.

The member arm fails the bar for member-heavy ids. GIN `jsonb_ops ?|` is
lossy, so every candidate row is rechecked, and the recheck detoasts the
whole ~4.7 KB payload. CVE-2026-14257 warm read 48,555 rows over 17,658 heap
blocks with 284,245 shared hits in 1,027 ms. CVE-2026-53571 cold read 26,093
rows with 34,979 reads and 7.12 s of I/O. On QA every member-arm row of the
12 hot ids is also a cve-arm row (member-only rows 0), but a GHSA alias of a
CVE-keyed row is a foreign member: 13,845 to 49,133 foreign rows per alias
(M20). Key ranges cannot bound a GIN bitmap, which has no key order.

Plan pin. Under `force_generic_plan`, PostgreSQL 18.6 cannot see the
fact_id bounds and reads them through `fact_records_pkey` (a Bitmap Index
Scan with Index Cond `(fact_id >= $3) AND (fact_id < $4)`). On QA that would
be a 1/16 slice of the whole table per statement. The store therefore pins
`force_custom_plan` in the read transaction. `TestHolderKeyRangeStatementPlanLive`
asserts the pinned plan reads both lookup indexes with fact_id in the index
condition and never the primary key. It logs the generic plan as the seeded
RED.

## Correctness evidence

One REPEATABLE READ transaction per comparison, set equality against the
shipped statement.

- QA replica, all on snapshot `93513862:93514764:`, `range_eq=true`:
  CVE-2026-14257 (150 scopes, member-heavy), CVE-2026-112944 (287,
  member-heavy), CVE-2026-109932 (218), GHSA-xq3m-2v4x-88gg (172),
  CVE-2025-27789 (259), and the 5-id chunk (982). The seeded RED, the same
  read with one shipped holder as the asking scope, is caught on all six.
  A dropped last range is masked on QA data, because each holder scope has
  about 300 rows per id spread over every range.
- Local PG 18.6 shim, 93,622 rows per hot id with toasted payloads:
  CVE-HOT-C 321 = 321 and GHSA-HOT-A 321 = 321, with a lone holder keyed in
  the last range, so a dropped last range is caught. GHSA-HOT-MEMBER-ONLY
  21 = 21, CVE-BG-17 81 = 81, CVE-LONE-MEM 21 = 21, chunk 102 = 102. The
  member-heavy CVE-HOT-M did not finish within 600 s on the slow local disk
  and is covered by the QA member-heavy ids.
- Generation flip mid-read (local): a lone holder's active generation flips
  to a rowless one after the first range statement. REPEATABLE READ: the
  ranged read equals the shipped read at the snapshot (321 = 321, holder
  kept). READ COMMITTED as the seeded RED: the holder is lost (320).
- Store live tests (`supply_chain_impact_fanout_key_range_live_test.go`,
  enrolled in the reducer contention gate): set equality with every edge
  holder (last range, NULL impact_status, advisory-only, member-only,
  fact ids below and above every bound) and every exclusion (asking scope,
  tombstones, superseded-only); the dropped-range RED; the flip; the plan
  pin.

## Observability Evidence

`eshu_dp_supply_chain_impact_fanout_read_duration_seconds` takes one sample
per holder-read statement. Labels: `domain`; `kind` (`holders`,
`holders_key_range`, `holders_members`, `mass_probe`,
`holders_key_range_read`); `outcome` (`ok`, `read_timeout`, `read_error`).
Buckets bracket the 2 s bar and the 10 s guard. At 3 AM, read p95 per
`kind` against 2 s, the `read_timeout` share at 10 s, and
`holders_key_range_read` for how long a hot-id read held its snapshot. The
fan-out span and summary log gain `key_ranged_ids` and
`holder_key_range_statements`. The result sub-signals gain
`fanout_key_ranged_ids` and `fanout_holder_key_range_statements`. Timeouts
still count on `eshu_dp_supply_chain_impact_fanout_total{outcome="read_timeout",site="holders"}`.

## Residual

The member arm for ids with member mass (QA: 13,805 to 48,555 rows) stays one
statement and fails the bar cold. GHSA aliases with 13,845 to 49,133 foreign
member rows can still hit the 10 s guard. #10643 stays open.

The second PR, a bucketed foreign-member token index, is designed and not
proven. Its bucket is `strpos('0123456789abcdef', right(fact_id, 1))`, not
md5: md5 fails on FIPS PostgreSQL and the `fips-md5-sql-sweep` gate forbids
it in shipped SQL. Token agreement passed only on the md5 variant. Exactness,
plan cost, and write cost are NOT_CHECKED.

On QA, read holder timeouts by `kind` on the read-duration histogram. A
`read_timeout` with `kind=holders_members` is this open residual. One with
`kind=holders_key_range` is a regression from the key-range change and blocks.
