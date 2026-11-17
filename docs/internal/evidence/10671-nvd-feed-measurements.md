# 10671: NVD outage measurements and the feed-transport theory

Refs #10671. Measured 2026-11-15 between 04:30Z and 09:50Z by the coordinator,
read-only, from a laptop (public endpoints) and the QA cluster (a Postgres read
replica with `default_transaction_read_only=on` and a 20 s statement timeout).
Environment identifiers are redacted per #7803.

**Decision, threshold, deadline (prove-the-theory-first).** Decision: can the
public NVD 2.0 JSON data feeds replace the paged NVD API for windows the API
cannot finish, with no contract change? Threshold: the feed record must match
the API record shape and `lastModified`, carry `matchCriteriaId`, `vulnStatus`
and every CVSS family, answer faster than the API, and be wide enough to cover
the gap. Deadline: 90 minutes of measurement; a failed approach does not
restart the clock. Every number below has its sample size.

## M1. The outage is live and the drain was already four weeks behind

`vulnerability_source_states`, summary at 04:46Z:

| Source | State | Rows | Last success | Last attempt |
| --- | --- | --- | --- | --- |
| nvd | failed_terminal | 14 | none | 2026-11-15 04:43Z |
| nvd | failed_retryable | 1 | none | 2026-11-12 20:54Z |
| nvd | fresh | 57 | 2026-11-13 17:10Z | |
| nvd | partial | 2 | 2026-11-10 13:59Z | |
| cve_list | fresh | 1 | 2026-11-15 04:24Z | |
| ghsa | fresh | 2 | 2026-11-15 04:22Z | |
| cisa_kev | fresh | 2 | 2026-11-15 04:22Z | |
| osv | fresh | 6,063 | 2026-11-15 04:26Z | |

`vulnerability_nvd_window_cursors`: cursor `2026-10-17T20:18:09Z`, head
`2026-11-13T17:10:24Z`. The cursor trails wall clock by about 28 days (28.3 at the 05:00Z read), not the
"11+ hours" the issue states: the drain was already about 27 days behind when
the latest failures began. The CVE List `deltaLog.json` covers about 30 days,
so the gap start sits at the edge of that window.

External probe, one sample at 04:38Z: NVD API `resultsPerPage=1`, 3 h
`lastMod` window, HTTP 200, time to first byte 10.45 s.

## M2. The feed is live, fast, and the same record shape

- `nvdcve-2.0-modified.meta`: HTTP 200, TTFB 0.29 s, `lastModifiedDate`
  `2026-11-14T20:00:08-05:00`, `gzSize` 7,350,752, sha256 `9670229A...`.
  The downloaded `.gz` hashes to the `.meta` size.
- Modified feed, `format` `NVD_CVE`, `version` `2.0`, 12,818 records,
  `lastModified` from `2026-11-06T05:18` to `2026-11-15T00:16`: a span of about
  nine days. 4,522 records carry `configurations`; 34,675 of 34,675 `cpeMatch`
  entries carry `matchCriteriaId`. `vulnStatus` shows all seven states
  (Analyzed 2,816; Awaiting Analysis 3,383; Deferred 2,979; Undergoing
  Analysis 872; Modified 1,667; Received 607; Rejected 494). Metric families
  seen: `cvssMetricV2` 311, `cvssMetricV30` 111, `cvssMetricV31` 10,763,
  `cvssMetricV40` 2,963, `ssvcV203` 10,466.

## M3. Year-feed footprint and generation times

25 year feeds (2002 to 2026): sum `gzSize` 237.6 MiB (0.23 GiB), sum
decompressed 2,809 MiB, largest 2026 at 528 MiB decompressed and 44 MiB
compressed. Generation times differ by year, all near 03:0x US Eastern:
2003, 2004, 2005, and 2008 were last written 2026-10-21; 2010 on 10-31; 2016
on 10-30; 2023 to 2026 on 11-14.

## M4. Do year files regenerate on change?

A dormant file with an old generation time is only safe if NVD rewrites a year
file whenever one of its records changes. Test: for every Modified-feed record
(n = 12,818, 21 id-years), look for a `lastModified` after its own year file's
generation time and at or before the latest daily run (`2026-11-14T08:02Z`).
Result: **0 violations**. Two records in dormant years have a later
`lastModified`, both after the latest run. This supports regenerate-on-change
on one snapshot. It is not a NIST guarantee, so the design needs a runtime
guard (see the design doc) and a later re-read of the `.meta` times.

## M5. Facts per record and the backfill arithmetic

Facts per NVD record under the satellite's envelope rules (one `cve` fact, one
fact per distinct sanitized reference URL, one fact per distinct
`criteria` and `matchCriteriaId` pair), Modified feed, n = 12,818: mean 7.07,
p50 4, p95 22, p99 46, max 2,048; 90,642 facts in total over nine days, about
10.1k facts a day. This is an upper bound: the satellite folds repeated
matches. Per-day facts: 13.8k (Nov 6, partial), 3.7k, 1.2k, 6.5k, **23.5k
(Nov 10)**, 23.4k, 10.1k, 7.3k, 1.1k.

- The 28-day gap is about 290k facts by this sample's rate (extrapolated; the
  arbiter's 45,757 records over 25 days at 7.07 facts a record give about
  375k, so read it as a range).
- One 6 h window per 30 min planner run gives about 116 windows, about 58 h
  of wall clock. Planner cadence binds, not the 10,000-fact claim cap.
- A dense 6 h slice can exceed 10,000 facts (Nov 10 averages 5.9k per 6 h, so
  a front-loaded slice would), so the prefix-commit overflow rule is expected to
  run. The 4,982-result stuck window in M10 is the nearer evidence for it.

## M6. The dirty log and who feeds it

`supply_chain_impact_winners_dirty_log` at 04:46Z: 1,080,966 rows, oldest
`captured_at` 2026-11-12 18:44Z (about 58 h). By `source`: `fact_upsert`
960,818; `fact_retract` 113,427; `activation` 6,721. Last 3 h: `fact_upsert`
238,282. Hourly inflow, 22:00Z to 04:00Z: 15,647 (partial), 59,975, 80,753,
109,133, 95,266, 75,277, 81,079 rows, about 1.0 to 1.8k a minute. The
`fact_upsert` rows come from the reducer's finding upserts, not from raw facts
(migration 272, lines 10 to 17), so the rows an NVD fact produces is not
measured.

Attribution. Newest 50,000 rows by `advisory_id` prefix: `GHSA` 49,561, `CVE-`
433, blank 6. Oldest 50,000: `GHSA` 47,847, `CVE-` 1,708, blank 371, `PYSE`
74. These are contiguous slices, not random. `vulnerability_source_states`
(latest run per scope, so an undercount), last 24 h: osv 85,439 results in
1,022 runs, first_epss 1,880, cisa_kev 1,782, cve_list 18, ghsa 2, **nvd 0**.
With NVD down, the current inflow is OSV/GHSA-keyed. #10706 describes it as
NVD inflow; that does not match these reads.

Effect of an NVD catch-up, as an upper bound that assumes one dirty row per
committed fact. That is a guess, not a ceiling (one CVE can touch many
findings), and it is unmeasured: 290k to 375k over about 58 h is 5.0k to 6.5k
rows an hour, 5 to 11 percent of the current inflow.

## M7. Deployed configuration

QA collector deployment: claim lease 10 m, heartbeat 30 s, canonical write
timeout 300 s, an NVD API key referenced. The code default lease is 60 s
(`go/internal/workflow/defaults.go`). The core image is an ancestor of
`origin/main` and contains the 6 h planner commit `adc011d49f`, so the
recovery ruling's option A is live, and its option B (a 10 m lease) is
configured.

## M8. Feed record equality against the API

Sample: 20 CVE records taken from the Modified feed, 10 with CPE
configurations and 10 without, every one last modified more than a day before
the run (feed `lastModified` 2026-11-06T17:18 to 2026-11-12T22:09). Each was
fetched from the API with `cveId=`, paced at about one request every 25 s
because the API answered slowly. Result: **20 of 20 deep-equal on the whole
`cve` object, 20 of 20 identical top-level key sets, 0 request errors.** The
arbiter's own comparison before this run covered one record from a third-party
mirror; this run is the first-hand check on NVD's own feed. Limits: 20 records
out of 12,818, none from a window newer than a day, and no record whose state
changed between feed generation and the API read.

## M9. NIST's stated stance (secondary)

Search over `nvd.nist.gov/general/news` and `nist.gov/itl/nvd/nvd-news`
reports that NIST retired the 1.1 feeds in 2025 and replaced them with 2.0
year, Recent, and Modified feeds, and that a June 2026 notice warns the
Modified feed will grow for eight days. No retirement notice for the 2.0 feeds
turned up. The data-feeds page is a JavaScript app, so this is not a
first-hand reading of the page; the live `.meta` and `.gz` are the first-hand
evidence.

## M10. Is the API failure keyed, egress-bound, or depth-bound? (no)

The arbiter's confirm pass read the satellite pod log: 66 of 66 NVD claim
attempts between 2026-11-14T21:39Z and 2026-11-15T09:07Z ended
`nvd_http_503`, with `source_latency_ms` of 12,043 (min), 57,346 (median) and
118,612 (max). The coordinator re-counted the same log and got the same
figures. Each failing attempt made one upstream request
(`status_count: 1`, `fact_count: 0`) that returned 503 after 28 to 50 s in the
two lines read in full. The arbiter's theory was a deterministic 503 tied to
the keyed request shape or the pod's egress IP. I tested it from inside the
QA satellite container (`kubectl exec` with `curl`; the key stayed in the
container's environment and was never printed), against the stuck window
`[2026-10-17T20:18:09Z, 2026-10-18T02:18:09Z)` (4,982 results at the time):

| Request | Result |
| --- | --- |
| keyed and anonymous, 1 result, `Z` dates | 200 in 6.1 to 12.6 s (3 of 3) |
| keyed and anonymous, 1,000 results, `Z` dates | 200 in 6.2 s and 8.9 s |
| satellite's exact shape (no-`Z` dates, sorted params, `Accept`, `Connection: close`, gzip, `apiKey` header), keyed | 200 in 13.6, 3.8, 6.4 s (HTTP/2 twice, HTTP/1.1 once) |
| same shape, anonymous | **503 in 0.07 s** (one sample) |
| exact shape, keyed, `startIndex` 1000 / 2000 / 3000 / 4000 | 200 in 12.9 s / **503 in 0.06 s** / 200 in 2.8 s / 200 in 7.0 s |

Reading: the key and the egress IP are not the cause (keyed requests from the
pod succeed in 11 of 13 requests), and depth is not deterministic. NVD returns
fast edge 503s on 2 of 13 requests, and slow 3 to 13 s first bytes
between them, which matches the issue's two failure faces. It does not explain
why the satellite fails 66 of 66 with 12 to 118 s latencies; that stays open
under collector#97. The arbiter's proposed step of unsetting or rotating the
key is not supported by these results and is not taken. Samples are small (13
requests, one session, one source IP).

## M11. Baseline corpus, chunk sizes and pace (arbiter, 2026-11-16)

Measured by the arbiter on 2026-11-16, read-only, on a `git archive` copy of the
collector at origin/main `a4f7b31` over the 25 cached year files (generated
2026-11-15), on a darwin/arm64 laptop with go1.26.11. The ruling and its item
numbers are in
[10728-baseline-pace-ruling-2026-11-16.md](../arbiter/10728-baseline-pace-ruling-2026-11-16.md),
section (a). This replaces the upper-bound extrapolation in M5 for the baseline
corpus. The design consequences are in
[10728-baseline-pacing.md](../design/10728-baseline-pacing.md) section 2.

Method. A scratch test ran the shipped `scanNVDFeed`, `nvdFeedRecordStamp`,
`NVDRecordEnvelopes` and `nvdFactBudgetBytes` over every record of the 25 files,
with scope `vuln-intel://nvd/_baseline/qa-baseline-2026-11/<year>/<us>`, a
64-character generation id and the public year-feed source URI. Chunk counts cut
the (stamp, CVE ID) ordered record list (a records table plus a chunking script,
both in the arbiter's scratch directory) at each budget. Scratch artifacts are
not committed.

| Quantity (ruling item) | Measured |
|---|---|
| Records (1) | 425,782 |
| Facts (1) | 5,039,862: 425,782 cve, 3,247,201 affected_product, 1,366,879 reference |
| Budget bytes (1) | 6,404,997,673, or 1,270.9 a fact (the earlier 1,256 differs only by id lengths) |
| Shipped group-atomic caps, 2 MiB and 10,000 facts (2) | 3,124 chunks plus 2 groups refused forever; 3,124 x 30 min = 65 days; byte cap binds at 2,097,152 / 1,270.9 = 1,650 facts |
| Bytes a fact by kind (5) | cve 2,166; affected_product 1,248; reference 1,047 |
| Bytes a fact by field (5, every 25th fact) | payload 614.2 (48%); `source_ref` 444.5 (35%: generation_id 83, scope_id 82, fact_key 78, uri 72, record_id 69.5, source_system 45); stable_key 80; kind 37; observed_at 37; source_confidence 31; schema_version 25 |
| Folding all 25 files (6) | 163 s CPU; streaming them (gunzip, sha256, scan) 28.3 s |
| Folding 2026 alone (6) | 27.4 s fold, 4.4 s stream |
| A 32 MiB chunk of 24,922 facts (6) | folds in 0.40 s, splits into 9 pages in 0.14 s |

Chunks by budget, record-atomic (item 3):

| Chunk budget | Chunks | Detail |
|---|---|---|
| 2 MiB | 3,180 | |
| 8 MiB | 787 | |
| 16 MiB | 397 | mean 12,695 facts; max 14,855 facts; max 2,917 records |
| 32 MiB | 203 | max 29,550 facts; max 5,213 records |
| 64 MiB | 108 | |

The two groups that stall a group-atomic baseline (item 4):

| File | Stamp | Records | Facts | Bytes |
|---|---|---|---|---|
| 2025 | `2026-10-18T21:10:00.120` | 1,402 | 17,478 | 22,945,411 |
| 2026 | `2026-10-26T21:10:00.170` | 1,581 | 14,156 | 18,691,477 |

They are the only groups over 10,000 facts. 73 groups exceed 2 MiB, 2 exceed 16
MiB and none exceeds 32 MiB. The largest single record is CVE-2021-3942, 5,485
facts, 6,983,745 bytes. This is the first corpus-wide measurement of facts per
record under the satellite's folding: 5,039,862 / 425,782 is about 11.8, against
the Modified-feed mean of 7.07 in M5.

Collector memory (item 7 and the coordinator adjustment). Retained Go heap is
1.50 x budget bytes: 24.1 MiB at 16 MiB, 48.0 at 32 and 96.0 at 64 (a scratch
memory test). The collector child's peak RSS is a separate measurement, recorded
in [collector issue
#203](https://github.com/eshu-hq/eshu-collector-vulnerability-intelligence/issues/203)
and quoted in the ruling's coordinator adjustment: a 553 MB synthetic year file
with the real handler, one process per call.

| Chunk budget | Peak RSS | Against the 256 MiB budget |
|---|---|---|
| 8 MiB | 109 MiB | under |
| 16 MiB | 184 MiB | under |
| 32 MiB, first page | 279 MiB | over |
| 32 MiB, continuation | 319 MiB | over |

Pace to the milestones (ruling table in section (a); item 9 gives the adaptive
arithmetic, 3 h + (397 - 14) / 12 = 34.9 h):

| Milestone | Unchanged 30 min | Open-loop 15 min | Adaptive, 5 min floor |
|---|---|---|---|
| 2026 + 2025 (79 chunks; 32.1% of records, 19.6% of facts) | 39.5 h | 19.8 h | 8.4 h |
| 2026 to 2023 (139 chunks; 48.8% of records, 34.4% of facts) | 69.5 h | 34.8 h | 13.4 h |
| All 25 years (397 chunks) | 8.3 days | 4.1 days | 34.9 h |

Trip-line arithmetic (item 8). The trip opens above 15,000 CVE-keyed dirty rows
an hour for two hours. The only measured dirty-row ratio is 0.25 rows a fact
([10729-baseline-disposable-proof.md](10729-baseline-disposable-proof.md)
section 2, n = 80 facts), so 15,000 / 0.25 = 60,000 facts an hour, or 84 hours
for the corpus at any chunk size. The lane today runs at about 3,300 facts an
hour. The middle column above is about 50,800 facts an hour.

Limits: the adaptive column assumes a chunk's collect, commit, projection and
reducer pass finishes inside 5 minutes, which is not measured; the 0.25 ratio is
from 80 facts with every CVE planted to yield a finding; the files are a day
old; and the RSS figures come from the collector issue, not from this
repository.

## M12. Window gap served from the year seed (arbiter, 2026-11-16)

Measured by the arbiter on 2026-11-16, read-only, on a `git archive` copy of the
collector at `3e3968692e` over the same 25 cached year files, on an Apple M5 Max
with go1.26.11. The host was busy (load average 8 to 40), so absolute times are
pessimistic for this CPU; the ratios carry the ruling. The ruling is
[10671-window-seed-scan-ruling-2026-11-16.md](../arbiter/10671-window-seed-scan-ruling-2026-11-16.md);
the design consequence is in sections 1 and 5 of
[10671-nvd-feed-transport-fallback.md](../design/10671-nvd-feed-transport-fallback.md).
This closes R7b, the per-claim scan cost of the year set that the design left
unmeasured.

Method. Two scratch tests ran as test binaries in the scratch copy: one timed
each component of a per-claim scan (gunzip, sha256, a byte-level record scanner,
`json.Decoder` raw elements, and the shipped `nvdFeedRecordStamp` path) over all
25 files in two runs; the other built a 60-day tail in one pass and served every
6 hour window of the gap from it, comparing record ids with a full-scan serve. A
claim simulation ran at the 2 MiB and 10,000-fact caps with a lone over-budget
group paged. Outputs and analysis scripts stayed in the scratch directory.

| What | Result |
|---|---|
| Corpus | 25 files, 2,946,235,735 decoded bytes, 425,782 records |
| Shipped scan, all 25 files | 61.7 s and 47.4 s (two runs); 2026 alone 9.5 s and 10.1 s. The #198 reviewer measured 19.5 s for 2026, so 78 to 104 s per claim at that rate |
| Floor, gunzip only | 3.0 to 4.3 s |
| Floor, plus sha256 | 3.9 to 6.1 s |
| Floor, plus a byte-level record scanner | 7.2 to 10.0 s |
| Floor, plus `json.Decoder` raw elements | 15.7 to 20.4 s |
| Byte-pattern pre-filter prototype | 42.5 to 56.5 s, no real gain: 37,127 of 39,376 records in the 2024 file hold non-ASCII bytes |
| Gap [2026-10-17T20:18Z, 2026-11-07T08:00Z) | 48,202 records, 277,973,569 raw bytes, 9.4% of the corpus |
| Per 6 h window of the gap | p50 219 records and 1.3 MB; p90 2,198 and 11.9 MB; max 4,983 and 27.7 MB |
| File-level pruning | all 25 files hold a gap record; a perfect per-window file filter still opens 992 MB at p50 and 1.81 GB at p90 |
| Claims to cross the gap (simulated) | 198 claims, 225 collector invocations, 32 paged groups, 6 pages at most; 314,548 facts, 416,504,322 budget bytes |
| Largest paged group on the #198 branch | 3 pages (2026-10-23T23:10:00.157, 8.5 MB): 141 to 312 s of scanning in one lease |
| Largest paged group once #197 pages over the fact cap | 6 pages (1,402 records at 2026-10-18T21:10:00.120): 284 to 370 s at the arbiter's rate, 624 s at the reviewer's, against a 600 s lease in the docs |
| Tail build, 60 days | 88,927 records, 61 day buckets, 549,909,230 raw bytes, 67,664,342 gzip bytes (BestSpeed), largest bucket 44,991,120 bytes, 59.2 s, 135 MiB heap in use (naive: 61 gzip writers open at once) |
| All 82 six-hour windows from buckets | 0 windows differ from the full-scan record set (48,202 records); p50 240 ms, p90 724 ms, worst 1,397 ms (two buckets, 65.5 MB); 27.4 s for all 82 |
| Continuation re-read of the 5-page group at 2026-10-26T21:10:00.170 | 873 ms, one bucket of 45.0 MB |
| Bucket read rate | 47 to 51 MB/s; the ruled floor of 4 MiB/s is about 11 times under it |
| Record's id-year equals its file's year | 0 mismatches in 425,782 |

Limits: one CPU under load, not production hardware; the equality check compared
ids, not facts; the tail size is at BestSpeed gzip, and the builder's peak RSS
is unmeasured (heap only); the 6-page figure assumes #197 pages the two over-cap
groups as its commit message says.

## Commands

All read-only. `curl -s -H 'User-Agent: eshu-diagnostic'` against the feed
`.meta`, `.json.gz`, and per-year `.meta` files at one request a second;
Python run with `python3 -I` over the downloaded gzip; Postgres reads through a
`kubectl exec` session on the read replica with the password piped over stdin.
SQL used: `vulnerability_source_states` grouped by source, state, and terminal
status; `vulnerability_nvd_window_cursors`; counts, minimum `captured_at`, and
prefix groups over `supply_chain_impact_winners_dirty_log`. A 30-minute
`fact_records` scan by source cancelled at the 20 s statement timeout on a
407 GB table and was not retried.

## NOT_CHECKED

- Whether NVD regenerates a year file for every record change (one snapshot
  only; M4).
- Feed cadence across a full week; one `.meta` sample.
- Facts per record by year under the satellite's folding. The corpus total is
  measured in M11 (about 11.8 a record); a per-year split is not.
- Per-source fact counts from `fact_records` (query cancelled).
- Whether the satellite runtime itself carries a non-blank NVD key. The in-pod
  curl tests in M10 ran keyed and anonymous from the container's own
  environment, but per-claim telemetry does not record keyed versus anonymous,
  so the 66 failing claim attempts cannot be split by that variable.
