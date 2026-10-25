# #8950 PR-A: count content_files upsert outcomes

Issue #8950 asks whether Eshu spends a material share of its content writes
rewriting a file whose content did not change, only its `commit_sha` or
`generation_id`. The prove-the-theory run for #8950 showed the mechanism (a
metadata-only update at a lower heap fillfactor writes about 17 KB of WAL for a
50-file, 469 KB-per-file batch, against 97 to 128 MB today) but not how often
production hits it. This change adds the counter that measures that share. It
does not change what the writer writes.

## Scope

`ContentWriter` classifies each `content_files` row of every upsert batch as
`inserted`, `unchanged_skipped`, `metadata_only` or `content_changed`, and
counts it in `eshu_dp_content_file_upserts_total{outcome,snapshot}` after the
batch's upsert succeeds. `eshu_dp_content_file_upsert_body_bytes_total{outcome}`
counts the body bytes. The `upsert_files` stage log carries the same counts.
The operator reference is `docs/public/reference/telemetry/content-file-upserts.md`.

The classification is a proxy. One plain `SELECT` per batch reads the stored
row without its body (the body is read only through `octet_length`, which does
not detoast), and Go compares it with the incoming row using the guard's
NULL-safe columns. Content equality is the caller-supplied digest plus the body
size. A digest that does not describe its body reads as unchanged here while the
guard, which compares the text, rewrites the row
(`TestContentFileUpsertOutcomeProxyLimitLive` pins that case). The
implementation PR for #8950 takes exact counts from the statements' own results
and drops the pre-read.

## Proof

Test files, all in `go/internal/storage/postgres` unless noted:

- `content_writer_upsert_outcome_test.go`: one test per outcome class and per
  column transition, a batch that mixes all four outcomes plus a row whose digest
  matches while its language changed, delta versus full labels, a failed
  pre-read, a failed upsert, a writer with no observers, and the SQL contract
  (no row lock, no write, no `content` read outside `octet_length`).
- `content_writer_upsert_outcome_live_test.go` (PostgreSQL 18.6): the class
  agrees with the row version the real guarded upsert leaves (a skipped row keeps
  its `xmin`, every other class gets a new version); the pre-read returns at once
  while another session holds an uncommitted edit and row lock on the same row and
  sees only the committed state; an observed and an unobserved writer leave
  identical rows; six concurrent observed writers on the same keys (editing and
  unchanged) hit no error or deadlock and count every written row once.
- `instruments_content_file_upserts_test.go` in `go/internal/telemetry`: the
  closed outcome and snapshot sets, the `snapshot` dimension in the frozen
  registry, and the Prometheus series names.

Mutation check: changing the `metadata_only` return to `content_changed` fails
the classification table and the mixed-batch test; adding `FOR UPDATE OF c` to the
pre-read fails the SQL contract test and the live lock test (the pre-read blocks
past its 3 s deadline).

## Pre-read overhead

Performance Evidence: the pre-read has a fixed cost of about 0.1 to 0.3 ms per
batch (a round trip and a plan; the server-side execution is 0.02 to 0.1 ms). It
is a negligible share of a large batch and a visible one of a small batch, so the
writer classifies every batch of 100 rows or more and one small batch in eight.
With that policy a whole `ContentWriter.Write` measured between -1.4% and +2.2% of
an unobserved writer across 1 to 50 files, with and without entities, and between
-1.7% and +5.1% in an independent run of the same bench. That spread is the noise
floor of an alternating whole-Write comparison; the expected cost is the per-batch
share below divided by eight.

Method: PostgreSQL 18.6 (`postgres:18.6-bookworm`, Docker, a free local port,
`shared_buffers=1GB`, removed afterwards), all 251 migrations applied, 30,000
background rows in `content_files` for index depth, autovacuum on. Each round
times the pre-read and then the real batch upsert (`upsertContentFileBatches`,
which includes the reference delete and reference upsert) against the same stored
state, medians. "Before" is the upsert alone; "after" adds the pre-read.
`TestContentFileUpsertOutcomePreReadCostLive` reproduces it with
`ESHU_CONTENT_UPSERT_OUTCOME_BENCH=1`. An earlier run of the same test reported
only the 50-file and 500-row shapes and concluded the 3% cap held everywhere; the
small shapes below show it does not, which is why the policy samples.

Per batch, every batch classified (no sampling), rows of about 2 KB unless noted:

| Batch | Rounds | Rewrite: upsert / pre-read (ms), share | Identical replay: upsert / pre-read (ms), share |
|---|---|---|---|
| 50 files, about 469 KB, 491 findings each | 15 | 6,851.7 / 0.6, 0.01% | 2,715.7 / 0.6, 0.02% |
| 500 rows | 30 | 461.0 / 2.6, 0.57% | 139.0 / 2.0, 1.45% |
| 50 rows | 60 | 45.4 / 1.2, 2.68% | 24.8 / 1.0, 4.22% |
| 20 rows | 60 | 22.9 / 1.1, 4.93% | 14.7 / 0.4, 2.50% |
| 5 rows | 60 | 9.0 / 0.2, 1.70% | 5.5 / 0.2, 3.63% |
| 1 row | 60 | 6.6 / 0.1, 1.75% | 4.6 / 0.1, 1.22% |

The small-batch shares are noisy (the pre-read is timed to 0.1 ms and the
shares move by a point between runs; an independent run by the reviewer measured
1 row at 6.8% and 8.2%, 5 rows at 3.7% and 5.3%, 20 rows at 1.6% and 3.6%). All of
them can sit above the 3% cap, and every Write ends in a partial batch, so
classifying every batch is not inside the cap on common shapes. Hence the policy:

- a batch of at least 100 rows is always classified (the measured share there is
  at most 1.45% and falls with size);
- a smaller batch is classified one time in eight, by a counter shared across
  writers, so the expected per-batch cost is the table above divided by eight;
- every row of a skipped batch is counted in
  `eshu_dp_content_file_upsert_unclassified_rows_total{reason="sampled_out"}`
  (and `..._unclassified_body_bytes_total` for the bytes), and a failed pre-read
  in `{reason="classify_failed"}`, so the outcome counters plus the unclassified
  counters are every row and byte written;
- every counter carries `batch_class` (`small` below 100 rows, `large` at or above),
  because a share over the classified rows alone counts small-batch rows at one
  eighth of their weight: with 90% of rows in 0%-share deltas and 10% in 90%-share
  full snapshots the true share is 9% and the classified counters read about 42%.
  The reference page gives a scaled estimate and a lower bound;
- the small batches are picked by a golden-ratio (Weyl) sequence, so a period-8
  workload is not systematically sampled or missed
  (`TestOutcomeSamplerRateAndAliasing`).

Whole Write with the shipped policy, observed against unobserved writer
alternating on one database, 60 rounds, medians (ms per Write; files alone are
the cheapest Write, the entity rows are the common shape):

| Files | Entities per file | Rewrite: unobserved / observed, overhead | Identical replay: unobserved / observed, overhead |
|---|---|---|---|
| 1 | 0 | 1.90 / 1.92, +0.87% | 1.49 / 1.51, +1.41% |
| 1 | 10 | 3.30 / 3.35, +1.67% | 2.88 / 2.90, +0.61% |
| 5 | 0 | 4.49 / 4.49, +0.05% | 2.48 / 2.46, -0.61% |
| 5 | 10 | 7.38 / 7.54, +2.20% | 5.62 / 5.60, -0.35% |
| 20 | 0 | 14.22 / 14.23, +0.11% | 5.84 / 5.85, +0.17% |
| 20 | 10 | 23.58 / 23.59, +0.02% | 14.92 / 14.71, -1.42% |
| 50 | 0 | 31.51 / 31.41, -0.32% | 12.91 / 12.93, +0.11% |
| 50 | 10 | 46.06 / 45.44, -1.35% | 26.28 / 26.55, +1.00% |

The spread is the noise floor of an alternating whole-Write comparison. The
claim is that the policy keeps the added cost inside that floor, not that it is
zero: two runs gave -1.4% to +2.2% and -1.7% to +5.1% across the same shapes. The absolute times are this box's, which was quieter for this run
than for the first, so they differ from the first table's; each table compares
within its own run.

Plan: with a 2,000,000-row scratch table (primary key only) and 500 keys the
pre-read is an index nested loop on the primary key, 2,000 shared buffer hits,
1.26 ms; with 50 keys, 200 hits, 0.15 ms. On the 34,000-row bench table the 500-key
plan is a hash join over a sequential scan (7 ms, 973 buffers); the planner's own
costing moves to the index plan as the table grows, and 1 to 50 keys use the index
at that size.

Benchmark Evidence: the two tables above, plus the lock test (the pre-read
returned at once behind an uncommitted writer) and the concurrency test (6
sessions by 5 rounds over 40 shared keys, no error, 1,200 of 1,200 rows counted).
The sampling policy is pinned by `TestContentWriterSamplesSmallBatches`,
`TestContentWriterAlwaysClassifiesLargeBatches`, `TestOutcomeSamplerRateAndAliasing`
and, for the shipped default, `TestNewContentWriterSamplesByDefault` (deleting the
default in `NewContentWriter` makes it classify all 160 writes).

## Operator signals

Observability Evidence: an operator reads the share from `eshu_dp_content_file_upserts_total` and
`eshu_dp_content_file_upsert_body_bytes_total` (PromQL in the reference page),
and per write from the `upsert_files` stage log: `snapshot`, the four
`*_count` fields, `metadata_only_body_bytes`, `classified_batch_count`,
`sampled_out_batch_count`, `classify_failed_batch_count` and `classify_seconds`.
`eshu_dp_content_file_upsert_unclassified_rows_total{reason,snapshot}` counts the
rows the pre-read did not classify, so the outcome counter plus it is every row
written. A failed pre-read logs `content_file_upsert_outcome.classify_failed` and
shows as `reason="classify_failed"`; the write is unaffected. The HOT share of
`content_files` comes from `pg_stat_user_tables` (query in the reference page).
Labels are closed (`outcome` has four values, `snapshot` two, `reason` two, `batch_class` two);
there is no repository, path or generation label.

## Measured share on one local corpus

Corpus: this repository's own text files at revision `29a8958398` (blobs under
1 MiB, valid UTF-8, no NUL byte; 30,074 files, about 187 MiB), written through the
real `ContentWriter` into the scratch database. Each run seeds an older revision,
then re-indexes at `29a8958398` with the commit changed. The older revisions are
`29a8958398~100` (`54215cfe39`, 29,297 files) and `29a8958398~1000`
(`8ffb434fd0`, 24,596 files), both along the first parent. This run classified
every batch; under the sampling policy a trailing batch of fewer than 100 rows can
be skipped, which moves each count by at most that tail.

| Re-index after | Files changed or new | Shape | `inserted` | `metadata_only` | `content_changed` | `unchanged_skipped` | `metadata_only` share (rows / body bytes) |
|---|---|---|---|---|---|---|---|
| `29a8958398~100` (about a day) | 1,568 | full snapshot | 785 | 28,506 | 783 | 0 | 94.8% / 90.4% |
| the same, new generation at the same commit | 0 | full snapshot | 0 | 30,074 | 0 | 0 | 100% / 100% |
| the same, changed paths only | 1,568 | delta | 785 | 0 | 783 | 0 | 0% / 0% |
| `29a8958398~1000` (about ten days) | 14,609 | full snapshot | 9,139 | 15,465 | 5,470 | 0 | 51.4% / 42.1% |
| the same, new generation at the same commit | 0 | full snapshot | 0 | 30,074 | 0 | 0 | 100% / 100% |
| the same, changed paths only | 14,609 | delta | 9,139 | 0 | 5,470 | 0 | 0% / 0% |

`unchanged_skipped` is 0 in every row because every re-index moved the commit or
the generation, and the writer stamps both on every row. A replay that changes
neither is the #8915 case and is skipped.

What this shows: when a repository is re-indexed as a full snapshot, most rows
are `metadata_only`: 94.8% of rows after a day of churn, 51.4% after ten days,
100% when only the generation moved. When the collector sends a delta, the share
is 0% by construction. What it does not show: the production share. It depends on
how many production writes are full snapshots against deltas, how often
repositories are re-indexed, and how much of each repository changed between
re-indexes. None of that is known from this corpus. **The production share is
unmeasured**; the counters added here are how to measure it, and the gate in
#8950 for the implementation PR (`metadata_only` at least 20% of upserted rows
or 10% of body bytes, or a projected saving of at least 5% of the content stage)
should be read from production, not from this table.

The corpus is Go-heavy source text with no findings-heavy files; the 50-file,
469 KB shape from #8915 is not in it.

## Not done

- No setting for the sampling policy: the minimum batch (100 rows) and the period (one in 8) are constants.
- No change to the single-row `ContentStore` upsert, which is not counted.
- No change to the writer, the guard, the schema, fillfactor or any index. That
  is the implementation PR.
- Multi-hour equilibrium behavior of the pre-read against a busy primary was not
  measured.
