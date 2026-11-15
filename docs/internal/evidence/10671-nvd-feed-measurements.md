# 10671: NVD outage measurements and the feed-transport theory

Refs #10671. Measured 2026-11-15 between 04:30Z and 05:30Z by the coordinator,
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
`2026-11-13T17:10:24Z`. The cursor trails wall clock by about 29 days, not the
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

- The 29-day gap is about 290k facts.
- One 6 h window per 30 min planner run gives about 116 windows, about 58 h
  of wall clock. Planner cadence binds, not the 10,000-fact claim cap.
- A dense 6 h slice can exceed 10,000 facts (Nov 10 averages 5.9k per 6 h, so
  a front-loaded slice will), so the prefix-commit overflow rule is exercised.

## M6. The dirty log and who feeds it

`supply_chain_impact_winners_dirty_log` at 04:46Z: 1,080,966 rows, oldest
`captured_at` 2026-11-12 18:44Z (about 58 h). By `source`: `fact_upsert`
960,818; `fact_retract` 113,427; `activation` 6,721. Last 3 h: `fact_upsert`
238,282. Hourly inflow, 22:00Z to 04:00Z: 15,647 (partial), 59,975, 80,753,
109,133, 95,266, 75,277, 81,079 rows, about 1.0 to 1.8k a minute. One
committed fact upsert writes about one row.

Attribution. Newest 50,000 rows by `advisory_id` prefix: `GHSA` 49,561, `CVE-`
433, blank 6. Oldest 50,000: `GHSA` 47,847, `CVE-` 1,708, blank 371, `PYSE`
74. These are contiguous slices, not random. `vulnerability_source_states`
(latest run per scope, so an undercount), last 24 h: osv 85,439 results in
1,022 runs, first_epss 1,880, cisa_kev 1,782, cve_list 18, ghsa 2, **nvd 0**.
With NVD down, the current inflow is OSV/GHSA-keyed. #10706 describes it as
NVD inflow; that does not match these reads.

Effect of an NVD catch-up: about 290k rows over about 2.6 days is about 4.6k
rows an hour, about 4 to 8 percent of the current inflow, but about 27 percent
of today's backlog.

## M7. Deployed configuration

QA collector deployment: claim lease 10 m, heartbeat 30 s, canonical write
timeout 300 s, an NVD API key referenced. The code default lease is 60 s
(`go/internal/workflow/defaults.go:9`). The core image is an ancestor of
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
pod succeed 12 of 13 times), and depth is not deterministic. NVD returns
fast edge 503s on roughly one request in five, and slow 3 to 13 s first bytes
between them, which matches the issue's two failure faces. It does not explain
why the satellite fails 66 of 66 with 12 to 118 s latencies; that stays open
under collector#97. The arbiter's proposed step of unsetting or rotating the
key is not supported by these results and is not taken. Samples are small (13
keyed requests, one afternoon, one source IP).

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
- Facts per record under the satellite's folding, rather than this upper bound.
- Per-source fact counts from `fact_records` (query cancelled).
- Whether a keyed NVD request changes outage-mode latency; no key was used.
