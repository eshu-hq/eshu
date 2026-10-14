# Deferred Backfill Block Timing

The deferred relationship maintenance pass runs on the ingester after every
drained batch. `eshu_dp_deferred_backfill_duration_seconds` and the
`deferred_backfill_completed` log report only the pass total. The block
signals below split that total into the steps that own it, so an operator can
name the slow step from one pass instead of guessing.

## Blocks

Eight blocks are child spans of `relationship.backfill_deferred`; the ninth,
`reopen`, runs after that span ends and sits under the `bootstrap.reopen_*`
span of its domain. Each block emits one `deferred_backfill_block_completed`
log line. The set is closed: nine blocks, no others.

| Block | Span name | Attributes |
| --- | --- | --- |
| Repository catalog read, row iteration, and JSON decode | `relationship.backfill.catalog_load` | `rows` (fact rows scanned), `entries` (catalog entries kept), `decode_seconds` |
| Anchor-scoped query parameter build | `relationship.backfill.params_build` | `anchor_terms` |
| Active scope-generation partition list | `relationship.backfill.partition_list` | `partitions` |
| Partition memo lookup and skip decision | `relationship.backfill.memo_gate` | `candidate_partitions`, `skipped`, `loaded` |
| Per-partition fact load, merge, and ArgoCD config reload | `relationship.backfill.fact_load` | `partitions`, `facts`, `workers` |
| Evidence discovery, dedupe, and per-source grouping | `relationship.backfill.discover` | `facts_in`, `evidence_out` |
| Per-repository evidence write batches | `relationship.backfill.write_batches` | `batches`, `readiness_rows`, `inserted_rows` |
| Per-partition readiness and memo publication | `relationship.backfill.fanin` | `partitions`, `published`, `skipped`, `argocd_probe_seconds` |
| One reopen domain call after the backfill commits | `relationship.backfill.reopen` | `domain`, `reopened`, `skipped_by_memo` |

Rules that make the numbers safe to compare:

- `catalog_load` covers the whole catalog loader call. It includes row
  iteration and decode, so it does not stop at the first row the way a
  `postgres.query` span does. `rows` minus `entries` is the count of older
  duplicate repository facts the loader read and dropped. `decode_seconds` is
  the part of the duration spent decoding payloads.
- `memo_gate` and `fact_load` do not overlap. `fact_load` starts after the
  gate returns and ends after the ArgoCD config reload.
- `write_batches` duration is the batches only. The fan-in runs after it and
  has its own span, so the two durations add up. `readiness_rows` is an
  attribute: the number of readiness rows the fan-in published, known only
  after the fan-in returns.
- `fanin` `argocd_probe_seconds` is the sum of the per-partition
  ArgoCD-bearing-partition probe durations across the fan-in workers. The
  probes overlap, so the sum can exceed the block's duration; it is the probe's
  cost, not wall time.
- `reopen` spans are siblings of `relationship.backfill_deferred`: the reopen
  runs after that span closes, so they are not its children. With a tracer
  set, each sits under the `bootstrap.reopen_deployment_mapping`,
  `bootstrap.reopen_code_import_repo_edge`, or
  `bootstrap.reopen_correlation_work_items` span of its call. One span per
  domain call: `deployment_mapping`, `code_import_repo_edge`, and
  each cross-scope correlation domain. The `domain` attribute tells them apart.
  Correlation domains are not memo-gated, so their `skipped_by_memo` is always
  0. The partition-scoped pass also emits one per domain call, tagged
  `pass=targeted`.
- `bootstrap-index`'s standalone `Reopen*` calls emit the same `reopen` span,
  the same `deferred_backfill_block_completed` line, and the same histogram
  point as the ingester's maintenance pass.
- Attributes are counts, seconds, and a closed domain name. No scope ID or
  repository ID appears on any block.

## Metric

`eshu_dp_deferred_backfill_block_duration_seconds` is a histogram in seconds
with exactly one label, `block`, whose values are the nine blocks above:
`catalog_load`, `params_build`, `partition_list`, `memo_gate`, `fact_load`,
`discover`, `write_batches`, `fanin`, `reopen`. It records one sample per
completed block per pass. A pass that returns early (no catalog loader, no
anchor term, or zero partitions) reaches fewer than eight in-pass blocks and
records fewer samples. The `reopen` block records once per domain call, always
under the single value `reopen`; the per-domain split lives only in the span
`domain` attribute and the log line. A block that fails the pass records no
sample; a degraded block (the memo gate falling back) does. The
partition-scoped (targeted) pass runs with instruments off and records no
sample, so the histogram measures the whole pass only. Its reopen calls still
emit spans and log lines, tagged `pass=targeted`.

The histogram is the cheap series to graph and alert on. Read the span or the
log line when you need the attributes next to a duration.

## Log line

One line per completed block per pass, with the same name and the same numbers
as the span:

```text
deferred_backfill_block_completed block=memo_gate duration_s=38.62 candidate_partitions=830 skipped=761 loaded=69
```

A block that completes emits the line above. A block that fails ends its span
with the error and emits, instead of the completed line, exactly one failed
line carrying the quoted error. It records no histogram sample:

```text
deferred_backfill_block_failed block=catalog_load duration_s=39.12 error="query repository catalog: context deadline exceeded"
```

A block that errors while the pass continues on a fallback path emits a third
line, `deferred_backfill_block_degraded`. Today only the memo gate does: a
failed memo lookup falls back to a full load and the pass still succeeds. The
line carries the block's fields, `fallback=true`, and the quoted error. The
block's span keeps Error status with `fallback=true`, and the block still
records its histogram sample because it ran and fell back. It emits neither the
completed line nor the failed line, so a failed line always means the pass
failed:

```text
deferred_backfill_block_degraded block=memo_gate duration_s=4.10 candidate_partitions=830 skipped=0 loaded=830 fallback=true error="lookup deferred backfill partition memos: context deadline exceeded"
```

The existing `deferred_backfill_partition_memo_gate_failed` line stays. The
closed set of block line names is `deferred_backfill_block_completed`,
`deferred_backfill_block_failed`, and `deferred_backfill_block_degraded`. The
existing `deferred_backfill_completed` line, the existing
`deferred_backfill_fanin_completed` line, and the
`eshu_dp_deferred_backfill_duration_seconds` histogram are unchanged.

## At 3 AM

1. Find the pass in the trace and read the child durations. The longest block
   owns the pass.
2. A long `catalog_load` with `rows` far above `entries` means the catalog
   query reads many superseded repository facts. A long `catalog_load` with
   `decode_seconds` close to its duration means JSON decode, not the database.
3. A long `memo_gate` points at the memo lookup. A long `fact_load` with
   `loaded` near `candidate_partitions` means the memo is not skipping
   partitions.
4. A long `write_batches` points at lock waits or write batches. Compare with
   `eshu_dp_deferred_backfill_batch_duration_seconds`.
5. A long `fanin` with `skipped` above 0 means generations advanced during the
   pass. See `eshu_dp_deferred_backfill_fanin_skipped_total`. A long `fanin`
   with `argocd_probe_seconds` near its duration times the worker count means
   the per-partition ArgoCD probe owns it.
6. A `deferred_backfill_block_failed` line names the block that failed the
   pass and how long it ran before failing. The memo gate is the one
   non-fatal case: a failed memo lookup degrades to a full load and logs
   `deferred_backfill_block_degraded` with `fallback=true`, not a failed line.
   The pass did not fail; it ran every partition because the memo could not
   be read, which is why `loaded` equals `candidate_partitions`.

No environment variable is added by these signals. The block spans reach any
tracing backend the runtime already exports to. When tracing is off the log
lines remain.

## Log volume

A whole pass emits at most eight in-pass lines plus one reopen line per domain
call (six today: `deployment_mapping`, `code_import_repo_edge`, and four
correlation domains), and one `deferred_backfill_block_failed` line only on a
failure. The partition-scoped pass emits two plus four reopen lines per pass,
tagged `pass=targeted`. The frequency of the partition-scoped pass is not
measured, so its total line volume is not stated here.

## Evidence

No-Regression Evidence: baseline is the pass with no per-block timing (the
base commit). After, measured with `go test -run '^$' -bench
'BenchmarkBlockSpanStartEnd|BenchmarkArgoCDProbeTimerRecord|BenchmarkCatalogLoadStatsPerRow'
-benchmem -count=3 ./internal/storage/postgres/backfill/` on an Apple M5 Max
(18 cores), darwin/arm64, Go 1.26.9, no database (a benchmark of the timing
code only). The author's three runs came from one run on one machine; an
independent review reproduced them within 4 to 14 percent, so the ranges below
widen to that reviewer's measured numbers:

| Added call | ns/op | allocs/op |
| --- | --- | --- |
| one block, real histogram, noop tracer, log line discarded | 736 to 813 | 29 |
| one block, real histogram, SDK tracer with no processor | 1107 to 1173 | 33 |
| one per-partition ArgoCD probe timing (2 clock reads, 1 atomic add) | 44.0 to 46.7 | 0 |
| one per-catalog-row decode timing (counter + 2 clock reads) | 42.7 to 48.7 | 0 |

Input shape: a whole pass runs 8 in-pass blocks plus one reopen block per
domain call (6 today), so 14 block calls; about 830 partitions per pass (the
fan-in probe runs once per partition); catalog rows scale with the repository
facts retained, assumed here to be 22,500 (900 repositories times 25
generations). This is an assumption, not a bound: the catalog read has no
generation filter or LIMIT. The added cost is then about 14 x 1.11 us = 16 us
for blocks, 830 x 44 ns = 37 us for probes, and 22,500 x 42.8 ns = 0.96 ms for
catalog rows: about 1.0 ms in total. Sensitivity: at 10 times the rows
(225,000) the catalog term is about 10 ms and the total about 11 ms, which is
0.02 percent of a 62 s pass. Against the measured 62 to 99 s pass the change is
safe on the pass's critical path and no worker count, batch size, lock, or
query changes. The pass wall time itself was not re-measured; this section
prices only the added code.

Observability Evidence: this change adds the histogram
`eshu_dp_deferred_backfill_block_duration_seconds{block}`, nine child and
sibling spans `relationship.backfill.<block>`, and the
`deferred_backfill_block_completed`, `deferred_backfill_block_failed`, and
`deferred_backfill_block_degraded` log lines described above. They make each
block of the pass visible to an operator at 3 AM with only dashboards, traces,
and logs.
