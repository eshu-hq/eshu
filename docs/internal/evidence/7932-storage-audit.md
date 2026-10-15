# #7932: generation history and search-index storage audit

Refs #7932, #7934. Related: #7585 (retention policy), #7399 (staged
activation), #7420, #7427.

This is a read-only audit. It changes no retention setting, deletes nothing, and
authorizes no deletion, VACUUM, DDL, or deployment change. The bounded queries
live in `7932-storage-attribution.sql`; the seeded fixture that checks them is
`7932-storage-attribution-fixture.sql`.

## What this audit can and cannot claim

| Claim | Status |
|---|---|
| Relation sizes, row estimates, generation counts, and the retention settings of the deployment | Reported in #7932 and its comments on 2026-10-10 and 2026-10-11. Not re-measured here: this work has no access to that database. |
| The lifecycle categories the attribution query assigns | Proved here on a seeded fixture (below). |
| Bytes by lifecycle category on the deployment | NOT_CHECKED. The query is ready; it has not run against the deployment. |
| Cleanup cost per generation (rows, buffers, WAL, lock hold) | Measured here on a scratch PostgreSQL 18.6 with the production schema, at fixture scale. Production scale is labelled as such. |
| Physical reclaimable bytes | NOT_CHECKED. A DELETE returns space to the table, not to the filesystem. No bloat measurement exists. |

## Method

`7932-storage-attribution.sql` runs in one `READ ONLY` transaction with a
60 s statement timeout and a 1 s lock timeout. It has three sections:

1. Relation bytes by family: heap, indexes and TOAST, partitions rolled up to
   their root. Catalog and file-size reads only.
2. One row per generation lifecycle category, with a deterministic sample
   (`md5(generation_id)` order, `:sample` per category) of exact row counts in
   `fact_records`, `eshu_search_index_documents`, and `eshu_search_index_terms`.
   A category total is the sample mean times the category size, so it carries
   sampling error; `sd_*` shows the spread.
3. The retention event ledger for the last 14 days.

The category predicate mirrors `generationRetentionCandidateQuery`
(`go/internal/storage/postgres/generation_retention_sql.go`, the
`ranked_superseded_generations`, `last_activated_full_write`, `live_work` and
`eligible` CTEs). The categories are:

| Category | Meaning | Retention can prune it |
|---|---|---|
| `active` | the scope's active pointer | no |
| `pending`, `failed`, `other_status` | not superseded | no |
| `superseded_no_timestamp` | superseded with NULL `superseded_at` | no |
| `superseded_pinned_live_work` | a claimed, running or retrying work item names it | no, until the work ends |
| `superseded_uncovered_writer` | wrote, never activated, newer than the last full activation (#7472) | no |
| `superseded_eligible` | past the soft age and rank, or the same-SHA arm, or past the hard ceiling | yes |
| `superseded_retained` | superseded and none of the above: kept by policy | no |

"Pinned" has no flag in the code. `generation_retention_policy.go` describes
dependency-pinned generations as the active pointer, live work and uncovered
writers; the query implements exactly those three exclusions. A delta
baseline is not a retention predicate on main: `projector_queue_delta_baseline.go`
fences the Ack path, and the changed-since ledger is pruned with its generation
(#7127). #7802 reports that a delta-active scope can lose its base full
generation to retention past the hard ceiling, and PR #8139 adds that guard; it
was open and unmerged when this audit was written. Until it merges, any
relaxation of the hold (recommendation 2) can prune a delta scope's base.

## Proof that the categories are assigned as stated

`7932-storage-attribution-fixture.sql` seeds every category at a known count. The
query was run on it with the shipped defaults (`sample=40`, `soft_hours=168`,
`min_newer=24`, `hard_hours=2160`) on PostgreSQL 18.6:

| Category | Seeded | Query result |
|---|---:|---:|
| superseded_retained | 29 (24 by rank in S1, 5 by age in S2) | 29 |
| superseded_eligible | 13 (6 by rank in S1, 3 by hard age in S3, 4 same-SHA in S6) | 13 |
| active | 7 (6 seeded plus the schema's own `eshu:global` genesis scope) | 7 |
| superseded_pinned_live_work | 2 | 2 |
| superseded_uncovered_writer | 2 | 2 |
| superseded_no_timestamp | 1 | 1 |
| pending | 1 | 1 |
| failed | 1 | 1 |

The first fixture expectation said 6 active. The query returned 7 and was right:
the schema seeds an `eshu:global` genesis scope. The fixture comment was
corrected, not the query. Sampled means matched the seeded per-generation row
counts (10 facts, 5 documents, 50 terms; active 20/10/100 with the genesis
scope at its own size).

This proves the predicate, not the deployment's bytes.

## What the deployment numbers say

All of this is from #7932 and its comments, not re-measured.

- The database is about 2.8 TiB. Search terms are about 2.06 TiB, 65% of it
  indexes. Fact records are about 0.5 TiB. Search documents are about 0.19 TiB.
- There are 827 active generations (one per scope), 30,873 superseded
  generations with no active pointer, and one failed generation. Superseded
  timestamps run from 17 September to 11 October.
- Both the soft and the hard superseded-age limits are explicitly 87600h
  (10 years) in the inspected deployment configuration, so the predicate selects
  nothing and history is retained on purpose.

Two arithmetic facts follow from those numbers; neither is a byte measurement:

- 827 of 31,701 generations are active, 2.6%. If a superseded generation's
  search terms were about the same size as an active one's, about 97% of the
  terms bytes would belong to generations no query reads. That is an upper
  bound under a uniformity assumption; the sampled footprint in section 2 of the
  script replaces it with a measurement.
- Terms are hash-partitioned by `scope_id` across 64 leaves, not by generation,
  so no partition can be dropped to reclaim a generation's terms.

## Who reads search-index rows

Every reader of `eshu_search_index_terms`, `eshu_search_index_documents`,
`eshu_search_index_stats` and the vector tables joins
`ingestion_scopes.active_generation_id`
(`go/internal/storage/postgres/search/index/store.go`,
`eshu_search_vector_*.go`, `eshu_search_document_pending.go`). The changed-since
and MCP paths do not read these tables. The only ungated access is retention's
own row count. The accuracy basis is recorded in
`docs/internal/evidence/7458-search-document-supersede.md`: every row is keyed by
`(scope_id, generation_id)`, every reader joins the active generation, and
retention prunes the rows by cascade from `scope_generations`.

So for a superseded generation the derived search index is storage with no
reader, and retention's cascade is the only thing that deletes it. With
retention held at 87600h that cascade never runs.

## Cleanup cost, measured at fixture scale

PostgreSQL 18.6, production schema, shared scratch host (buffer and call counts
are the primary evidence; wall time is secondary). These are costs a policy
change would pay; they are not a recommendation to run them.

| Measurement | Result |
|---|---|
| Prune of one 400k-fact generation through `DELETE FROM scope_generations` (the retention path), key tables at 100%, 20% and 5% coverage | 6.64M buffer hits. The three per-fact referential-integrity triggers account for about 4.6M of them (400,000 calls each). 8.6 s on a quiet moment, 18.9 s under host load. |
| WAL for that prune | about 47 MB: 21 MB the fact cascade, 21 MB the first key table, 4 MB and 1 MB the others. About 118 bytes per fact at those coverage ratios. |
| Prune of one 1.5M-fact generation, 20% of each child table, 7.5M facts, warm (#7420) | 28.133 s, 26.4M buffer hits, 182 MB WAL. The three per-fact RI cascades took 7.05 s, 5.71 s and 5.06 s at 1,500,000 calls each; the fact cascade took 1.96 s. |
| Same generation with the per-fact FKs dropped and explicit `(scope_id, generation_id)` child deletes (new partition indexes on two tables) | 2.428 s, 5.04M buffer hits, WAL unchanged at 182 MB. Cold shared buffers: 36.5 s before, 6.8 s after. The WAL is the cost of deleting the rows, which no ordering removes. |
| Same prune with children deleted first and the foreign keys kept | the fact delete still took 6.2 s with 1.2M trigger calls and zero child rows: the trigger invocation, not child work, is the cost (#7420). |
| Terms cascade for one superseded generation at 18% of a 10M-row leaf | 3.3 s under the scope lock: about 1 buffer per deleted row, plus a full-leaf scan of 99,998 pages with no `scope_id` predicate (#7427). |
| Pre-deleting that generation's terms in 20,000-row batches | 91 batches, 3.1 s total, about 23 ms and 1.08 MB of WAL per batch, no parent-row lock; the active-generation read probe stayed at 189 buffers and p50 0.28 ms during the loop. |

The lock-hold figures describe the scope row lock that retention holds for the
whole pruning transaction. #7420 and #7427 own the changes to those costs; this
audit does not duplicate their fixes.

## Recommendations

None of these is applied here. They are ordered by what must be known first.

1. Run `7932-storage-attribution.sql` on a read replica with the deployed
   settings (`hard_hours=87600`) and record the category counts and sampled
   footprint. Until then the byte shares above are arithmetic, not measurement.
2. Keep the fact-history hold. The history exists to keep exact diff and
   provenance, and the retention predicate already refuses pinned work and
   uncovered writers. Any change to the 10-year hold belongs to #7585 and #7399
   and should follow their consent path.
3. Treat the derived search index as a separate lifecycle from fact history.
   For a superseded generation it has no reader and is derived from facts that
   the history hold keeps anyway, so a hold that is right for history also keeps
   up to the upper bound above of unread index bytes (not measured). The batch
   pre-delete measured above is the mechanism; it does not need to wait for the
   history policy to change. This needs an owner decision and its own issue:
   retention only deletes eligible generations, so with the current hold even
   #7427's pre-delete would not run for a generation held by policy.
4. Do not drop or narrow an index on this evidence. The terms table's only index
   is the four-column primary key, 65% of its bytes. Whether a narrower key is
   possible is a design change and is NOT_CHECKED.
5. Plan physical reclamation separately. Deleting rows makes space reusable
   after VACUUM; it does not shrink the files, and the partitioning key means no
   partition drop. Rewriting a 2 TiB relation (`VACUUM FULL`, `pg_repack`) takes
   locks or double space and is not authorized or measured here.

## Open questions

- Does exact historical diff need the facts of a pruned superseded generation
  beyond the delta-base case that #7802 and PR #8139 cover? The retention
  predicate on main checks neither.
- What are the real bytes of `superseded_eligible`, `superseded_retained` and
  `superseded_pinned_live_work` on the deployment? The script answers it.
- How much of the 2 TiB is dead tuples and bloat today? No measurement exists.
- Whether the deployed binary matches current source; only the configuration was
  read.

## Limits

- This work had no database access to the deployment. Every deployment number is
  quoted from the issue and labelled as such.
- The fixture is small and has uniform generation sizes; sampling error on a real
  database will be larger. Read `sd_terms` before trusting a category mean.
- Cleanup costs are from a scratch instance with other work running on the same
  host and a warm cache. They set the shape of the cost, not the production
  seconds.

No-Observability-Change: this change adds a read-only diagnostic query and a
report; it changes no runtime behavior.
