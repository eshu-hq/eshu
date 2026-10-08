# Evidence: #7729 — the #6540 EXISTS gate walks the corpus-wide language/type index for a repository whose rows sort late

Scope: `SearchEntitiesByLanguageAndTypeForAccess`
(`go/internal/query/content_reader_entity_search.go`), the content read behind
`POST /api/v0/code/language-query`. The fix is one index, migration
`163_content_entities_repo_language_type_idx.sql`:
`content_entities (repo_id, language, entity_type)`. No Go SQL changes.

This is a **Prove-The-Theory-First** record. The defect was reproduced on the
ops-prod read replica, three predicate-only rewrites were measured and rejected
there, and the index was measured on a scratch clone. No DDL ran on production.

## The defect

The #6540 gate wraps the ordered page read in
`WHERE EXISTS (SELECT 1 FROM content_entities WHERE <same filters>) AND <same filters>`.
For a repository with many JavaScript functions the planner probes
`content_entities_language_type_idx (language, entity_type)` and applies
`repo_id` as a Filter. The index walks the corpus-wide
`(javascript, Function)` entries in heap order and discards every entry that
belongs to another repository until the first row of the target repository.

ops-prod read replica (PG 18.3, 2,240,821 `content_entities` rows),
`repository:r_8946df89` (241,726 rows, 44,433 JavaScript functions), body
`{language: javascript, entity_type: function, limit: 10}`, 2026-10-08:

```text
InitPlan 1: Index Scan using content_entities_language_type_idx
  Filter: (repo_id = 'repository:r_8946df89')   Rows Removed by Filter: 121106
  Buffers: shared hit=155688   actual time=276.233 rows=1
Execution Time: 277.489 ms        (same page without the gate: 5.679 ms)
```

Four other repositories ran the same request in 2.1 to 21.2 ms: the planner
takes `content_entities_repo_idx` for the gate when its estimate for the
repository is small.

## Hypothesis ledger

| candidate | cheapest proof | old | new | accuracy | concurrency | disposition |
| --- | --- | ---: | ---: | --- | --- | --- |
| Drop the gate when `repo_id` is set | ops-prod reader `EXPLAIN` | JS TerraformResource on the slow repo 0.2 ms | 4548.6 ms, 1,431,639 buffers | same rows | no change | rejected |
| Fence the gate onto the repository (`OFFSET 0`) | ops-prod reader `EXPLAIN` | JS Function 277.5 ms | 314.0 ms; kotlin Function 25.6 ms to 387.4 ms | same rows | no change | rejected |
| Gate on the corpus pair only | ops-prod reader `EXPLAIN` | JS Class (repo-empty) 25.5 ms | 1034.1 ms, 1.44M buffers | same rows | no change | rejected |
| Index `(repo_id, language, entity_type)` | scratch clone `EXPLAIN` | gate 51,000 to 55,000 buffers (forced shape) | gate 3 to 6 buffers | same rows | one more btree on a hot table | proven on the clone, production confirmation pending |

Probe details for the three rejected rows (warm, literal parameters, custom-plan
shape; slow repo `r_8946df89`):

| case | current gate | no gate | repo-fenced gate | corpus-pair gate |
| --- | ---: | ---: | ---: | ---: |
| JS Function | 277.5 ms | 5.7 ms | 314.0 ms (105,053 buffers) | gate fixed |
| JS TerraformResource (correlation-empty, OR languages) | 0.2 ms | **4548.6 ms** | not run | not run |
| kotlin Function (repo-empty, corpus-nonempty) | 25.6 ms | 2.9 ms | **387.4 ms** | not run |
| JS Class (repo-empty, corpus-nonempty, OR languages) | 25.5 ms | 979.9 ms (1.44M buffers) | not run | **1034.1 ms** |

No index in the set carries `repo_id` with `language` and `entity_type`, so each
predicate-only form trades one failure class for another.

## Scratch-clone proof of the index

Clone: pod `pg-rehearsal-s1`, PG 18.3, `shared_buffers` 128 MB, default
`random_page_cost`, a 2026-10-04 snapshot with 1,879,576 `content_entities`
rows. The clone ran the four scripts with rc=0 and no ERROR lines. Raw outputs
are in the operator-local 7247 preflight directory, not committed.

**Honest limit.** The clone does not contain `r_8946df89`. Its largest
JavaScript-function repository has 12,982 rows, and the planner already takes
`content_entities_repo_idx` for the gate there (worst natural gate 63.6 ms
first-touch). The clone therefore cannot show the planner choosing the
language/type index for the gate, which is what produced 121,106 discarded
entries on production. It proves the effect of the new index, not the
production plan flip. The "forced" arm below reproduces the production plan
shape on clone data; it is an emulation.

Build: `CREATE INDEX CONCURRENTLY` took 2,227 ms, `indisvalid = t`, size
14,311,424 bytes (14 MB) on 1.88M rows, against 21 MB for `content_entities_repo_idx`
and 17 MB for `content_entities_language_type_idx` on the same clone.

Forced arm: the gate written `(repo_id || '') = ...` with `enable_seqscan` and
`enable_bitmapscan` off, so the gate must use a language/type btree with
`repo_id` as a Filter. In all 14 cases the planner chose
`content_entities_language_type_idx`. The three slow-position repositories
(entries before the first JavaScript-function entry: 126,289, 126,071,
117,648) reproduce the production shape:

```text
S1a r_df9e7bda forced: Rows Removed by Filter 126289, gate Buffers 55170, 204.2 ms first-touch
S1b r_3368ccc4 forced: Rows Removed by Filter 126071, gate Buffers 54998, 180.9 ms
S1c r_d770bcfd forced: Rows Removed by Filter 117648, gate Buffers 50998, 166.8 ms
```

Natural arm, the shipped query with the planner free. Times are
first-touch / repeat in ms. "Gate bufs" is the gate InitPlan's buffer count.

| case | before: gate plan | before ms | before gate bufs | after: gate plan | after ms | after gate bufs |
| --- | --- | ---: | ---: | --- | ---: | ---: |
| S1a r_df9e7bda JS Function | repo_idx | 6.6 / 0.6 | 114 | repo_language_type_idx | 0.8 / 0.5 | 4 |
| S1b r_3368ccc4 JS Function | repo_idx | 6.1 / 0.4 | 53 | repo_language_type_idx | 0.5 / 0.3 | 4 |
| S1c r_d770bcfd JS Function | repo_idx | 4.6 / 0.3 | 11 | repo_language_type_idx | 0.4 / 0.2 | 5 |
| S3a r_957cd853 php Function | Seq Scan | 756.2 / 157.1 | 175 | repo_language_type_idx | 170.6 / 150.1 | 4 |
| S3b r_b0ff4ca0 php Function | repo_idx | 3.4 / 0.1 | 30 | repo_language_type_idx | 0.1 / 0.1 | 4 |
| S5 r_df9e7bda kotlin Function (repo-empty) | Bitmap Heap Scan | 0.4 / 0.3 | 21 | repo_language_type_idx | 0.04 / 0.02 | 3 |
| S6 r_d770bcfd JS Class (repo-empty) | language_type_idx | 0.9 / 0.1 | 265 | repo_language_type_idx | 0.05 / 0.03 | 6 |
| S7 r_df9e7bda JS TerraformResource (corpus-empty) | Bitmap Heap Scan | 0.04 / 0.03 | 6 | repo_language_type_idx | 0.04 / 0.03 | 6 |
| S2 r_df9e7bda hcl Function (corpus-empty) | repo_idx | 4.9 / 2.5 | 1156 | repo_language_type_idx | 0.03 / 0.02 | 3 |
| R1 r_957cd853 JS Function | Seq Scan | 0.7 / 4.5 | 178 | repo_language_type_idx | 0.3 / 0.1 | 4 |
| R2 r_4ff9d0b5 JS Function | repo_idx | 1.7 / 0.6 | 7 | repo_language_type_idx | 0.4 / 0.07 | 4 |
| R3 r_2645123f JS Function | repo_idx | 1.6 / 0.6 | 44 | repo_language_type_idx | 0.3 / 0.07 | 4 |
| R4 r_0cfe3508 JS Function | repo_idx | 3.1 / 0.5 | 27 | repo_language_type_idx | 2.3 / 0.5 | 4 |
| S4 r_4ff9d0b5 php Function (repo-empty) | repo_idx | 1.2 / 0.5 | 305 | repo_language_type_idx | 0.02 / 0.02 | 3 |

Findings:

- With the index the planner chooses it unforced for the gate in all 14 cases,
  as an Index Only Scan with 3 to 6 buffers. The gate cost no longer depends on
  where a repository's rows sit in any other index.
- The gate cost of the forced production shape (51,000 to 55,000 buffers,
  167 to 204 ms on the first, cold run) becomes 4 to 5 buffers whenever the planner takes the new
  index. The forced arm hides `repo_id`, so it cannot use the new index and was
  not rerun after the DDL.
- No case regresses. The repo-empty and corpus-empty cases (S2, S4, S5, S6, S7)
  need 3 to 6 buffers. R1 to R4 gate buffers are 4. R4 first-touch total went
  from 3.1 to 2.3 ms; the page read dominates it.
- S3a (a php-heavy repository) is dominated by the page read, not the gate:
  the page plan touches about 80,000 buffers before and after (150 to 171 ms).
  The gate went from a Seq Scan to a 4-buffer Index Only Scan. This index does
  not change that page plan. It is a separate defect and is not claimed fixed.
- `ANALYZE content_entities` ran after the index build (reltuples 1,879,576 to
  1,884,258), so the after plans also reflect refreshed statistics.

Insert cost, from `s1-wal.sql`: a 93,878-row sample reinserted into a copy of
`content_entities` with its btree indexes (the two GIN trigram indexes dropped
to bound the run; GIN cost is the same in both arms), arms alternated twice,
`CHECKPOINT` before each, `EXPLAIN (ANALYZE, WAL)`:

| arm | WAL records | WAL bytes | insert time |
| --- | ---: | ---: | ---: |
| without the index, round 1 / 2 | 1,152,852 / 1,152,853 | 180,402,348 / 180,404,638 | 3,655 / 3,654 ms |
| with the index, round 1 / 2 | 1,252,071 / 1,252,071 | 190,653,414 / 190,647,125 | 3,984 / 3,957 ms |

The index adds about 1.06 WAL records and about 109 bytes of WAL per inserted
row (+8.6% records and +5.7% bytes on this btree-only copy; the relative figure
is smaller on production, where the GIN indexes add WAL in both arms). The
index measured 1,248 kB on 93,878 rows.

## Cost on production

Not measured. Estimate from the clone: 14 MB on 1.88M rows, so about 17 MB at
2.24M rows. `content_entities_repo_idx` becomes a prefix of the new key; it
stays (no `DROP` in the migration tree) and can be reviewed for removal
separately.

## Cold cache

Not reproduced on production. On the clone, the forced arm's gate reads
50,998 to 55,170 pages first-touch; at the 0.1 to 0.17 ms per cold page
measured on the replica that is 5 to 9 s, the same order as the 17.6 s first
call in the sweep. The clone and the replica differ, so this is arithmetic,
not a measurement.

## Production confirmation (not yet done)

After migration 163 is applied to ops-prod, run read-only
`EXPLAIN (ANALYZE, BUFFERS)` of the gate for `repository:r_8946df89` on the read
replica. Expected: the gate InitPlan is an Index Only Scan on
`content_entities_repo_language_type_idx` with single-digit buffers, and the
page read stays near 5.7 ms.

## Performance Evidence

Performance Evidence: the gate InitPlan for `repository:r_8946df89` JavaScript
Function was an Index Scan on `content_entities_language_type_idx` with
`Rows Removed by Filter: 121106` and 155,688 buffers (277.5 ms warm). On the
clone, the same plan shape (forced) costs 50,998 to 55,170 gate buffers across
three repositories, and the unforced gate with migration 163 costs 4 to 5
buffers. Production confirmation is pending (above).

## Observability Evidence

No-Observability-Change: the change adds one btree index and no code. Index
build progress and size are visible through `pg_stat_progress_create_index` and
`pg_stat_user_indexes`; the query path keeps its existing `postgres.query`
span (`db.operation` = `search_entities_by_language_and_type`).
