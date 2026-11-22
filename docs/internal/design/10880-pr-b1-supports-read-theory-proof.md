# PR-B1 theory proof: a supports-table read for grant-scoped impact reads (#10880)

Refs #10880. Companion to
[the PR-B evidence record](10880-pr-b-reader-moves-and-scoped-fork.md) and
[the PR-B0 execution record](10880-pr-b0-execution-record.md). Governing rule:
the root AGENTS.md "Prove-The-Theory-First" section. This record states the
decision, the threshold, and the deadline before any measurement, and holds no
production code.

## Decision, threshold, deadline (stated before measuring)

- **Decision.** Whether PR-B1 builds a supports table (one row per support, with
  the filter columns, the payload, and the active-generation join kept) so that
  grant-scoped list, aggregate, and explain reads can leave the fact CTE
  (arbiter ruling: option B). The theory under test: a read over a typed supports
  table, with the grant applied to the support row and the same pick as the legacy
  CTE, is no worse than the legacy CTE on buffers and latency at the same row
  counts, and returns the same findings.
- **Threshold.** For each grant shape (scope grant, repository grant, mixed
  grant), at the same row counts:
  1. the supports read returns the same finding set and the same chosen
     support (by fact id) as the legacy read;
  2. its buffers (shared hit plus read) are at most the legacy read's, and its
     execution time is at most the legacy read's, measured as the median of seven
     warm runs; and
  3. it stays inside the capability matrix p95 (1000 ms local, 1500 ms
     production) at the largest row count measured.
  The unscoped winners route must not regress: its plan and timing with the
  supports table present equal its plan and timing without it (the table adds no
  index or join to that read).
- **Deadline.** 90 minutes wall clock from 05:58Z, so 07:28Z. A failed attempt does
  not restart the clock. If the deadline passes with a threshold unproved, this
  record says which part is unproved and PR-B1 does not build on it.
- **Out of scope here.** The same-transaction maintainer, the backfill, the
  contract version, the concurrency proof, and drift telemetry are PR-B1 work
  that follows a passing proof; none is measured here.

## Method

- **Database.** Private `postgres:18` (shared_buffers 1 GB), full production
  bootstrap (358 migrations, 153 indexes on `fact_records`), no other workload.
  The host was busy (load 20 to 50); timings are medians of seven warm runs of
  `EXPLAIN (ANALYZE, BUFFERS)` after one discarded run, so every read was served
  from cache (`read=0` throughout). Cold-cache behavior is not measured.
- **Corpus (synthetic).** 200,000 canonical keys over 2,000 repository scopes
  and 200 image scopes, each key with one support in its repository scope,
  35 percent with a second support in an image scope, and 5 percent with a
  third: 280,000 finding facts, 72,000 multi-support keys (8,000 with three
  supports), plus 300,000 `content_entity` facts as noise. The real
  `RebuildAllWinners` built the winners table (200,000 rows). Supports of a key
  differ in priority score, so the winner is not always the repository copy, and 5
  percent of second supports are `comprehensive` where the rest are `precise`.
- **Legacy read.** The shipped `ListFindingsQuery`, `AggregateCombinedCountQuery`,
  and `ExplainFindingByPublicIDQuery`, called through the stores with the real
  argument shapes, with scoped-token grants (which route to the legacy CTE).
- **Candidate read.** A throwaway `supply_chain_impact_finding_supports` table
  built from the facts (`CREATE TABLE ... AS`): one row per fact with the
  canonical key, scope, generation, finding id, priority score, the
  payload-finding-id flag, the filter columns, the payload, and the source
  confidence; primary key on `fact_id`; indexes on `(scope_id, canonical_key)`,
  `(repository_id, canonical_key)`, and `(finding_id)`. It is not a migration and
  is not committed. The candidate keeps the active-generation join
  (`ingestion_scopes` and `scope_generations`), applies the grant to the support
  row, and picks with the legacy order (`priority_score DESC`, payload finding id
  first, `fact_id ASC`).
- **Equality.** For each grant shape the candidate returned the same 51 list rows
  (finding id and priority score, in order) as the legacy read, and the same
  count total and `critical` bucket.
- **Out of the candidate.** The operator-suppression overlay, the runtime filter
  legs, and the rank and profile filters. The legacy read carries the overlay
  join (a bounded read of the operator scope, empty in this corpus), so the
  comparison slightly favors the candidate. Aggregate inventory is the count
  pipeline with a `GROUP BY` and was not measured separately.

## Results

Grant shapes: a scope grant over 20 repository scopes (about 1,600 findings), a
scope grant over 5 image scopes (3,128), a repository grant over 20 repositories
(1,600), a mixed grant (2,676), and a large scope grant over 400 repository
scopes (32,000, 16 percent of the keys). Status filter `affected_exact`,
priority-descending page of 51.

Three candidate shapes were tried. **OR-form** applies the grant as one `OR`
predicate beside the active-generation join. **Raw-first** reads the granted
supports through the two index arms (repository arm, scope arm, `UNION ALL`) in a
materialized CTE, then applies the active-generation join and the pick.

| Grant | Legacy list | OR-form list | Raw-first list |
| --- | --- | --- | --- |
| 20 repo scopes | 281 ms, 727,456 buffers | 120 ms, 292,271 | **8.5 ms, 7,168** |
| 5 image scopes | 293 ms, 730,556 | 125 ms, 292,271 | not run |
| 20 repositories | 282 ms, 728,456 | 119 ms, 292,271 | **10.6 ms, 7,218** |
| mixed | 284 ms, 730,228 | 125 ms, 292,271 | **17.6 ms, 12,914** |
| 400 repo scopes | 416 ms, 788,256 | 206 ms, 292,271 | 275 ms, 123,510 |
| 400 repositories | not run | 254 ms, 292,271 | 315 ms, 139,343 |

| Grant | Legacy count | Raw-first count |
| --- | --- | --- |
| 20 repo scopes | 282 ms, 727,252 | **7.9 ms, 6,964** |
| 5 image scopes | 288 ms, 730,352 | **18.1 ms, 14,720** |
| 20 repositories | 285 ms, 728,252 | **10.0 ms, 7,014** |
| mixed | 302 ms, 730,024 | **15.7 ms, 12,710** |
| 400 repo scopes | 470 ms, 788,052 | 270 ms, 123,306 |

Explain by public finding id inside a scope grant: legacy 132.5 ms and 284,056
buffers; supports 0.02 ms and 10 buffers (it returns the support the legacy pick
would, `fact:4:1`, the only support of that key).

**Unscoped winners route.** The gate-on list read on the winners table measured
0.58 ms and 403 buffers before the supports table existed and 0.62 ms and 403
buffers after, with the same plan shape. The supports table adds no index or join
to that read.

**Plan shape matters.** The OR-form reads every row of the supports table
(292,271 buffers for every grant) because the planner joins the 2,202 active
scopes first and probes the supports index once per scope; it still beats legacy
because the typed columns are narrower than the fact heap. A first index-driven
attempt that left the active-generation join inside each arm planned the same
way for repository grants and took 2.6 s for the 400-scope grant. Reading the
granted supports first, in a materialized CTE, and joining the generation tables
afterwards made the small grants 16 to 35 times faster than legacy. For the
largest grant (32,000 findings, 16 percent of the keys) the raw-first form is slower than the
OR-form (275 ms against 206 ms) and still faster than legacy (416 ms).

## Threshold check

| Threshold | Result |
| --- | --- |
| Same finding set and chosen support as legacy | **Met** for list (51 rows, ids and priority scores in order), count (total and critical bucket), and explain (existence and support) on all five grant shapes. |
| Buffers and time at most legacy | **Met** for every shape and both reads. Worst case: raw-first 400-scope list at 275 ms and 123,510 buffers against 416 ms and 788,256. |
| Inside matrix p95 (1000 ms local, 1500 ms production) | **Met**: the slowest candidate run was 315 ms. |
| Unscoped winners route not regressed | **Met**: 0.58 to 0.62 ms, 403 buffers, same plan. |

Verdict: the theory holds on this corpus. PR-B1 may build the supports table.
Two conditions travel with it: the read must be written raw-first (the generation
join after the grant arms), and a plan-shape test must pin it, because the
natural single-`WHERE` form reads the whole table and a partly rewritten form was
six times slower than the legacy read for a large grant.

## Census

**Can one scope hold two active supports of one canonical key?** Not for static
rows, by construction. `supplyChainImpactFactID` hashes the logical identity
(`supplyChainImpactLogicalIdentity`: exactly the canonical-key fields, plus the
scan target for derived rows), the scope id, and the generation id (plus the
fencing token for derived rows), and an `ingestion_scopes` row has one active
generation. Two facts of one key in one scope and generation share a fact id and
upsert onto one row. The census therefore proves `(canonical_key, scope_id)`
unique among active static facts for rows the current writer wrote. It does not
cover rows an older writer left in the active generation during a rolling
upgrade (the canonical key has a fallback for payloads without a finding id), so
B1 should key the supports table on `fact_id`, keep `(canonical_key, scope_id)`
as a non-unique index, and run the QA census below before merging. The synthetic
corpus shows 40 keys with two supports in one scope; that is an artifact of the
seed generator (a third support can land in the second support's image scope),
not a property of the writer, and it shows the query detects the shape.

**Can supports of one key differ in precise eligibility?** Yes. The detection
profile is a function of status, observed version, and match reason; the match
reason is not in the key (see the B0 record). The synthetic corpus plants this in
4,000 keys.

**QA census query** (read-only, runs against the read replica; the first three
columns size the table, the last three decide the row identity and the existence
filters):

```sql
WITH active AS (
  SELECT fact.fact_id, fact.scope_id,
    CONCAT_WS('|', <the canonical-key expression of the winners select>) AS canonical_key,
    COALESCE(fact.payload->>'detection_profile', '') AS detection_profile,
    COALESCE(fact.payload->>'image_ref', '') AS image_ref
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
  LEFT JOIN scanner_image_result_fact_classifications AS class ON class.finding_fact_id = fact.fact_id
  LEFT JOIN scanner_image_result_fact_associations AS assoc ON assoc.finding_fact_id = fact.fact_id
  WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding' AND fact.is_tombstone = FALSE
    AND generation.status = 'active' AND fact.scope_id <> 'operator:vulnerability_suppressions'
    AND assoc.finding_fact_id IS NULL AND fact.scope_id NOT LIKE 'image://derived/%'
    AND (class.finding_fact_id IS NULL OR (class.source_class = 'static' AND class.lifecycle = 'active'))
), per_key AS (
  SELECT canonical_key, count(*) AS supports, count(DISTINCT scope_id) AS scopes,
         count(DISTINCT detection_profile) AS profiles, count(DISTINCT image_ref) AS image_refs
  FROM active GROUP BY canonical_key
)
SELECT count(*) AS canonical_keys, count(*) FILTER (WHERE supports > 1) AS multi_support_keys,
       max(supports) AS max_supports,
       count(*) FILTER (WHERE supports > scopes) AS keys_with_two_supports_in_one_scope,
       count(*) FILTER (WHERE profiles > 1) AS keys_whose_supports_differ_on_profile,
       count(*) FILTER (WHERE image_refs > 1) AS keys_whose_supports_differ_on_image_ref
FROM per_key;
```

On the synthetic corpus it returned 200,000, 72,000, 3, 40, 4,000, and 0.

## Storage

The supports shim holds 280,000 rows in 293 MB with its payload column and three
indexes. For the same finding facts the ten dedicated impact indexes on
`fact_records` total 266 MB, and the winners table (with its payload since PR-A)
is 296 MB. Slices 3 and 4 retire the fact rows and drop those ten indexes, so the
supports table replaces the fact payloads rather than adding to them; the net
change is not measured here.

## What this proof does not show

- No cold-cache run, no production-scale corpus, no concurrent load.
- No write cost: the same-transaction insert of supports at write time, the
  backfill duration, lock behavior, and the resweep's read of supports are PR-B1
  work and need their own proof (concurrency proof for the writer and the keyed
  resweep, per the ruling).
- No operator-overlay, runtime-filter, suppression, rank-filter, or profile
  predicate in the candidate; each adds work to both reads.
- Aggregate inventory by dimension and grouped inventory were not measured.
- The plan was chosen on one machine with default statistics targets; a plan-shape
  test in B1 must pin the raw-first form rather than rely on the planner.

## Reproduction

The prover was a throwaway Go test (`EXPLAIN (ANALYZE, BUFFERS)` through the
stores' real constants) and a throwaway bootstrap command, neither committed. The
candidate queries are the ones above: the supports build (`CREATE TABLE ... AS`
over the facts with the listed indexes), the raw-first list (two `UNION ALL`
index arms in a `MATERIALIZED` CTE, then the generation join, then
`DISTINCT ON (canonical_key)` and a `LIMIT 51` page joined back to the support
row), the same pick feeding a `count(*)` with a `FILTER` per bucket, and the
explain read (`WHERE finding_id = $1` with the grant, `LIMIT 2`).

Wall clock: started 05:58Z, last measurement 06:12Z, inside the 07:28Z deadline.
