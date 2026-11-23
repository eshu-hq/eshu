# Route V theory proof: results (#10880 PR-B2a)

Refs #10880. Measured 2026-11-23, PostgreSQL 18.6 on a shared host (load average 20
to 34 while measuring), `shared_buffers=2GB`, `work_mem=16MB`, `jit` at the default
(on), schema from `ApplyBootstrap`. Method and threshold: [README](README.md).
Timings are the median of four runs after one warm-up (`run_v.sh`); raw rows are in the
`results_*.tsv` files. All data is synthetic.

## Corpus

80,000 canonical keys x 100 supports = 8,000,481 supports (one key of 281 precise
supports, 5,000 trap keys whose best support is not precise, 200 operator overrides),
80,001 winners, 15,000 keys (1,500,281 supports) in the package `pkg:npm/axios`.
`detection_profile` is stamped on 90% of supports as the writer does, 10% empty (legacy).
4,205,796 supports are precise-eligible. 42,065 winners pass the precise filter
(branch A); 37,936 fail it (branch B candidates with no selector), 21,936 of them with
`status=affected_exact`, 6,557 in axios, 1 for one CVE ([candidate_profile.out](candidate_profile.out)).
The generator's uniform 100 supports per key is not QA's skew (mean 103, max 281);
the skewed corpus (`gen_skew_corpus.sql`) was not run in the time available.

## Finding 1: the ruled plain `(canonical_key)` partial index does not meet the bar

The supplement probe must return the best precise-eligible support of a key (priority,
payload-id flag, fact id order). With a plain `(canonical_key)` index a probe fetches every
precise support of the key (about 53 of 100) and sorts them. With
`(canonical_key, priority_score DESC, has_payload_finding_id DESC, fact_id ASC)` it reads in
order and stops at the first eligible support.

| Read (status filter, 21,936 candidate keys) | plain index | ordered index |
| --- | --- | --- |
| V list, finding_id sort | 5,606 ms | 754 ms |
| V list, priority sort | 5,539 ms | 765 ms |
| V count (totals and both bucket maps) | 5,856 ms | 774 ms |
| V count, no selector (37,936 candidate keys) | not run | 846 ms (max 1,084; first run 50.8 s cold) |

On an earlier, pre-restamp corpus (legacy arm only) the plain index took 64 to 137 s for the
same list and the ordered index 0.76 to 0.80 s; the direction is the same, the sizes differ.
The ordered probe's `EXPLAIN (ANALYZE, BUFFERS)` is
[explain_ordered_probe_v_list_status.txt](explain_ordered_probe_v_list_status.txt): execution
722 ms of which JIT 420 ms, 21,936 probes, `Index Scan` on the ordered index.
The probe must be written so the planner keeps that scan: the eligibility checks (active
generation, static authority) are scalar subqueries, not joins. A join form made the planner
drive from the scope tables (137 s with the plain index, 182 s with the ordered one).
`build_v_reads.py` holds the shape.

## Finding 2: the image read needs a fenced candidate fetch

With the partial `(image_ref)` index the planner still drove the read from the scope join
(10 s for a typical image). A materialized candidate CTE over the image index fixes it:

| Read (ordered-index database) | median |
| --- | --- |
| typical image (about 380 supports): list / count / inventory | 8.9 / 14.9 / 19.0 ms |
| hot image (about 160,000 supports, 2% of the table): list / count / inventory | 1,145 / 1,534 / 1,192 ms |

The plan is a Bitmap Index Scan on the image index. The hot image is a stress case, not a QA
figure; QA's maximum supports per image is a census item. The image reads ran while two
orphaned sessions from an earlier killed run were still executing, so they are an upper bound.

## Writer WAL and index size (real writer, `wal_summary.txt`)

3,000 supports per pass, 3 repetitions. Shape S1 is the worst case (every support precise-stamped
and with an image ref); S2 is a blend (55% precise, 95% image ref). "Cold" runs `CHECKPOINT` before
each pass; "warm" follows a priming pass. Mean WAL bytes against the shipped table, full-page-image
bytes removed (checkpoint timing makes them the noise):

| Variant | insert S1 cold / warm | insert S2 cold / warm | replay S1 / S2 (cold) |
| --- | --- | --- | --- |
| plain precise | +2.5 / +2.2 % | +1.4 / +1.4 % | +2.0 / +1.1 % |
| ordered precise | +4.9 / +3.1 % | +2.6 / +2.4 % | +2.6 / +1.7 % |
| trim `(canonical_key, priority_score)` | +2.7 / +1.7 % | +1.5 / +1.5 % | +1.9 / +4.1 % (FPI artifact) |
| plain + image (ruled) | +3.8 / +3.5 % | +2.6 / +1.6 % | +2.6 % (S2; S1 sample FPI-spiked) |
| ordered + image | +6.2 / +5.0 % | +3.8 / +3.3 % | +4.1 / +3.2 % |

Retract adds +0.5 to +2.9 % (bytes without full-page images) for every index variant in the cells whose
baseline carries no full-page-image artifact; ordered + image is +2.9 % in S1 cold (+3.6 % with full-page
image bytes), +2.0 % and +2.4 % in the other two cells. The share of replay updates that are HOT is
0% for every variant, including the shipped table. Index size at 8,000,481 supports: ordered 199 MB
(a btree cannot deduplicate it), image 54 MB; the plain index was 29 MB on the earlier corpus
(heavy deduplication, 100 equal keys per key). After 3,000 inserts and 3,000 replays: plain
0.47 to 0.84 MB, ordered 0.90 to 1.61 MB, trim 0.50 to 0.87 MB, image 0.32 MB.

Against the arbiter's first bar (the two indexes together add more than 3 points on insert or 6 on replay,
return to the arbiter): replay is inside 6 for every variant. Insert for the ruled plain + image is
+1.6 to +3.8; for ordered + image it is +3.3 to +6.2, so the ordered index adds about +1.0 to +2.4
insert points over the plain one. The trimmed index costs about what the plain one does. The arbiter's
index ruling (`.claude/arbiter-b2a-index.md`, decision 5) replaced that bar for these two indexes with the
measured envelope: return if a re-measure of ordered + image exceeds +7 points on insert in any cell or +6
on replay. +6.2 insert and +4.1 replay are inside it.

## Arbiter index ruling and the measurements it required (08:05Z on)

The arbiter ruled (`.claude/arbiter-b2a-index.md`): the ordered precise partial index ships, the plain
one is dead, the image read keeps its `AS MATERIALIZED` candidate fetch, the status prune is required,
and four measurements must exist before the migration PR. Status:

1. **All winners fail precise, ordered index, with the prune (bar 1,500 ms warm).** Every one of
   80,001 winners fails the filter; 64,001 survive the prune. Warm medians: V count no selector 1,383 ms,
   V count status 1,383, V list none 1,373, V list status 1,258, V list by priority 1,229 to 1,263
   (largest single run 1,567 ms). That is 21.6 microseconds per candidate for the count and 19.7 for the
   list, JIT on. Without the prune (80,001 candidates) the count is 1,319 ms and the status list 1,327 ms.
   Files: `results_ordered_worst_*.tsv`.
2. **JIT pair** (`jit_pair.sh`, same session, median of five, worst-case state): list status 1,199 ms on /
   558 off (-53.5%); count status 1,436 / 1,004 (-30.1%); count no selector 1,235 / 701 (-43.2%). Off is at
   least 20% faster for all three. The repeat on the normal corpus state is owed (it needs
   `restore_winners.sql`). File: `results_jit_pair_worst_case.tsv`.
3. **Row equality both ways (`EXCEPT`, 0 rows each way), on the all-winners-fail corpus with
   64,001 candidate keys** (`row_equality.sh`, `row_equality.sql`):
   | Pair | Rows | `A EXCEPT B` | `B EXCEPT A` |
   | --- | --- | --- | --- |
   | ordered-index probe vs join form | 64,001 / 64,001 | 0 | 0 |
   | plain-index probe vs the saved ordered-index result | 64,001 / 64,001 | 0 | 0 |

   The set-based reference (a `DISTINCT ON` over every precise support of the candidates) was
   dropped: it exhausted the host's memory in the first attempt. Timings of these runs are not
   benchmarks (the cache was cold after a crash restart): probe table 392 s cold, join form 49 s
   warm, plain-index probe 89 s warm. Raw output: `row_equality_*.out`.
4. **Index size and build time at two scales four times apart** (`index_size_build.sql`,
   `index_size_build.out`; plain `CREATE INDEX` on an idle table, `maintenance_work_mem=512MB`):
   | Scale | precise-eligible | ordered index | build | image index | build |
   | --- | --- | --- | --- | --- | --- |
   | 8,000,481 supports (3,318 MB heap) | 4,163,731 | 197 MB | 39.1 s | 54 MB | 79.5 s |
   | 2,000,200 supports (820 MB heap) | 835,365 | 40 MB | 1.5 s | 13 MB | 2.4 s |

   Size scales 4.9x (ordered) and 4.0x (image) for 4.0x the rows. The 8M builds ran right after a
   cold full scan and the 2M builds on a copy that was already cached, so the build-time ratio
   (27x and 33x) mostly measures cache state, not the index; a clean build time at 8M with the
   table warm is not measured. A concurrent build on a live table is slower than these.

5. **Clean image-read rerun, no orphaned sessions** (`results_image_reads_by_size.tsv`; JIT on, warm
   median of 3 after a warm-up, `shared_buffers=1GB`, normal corpus state, host load 26 to 36). The
   former 160k-support stress image was split into images of 20,211, 39,916 and 100,219 supports:
   | Supports of the image | list | count | inventory |
   | --- | --- | --- | --- |
   | 329 (typical) | 7.7 ms | 6.1 ms | 8.9 ms |
   | 20,211 | 145 ms | 143 ms | 143 ms |
   | 39,916 | 311 ms | 289 ms | 308 ms |
   | 100,219 | 941 ms (max 3,430) | 599 ms | 559 ms |

   Cost per 10,000 supports of the image: about 70 to 85 ms between 20k and 40k, and about 85 ms (list)
   and 50 ms (count, inventory) between 40k and 100k. By that slope the list read reaches 1,000 ms at
   about 106,000 supports of one image, and the count at about 180,000. The typical image passes the
   bar by two orders of magnitude; the limit is a documented one for an image with more than about
   100,000 supports. QA's maximum supports per image is a census item for D2. The reads ran with JIT
   on; a `jit = off` image read was not measured. The earlier single-image numbers (1.1 to 1.5 s for 160k
   supports) came from the same shape.

The JIT pair repeated on the normal corpus state (`results_jit_pair_normal_state.tsv`): list status 609 ms
on / 212 off (-65.3%), count status 653 / 324 (-50.4%), count no selector 672 / 249 (-63.0%), so off is at
least 20% faster for all three in both states.

## NOT_CHECKED

- V inventory (five dimensions) and grouped inventory timings; the axios (6,557 candidates) and
  one-CVE reads; read timings for the trimmed index (not ruled in).
- The 20,000-key skewed corpus as a QA-like skew profile (the 2M copy above is uniform). Candidate counts
  for QA skew are unknown.
- The product-schema V statements with the runtime-filter CTE, rank and suppression-state filters and
  cursor (the shim omits them), the page payload cost at production payload width (shim payloads are
  about 200 bytes), cold-cache reads, and QA data.
- A warm build time at 8,000,481 supports; a concurrent build; a `jit = off` image read.
- Measurements ran under host load 9 to 36 and are not a baseline.
