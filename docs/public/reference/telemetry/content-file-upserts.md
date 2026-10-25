# Content File Upsert Outcomes

The content writer counts what each `content_files` upsert batch does to the rows
it carries (#8950). The count answers one question: how much of the write cost
is Eshu rewriting a file whose content did not change, only its `commit_sha` or
`generation_id`.

## Signals

| Signal | Labels | Meaning |
| --- | --- | --- |
| `eshu_dp_content_file_upserts_total` | `outcome`, `snapshot`, `batch_class` | Classified rows per upsert batch, counted after the batch's upsert statement succeeds. |
| `eshu_dp_content_file_upsert_body_bytes_total` | `outcome`, `snapshot`, `batch_class` | Body bytes of those rows. It is the bytes the writer was handed, not the bytes written. |
| `eshu_dp_content_file_upsert_unclassified_rows_total` | `reason`, `snapshot`, `batch_class` | Rows written but not classified: `sampled_out` (a small batch the writer skipped the pre-read for) or `classify_failed`. The outcome counter plus this one is every row written. |
| `eshu_dp_content_file_upsert_unclassified_body_bytes_total` | `reason`, `snapshot`, `batch_class` | Body bytes of those rows. The two body-bytes counters together are every body byte written. |
| `upsert_files` stage log | none | Per `Write`: `snapshot`, `inserted_count`, `unchanged_skipped_count`, `metadata_only_count`, `content_changed_count`, `metadata_only_body_bytes`, `classified_batch_count`, `sampled_out_batch_count`, `classify_failed_batch_count`, `classify_seconds`. |

`outcome` is a closed set, and the labels carry no repository or path:

| `outcome` | Meaning |
| --- | --- |
| `inserted` | No stored row for the repository and path. |
| `unchanged_skipped` | Every guarded column, the content digest and the body size match. The upsert guard (#8915) skips the row. |
| `metadata_only` | The digest and body size match, every other column matches, and only `commit_sha` and/or `generation_id` differ. The row is rewritten with the same content. |
| `content_changed` | The row is rewritten for any other reason: the digest or size changed, or another stored column (`language`, `line_count`, `artifact_type`, `template_dialect`, `iac_relevant`) changed. |

`snapshot` is `full` when the `Write` carried the repository's complete file set
and `delta` otherwise. A delta carries only changed paths, so `metadata_only` is
close to zero there by construction.

`batch_class` is `large` for a batch of 100 rows or more and `small` for a
smaller one. It exists because of sampling (below): a combined share needs it to
weight the sampled small batches back up.

## How the outcome is decided

Before each batch's upsert the writer runs one plain `SELECT` for the batch's
keys. It takes no row lock, writes nothing, and reads the body only through
`octet_length`, which sizes a TOASTed value from its header without detoasting
it. The writer compares the stored row with the incoming row in Go, using the
same NULL-safe columns as the upsert guard.

This is a proxy for body equality. It compares the caller-supplied content
digest and the body size, not the text. A digest that does not describe its body
can read as unchanged here while the guard, which compares the text, rewrites
the row. The pre-read and the upsert are separate statements, so a concurrent
writer can change a row between them and shift one row's class. The totals stay
exact: every row of a successfully upserted, classified batch is counted once.

The pre-read has a fixed cost per batch (about 0.1 to 0.3 ms), so the writer
classifies every batch of 100 rows or more (`batch_class="large"`) and about one
in eight smaller ones (`batch_class="small"`). The small batches are picked by a
golden-ratio (Weyl) sequence, not a fixed modulus, so a workload that cycles a
fixed list of repositories is not systematically sampled or missed. Every row and
body byte of a skipped batch is counted in the unclassified counters with
`reason="sampled_out"`.

**A share over the classified counters alone is wrong when the mix of batch
sizes differs between outcomes**, because small-batch rows are present at about
one eighth of their weight. Deltas and the tail batch of every `Write` are small
batches. Example: if 90% of written rows are deltas with a 0% `metadata_only`
share and 10% are full snapshots with 90%, the true share is 9%, but the
classified counters read about 42%, which would wrongly pass a 20% gate. Use the
scaled estimate or the lower bound below.

The pre-read is observability only. If it fails, the `Write` still succeeds, the
batch is counted under `reason="classify_failed"`, the log carries a
`content_file_upsert_outcome.classify_failed` warning, and
`classify_failed_batch_count` in the `upsert_files` stage log is above zero. A
writer with no logger and no instruments skips the pre-read.

Only the batched `ContentWriter` path is counted. The single-row `ContentStore`
upsert is not.

## Queries

Write C(o, b) for `sum(increase(eshu_dp_content_file_upserts_total{outcome="o",batch_class="b"}[1d]))`
and U(b) for `sum(increase(eshu_dp_content_file_upsert_unclassified_rows_total{batch_class="b"}[1d]))`.
Add the same `snapshot="full"` or `snapshot="delta"` matcher to every selector for
a per-snapshot value.

**Scaled estimate of the `metadata_only` share of rows written.** Large batches
count as they are; small-batch rows are scaled by the observed sampling ratio
(about 8):

```promql
(
  sum(increase(eshu_dp_content_file_upserts_total{outcome="metadata_only",batch_class="large"}[1d]))
  +
  sum(increase(eshu_dp_content_file_upserts_total{outcome="metadata_only",batch_class="small"}[1d]))
    * (
        sum(increase(eshu_dp_content_file_upserts_total{batch_class="small"}[1d]))
        + sum(increase(eshu_dp_content_file_upsert_unclassified_rows_total{batch_class="small"}[1d]))
      )
    / sum(increase(eshu_dp_content_file_upserts_total{batch_class="small"}[1d]))
)
/
(
  sum(increase(eshu_dp_content_file_upserts_total[1d]))
  + sum(increase(eshu_dp_content_file_upsert_unclassified_rows_total[1d]))
)
```

It is an estimate: it assumes the sampled small batches are typical of the
unsampled ones, and it counts `classify_failed` rows only in the denominator, so
it reads low while `reason="classify_failed"` is above zero.

**Lower bound of the same share.** It treats every unclassified row as not
`metadata_only`, so the true share is at least this:

```promql
sum(increase(eshu_dp_content_file_upserts_total{outcome="metadata_only"}[1d]))
/
(
  sum(increase(eshu_dp_content_file_upserts_total[1d]))
  + sum(increase(eshu_dp_content_file_upsert_unclassified_rows_total[1d]))
)
```

**Body-byte share.** The same two queries over the byte counters, replacing
`eshu_dp_content_file_upserts_total` with `eshu_dp_content_file_upsert_body_bytes_total`
and `eshu_dp_content_file_upsert_unclassified_rows_total` with
`eshu_dp_content_file_upsert_unclassified_body_bytes_total`. The scale factor then
uses bytes: (small classified bytes + small unclassified bytes) / small classified
bytes.

**Coverage.** Share of written rows the pre-read did not classify:

```promql
sum(increase(eshu_dp_content_file_upsert_unclassified_rows_total[1d]))
/
(sum(increase(eshu_dp_content_file_upserts_total[1d]))
 + sum(increase(eshu_dp_content_file_upsert_unclassified_rows_total[1d])))
```

`reason="classify_failed"` above zero is a fault; `reason="sampled_out"` is the
policy.

### HOT update share of `content_files`

A heap-only tuple (HOT) update rewrites a row without touching its indexes. The
current writer rewrites a `metadata_only` row as a non-HOT update, so the HOT
share of `content_files` should sit near zero while `metadata_only` is high. Run
this on the primary:

```sql
SELECT n_tup_upd,
       n_tup_hot_upd,
       round(100.0 * n_tup_hot_upd / NULLIF(n_tup_upd, 0), 1) AS hot_pct
FROM pg_stat_user_tables
WHERE relname = 'content_files';
```

The counters are cumulative since the last statistics reset. Read them twice and
subtract to get the share for a window. `n_tup_upd` counts every update of the
table, including ones that did not come from the content writer, so treat the
figure as an approximation of the writer's HOT share.

## Reading the numbers

- `metadata_only` high on `snapshot="full"` and near zero on `delta`: the cost
  is in full snapshots (first index of a repository, forced reconciliation,
  reindex). A metadata-only write path would help only those.
- `metadata_only` low overall: unchanged-content rewrites are not a material
  share of writes. #8950 proposes deferring the metadata-only writer if
  `metadata_only` stays under 20% of upserted rows and under 10% of body bytes.
  Judge the gate on the scaled estimate; if the lower bound already clears it,
  the true share does too.
- `content_changed` dominant: the write cost is real change, not rewrite.
- `reason="classify_failed"` above zero: the pre-read is failing. Look for the
  `classify_failed` warning, which carries the database error.

The `classify_seconds` field in the same stage log is the measured cost of the
pre-read for that `Write`. A whole `Write` with the sampling policy measured
between -1.7% and +5.1% of an unobserved writer across the benchmark shapes in two
independent runs. That spread is the noise floor of an alternating comparison;
the expected cost is about 0.1 ms per eighth of the small batches. See the
evidence note, `docs/internal/evidence/8950-upsert-outcome-counter.md`.
