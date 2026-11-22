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
`pick_removal_list.sh` in `scripts/dev/10881-partition/`, which land with the #10881
pull request): one fixed removal list of
307 superseded generations from non-giant scopes, the same in both cells (543,145
rows deleted in each); `VACUUM (FREEZE)` and `CHECKPOINT` before the arm; then the
shipped retention drain and `VACUUM (VERBOSE)` in one session with per-backend WAL;
cells run alone. Fresh sample from the replica (2,082,061 rows, S1 shape, giant scope
kept whole), undivided table, loaded twice from the same files: once with all 153
production indexes, once with the retained profile (primary key, two scope-generation
indexes, two retention key indexes). Insert cost per row comes from
`insert_wal.sql`: 61,368 fact rows copied from one scope's active rows into the same
table with the production `INSERT ... ON CONFLICT` shape, triggers on, per-backend
WAL. Raw output: `docs/internal/evidence/11202-snapshot-key-census/`.

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

(Seconds were 346 s and 743 s for the deletes under that host I/O; they are not
evidence.)

**What it shows.**

- Removal WAL per row is almost the same at both profiles: 2,780 against 2,941
  bytes, 5.5% lower with 148 fewer indexes. The delete itself writes 2.4 KB a row at
  either profile (heap and TOAST changes); only VACUUM WAL falls (332 against 520
  bytes a row). Dropping read indexes does not make removal cheap.
- Insert WAL per row is also profile-insensitive (3.3 KB against 3.8 KB), because the
  150 partial indexes mostly do not match a given row.

**Share of the fact-table write load.** Inherited counts from #10881 and #10860 (QA
primary, 2026-11-17): 201.8M inserts and 73.0M deletes, plus "88 million rewrites"
(updates). The primary's statistics were not re-read (out of bounds). Share of removal
(delete plus VACUUM) in `fact_records` write WAL:

| Profile | Removal share by the rule written above (inserts, deletes, vacuum) | If the 88M rewrites are counted at insert cost |
| --- | --- | --- |
| Retained 5 indexes | **23.3%** | 17.5% |
| 153 indexes | 21.9% | 16.4% |

By the decision rule written before measurement (20% at the retained profile),
removal is a top cost: 23.3% is above the bar, so box 1 does **not** close the issue
as a no-go. The margin is thin and it flips if rewrites are counted: with them at
insert cost it is 17.5%, below the bar. The rule excluded updates by wording; whether
the rewrites belong in the denominator is a call for the arbiter, not one to make
after seeing the number. Limits: WAL is a proxy for load, not CPU or time (the
epic's measured limit is database CPU, 11.5 of 12 cores, and no CPU split by
statement was available without the primary); the counts are cumulative counters
from one day, not rates; the sample's removal set is non-giant scopes only.

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

**Creation and retirement rate** (last 30 complete days): 807 generations created a
day on average (median 881, maximum 1,338); 1.92M rows retired a day on average
(median 0.84M, maximum 6.47M).

**Where the rows are.** Repository scopes hold 78.17M of the 95.49M rows and 66.54M of
the 67.91M superseded rows. Vulnerability-intelligence scopes hold 12,880
generations and 13.48M rows but only 0.19M superseded rows: they are rewritten in
place, not stacked.

What the census says for the candidates (boxes 3 and 4, not measured here):

- Retired rows are 3.3 times the live rows (67.9M against 20.7M). Retention is not
  keeping the table at its live size; it holds up to 24 superseded generations per
  scope and the census shows scopes above that (maximum 34).
- One partition per generation would be about 26.8k partitions today and grow by about
  800 a day, with a median of a few hundred rows each: out by arithmetic, as the issue
  predicted, unless generations are grouped.
- A time bucket for superseded rows has a natural width: lifetimes are short (p99 6
  days), so a 7-day bucket would hold almost only retired generations, while the
  rows of a still-active generation that was created in the bucket stay (21.6% of all
  rows are active). That share is box 3's question and is not measured here.
