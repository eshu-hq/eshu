# #10643 holder read: the foreign-member token index (PR 2)

Refs #10643, epic #10860, serving #11147. Follows PR 1
([key ranges](10643-holder-read-bound.md)). Ruling records:
[10643d to 10643k](../arbiter/10643-holder-read-bound-rulings-2026-11-21.md#10643d-to-10643k-the-token-index).

PR 1 bounded the cve_id and advisory_id arms of a hot id's holder read. It left
the advisory-group member arm as one statement on the lossy GIN members index.
A GHSA alias is a foreign member of up to 49,133 finding rows on QA (M20), so
that statement could still hit the 10 s guard. This change splits the member
arm over the 16 key-range statements. #10643 stays open until the QA bar
after deploy is met (below).

## What changed

- A frozen SQL function, `eshu_sci_foreign_member_tokens_v4(fact_id,
  payload) RETURNS text[]`, `IMMUTABLE STRICT PARALLEL SAFE`, built-ins only.
  For a finding row it returns `bucket:member` for every string that jsonb `?|`
  matches on `provenance.advisory_group_members` that is not the row's own
  `cve_id` or `advisory_id`. Those strings are the string elements of a
  top-level array, the keys of a top-level object, and a top-level string
  scalar. It returns NULL when no such string remains. The bucket is
  `strpos('0123456789abcdef', right(fact_id, 1))`, 1 to 16 for a lower-case
  hex last character and 0 for any other. It does not use md5, which fails on
  FIPS PostgreSQL.
- A GIN `array_ops` index on it, `fact_records_sci_foreign_member_tokens_v4_idx`,
  partial on `fact_kind = 'reducer_supply_chain_impact_finding' AND
  is_tombstone = FALSE`. This is the variant T3 and T4r measured.
- A third UNION arm in both key-range statements
  (`FindSupplyChainImpactHolderKeyRangeQuery` on `$5`,
  `FindSupplyChainImpactHolderOpenKeyRangeQuery` on `$4`):
  `eshu_sci_foreign_member_tokens_v4(finding.fact_id, finding.payload) &&
  $N::text[]`, with no fact_id bound. `HolderMemberTokens` binds statement 1
  to buckets 0 and 1 and statement k to bucket k. The separate member-arm
  statement is gone: a hot read is 16 statements, not 17, on one snapshot.
- Not changed: the non-hot path (the shipped single statement and the old
  members index), the mass probe (#11166), the cve_id and advisory_id arms,
  and the snapshot, plan mode, guard and timeout classification of PR 1.

## Why it is exact

A row matches the shipped member arm when the lookup id is in its members.
Either the id is the row's own `cve_id` or `advisory_id`, and the cve or
advisory arm returns that row under the same predicates, or it is not, and the
row carries the token `bucket:id` for its own bucket. Each bucket belongs to
exactly one statement, so over the 16 statements every foreign row is read
once. All 16 run in one read-only REPEATABLE READ transaction, so their union
equals the shipped statement on that snapshot. The bucket does not need to
match the statement's fact_id range: the result is the union.

The function mirrors `?|`. An array-only function loses object-valued and
string-scalar members, which `?|` matches (fixture edge 309). The writer
stores only arrays today (`core/provenance.go`), and the proof does not rest
on that.

## Function text and versioning

The body is the lane's `func4.sql` byte for byte. Its sha256 is
`f27cf0276991ca3faf9ab96a436dd3699e02427a064ffe3d989f0a1d0216bb90`, pinned
on the Go constant (`TestForeignMemberTokensFunctionIsTheFrozenV4`) and on
`pg_proc.prosrc` after the migrations
(`TestHolderKeyRangeTokenIndexValidFromMigrationsLive`). The body is never
altered after release. A change needs a new function name, a new index, a
new migration, the F1 matrix and a whole-fixture identity scan against v4.
The migration uses `CREATE OR REPLACE` with the same body only so that the
local supervisor can re-apply it.

## Migration and deploy order

- `336_eshu_sci_foreign_member_tokens_v4.sql` creates the function. It is
  catalog-only and takes no table lock. 333 is held by the #10882 lane and
  335 is taken on main (#11131 audit sweep); the gaps pass the order gates.
- `337_fact_records_sci_foreign_member_tokens_v4_idx.sql` builds the index
  `CONCURRENTLY IF NOT EXISTS` as one statement, the pattern of 289. A failed
  build leaves an INVALID index. The apply path drops it by name before
  re-running the file (`concurrentIndexNamesForInvalidCleanup`, asserted in
  `TestForeignMemberTokensMigrationsMatchTheGoDDL`).
- The Go DDL (`schema_fact_records_supply_chain_impact_indexes.go`) carries
  the same function and the same index without CONCURRENTLY.
  `TestForeignMemberTokensMigrationsMatchTheGoDDL` and `TestDDLMigrationParity`
  keep the two in step.
- The index is valid before the reader uses it. The schema Job runs
  `ApplyBootstrap` over the embedded migrations as a pre-install and
  pre-upgrade hook. `CREATE INDEX CONCURRENTLY` returns only once the index
  is valid, and the Job fails otherwise. The new reducer rolls out after the
  Job, and pod readiness checks the migration ledger. The live tests bootstrap
  through the same migration path, and
  `TestHolderKeyRangeTokenIndexValidFromMigrationsLive` asserts the index is
  valid, ready, GIN and `array_ops`, and partial on the kind and live rows.
- Without a valid index the token arm would scan finding rows and call the
  function on each. The guard bounds that at 10 s per statement, and the
  timeout counts under `kind=holders_key_range`.

## Tests in this change

Each test failed first (RED commit `dbf4328953`). The live tests ran on a
disposable postgres:18.6.

| Test | Proves |
|---|---|
| `TestHolderMemberTokensPartitionTheBuckets` | every (bucket, id) pair is carried by exactly one statement for 16, 1, 3, 17 and 40 statements; bucket 0 rides in statement 1 |
| `TestHolderKeyRangeStatementsCarryTheTokenArm` | derived from the shipped constants: three arms, the token arm on `$5` or `$4`, no fact_id bound, no `advisory_group_members` read |
| `TestForeignMemberTokensFunctionIsTheFrozenV4` | the name, signature, attributes and body digest; the bucket alphabet in all three branches |
| `TestForeignMemberTokensMigrationsMatchTheGoDDL` | the migrations carry the Go DDL byte for byte; the function sorts first; the index migration is one concurrent statement |
| `TestHolderKeyRangeTokenIndexValidFromMigrationsLive` | the function stored as v4 and the index valid from the migration path |
| `TestHolderKeyRangeTokenShapeMatrixLive` | F1: 840 cells, 840 agree, 112 true, 14 shapes; the array-only RED disagrees |
| `TestHolderKeyRangeTokenAgreementLive` | Go lookup tokens against SQL stored tokens for 21 last characters (16 hex digits, `A`, `Z`, `é`, `日`), colon members, object keys, a scalar, prefix look-alikes and own keys |
| `TestHolderKeyRangeTokenReadMatchesShippedLive` | the differential against `FindHolderScopes`, through the store and through the shipped constants, and three REDs (below) |
| `TestHolderKeyRangeStatementPlanLive` | the pinned custom plan: the token arm is a Bitmap Index Scan on the token index with `&&` in the Index Cond and no fact_id bound |
| `TestKeyRangePlanViolationShapes` | the plan checker refuses a missing token arm, the lossy members index, a token heap filter, and a token fact_id bound |

The differential seeds cover the object, scalar, NULL `cve_id`, NULL
`advisory_id`, missing, empty, JSON null, number, nested, colon, prefix,
tombstone and superseded-generation shapes, plus bucket-0 fact ids that end in
a non-hex or an upper-case character. Its REDs are each derived from the
shipped statements. Dropping bucket 0 from statement 1 loses `tok:bucket0`
and `tok:upper`. Dropping the token arm loses every foreign-only holder.
Swapping in the array-only function loses `tok:object` and `tok:scalar` and
keeps the array holders.

## Fixture proofs (theory first)

Lane pr2-t measured these on a local PostgreSQL 18.6 fixture: 1,008,010
`fact_records` rows, toasted payloads, and an alias with 93,561 foreign rows.

- T1 exactness: the 16 three-arm statements equal the shipped statement on
  one snapshot for the alias and for an id that is hot both as an own key and
  as a foreign member. This is run 1 plus the composition after the fix (F2
  and F4).
- Identity: v4 equals v3 on all 997,633 live finding rows (0 differ), and v3
  differs from the array-only v2 on exactly the two non-array seeds.
- F1 on v4: 840 of 840.
- T2: the seeded live agreement test, and the corpus bucket range (0 tokens
  outside 0 to 16).
- T3 plan, generic and custom: the member arm uses the token index with 0
  rows removed by recheck and 0 lossy blocks. A CVE lookup with no foreign
  rows reads 0 member-arm heap blocks.
- T3 cost A, buffers per returned row: alias 1.0177 against cve 1.0532, ratio
  0.9663 (bar 1.5).
- T3 cost B. B1, cold blocks per row: ratio 0.9667 (bar 1.5). Condition L:
  pooled dispersion differs by 0.0002 (bar 0.02). B2, projected to QA's
  layout: the largest alias statement scaled to 49,133 rows is 3,185 blocks
  against 5,031 for the largest cve-arm statement.
- Layout: the 0.9 bar was withdrawn by 10643f and is not claimed. Recorded
  only: whole-id pages per row are 0.870 on the fixture and 0.7896 on QA (one
  id, 5,000 rows).
- Stress, not gated: the largest alias statement equals the largest cve-arm
  statement on QA's layout at N = 77,605 foreign rows. QA's largest alias is
  49,133, so N is under twice that (98,266). Foreign rows grow until the
  #9432 cutover.
- Index size on the fixture: 2,301,952 bytes for 93,563 token rows. The build
  took 96 s.

## Write cost, as measured

The write cost is accepted as a stated cost (10643k decisions 2 and 3). The
measurement inserted 50,000 finding rows with and without the index, in four
A B B A pairs on the fixture. 9.33 percent of those rows carry a foreign
member by the array-only count; the v4 share was not counted separately.

- WAL bytes and relation blocks written: within +10 percent (mean of the pair
  deltas plus 2 SE: 5.25 and 3.59 percent).
- CPU on the fixture's insert statement: over +10 percent. The two noisy
  rounds gave +14 percent (array-only function) and +33 percent (v3). The
  function call costs about 15 us per row in a `SELECT` (v4: 14.840 us). The
  measured total add is about 42 us per row, at most about 62 to 103 us per
  row. That is about 1 percent of QA's 10.8 ms per row
  (`BatchInsertVersionedQuery`: 10,807 us per row and 83,820 WAL bytes per
  row). At 280 rows per second the add is 2.88 percent of one core.
- This change does not claim to reduce primary load. The long statements it
  removes were not measured as load.

## Deploy review (no deploy is authorized)

This change authorizes no deploy. The QA primary is CPU-throttled: 11.958 of
12 cores at p95 over 6 hours, and a throttled share of 0.28 to 0.99. The
deploy review must carry each item below.

The index build is the main risk. The projection below comes from lane
constants and was not measured. The fixture built at 114 to 133 s per million
warm rows. QA has about 8.4 million finding rows with cold payloads, roughly
25 million block reads, near three hours at one stream's 2,500 reads per
second. For that time the build is one more read stream on the last spare
IOPS and one more core on a throttled container. A concurrent build also
holds back the vacuum horizon.

- Time window. Name the start, the expected end, and a hard stop.
- Readings before, during and after the build: primary CPU p95 and throttled
  share as context only (they swing within hours and cannot attribute a
  change), IOPS, replication lag, and the oldest `backend_xmin` age.
- The build's actual duration and reads, from `pg_stat_progress_create_index`
  during the build and the Job log after it.
- Cancel rule. At the hard stop, or if replication lag or the vacuum horizon
  passes the review's stated limit, cancel the Job. Then confirm
  `indisvalid = false` for `fact_records_sci_foreign_member_tokens_v4_idx`.
  The next schema apply drops the invalid index by name before it rebuilds
  (`concurrentIndexNamesForInvalidCleanup`). To leave it absent until a new
  window opens, drop it with `DROP INDEX CONCURRENTLY IF EXISTS
  fact_records_sci_foreign_member_tokens_v4_idx`. The new reducer must not
  roll out until the index is valid.
- Benefit. From `pg_stat_statements`, take calls, mean and total time of the
  old holders and member query ids before the deploy. After it, take the same
  figures for the new three-arm key-range query ids.
- Share gate. Measure over at least 30 minutes after the deploy. If any new or
  changed statement holds more than 5 percent of total statement time, it
  returns to the arbiter before the next deploy.

The QA bar after deploy follows. It is NOT_CHECKED here.

- A 4-way cold burst over the three GHSA aliases and CVE-2026-14257. Every
  statement must have p95 under 2 s and max under 5 s, with zero 57014. Then
  two stable reads with zero new fan-out dead letters. If a wall-time bar
  fails, read the throttled share for that window before blaming the design.
- Holder timeouts. Any `read_timeout` with kind `holders_key_range` or
  `holders_key_range_read` blocks. `holders_members` must show zero new
  events, which proves the old statement is gone.
- One differential on real data: the shipped member statement against the 16
  ranged statements for the 13,845-row alias on the replica, set difference
  0 and 0.
- The index size and entry count, which give QA's foreign-member row share.
- A new reading of `BatchInsertVersionedQuery` over a like window. A rise of
  more than 1,081 us per row returns to the arbiter.

## Performance and observability

Performance Evidence: the fixture T3 counts above (cost A 0.9663, B1 0.9667,
B2 3,185 <= 5,031 blocks) and the T4r write rows. No wall-time claim is made
locally; the QA bar after deploy is the wall-time truth.

Observability Evidence: each key-range statement, the token arm included, is
one sample of `eshu_dp_supply_chain_impact_fanout_read_duration_seconds{kind="holders_key_range"}`,
and the whole read is `kind="holders_key_range_read"`. A timeout counts on
`eshu_dp_supply_chain_impact_fanout_total{outcome="read_timeout",site="holders"}`
and names the statement index in the dead-letter message. `holders_members`
is retired. `fanout_holder_statements` now counts 16 per hot id, not 17.
One statement cannot be timed by arm (ruling 10643l). Instead, a key-range
statement that fails or runs at or over the 2 s bar adds the
`holder_key_range_statement` event on the `supply_chain_impact.fanout` span
and the warn log `supply chain impact holder key-range statement slow or
failed`. Both carry `range_index` (1 to 16), `range_count`, `non_cve_lookup`
(the lookup set held a non-CVE id, so the token arm was in play), `kind`,
`outcome` and `duration_seconds` (`noteKeyRangeStatement`, pinned by
`TestKeyRangeStatementSignalsNameTheRangeAndLookupKind`).

## Unverified

- QA's object-valued or scalar members (a census needs a detoast scan). The
  function matches them either way.
- QA's foreign-member row share. The index entry count after deploy gives it.
- Function cost inside an index insert against inside a `SELECT`. The
  measured index arm rose more than the function body accounts for.
- The plan under a generic plan on QA. The store pins `force_custom_plan`.
