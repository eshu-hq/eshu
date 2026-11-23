# #10880 PR-B2c reader plans: security-alert reconciliation and service-catalog vulnerability loaders

Refs #11198, #11199, #11200, #11201, #10880. Ruling:
[arbiter-10880-prb-ruling.md decision 5](../../../.claude/arbiter-10880-prb-ruling.md)
and the B2c ruling `arbiter-b2c.md`.

Two reducer loaders read supply-chain impact finding facts as input. They now
read `supply_chain_impact_finding_supports` (migration 338), whose payload is the
fact payload verbatim. This note records the read shape, what it costs at 600,000
and 8,000,000 supports, and what the shape needs from the schema.

## The read shape

Both reads page the supports table with the statement of the hot-key ruling
(`supplyChainImpactSupportsPageQuery`, one builder, two texts: first page and next
page). Each filter arm is a per-key ordered `LIMIT 500` index scan:
`unnest($n) AS k(v) CROSS JOIN LATERAL (SELECT s.fact_id FROM supports s WHERE
s.col = k.v [AND s.fact_id > $cursor] ORDER BY s.fact_id LIMIT 500)`. Nothing about
the scope tables sits inside the `LIMIT`, so it fences the planner: the page
stops after 500 index entries per key and cannot flip to one probe per active
scope. The arms are `UNION ALL`, cut to the first 2,000 ids, made distinct and cut
to 500: the raw page. The outer query joins the raw ids to the supports by primary
key, `LEFT JOIN`s the two scope tables on their keys, and returns every raw row
with `is_active` and the payload only when active.

The reader appends the active rows, advances its cursor on the last raw id and
stops only when the raw page holds fewer than 500 rows. Each page is its own
autocommit statement. The security-alert loader drains its alert and consumption
facts on their own cursor and then the supports, so order equality is per kind.
The package, CVE and advisory arms repeat the literal `<> ''` of their partial
btrees; the package one comes from the same constant as migration 345's predicate.

The statement it replaced, one `OR` over the four arms in a `MATERIALIZED` CTE,
and the earlier measurements below are kept as the record of why: an `OR` is only
indexable when every arm is, and on a hot key the CTE materializes every match.

## Measurements at 8,000,000 supports (the superseded OR statement)

Disposable PostgreSQL 18.6 (`postgres:18.6`), shared_buffers 2 GB, work_mem 64 MB,
two parallel workers per gather, host load average 14 to 34 from other lanes.
Corpus (synthetic): 8,000,000 supports, heap 5,207 MB, 9,000 active scopes with
one active generation each, 400,000 repositories, 60,000 packages, 40,000 CVEs,
50,000 advisories, the migration-338 primary key and `(scope_id, generation_id)`,
`repository_id`, `finding_id` and `generation_id` btrees, plus partial btrees
`(cve_id, fact_id) WHERE cve_id <> ''` (337 MB) and
`(advisory_id, fact_id) WHERE advisory_id <> ''` (304 MB). The candidate index is
`(package_id, fact_id) WHERE package_id <> ''`: 451 MB, built `CONCURRENTLY` in
115 s. `LIMIT 500`; each shape is the supports arm of the loader statement with
its other arms bound to empty arrays.

Raw `EXPLAIN (ANALYZE, BUFFERS)` output: the rerun is
[reader_plans_8m_raw_rerun.txt](reader_plans_8m_raw_rerun.txt) (every row below;
block headers carry load and memory), and the first run, on a container that was
later lost, is [reader_plans_8m_raw.txt](reader_plans_8m_raw.txt). "First run" is
the first execution of the shape after a container restart: `shared_buffers` is
empty, the OS page cache was not dropped. "Second run" repeats it at once.

| Shape | With the package index, first / second | Without it, first / second |
| --- | --- | --- |
| all four arms (3 repos, 2 packages, 3 CVEs, 2 advisories; 1,022 rows) | 583.6 ms / 5.1 ms | 52,186 ms / 828.6 ms |
| repository only (60 rows) | 1.47 ms / 1.22 ms | 769 ms / 729 ms |
| package only (268 rows) | 2.44 ms / 1.94 ms | 749 ms / 693 ms |
| CVE only (534 rows) | 3.50 ms / 2.88 ms | 753 ms / 705 ms |
| advisory only (160 rows) | 1.95 ms / 1.55 ms | 719 ms / 703 ms |
| prepared, `force_generic_plan`, bound parameters, all four | 5.61 ms | 968 ms |
| prepared, generic: repository / package / CVE / advisory only | 1.44 / 2.42 / 3.91 / 2.26 ms | not run |

Plans: with the index every shape is a `BitmapOr` over the four index scans
(`s_repo`, `s_pkg`, `s_cve`, `s_adv`); unused arms are bound to empty arrays and
cost 0 ms (`Index Searches: 0`), also under the generic plan. The all-arms read
touches 3,869 buffers on its first run and 3,866 on the second. Without the index
every shape is a parallel sequential scan of the 5.2 GB heap, including the
repository-only and CVE-only shapes that have their own btrees.

The earlier run on the lost container, same corpus shape, was colder: the
sequential scan took 93 to 111 s for the repository, package and CVE shapes and
4.8 s for the advisory shape, the join form 159 s with 7.25 million buffer hits,
and the package index brought the package-only read to 86 ms and the all-arms
read to 172 ms.

What the numbers say:

- The filter is an `OR`. An `OR` is only indexable when every arm is. Without a
  `package_id` index the planner scans the whole table for every shape.
- The sequential scan is fast (0.7 to 0.97 s) only while the 5.2 GB heap sits in
  the OS cache, and 52 to 111 s when it does not. A reducer worker does not keep
  5 GB hot.
- With the partial `package_id` btree the read is 1.4 to 5.6 ms warm and 584 ms on
  the first run after a restart (858 buffers read), bounded by the index.

The same shapes at 600,000 supports (6,000 scopes, 419 MB with indexes): join form
313 to 389 ms; raw-first 0.1 to 1.1 ms for the repository arm, 3.9 ms for all
four arms with three new btrees, 51 ms for all four arms with none.

**The reconciliation read at QA scale depends on the partial
`(package_id, fact_id) WHERE package_id <> ''` btree on the supports table.**
Not measured here yet: the hot-key page cost and the write cost (WAL) of the
extra index.

## Hot key: a package with 1,600,000 supports

Raw output: [hot_key_raw_noindex.txt](hot_key_raw_noindex.txt),
[hot_key_raw_index.txt](hot_key_raw_index.txt) and
[hot_key_raw_variants.txt](hot_key_raw_variants.txt) for the superseded OR
statement and the variants tried, and
[hot_key_ruled_statement_raw.txt](hot_key_ruled_statement_raw.txt),
[hot_key_bounds_raw.txt](hot_key_bounds_raw.txt),
[plan_guard_hot_corpus_raw.txt](plan_guard_hot_corpus_raw.txt) and
[hot_key_drain_binary_raw.txt](hot_key_drain_binary_raw.txt) for the ruled
statement. Corpus: 3,005,000 facts, of which the hot package has 1,600,000 impact
findings, 150,000 consumption correlations and 5,000 alerts; 2,600,000 supports,
1,600,000 for the hot package; 9,000 active scopes; the full bootstrap schema
(fact_records 6.4 GB, supports 3.0 GB). Bound parameters, `LIMIT 500`.

The OR statement (merged with the fact arm) against the legacy fact read of
`bccdb88982`, with `s_pkg`, `s_cve` and `s_adv` present, after a restart:

| Page | Legacy fact read, first / second | Merged statement, first / second | Buffers, legacy / merged | Temp blocks written |
| --- | --- | --- | --- | --- |
| page 1 | 62,186 / 20,696 ms | 20,142 / 6,349 ms | 3.03M / 5.52M | 0 / 120,463 |
| midpoint | 11,489 / 2,584 ms | 3,526 / 3,523 ms | 3.03M / 3.13M | 0 / 60,238 |

Legacy wall time swung 10x between two passes on identical buffer counts. With a
package that covers 61% of the table the planner scanned the supports
sequentially and materialized the whole match set in the CTE, which spilled to
temp. `NOT MATERIALIZED` and per-arm lateral variants with the active check inside
avoided the temp but drove from the 9,000 active scopes (2.4M to 2.6M buffers). An
ordered scan of `s_pkg` with `LIMIT 500` and no active-generation predicate took
2.9 ms and 510 buffers; any formulation that states the active check makes the
planner leave it. That is why the ruled statement keeps the active check outside
the fence.

The ruled statement, single hot key (`pkg:hot`, 1,600,000 supports), warm second
run, both plan cache modes (`force_generic_plan`, `force_custom_plan`), temp 0,
no sequential scan:

| Page | Buffers | Time |
| --- | --- | --- |
| page 1 | 5,958 | 3.8 to 3.9 ms |
| midpoint (cursor at the 800,000th id) | 5,944 | 3.9 to 4.2 ms |
| last page (250 rows left) | 2,959 | 1.9 to 2.2 ms |
| 200-key filter, three hot keys (1.6M, 188k, 188k), page 1 | 9,130 | 7.3 to 7.7 ms |
| 200-key filter, three hot keys, next page | 9,128 | 7.9 to 8.1 ms |

The first execution after a container start is cold: 763 ms for page 1, 407 ms for
the midpoint. The bounds of the ruling (at most 6,000 buffers, temp 0, at most 50
ms warm, midpoint at most 1.5x page 1) hold for the single hot key, with page 1 at
5,958 of 6,000. The plan guard (`TestSupplyChainImpactSupportsPagePlanGuardLive`)
is green on this corpus for both modes and both page texts, and the package-index
and cursor-as-OR violations fail it. On this corpus the planner keeps the ordered
scans even with the active join inside the lateral, so that violation fails the
statement-text test, not the plan guard; on the 400,000-row seed with 300 scopes
it fails the plan guard too.

Drain of the hot key on a built binary (`ListActiveSecurityAlertReconciliationFacts`,
package filter only): 3,201 supports pages, 1,600,000 raw rows, all active.
Cold first run: supports 468.5 s, whole loader 554.9 s, peak RSS 3.8 GB. Second run
in a fresh process: supports 186.3 s, loader 269.1 s. With the real
`SecurityAlertReconciliationHandler` on top (a discarding writer, scope-2, 5,009
alerts evaluated): handler 723.5 s, supports drain 416.4 s inside it, peak RSS
5.26 GB. A page is 3.9 ms in the database; the remaining 58 to 146 ms per page is
the client reading 500 payloads of about 540 bytes and decoding them, plus cold
buffers. The legacy drain of the same key was estimated, not run, at 2.5 hours or
more (1.06e10 buffer touches).

The same statement on the 8,000,000-support corpus, five selective shapes
([selective_8m_ruled_raw.txt](selective_8m_ruled_raw.txt)), first page, both plan
cache modes, `s_pkg` built in 75 s:

| Shape | Buffers | Warm | First execution after a restart |
| --- | --- | --- | --- |
| repository only (3 keys) | 494 | 2.5 to 2.6 ms | 260 ms |
| package only (2 keys) | 1,631 | 4.0 to 4.1 ms | 835 ms |
| CVE only (3 keys) | 2,870 | 5.5 to 5.6 ms | 669 ms |
| advisory only (2 keys) | 1,011 | 3.1 to 3.3 ms | 387 ms |
| all four arms | 3,191 | 6.3 to 6.4 ms | 6.3 ms (cached by the other shapes) |

All shapes are at or under 3,191 buffers and 6.4 ms warm, against the ruling's
bounds of 7,738 buffers and 15 ms. The superseded OR statement read 3,869 buffers
and took 5.1 ms warm for the all-arms shape.

## The repository arm on a repository with 160,000 supports

QA holds repositories of an estimated 120,000 to 150,000 findings, above the
ruling's 20,000 line for the repository arm, so it was measured
([repository_arm_raw.txt](repository_arm_raw.txt)) on `b2c_hot` with `repo:hotrepo`
seeded to 160,000 supports, repository array only, both plan cache modes:

| Index | Page | Buffers | Time warm | First after restart |
| --- | --- | --- | --- | --- |
| `(repository_id)` only (migration 338) | page 1 | 108,862 | 103 to 115 ms | 797 ms |
| | midpoint | 108,862 | 104 to 117 ms | 108 ms |
| | last page (250 left) | 106,101 | 90 to 96 ms | 97 ms |
| `(repository_id, fact_id) WHERE repository_id <> ''` | page 1 | 5,909 | 3.7 ms | 374 ms |
| | midpoint | 5,897 | 3.7 to 3.8 ms | 153 ms |
| | last page (250 left) | 2,958 | 1.8 to 2.0 ms | 99 ms |

With only the existing index each page reads every one of the repository's 160,000
supports and keeps the first 500 by `fact_id` (a top-N sort, no temp): 108,862
buffers per page, 320 pages to drain the repository, about 35 million buffers.
With the candidate the page is an ordered Index Only Scan and costs what a hot
package costs. Index size and build time: 261 MB and 33.4 s at 2,600,000 supports
(the `(repository_id)` btree is 42 MB); 433 MB and 82.8 s at 8,000,000 (63 MB);
151 MB and 10.3 s at 1,500,000 (11 MB). The candidate is about 6 times the size of
the btree it would serve, so it is a candidate to replace `(repository_id)` rather
than sit beside it; that, and its WAL, are not measured here (SLOT 3).

## When the package array is non-empty

`securityAlertReconciliationFilter` (`reconciliation_handler.go`) builds the four
arrays from the intent's own envelopes: a provider repository-alert fact adds its
`repository_id`, `package_id`, CVE and GHSA ids, and a package-registry package
fact adds its `package_id` (or its scope id). So the package array is non-empty
for every item that carries a package-registry package fact and for every
provider alert that names a package. How often that is in QA data was not
measured here.

## The legacy payload array arms

The legacy reconciliation statement also matched `payload->'cve_ids' ?| ...` and
`payload->'ghsa_ids' ?| ...`. The typed finding payload
(`reducerderivedv1.SupplyChainImpactFinding`) has no such field, so those arms
never matched a finding fact, and keeping them would make the OR unindexable.
`TestSupplyChainImpactFindingPayloadCarriesNoIDArrays` fails if either field is
ever added.

## Equality

`TestSecurityAlertReconciliationAndAdvisoryLoadersEqualFactReadsLive` writes a
multi-scope seed through the production impact writer (two repositories sharing
a package, a CVE-less advisory finding, a repository-less finding, a superseded
generation, a retracted finding, interleaved alert and consumption fact ids) and
old-writer direct inserts (an operator-scope row, a numeric and a blank
repository id, a tombstone). It compares each new read with the frozen
finding-fact read for every filter shape: same fact ids, same kind, same payload,
same order. It runs once after the backfill is ready and once after an old-writer
insert and the drain. Deleting the CVE arm of the supports query fails its `cve`
and `combined` cases.

## Census of finding-fact reads left in the paths

`git grep` over `go/internal/storage/postgres` for the finding fact kind, outside
tests: the security-alert and service-catalog loaders read none (the one literal
in `facts_active_security_alert_reconciliation.go` is the kind the supports arm
reports). The remaining readers are the impact holder reads (fanout, key range,
mass, repository holders, scoped packages, out of stamp, the Go-modules fence:
#11197, PR-B2b), the supports backfill, drain and maintainer, the schema's partial
indexes, and the canonical-winner audits. `securityAlertReconciliationFactKinds`
and the three key-backfill statements no longer list the impact kind
(`TestSecurityAlertReconciliationPathsReadNoFindingFacts`). Rows the key sidecar
built for impact findings before this change stay, unread.

## Poll cost of a not-ready deferral

While the supports table is not ready a reducer intent defers without counting an
attempt. `TestSupplyChainImpactSupportsNotReadyDefersNonCountingLive` runs the real
queue, fact store and reconciliation handler on PostgreSQL 18.6 and logs one cycle
(claim, alert-fact load, readiness probe, retry update): 4 statements, 22 to 66 ms
on a loaded host, 19 to 163 KB of WAL (163 KB on the first cycle, 19 to 52 KB
after). The intent re-claims about every 60 s. This is measured in the test, not
on a built binary.

## No-Regression Evidence

No-Regression Evidence: baseline is the finding-fact loaders on `5bec799579`
(join form, 313 to 389 ms at 600,000 supports and 159 s at 8,000,000, cold);
after is the supports loaders on PostgreSQL 18.6 over the synthetic corpora
above: raw-first, 0.1 to 51 ms at 600,000 and 0.09 to 0.17 s at 8,000,000 with a
`package_id` btree, 0.9 s warm and about 100 s cold without it. Worker, lease and
batch settings are unchanged: each loader is one read statement per page. The
finding-fact write path gains no statement; the advisory-group cell admission
uses the production writer step (one more statement per admission batch, the
supports upsert, in the same transaction).

## Observability Evidence

Observability Evidence: `eshu_dp_supply_chain_impact_supports_reads_total`
(`source` = reader, `outcome` = `served`, `not_ready`, `failed`) shows each
loader's use of the supports table; a refusal is a retryable, non-counting error
with failure class `supply_chain_impact_supports_not_ready` in the durable queue.
`eshu_dp_supply_chain_impact_supports_backfill_ready` says when the refusals
stop. Both are documented in
[supply-chain-impact-supports.md](../../../public/reference/telemetry/supply-chain-impact-supports.md).

No-Observability-Change: #11200 (the key sidecar stops covering the impact kind)
changes no signal; the existing `eshu_dp_supply_chain_impact_supports_reads_total`
counter and the key backfill status keep their meaning.

## Open points

- The `package_id` btree above needs a migration and an arbiter ruling on its
  write cost before the reconciliation reader is enabled at QA scale.
- `LookupKeyBackfill` and `MarkSecurityAlertReconciliationKeysReady` have no
  production caller, so the keyed reconciliation reader is never ready unless a
  marker is set by hand (recorded on #10875).
