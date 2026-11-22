# fact_records snapshot-keyed layout: ranking and replica census (#11202)

Refs #11202. Part of #10860. Follows #10881, which measured `HASH(scope_id)` as a
no-go for removal cost (see
[the #10881 measurement record](10881-fact-records-hash-partition-measurements.md)).
This record covers boxes 1 and 2 of #11202: rank the removal cost first, then
census the read replica. Boxes 3 to 6 (candidates, kill criteria, copies) follow only
if these two do not close the issue.

## Decision rule, written before any measurement (2026-11-22 04:48Z)

Deadline for boxes 1 and 2: 06:47Z, two hours from the start. A failed attempt does
not restart the clock.

**Box 1, rank first.** Removal is "a top cost" if retention's delete plus VACUUM
write-ahead log (WAL) is at least **20%** of all `fact_records` write WAL (inserts,
deletes, vacuum) at the retained five-index profile, which is the index set the table
keeps after #10880 moves reads off it. If removal is below 20% at that profile, the
issue closes as a no-go on this evidence, and the record says so. The 153-index
profile is measured as the "today" comparison and decides nothing by itself, because
#10880 removes those indexes before any layout change.

*Dated note, 2026-11-22 (arbiter-11202):* the sentence above says the retained profile is
what the table keeps after #10880. The #10880 record does not support that. Its first
family (supply-chain impact findings) moves its writes off the table in a later slice
that had not landed, and it drops exactly ten indexes (152 to 142); four more families
are listed and not scheduled; the capabilities answered from collector facts keep their
indexes. Five indexes is a floor that #10880 does not reach by itself. The rule text
above is kept as it was written.

How the share is derived: per-row WAL for removal and for insertion is measured by the
#10881 R1 method on a real whole-scope sample (same removal list, `VACUUM (FREEZE)` and
`CHECKPOINT` before the arm, per-backend WAL, one cell at a time). It is scaled by the
production row-operation counts already recorded on #10881 and #10860 (201.8M inserts
and 73.0M deletes on the primary in the measured period of 2026-11-17). The primary's
statistics are out of bounds for this work, so the counts are inherited, not re-read,
and the share is an estimate with that stated limit.

**Box 2, census.** Read-only, on the QA read replica, aggregate numbers only (no scope,
repository, host or account names in this file). Measured: generations per scope by
status; rows per generation by status; lifetime of a superseded generation (activation
to supersession); generations created per day. Nothing is built or copied from it.

Kill criteria for any later candidate belong to box 4 and are written before any
candidate is measured; none is measured here.

## Box 1: removal cost at the retained and the 153-index profiles

Method: the #10881 R1 cell (`removal_bench.sh`, `removal_procs.sql` and
`pick_removal_list.sh` in `scripts/dev/10881-partition/`, merged to main with the #10881 pull
request #11220 at merge commit `74bb28e1e7`; script blobs `removal_bench.sh` 2347b92e,
`removal_procs.sql` 4ee77525, `pick_removal_list.sh` ae702f49): one fixed removal list of
307 superseded generations from non-giant scopes, the same in both cells (543,145
rows deleted in each); `VACUUM (FREEZE)` and `CHECKPOINT` before the arm; then the
shipped retention drain and `VACUUM (VERBOSE)` in one session with per-backend WAL;
cells run alone. Fresh sample from the replica (2,082,061 rows, S1 shape, giant scope
kept whole), undivided table, loaded twice from the same files: once with all 153
production indexes, once with the retained profile (primary key, two scope-generation
indexes, two retention key indexes). Insert cost per row comes from
`insert_wal.sql`: the script asks for up to 100,000 rows and the scope's active
generation held 61,368, which it copied under new ids into the same table with
`INSERT ... ON CONFLICT (fact_id) DO NOTHING` (not the production `DO UPDATE` suffix),
triggers on, per-backend WAL. No checkpoint preceded the probe, so it differs from the
removal cells, which follow `VACUUM (FREEZE)` and a checkpoint. Raw output: `docs/internal/evidence/11202-snapshot-key-census/`.

| Metric | 153 indexes | Retained 5 indexes |
| --- | --- | --- |
| rows deleted | 543,145 | 543,145 |
| delete WAL (bytes) | 1,315,035,107 | 1,329,970,103 |
| VACUUM WAL (bytes) | 282,221,972 | 180,219,102 |
| delete + VACUUM WAL per deleted row (bytes) | 2,941 | 2,780 |
| index pages in total (VACUUM VERBOSE) | 437,454 | 222,515 |
| index pages newly deleted | 30,055 | 23,727 |
| heap pages scanned | 188,574 | 210,844 |
| relation size (bytes) | 9,576,964,096 | 7,815,946,240 |
| insert WAL per row (bytes) | 3,790 | 3,310 |
| loadavg and I/O pressure avg60 at the delete cell | 21.8 / 62.7 | 35.4 / 60.3 |

(Timings are not used: the host was under heavy I/O, and the decision rests on WAL
bytes and page counts.)

**What it shows.**

- Removal WAL per row is almost the same at both profiles: 2,780 against 2,941
  bytes, 5.5% lower with 148 fewer indexes. The delete itself writes 2.4 KB a row at
  either profile; the cause was not split. An untested reading: the VACUUM log of the
  retained cell shows 109,237 heap pages and 38,128 TOAST pages holding dead items, and
  147,365 pages of 8,192 bytes is 1.21 GB of the 1.33 GB of delete WAL, which would be
  full-page images after the forced checkpoint and would make the per-row figure a
  function of rows per page and checkpoint spacing. `wal_fpi` was not recorded. Only
  VACUUM WAL falls (332 against 520
  bytes a row). Dropping read indexes does not make removal cheap.
- Insert WAL per row is also profile-insensitive (3.3 KB against 3.8 KB), because the
  150 partial indexes mostly do not match a given row.

**Share of the fact-table write load.** Inherited counts from #10881 and #10860 (QA
primary, 2026-11-17): 201.8M inserts and 73.0M deletes, and `n_tup_upd` of 88,254,540
for the whole table, read 2026-11-17, before #10873 merged. That counter mixes a class of update
already cut by #10873 and #10965 with a class #10880 removes, and no record splits it by
fact kind. The primary's statistics were not re-read (out of bounds). Share of removal
(delete plus VACUUM) in `fact_records` write WAL:

| Profile | By the rule written above (inserts, deletes, vacuum) | If the 88,254,540 updates (`n_tup_upd`, read 2026-11-17, before #10873 merged) are counted at insert cost |
| --- | --- | --- |
| Retained 5 indexes | **23.3%** | 17.5% |
| 153 indexes | 21.9% | 16.34% (the arbiter's 16.4%) |

By the rule written before measurement, box 1 does not close the issue (23.3% against
a 20% bar). The rule counted inserts, deletes and vacuum and excluded updates. With the
88,254,540 updates (read 2026-11-17, before #10873 merged) at insert cost the share is 17.5%. The arbiter ruled (arbiter-11202) that
the rule stands, that the supportable range is 16% to 46%, and that this is a share of
write WAL, not a rank against the measured CPU limit. The range: 16.4% to 17.5%
(all updates kept, the period before #10873); 21.9% to 23.3% (updates at zero, the
target of a fact log written once); 43.7% to 45.6% (steady state, one delete per
insert, updates at zero, `2,780 / (2,780 + 3,310)` at the retained profile). The
steady state has not been reached: retention keeps a superseded generation while it is
among the last 24 of its scope or under 7 days old and deletes regardless of rank
only after 90 days (`defaultGenerationRetentionMinSuperseded`, `MaxAge` and
`HardMaxAge` in the retention package), and the replica holds 36.3 days of history.
No single number is binding, and the direction after #10880 is upward. Limits: WAL is a
proxy for load, not CPU or time; the epic's measured limit is database CPU, and its
60-second sample puts 91% of database query time in six read and claim families, none
of them retention; the counters are cumulative and from before two merged changes; the
removal set is non-giant scopes only; some of the 73.0M deletes are reducer retire
deletes outside retention that a snapshot key cannot turn into drops.

## Box 2: census of the QA read replica

Source: `scripts/dev/11202-census/census_generations.sh` (read-only session, 24 short
slices by generation hash, one retry loop each) and `aggregate_census.py`, run
2026-11-22 05:00Z. The raw file carries only a 64-bit hash of each scope id and stays
out of the repository; the aggregate output is committed as
`docs/internal/evidence/11202-snapshot-key-census/census-aggregate.txt`. Window: the
generations on the replica span 36.3 days of ingest; the per-day figures use the last
30 complete days.

**Size and status.** 26,813 generations in 12,821 scopes cover 95.49M fact rows.

| Status | Generations | Fact rows | Share of rows |
| --- | --- | --- | --- |
| active | 11,921 | 20.66M | 21.6% |
| superseded | 11,555 | 67.91M | 71.1% |
| failed | 2,413 | 4.93M | 5.2% |
| pending | 924 | 1.99M | 2.1% |

**Generations per scope.** Every scope has at most one active generation (mean
0.93). Superseded generations are concentrated: 11,784 of 12,821 scopes have none,
and the other 1,037 hold all 11,555 (p99 19, maximum 34 per scope; the histogram has
a spike of 436 scopes at exactly 11 and another of 64 at 24). Failed generations:
mean 0.19 per scope, p99 1, but one scope has 199.

**Rows per generation.**

| Status | Count | p50 | p90 | p99 | Maximum |
| --- | --- | --- | --- | --- | --- |
| active | 11,921 | 186 | 4,235 | 11,353 | 1,407,121 |
| superseded | 11,555 | 865 | 9,102 | 77,250 | 1,390,369 |
| failed | 2,413 | 183 | 4,178 | 36,658 | 117,027 |
| pending | 924 | 160 | 3,680 | 38,940 | 150,237 |

**Lifetime of a superseded generation** (activation to supersession, 11,035
generations with both stamps): p10 0.15 days, p50 1.01, p90 3.03, p99 6.01, maximum
22.69. About 99% of generations are superseded within 6 days of activation.

**Creation and retirement rate** (last 30 complete days, this 05:00Z run): 807 generations created a
day on average (median 881, maximum 1,338); 1.92M rows retired a day on average
(median 0.84M, maximum 6.47M).

**Where the rows are.** Repository scopes hold 78.17M of the 95.49M rows and 66.54M of
the 67.91M superseded rows. Vulnerability-intelligence scopes hold 12,880
generations and 13.48M rows but only 0.19M superseded rows: they look rewritten in
place, not stacked (inferred from the superseded row count, not from reading the
writers).

What the census says for the candidates (boxes 3 and 4, not measured here):

- Retired rows are 3.3 times the live rows (67.9M against 20.7M). 3.3 times live is what the
  retention policy produces (a superseded generation stays while it is among the last 24
  of its scope or under 7 days old; a 90-day ceiling applies regardless of rank), observed
  over a 36.3-day window in which removal volume has not reached steady state. The
  census shows scopes above 24 (maximum 34).
- One partition per generation would be about 26.8k partitions today and grow by about
  800 a day, with a median of a few hundred rows each: out by arithmetic, as the issue
  predicted, unless generations are grouped.
- A time bucket for superseded rows has a natural width: lifetimes are short (p99 6
  days), so a 7-day bucket would hold almost only retired generations, while the
  rows of a still-active generation that was created in the bucket stay (21.6% of all
  rows are active). That share is box 3's question and is not measured here.

## Kill criteria for candidate layouts (box 4), written 2026-11-22 before any box 3 computation

The arbiter's text (arbiter-11202, section 3.2), verbatim. The criteria were committed in
`980726e061` before the box 3 data was computed, with editorial wording changes only ("the
files" for "the 13 non-test files", a shortened K6 path); the verbatim wording is restored
here in the review fix commit and nothing was loosened. A candidate that fails any of K1 to K4 is killed; none is loosened
after the numbers are known.

- K1, key known at insert. The partition key value must be derivable by every writer from data it holds at insert time (collector upsert and the three reducer upserts). A key on activation or supersession time fails: facts are written while a generation is pending (924 pending generations hold 1.99M rows). Only the generation's creation time, stamped on the row, qualifies.
- K2, no row moves. A design that moves a live row to another partition is killed. A unique constraint on a partitioned table must include the partition key (PostgreSQL 18 docs, 5.12.2.3), so the conflict key becomes (fact_id, key); a moved row has two possible homes and `ON CONFLICT` can no longer prevent a duplicate.
- K3, partition count. Leaf partitions at the policy horizon (creation to supersession p99 6 days, plus the 90-day ceiling, plus one bucket width, times any sub-part modulus) above 128 is killed without measurement. Reason: the 13 non-test files that address a fact by `fact_id` alone (among them the retention delete, `generationRetentionChunkDeleteFactsQuery`, `WHERE fact_id = ANY($1)`) cannot prune, and the docs' "few thousand" holds only "provided that typical queries allow the query planner to prune all but a small number". LIST per generation (26.8k today, about 800 more a day) is killed by K3 on the box-2 numbers.
- K4, whole-bucket droppable share. For each candidate and bucket width (1 day and 7 days), over buckets aged at least 7 days and at least 30 days: droppable share = rows in buckets that hold zero rows of a non-eligible generation, divided by all superseded rows in those buckets. Non-eligible means not removable under the shipped predicate at the horizon: active, pending, failed-current, first-generation, a base that an active delta chain still needs (#8780, `TestGenerationRetentionKeepsCumulativeDeltaBaseLive`), or held. Kill if the droppable share is below 67% at both ages. Reason: rows outside a droppable bucket still cost a row delete, and a 415 GB rebuild with a changed conflict key must remove at least two thirds of removal work.
- K5, the 90-day reading. The census window is 36.3 days, so the 90-day share cannot be observed. State it as a model: the share of active-generation rows whose generation is older than 30 days, by scope kind, taken as the lower bound of rows pinned at 90 days. Label it a model, not a measurement.
- K6, scope of the saving. A drop replaces only the `fact_records` delete and its vacuum. Retention still pages every fact of a retired generation to prune content files, entities and key sidecars (`go/internal/storage/postgres/generation_retention_chunk_predrain_batch.go`, the page select above `generationRetentionChunkDeleteFactsQuery`). A candidate whose claimed saving includes that work is mis-costed.

## Box 3: creation-time buckets (computed 2026-11-22, after the kill criteria above)

Input: `generations.csv`. The file from box 2 had been deleted, so, as the arbiter allowed,
`census_generations.sh` was rerun once at 06:16Z with extra columns (first generation
of the scope, a base an active delta generation still needs, rank within the scope). It
holds 26,882 generations in 12,876 scopes over 36.4 days (box 2: 26,813 and 36.3 days; the
difference is ingestion between the two reads). No primary, no new sample, no copy.
`scripts/dev/11202-census/aggregate_buckets.py` computes everything below; the
aggregate output is `docs/internal/evidence/11202-snapshot-key-census/box3-buckets.txt`
(the per-bucket rows by status and scope kind are in it). The bucket key is the
generation's creation time (`scope_generations.ingested_at`). Non-eligible generations
(K4) are active, pending, failed, the first generation of its scope, and a base an active
delta generation still needs. "Held" generations could not be identified and are not
counted (NOT_CHECKED).

**Rows by 7-day creation bucket** (millions; oldest first; the last bucket is partial):

| Bucket age (days) | Active | Superseded | Failed | Pending | Repository | Vulnerability intel | Other |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 29.4 | 0.02 | 14.75 | 0.13 | 0.00 | 14.89 | 0.01 | 0.01 |
| 22.4 | 1.57 | 23.24 | 0.00 | 0.00 | 24.64 | 0.14 | 0.02 |
| 15.4 | 0.51 | 19.21 | 0.00 | 0.00 | 19.18 | 0.53 | 0.01 |
| 8.4 | 6.79 | 3.83 | 2.70 | 0.00 | 6.65 | 5.34 | 1.33 |
| 1.4 | 11.00 | 5.54 | 1.91 | 0.33 | 10.44 | 7.26 | 1.08 |
| -5.6 | 0.79 | 1.39 | 0.18 | 1.75 | 2.44 | 0.22 | 1.44 |

The 1-day table (37 buckets) is in the evidence file. Nothing in the window is older than
36.4 days, so "aged at least 30 days" has no 7-day bucket and six 1-day buckets.

**Active generations by age, weighted by rows.** 98.8% of active rows are in generations
older than 1 day, 51.8% older than 7 days, 0.1% (19,202 rows) older than 30 days; rows-weighted
age p25 3.9, p50 7.3, p75 11.4, p90 15.6 days. Half of the live rows are in generations
that were not superseded within a week of creation, so each old creation bucket carries
live rows.

### Box 4: the criteria applied

| Candidate | K1 | K2 | K3 (leaves at the horizon) | K4 | Result |
| --- | --- | --- | --- | --- | --- |
| LIST per generation | passes in principle | passes | 78,029 (812.8 a day over the trailing 30 days of the 06:16Z rerun, which includes the partial last day, x 96 days): **killed** | not computed | **killed by K3** |
| Scope hash x time sub-parts, modulus 32, 7-day buckets | as RANGE | as RANGE | 480: **killed** | not computed (adds no drop unit beyond time) | **killed by K3** |
| Scope hash x time sub-parts, modulus 32, 1-day buckets | as RANGE | as RANGE | 3,104: **killed** | not computed | **killed by K3** |
| RANGE by creation time, 7-day buckets | needs a creation-time column stamped by every writer (not verified) | key never changes for a fact; conflict key becomes (fact_id, creation time) | 15: passes | droppable share 0.0% at age >= 7 d (0 of 61.0M superseded rows); no bucket aged >= 30 d | **killed by K4** |
| RANGE by creation time, 1-day buckets | as above | as above | 97: passes | 0.0% at age >= 7 d (29 buckets, none free of pins; 0 of 61.3M) and 0.0% at age >= 30 d (6 buckets; 0 of 14.2M) | **killed by K4** |
| The same, static split (vulnerability-intelligence and the other non-stacking kinds kept out of the time buckets) | as above | as above | 97 or 15, plus the non-time partition | 0.0% at both ages and both widths, against all superseded rows | **killed by K4** |

The verdict rests on the arbiter's reading of K4 (all five pin reasons): 0.0% at both ages
and both widths, for every candidate, including the static split at age >= 30 d (script
output). The table below is a sensitivity on which pins count; it is not binding.

| Pins counted | 7-day, aged >= 7 d | 1-day, aged >= 7 d | 1-day, aged >= 30 d |
| --- | --- | --- | --- |
| active, pending, failed, first generation, delta base (K4) | 0 of 4 free / 0.0% | 0 of 29 / 0.0% | 0 of 6 / 0.0% |
| without the failed pin | 0.0% | 0.0% | not computed (raw file gone) |
| without the first-generation pin | 0.0% | 0.0% | not computed (raw file gone) |
| active and pending only | 0 of 4 / 0.0% | 3 of 29 / 14.9% | 3 of 6 / **64.0% to 64.4%** (derived) |
| active only | 0 of 4 / 0.0% | 3 of 29 / 14.9% | 3 of 6 / **64.0% to 64.4%** (derived) |
| static split, active only | 0 of 4 / 0.0% | 7 of 29 / 32.1% of all superseded rows | not determinable from the committed aggregates (64.0% to about 97%) |

The 64.0% to 64.4% figure (active only, all kinds, RANGE) is not script output. The raw
census file was not kept, so it is derived from the committed 1-day table (evidence file
`box3-buckets.txt`, appendix): the three buckets with no active generation hold 14.9% of
61,324,138 superseded rows (9.14M); only {35.4, 32.4, 31.4} or {34.4, 32.4, 31.4} days
old fit that sum, so all three are aged at least 30 days, and they hold 9.11M to 9.17M (from the rounded 14.9% share; the table sums give 9.13M to 9.18M; the percentages use the first basis) of
the 14,227,152 superseded rows in buckets aged at least 30 days. That is about three
points under the 67% bar. The fixed `aggregate_buckets.py` now prints these variants at
both ages; it was not rerun.

The static split, active only, at age >= 30 days cannot be determined from the committed
aggregates. At age >= 7 days it frees 7 buckets (32.1% of 61,324,138 = 19.65M to 19.72M
rows). Without the three zero-active buckets (9.13M to 9.18M, the table-sum basis) the other four
would hold 10.48M to 10.59M rows, more than any four of the 1-day buckets aged 14 to 19
days hold (at most 9.34M; all six together about 10.49M),
so they are not those. Whether the live rows of the pinned 35- and 34-day bucket are all
of the non-stacking kinds is not in the aggregates. If they are, the split frees that
bucket and the share at age >= 30 days is about 96% to 97%, which clears the 67% bar. K4
kills a candidate only when the share is below 67% at both ages, so that variant would
survive K4. Its value lies between 64.0% and about 97%.

K4 therefore kills every candidate under the arbiter's reading (0.0% at both ages), and
the NO-GO rests on that reading. The lenient sensitivities are not binding. For the
active-only RANGE reading both ages are below the bar (14.9% and 64.0% to 64.4%). For the
active-only static split the age >= 30 days value is undetermined, so this record does
not establish that variant is killed, and it does not claim the verdict is independent of
which pins count.

Cause. Of the 29 one-day buckets aged at least 7 days, 26 hold at least one active
generation and 3 hold none (all three at least 30 days old). One of the 35- and 34-day
buckets has no active generation: it is pinned only by failed, first-generation or
delta-base rows. The other buckets are lost to live generations: 51.8% of active rows are
older than 7 days, and live generations sit in most creation buckets.

The pins model the arbiter's list, not the shipped predicate exactly
(`retention/statements/targeted_candidate_lock.go` in the storage package): the shipped code has
no "first generation" counterpart (the census uses rank 1 by creation time), matches a delta
base by commit SHA against the newest full generation and everything newer, and has pins
(uncovered writers, live work items) that the census cannot see and does not model.

**Pinned share p** (rows of non-eligible generations as a share of all rows in buckets
aged at least 7 days): 25.5% for 7-day buckets and 27.3% for 1-day buckets by the K4
reading; 18.8% and 20.1% with the static split; 12.2% and 13.6% counting active and
pending only. The arbiter's rule (3.5) asks for a ruling when p is above 0 and at most
20% before costing a re-ingest or a row move. Here p is above 20% by the K4 reading and
the split variants sit at 18.8% to 20.1%, so the 0 < p <= 20% trigger applies to the
static-split rows. p is stated here for the arbiter and the close-out asks the arbiter to
confirm whether that trigger needs a ruling when K4 already kills every candidate under the
five-pin reading. No re-ingest or row move was costed or measured.

**K5, the 90-day reading (a model, not a measurement).** Active rows in generations older
than 30 days: 19,202, 0.1% of active rows and 0.02% of all rows (vulnerability-intelligence
337 of 13.1M, repository 15,619 of 7.5M, state snapshots 2,785 of 3,105). The window is 36.4
days, so this floor cannot show anything about 90 days: it says only that no scope kind
had a generation active for more than 36 days with many rows. The 51.8% older than 7 days is
the figure that matters for the bucket widths tested.

**K6.** No claimed saving here includes retention's content, entity and key-sidecar paging;
nothing was costed.

**The spikes at 11 and 24 superseded generations per scope.** 24 is the retention count
bound (`defaultGenerationRetentionMinSuperseded`): the 60 scopes at 24 are the scopes that
churn fast (gap between generations p50 1.0 hour) and sit at the bound. 11 is not explained
by any retention bound (24 generations, 7 days, 90 days). The 436 scopes at 11 are 434
repository scopes with about 13 generations each (11 superseded, one active, usually one
failed or pending), created over the whole window (span p50 34.8 days), one every 32.6 hours
(p50), the first one made between 36.4 and 13.5 days ago and the newest 1.5 days ago (p50),
with no fixed hour of day (busiest hour 10.5% of creations). That fits a cohort of scopes
first indexed in the same weeks and refreshed on a common cadence, which is an untested
reading. The count of 11 itself has no cause established.

## Required wording and the gate before any copy

Box 1: see the paragraph above. By the rule written before measurement, box 1 did not
close the issue; it did not establish a rank against the measured CPU limit.

**Gate before any copy (box 5), all required and followed by a new arbiter ruling** (arbiter-11202,
section 3.6):

1. At least one candidate passes K1 to K4. None does.
2. A current removal volume that does not rest on the 2026-11-17 counters: two reads of
   `pg_stat_user_tables` for `fact_records` at least 24 hours apart by whoever holds primary
   access, or the rate of `eshu_dp_generation_retention_rows_pruned_total` for the fact table
   (emitted by the retention runner in the reducer maintenance package) over at least 7 days,
   set beside the insert rate.
3. The share of rows and of updates that are `reducer_*` kinds.
4. Every later WAL cell records `wal_records` and `wal_fpi` beside `wal_bytes`.

## Close-out

#11202 is a measured no-go with the box 3 table, quoting arbiter-11202: no candidate
layout passes K1 to K4. LIST per generation and scope hash with time sub-parts are killed
by K3 (78,029 and 480 to 3,104 leaves against a limit of 128). RANGE by generation
creation time, at 7-day and 1-day buckets, with or without a static split of non-stacking
kinds, is killed by K4 under the arbiter's reading: the droppable share of superseded rows in
buckets aged at least 7 days is 0.0%, and 0.0% in buckets aged at least 30 days, against a bar of 67%.
Under a lenient reading where only active generations pin a bucket, the RANGE share is
14.9% (1-day buckets, aged >= 7 d) and 64.0% to 64.4% (aged >= 30 d, derived by hand), both
below the bar. The active-only static split is 32.1% at age >= 7 d and undetermined at age
>= 30 d (64.0% to about 97%); this record does not claim it is killed, and the no-go rests
on the arbiter's reading. The cause is that most creation buckets hold live generations
(26 of the 29 one-day buckets aged >= 7 days), because 51.8% of active rows are older than
7 days.

## NOT_CHECKED

- Primary-side counters and any current insert, update or delete rate.
- The share of rows and updates that are `reducer_*` kinds.
- "Held" generations; whether a creation-time column can be stamped by every writer (K1).
- Facts' own `ingested_at` against the generation's creation time.
- The aged >= 30 d lenient K4 figures are derived by hand from the committed table: the raw census file
  was not kept and no new replica read was made.
- Behaviour beyond a 36.4-day window (K5 is a model and says little about 90 days).
- No copy and no DDL.
