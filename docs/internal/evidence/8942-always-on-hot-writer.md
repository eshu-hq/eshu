# #8942: always-on two-statement content_files writer

Issue #8942: an unchanged `content_files` re-upsert rewrites every row, its
trigram indexes and its WAL. The #8950 prototype proved the two-statement
shape (pre-read, metadata-only UPDATE, guarded-upsert fall-through) but
engaged it only for observed, sampled-in batches; every other batch kept the
single-statement guarded upsert and paid the full rewrite. This note records
making that shape the write path for every batch.

## Ruling mapping

The arbiter ruling (2026-10-24) directs: pre-read `content_hash` for the
batch keys; changed rows take the full upsert; unchanged rows take an UPDATE
whose SET never lists `content`, guarded by `WHERE content_hash = $expected`;
a guard miss falls back to the full upsert; triggers 138 and 186 fire
identically; no `DO UPDATE ... WHERE`; no `indexed_at`/index-172 redesign.

The implementation reuses the #8950 statements byte-identical (no SQL text
changed): `contentFileOutcomePreReadSQL` always runs and drives
`partitionMetadataOnlyBatch`; the metadata-only UPDATE sets only
`commit_sha`/`generation_id` with the full row-equality twin recheck
(content text included, never the digest proxy) plus the lineage-differ
predicate; `RETURNING` misses fall through to the guarded upsert, which keeps
last-writer-wins. Triggers 138/186 are statement-level and fire on both
statements exactly as on the single statement. `indexed_at` keeps the #8950
frozen meaning on metadata-only rows (it sits in the index-172 INCLUDE, and
moving it costs HOT); only a batch whose pre-read failed keeps the legacy
single statement, which moves it as before. No migration: 237 already gives
new heap pages fillfactor-50 headroom.

One behavior changes with eyes open: a mixed batch (metadata-only plus
changed rows) now writes in two statements, each its own implicit transaction
as before, so the config-artifact fence (per-transaction contract: one bump
per touching transaction) advances by two instead of one. Readers compare
versions by inequality (`content_version IS DISTINCT FROM v.version`), so
magnitude is not contractual; the observed-writer path already bumped this
way since #8950.

## Proof

Backend: the environment's pinned PostgreSQL 18.6 (`server_version 18.6`,
`pg_trgm 1.6`), throwaway database, then the real `ContentWriter` on fully
migrated (255) schema-isolated databases.

Shim (faithful core: PK, both trigram GINs, language covering index,
`fillfactor = 50`), old shape = writer's guarded upsert, new shape =
pre-read + metadata UPDATE + guarded-upsert fall-through:

| Corpus | Shape | Updates | HOT | WAL | Time |
|---|---|---|---|---|---|
| 50 x 500 KB | old | 50 | 0 | 43.0 MB | 7.26 s |
| 50 x 500 KB | new | 50 | 50 | 12 KB | 91 ms (+0.13 ms pre-read) |
| 5000 small | old | 5000 | 0 | 26.9 MB | 295 ms |
| 5000 small | new | 5000 | 5000 | 1.49 MB | 13.5 ms |

Exactness: full-row digests (every column but `indexed_at`) identical after
old vs new on both corpora; the fall-through upsert after the UPDATE touched
0 rows. Concurrency: with stale pre-read expectations the UPDATE touched 197
of 200 rows and the 3 misses converged through the fall-through with their
edited content kept; four generations racing the same 200 keys in fixed key
order completed with no deadlock/timeout, one winner lineage, content intact.
145k rows: pre-read over 500 keys = nested-loop PK probes, 2.5 ms, ~2000
buffers; UPDATE = nested-loop PK probes, 4.5 ms, ~5300 buffers, 500/500 HOT.

Live (`TestContentWriterUnobservedRewriteIsHOTLive`,
`TestContentWriterUnchangedReplayStaysHOTLive`,
`TestContentWriterObservedAndUnobservedRewriteAlikeLive`, 12 files of
~200 KiB, secret every tenth line): unobserved rewrite 12/12 HOT, 2.5 KiB
WAL (pre-fix code: 0/12 HOT, 896 KiB WAL, trigram indexes grown); observed
and unobserved rewrites both ~2.5 KiB with byte-identical rows; 30 unchanged
replays under fresh generations all HOT (version moved, page kept, content
and `indexed_at` preserved, lineage advanced), every `content_files` index
byte-identical first to last, fence monotonic, secret lines surviving, final
generation exact.

Performance Evidence: unchanged re-upserts stop rewriting `content`, its
TOAST value, and both trigram GIN indexes on every batch, observed or not:
50x500 KB corpus 43.0 MB to 12 KB WAL (7.26 s to 91 ms); 5000-small corpus
26.9 MB to 1.49 MB WAL (295 ms to 13.5 ms); live 12x200 KB rewrite 896 KiB
to 2.5 KiB WAL; 30-round replay all HOT with flat index sizes. Baseline is
the pre-#8942 single-statement writer on the same backends and shapes above.

No-Observability-Change: `eshu_dp_content_file_upserts_total`,
`eshu_dp_content_file_upsert_body_bytes_total`,
`eshu_dp_content_file_upsert_unclassified_rows_total`,
`eshu_dp_content_file_upsert_unclassified_body_bytes_total`,
`eshu_dp_content_file_write_conflict_retries_total` and the `upsert_files`
stage log keep their names, labels, and sampling policy; the sampler now
gates counting only, never the write, so sampled-out small batches are still
counted unclassified with `batch_class="small"` for weighting back.
