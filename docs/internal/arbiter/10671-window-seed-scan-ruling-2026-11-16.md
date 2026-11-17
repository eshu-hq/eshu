# Arbiter ruling: collector #198, serving the window gap from the seed without a per-claim corpus scan (2026-11-16)

Recorded by the coordinator; the arbiter does not edit files. Arbiter:
`arbiter-eshu`. Goal: #10671. Parent rulings:
[10671-cvelist-mirror-ruling-2026-11-15.md](10671-cvelist-mirror-ruling-2026-11-15.md)
(amendment 1 requires year-set-served windows before QA enable) and
[10671-collector-118-107-ruling-2026-11-16.md](10671-collector-118-107-ruling-2026-11-16.md)
(defect 6 filed #198). Design:
[10671-nvd-feed-transport-fallback.md](../design/10671-nvd-feed-transport-fallback.md).
Measurement tables:
[10671-nvd-feed-measurements.md](../evidence/10671-nvd-feed-measurements.md)
(M12).

Ruling (the arbiter's headline): the year seed keeps serving the window gap, but
not by scanning year files per claim. The seed refresher writes a
day-partitioned "window tail" of recently modified records, and a window claim
reads only the one or two day buckets its window touches (measured 0.24 s p50,
1.4 s worst, against 47 to 62 s for the shipped scan). PR #198 does not merge
with the full-scan serve.

The arbiter verified collector `3e3968692e` (a collector worktree for #198) and
Eshu origin/main `91af5c9033`, and edited nothing in any checkout. Measurements
ran on a `git archive` copy of the collector in a local scratch directory (not
committed) over the 25 cached year files generated 2026-11-15, on an Apple M5
Max with go1.26.11. The host was busy (load average 8 to 40), so absolute times
are pessimistic for this CPU; the ratios carry the ruling. Collector paths name
the file only; they live in `eshu-hq/eshu-collector-vulnerability-intelligence`.
Environment identifiers, including local paths, are redacted per #7803, and
source line numbers the arbiter cited are dropped because they drift.

## Who builds and reads the tail

- The window tail is **built by collector issue
  [eshu-hq/eshu-collector-vulnerability-intelligence#204](https://github.com/eshu-hq/eshu-collector-vulnerability-intelligence/issues/204)**
  (the ruling's "new collector issue A"), which blocks #198.
- The tail is **read by collector #198** (the reader in decision 4), rebased on
  #204.
- The pre-enable read of the deployed limits and the QA canary are
  [eshu-hq/eshu#10817](https://github.com/eshu-hq/eshu/issues/10817) (the
  ruling's "new issue C").
- This record, the design amendments and the evidence figures are
  [eshu-hq/eshu#10816](https://github.com/eshu-hq/eshu/issues/10816) (the
  ruling's "new Eshu issue B"). The selector classes reach core's scope through
  a comment on
  [eshu-hq/eshu#10716](https://github.com/eshu-hq/eshu/issues/10716).

None of the tail, the tail reader or the deadline gate exists in code yet. This
record describes ruled design.

## What the arbiter verified about the blocker

The reviewer's blocker is real, and it is worse than stated because of paging.

- A claim opens every year file with `D_Y + 1 s >= s`
  (`nvd_feed_seed_window.go`), checks ctx only between files
  (`nvd_feed_seed_serve.go`), and streams with no ctx (`nvd_baseline_chunk.go`).
  The only deadline gate is 150 s before the probe (`nvd_feed_selector.go`,
  `nvd_feed_client.go`).
- The host starts a new collector process for every page
  (`extensionhost/process.go`, `pagination.go`), and all pages of a claim share
  one deadline, the lease (`source.go`, `request.go`). A seed continuation runs
  the selector and the whole seed read again (`nvd_feed_collect.go`). So a paged
  group of P pages costs P full scans inside one lease.
- By the arbiter's simulation the gap holds 32 groups that page. On the #198
  branch the largest is 3 pages (2026-10-23T23:10:00.157, 8.5 MB), so 141 to 312
  s of scanning in one lease. Once #197 pages groups over the fact cap, the
  largest is 6 pages (the 1,402-record group at 2026-10-18T21:10:00.120, ahead
  of the QA cursor): 284 to 370 s at the measured rate and 624 s at the
  reviewer's, on the fastest laptop CPU available, against a 600 s lease in the
  docs. On slower production CPUs that claim cannot finish, and the cursor stops
  at that instant for good.
- Planning is one 6 h window per coordinator tick (`nvd_window_planner.go`,
  `nvd_window_derivation.go`), and QA ticks every 30 minutes
  (`activation_derivation.go`). Catch-up time is set by the tick (198 claims is
  about 99 hours), not by scan speed. The scan is a liveness and CPU problem,
  not a throughput one.

## Measurements

The tables and their method are copied into [the evidence note,
M12](../evidence/10671-nvd-feed-measurements.md). The summary the decisions rest
on:

| What | Result |
|---|---|
| Corpus | 25 files, 2,946,235,735 decoded bytes, 425,782 records |
| Shipped scan, all 25 files (verified stream plus `nvdFeedRecordStamp` per record) | 61.7 s and 47.4 s in two runs; 2026 alone 9.5 s and 10.1 s. The reviewer measured 19.5 s for 2026, so 78 to 104 s per claim at that rate |
| Floors for any per-claim scan, all 25 files | gunzip 3.0 to 4.3 s; plus sha256 3.9 to 6.1 s; plus a byte-level record scanner 7.2 to 10.0 s; plus `json.Decoder` raw elements 15.7 to 20.4 s |
| Byte-pattern pre-filter prototype | 42.5 to 56.5 s, no real gain: 37,127 of 39,376 records in the 2024 file hold non-ASCII bytes, so a guard that stays sound sends them to the full path |
| Bytes a gap claim needs | Gap [2026-10-17T20:18Z, 2026-11-07T08:00Z): 48,202 records, 277,973,569 raw bytes, 9.4% of the corpus. Per 6 h window: p50 219 records and 1.3 MB, p90 2,198 and 11.9 MB, max 4,983 and 27.7 MB |
| File-level pruning | All 25 files hold a gap record. A perfect per-window file filter still opens 992 MB at p50 and 1.81 GB at p90 |
| Claims to cross the gap (simulated, 2 MiB / 10,000 facts, lone over-budget group paged) | 198 claims, 225 collector invocations, 32 paged groups, 6 pages at most. Gap is 314,548 facts, 416,504,322 budget bytes |
| Tail build, 60 days, one pass over all year files | 88,927 records, 61 day buckets, 549,909,230 raw bytes, 67,664,342 gzip bytes (BestSpeed), largest bucket 44,991,120 bytes, 59.2 s, 135 MiB heap in use (naive: 61 gzip writers open at once) |
| Window served from day buckets, all 82 six-hour windows of the gap | 0 windows differ from the full-scan record set (48,202 records). p50 240 ms, p90 724 ms, worst 1,397 ms (two buckets, 65.5 MB). All 82 windows together: 27.4 s |
| Continuation re-read of the 5-page group at 2026-10-26T21:10:00.170 | 873 ms, one bucket of 45.0 MB |
| Record's id-year equals its file's year | 0 mismatches in 425,782 |

Method: scratch tests for the component floors and for the tail build, run as
test binaries in the arbiter's scratch copy over the cached year files; outputs
(component timings from two runs, a distribution table, a claim simulation) and
analysis scripts stayed in that scratch directory. The equality check compared
record ids per window, not facts (see Unverified).

## Decisions

1. **The seed stays the source for the gap.** Amendment 1 of the #10671 ruling
   stands. The coverage rule, the minimum `V_Y` over every year, the guard,
   stale precedence and the Modified-wins overlay in #198 are kept as reviewed.

2. **No window claim opens a year file.** The full-scan serve (`readSeedYears`
   over `nvdcve-2.0-<year>.json.gz`) and the `D_Y + 1 s` file filter are removed
   from the window path. The baseline lane keeps reading year files.

3. **The refresher writes a window tail.** `collector refresh-nvd-seed` already
   holds the single-writer lock and reads every year file each pass (ADR-0006).
   At the end of each pass that reaches the end of the year loop, under the same
   lock:
   - Stream every pinned year file through the shared verified stream (digest
     against the pin, decode cap) and `nvdFeedRecordStamp`. Copy each record
     with stamp at or after the low bound `L` byte for byte into the bucket for
     the UTC day of its stamp. `L` is the start of the UTC day of the pass clock
     minus `--window-tail-days` (default 60, range 31 to 120, 0 turns the tail
     off).
   - A bucket is an NVD 2.0 feed document, gzip, so the reader reuses
     `scanNVDFeed`.
   - Fail the build, keep the previous tail, and name the class on the pass line
     when a record does not decode, repeats a key, or carries an id whose year
     is not its file's year. Count a record with an unreadable stamp per year;
     it goes in no bucket.
   - Buckets live in `window-tail/<gen>/`, where `gen` is a hash of the format
     version, `L`, and the sorted year digests. Re-read each bucket and check
     digest and record count before pinning. Then write
     `window-tail/manifest.json` by temp and rename: format version, `L`, build
     time, a full copy of each year's pin (sha256, size, `last_modified`,
     `verified_at`, record count, unreadable count), and each bucket's day,
     file, decoded sha256, decoded size and record count. When `gen` is
     unchanged, rewrite only the manifest so `verified_at` stays current. Keep
     the current and previous generation directories and delete older ones.
   - The tail manifest is self-contained on purpose. The year manifest is
     rewritten year by year during a pass, so a claim that joined the two would
     see torn states. A claim that reads only the tail manifest sees one
     consistent seed generation, the same as a claim that ran when that snapshot
     was current, and an old snapshot ages out through `V_Y` in the closed
     direction.

4. **Window reader.** After the span probe, for a window with `s < M0` and `e <=
   M1`:
   - Tail manifest missing, unreadable, unknown format, a year from 2002 through
     the newest not pinned, or any year with an unreadable count above zero:
     `nvd_feed_seed_unavailable`, API. The unreadable rule is stricter than #198
     today, which refuses only for a file it opens; that is the closed
     direction, and the cached corpus has none.
   - `V_Y`, stale precedence and the guard run on the tail's year pins with the
     existing `checkNVDYearSeedFreshness`: `nvd_feed_year_stale`,
     `nvd_feed_beyond_seed`, `nvd_feed_covered_seed` as in #198.
   - `s < L`: new class `nvd_feed_before_seed`, API. Expected, no alarm. There
     is no slow fallback to the year files.
   - Serve: read the Modified feed, then each bucket whose day overlaps `[s,
     e)`, verified against the tail manifest. Selection is by stamp alone. Each
     fact names the public year feed of its id-year. The response digest adds
     each bucket digest read, ascending by day.
   - A bucket that will not open or fails its digest: re-read the tail manifest.
     If `gen` changed, the failure is retryable (a refresh landed mid-claim). If
     not, hand the window to the API as `nvd_feed_seed_unavailable`, as #198
     does today for a file that will not open.

5. **Deadline floor.** After the probe and the tail manifest read, require
   remaining deadline of at least 75 s (one Modified read at the stall floor)
   plus bucket bytes to read divided by 4 MiB/s. Otherwise
   `nvd_feed_deadline_short`, API. The byte count comes from the manifest before
   any read, so the verdict is deterministic and a claim never starts a read it
   cannot finish. 4 MiB/s is about 11 times under the 47 to 51 MB/s measured on
   bucket reads; the worst measured window needs 16 s. The constant carries that
   measurement in its comment.

6. **ctx threading.** The shared local stream takes the claim ctx and checks it
   on every read, so cancellation lands within one buffer, and a deadline ends
   as retryable `nvd_feed_timeout`, never `nvd_feed_decode`. This covers the
   baseline chunk's use of the same function.

7. **Rejected, with the evidence.**
   - Serving a wider span per claim (candidate 2): core would accept it, since
     the cursor write is a plain compare-and-keep-max with no check against the
     scope end (`extensionhost/nvd_window_cursor.go` and
     `storage/postgres/vulnerability/nvd_window_cursor.go`). It does not help.
     The per-claim output cap binds, not the width: the gap needs 198 claims at
     2 MiB whatever the span. Raising the cap changes the ruled write governor
     (design sections 5 and 6), and the scope id would name a window its facts
     do not match.
   - A cache across claims (candidate 3): the process lives for one call, not
     even one claim, so memory is out. A disk cache written by claims breaks
     ADR-0006's single writer and makes the first claim pay the scan under a
     lease. The tail is that cache, built by the right writer.
   - Riding the baseline (candidate 4): the lane is not enabled, takes 35 hours
     to 8.3 days, and its own correctness leans on the window lane for records
     changed after a chunk read them. The pace ruling already refused to couple
     the lanes
     ([10728-baseline-pace-ruling-2026-11-16.md](10728-baseline-pace-ruling-2026-11-16.md)).
     It would also re-ingest 5.0 million facts to cover 48,202 records each time
     the cursor falls behind.
   - Shrinking or speeding the scan (candidate 5): no file can be skipped for
     the gap, and the floor for a sound scan is 7 to 20 s per invocation on this
     CPU, still proportional to the corpus and multiplied by pages.
   - A byte-offset index into the year files: gzip has no random access, so each
     invocation still pays gunzip and sha256 of every file it touches (3.9 to
     6.1 s for all), and the cost grows with the corpus.

8. **What lands where.** Every item is in this goal, under epic
   eshu-hq/eshu#9209.
   - **Collector issue A (#204), blocks #198:** the tail writer in the
     refresher, the ctx-aware shared stream (decision 6, including the baseline
     caller), pass-line fields, a new ADR for the tail format, and the ADR-0006
     amendment. It is inert for claims until #198 reads it. #199 rebases over
     the stream signature.
   - **PR #198, rebased on A:** keeps the selector work; replaces the serve with
     the tail reader (decision 4), adds the gate (decision 5) and
     `nvd_feed_before_seed`, removes the year-file read from the window path,
     and updates ADR-0001, the configuration reference, the guide and the
     nvd-api skill. The PR body's Risk line and "Production cost is not
     measured" are replaced by measured figures.
   - **Eshu issue B (#10816, docs):** record this ruling under
     `docs/internal/arbiter/`, amend design sections 1 and 5 (R7b is now
     measured; the tail; the new class), and add the figures to
     `docs/internal/evidence/10671-nvd-feed-measurements.md`. Add
     `nvd_feed_before_seed`, `nvd_feed_covered_seed`, `nvd_feed_beyond_seed` and
     `nvd_feed_seed_unavailable` to the scope of eshu#10716; core reads no
     selector class today (the arbiter's `git grep nvd_transport origin/main`
     found nothing).
   - **Issue C (#10817, enable preflight):** read the deployed lease, whether
     the QA instance sets `nvd_seed_dir`, the seed volume's free space (the tail
     adds about 135 MB for two generations at the measured size) and the
     sidecar's memory limit; run the canary in the proof list.

## Required proof

1. Issue A, red before green: tail equals a full scan filtered by stamp on a
   fixture seed; day edges at 00:00:00.000 and 23:59:59.999 UTC; a stamp equal
   to `L`; an unreadable stamp counted and not bucketed; an id in the wrong year
   file fails the build and keeps the old tail; a crash between bucket write and
   manifest rename leaves the old tail serving; unchanged `gen` rewrites only
   the manifest; generation cleanup keeps two; ctx cancel mid-file returns
   within one buffer on both the tail build and a baseline chunk. A
   child-process test holds the build at or under 256 MiB RSS at the 60-day
   default (the arbiter's naive prototype used 135 MiB of heap).
2. Issue A, env-gated corpus replay: every 6 h window from `L` to the newest
   stamp served from buckets equals the year-file scan, record for record and
   fact for fact. The arbiter checked ids only.
3. PR #198: a seed-served window with the year files deleted from the fixture
   still serves (proof that the corpus path is gone); `s == L` serves and `s ==
   L - 1 ms` is `nvd_feed_before_seed`; the gate boundary on both sides; a paged
   group and its continuation read one bucket; a refresh between page 0 and a
   continuation ends `nvd_generation_changed`; a missing bucket with `gen`
   unchanged goes to the API; each fact names its id-year feed. Mutants: drop
   the gate, drop the `L` check, read the year manifest instead of the tail's
   pins.
4. Evidence note in both PRs: `Performance Evidence:` with seed bytes and time
   per claim before and after, and `Observability Evidence:`. The claim's stderr
   line gains seed bucket count, bytes read, read milliseconds and the
   generation id. The pass line gains tail records, buckets, bytes, build
   seconds, low bound, age, and a closed-set class on failure. No path, URL or
   body.
5. QA canary before enable (issue C, #10817): three seed-served windows with
   read rate at least twice the 4 MiB/s floor, and one paged group.

## What the arbiter will not do

- Approve a per-claim scan of the year files with a deadline floor on top.
  Either the floor routes most claims to the API or it lets a 6-page claim run
  past its lease; both follow from the measured scan times.
- Add an attempt-count or health heuristic to pick the transport. Selection
  stays a function of coverage and of bytes known before the read.
- Move the cursor past the planned window end, raise the per-claim output cap,
  or widen the planner chunk in this change. Those are the write governor.
- Let a window claim write to the seed volume, or have two writers.
- Serialize lanes or hold the baseline for the window.

## Unverified

- Production CPU speed and disk speed. Every time above is from one M5 Max under
  load; settled by the canary.
- The deployed lease (docs say 10 minutes; the code default of 60 s would send
  every feed claim to the API today) and whether QA sets `nvd_seed_dir`.
- Seed volume free space, the sidecar's CPU and memory limits, and whether the
  extension host mounts the volume read-only.
- Tail size at the default gzip level, and peak RSS of a bounded builder. The
  arbiter measured BestSpeed and Go heap only.
- The final shape of #197. The arbiter read its commit messages, not its diff;
  the 6-page figure assumes it pages the two groups over the fact cap as its
  message says.
- The Modified overlay's effect on the claim simulation (not applied), and
  fact-level equality of bucket and year-file serves (the arbiter compared the
  ids in each window).
- Whether NVD ever stamps a record later than its year file's generation time.
  Stamp-only selection serves such a record; the shipped file filter would have
  skipped it.
- The reviewer's 19.5 s for the 2026 file. The arbiter's two runs gave 9.5 and
  10.1 s for the stamp path without the fold; both rates are reported.
- The #198 test suite and `make ci` were not run.
- Baseline chunks re-stream a whole year file per page too (2026: about 10 s
  here). That is outside this question and was not ruled; the collector #199
  owner should check it against the lease.

Refs #10671. Part of #9209.
