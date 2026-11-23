# #10880 PR-B2c reader plans: security-alert reconciliation and service-catalog vulnerability loaders

Refs #11198, #11199, #11200, #11201, #10880. Ruling:
[arbiter-10880-prb-ruling.md decision 5](../../../.claude/arbiter-10880-prb-ruling.md)
and the B2c ruling `arbiter-b2c.md`.

Two reducer loaders read supply-chain impact finding facts as input. They now
read `supply_chain_impact_finding_supports` (migration 338), whose payload is the
fact payload verbatim. This note records the read shape, what it costs at 600,000
and 8,000,000 supports, and what the shape needs from the schema.

## The read shape

Both reads use a raw-first shape (`supplyChainImpactSupportRawFirstCTE`): the
supports are selected by their filter in a `MATERIALIZED` CTE, then only those
rows are joined to the active-generation check. The natural single-`WHERE` form
lets the planner drive the join from the active scopes (one
`(scope_id, generation_id)` index probe per active scope), so its cost scales
with the scope count, not the rows kept.

The security-alert reconciliation read is one statement: an alert-and-consumption
fact arm (legacy fact scan, or the keyed sidecar once its marker is ready)
`UNION ALL` the supports arm, each arm ordered and limited by the page size, the
outer query ordered by `fact_id` with the same limit, one keyset cursor. The
supports arm has four filter arms: `repository_id`, `package_id`, `cve_id`,
`advisory_id`. The package, cve and advisory arms carry the literal `<> ''`
guard so a partial btree on the column can serve them.

## Measurements at 8,000,000 supports

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
