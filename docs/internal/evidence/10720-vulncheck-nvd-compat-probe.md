# 10720: VulnCheck as a backup or primary NVD source

Refs #10720. Part of #9209. Measured 2026-11-15 between 15:50Z and 16:05Z by the
coordinator, from a laptop, with a VulnCheck Community token. The user
authorized reading that token; it stayed in process memory and was never printed
or written to a file. Environment identifiers are redacted per #7803.

**Decision, threshold, deadline (prove-the-theory-first).** Decision: can
VulnCheck serve as a backup (or primary) for the NVD 2.0 CVE windows the NVD API
cannot finish? Threshold: it must slice by instant `lastModified` windows, return
the same record fields NVD does, cover the same CVEs, be at least as fresh, and
answer reliably faster than the NVD API, with licence terms that allow storing
the data. Deadline: 60 minutes of live probing, then write up. A failed probe
does not restart the clock. Every number below carries its sample size.

## Verdict

**Backup candidate, not primary, and not yet shippable.** Against NVD's Modified feed
it held all 11,017 feed records in the gap window, single calls were far faster
and steadier than the NVD API's, and it carried one record stamped after the
feed's generation time (freshness against the live NVD API was not measured).
It fails the fidelity threshold on one field that matters (`vulnStatus`) and carries
VulnCheck-authored additions that change what "NVD data" means, and the
licence terms are unresolved for a stored, redistributed product. NVD stays the
source of record (owner decision 2026-11-05). The ordering that follows from
this and from #10671 is: NVD API, then the official NVD 2.0 feed (identical
records, daily), then VulnCheck for the fresh edge as CVE id discovery only (see the
ruling below), gated on the open items at the end.

## M1. Two APIs, only one is a drop-in

`GET /v3/index` returned HTTP 200 in 0.3 s and listed six Community indices,
including `nist-nvd2`, `nist-nvd`, `mitre-cvelist-v5` and `vulncheck-kev`.

- **Native index** (`/v3/index/nist-nvd2`): not NVD-shaped. Records are flat
  (`id`, not `cve.id`) with `vcConfigurations` and `vcVulnerableCPEs`. Date
  filters take `YYYY-MM-DD` only (an instant is a 400). The filter does not key
  on NVD `lastModified`: in the 2026-11-14 window, 700 of 700 records carried
  `_timestamp` on that day but only 195 of 700 carried `lastModified` on it (the
  retained 100-record page agrees: 100 of 100 and 24 of 100), and the oldest
  `lastModified` in the window was 2026-06-17. The filter follows
  VulnCheck's own ingest time. Unusable for NVD-window slicing.
- **NVD-compatible endpoint** (`/rest/json/cves/2.0`, documented at
  docs.vulncheck.com under `nvd-compatibility`): `format NVD_CVE`, `version 2.0`,
  the NVD envelope, instant `lastModStartDate`/`lastModEndDate`, `cursor`
  paging, `resultsPerPage` up to 2,000. Both `Authorization: Bearer` and the
  NVD `apiKey` header were accepted (HTTP 200 each). `totalResults` 425,964.

The rest of this note measures the compatible endpoint.

## M2. The window the NVD API could not finish drains in two seconds

Window `[2026-10-17T20:18:09, 2026-10-18T02:18:09]`, the stuck window from
#10671. `totalResults` 4,982, matching the 4,982 the NVD API reported earlier.
Drained with `cursor` at 2,000 per page: 3 pages, 1.8 s, 4,982 unique ids, all
inside the requested bounds. Their `lastModified` values span only
00:10:00 to 01:10:00 on 2026-10-18, so the window is one mass-modification event,
which fits why the NVD API stalls on it.

## M3. The whole 28-day gap drains in about a minute

Window `[2026-10-17T20:18:09, 2026-11-14T20:18:09]`: `totalResults` 59,215.
Drained at 2,000 per page: 30 pages, 71.1 s, 279 MB, 59,215 unique ids, 30 of 30
requests HTTP 200. This is not a speed comparison with the NVD path: the roughly
58 hours in the #10671 measurements note is planner cadence (one 6 h window per
30 min run), which the feed transport is bound by too. The 59,215 count covers a
different span from the arbiter's 45,757 records over 25 days, so it is not a
coverage check either.

## M4. Reliability and limits

40 sequential unfiltered one-record requests: 40 of 40 HTTP 200, p50 0.20 s, p95
0.25 s, max 0.30 s. The 28-day drain above (30 of 30 HTTP 200, up to 2,000
records a page) is the stronger reliability evidence; the two figures measure
different calls. In the same hour the live NVD API, called with an NVD key from the
same laptop for 8 single-CVE reads, answered 6 in 3.9 to 21.0 s and failed 2
after 3 attempts each. The documented limit is 1,000 requests per minute per
token, with `Retry-After` on 429 and a retry-with-backoff rule for 503; no
`X-RateLimit` headers were returned. Samples are small, one session, one source
IP, one token.

## M5. Freshness and completeness

- A window `[15:00Z, 16:30Z]` on 2026-11-15, queried at about 16:01Z, returned 10
  records, newest `lastModified` 15:33Z; the native index read at 15:54Z showed
  the same newest record with `_timestamp` 15:52Z. The NVD Modified feed,
  generated 15:00Z by its `.meta`, held records only up to 14:17Z. What this
  supports is one VulnCheck record stamped 33 minutes after the feed's generation
  time, with about 30 minutes between it and wall clock at the last read (a
  Sunday, so NVD itself may have been quiet; 10 records, one afternoon, not a
  cadence).
- Ingest lag on the native index, `_timestamp` minus `lastModified` over 81
  records modified that day: min 2.8 min, p50 19.7, p95 22.0, max 530.0 (one
  outlier); no negative lags.
- Completeness against the NVD Modified feed: all 11,017 feed records in the
  28-day gap window were present in VulnCheck (0 missing), and window
  `[11-13T00:00, 11-13T06:00]` held 278 feed records with 0 missing (VulnCheck
  returned 279). The converse is open: 12 VulnCheck rows stamped 11-09 to 11-14,
  all `Awaiting Analysis` (for example CVE-2019-1491), are absent from the
  Modified feed and unexplained. On the native index, 60 of 60 random records modified 11-12 to 11-14 were
  present with an identical `lastModified`.

## M6. Window boundary: half-open, unlike NVD

Using a real `lastModified` as the pivot: the total for `end = pivot` equalled
the total for `end = pivot - 1 ms` (29,685 each), and a window starting at the
pivot returned at least 5 records stamped exactly at it (the request capped the
page at 5). So VulnCheck is start
inclusive, end exclusive. The NVD API includes both ends. A transport that
swaps one for the other drops or duplicates boundary records unless it trims;
the feed design already requires that trim (R1 in the #10671 design).

## M7. Fidelity against NVD: close, but not the same record

Comparisons are against the NVD Modified feed, which the earlier note showed
deep-equal to the NVD API on 20 of 20 records. Overlap in the gap window: 11,017
records, all with identical `lastModified`.

| Check | Result |
| --- | --- |
| `lastModified` equal | 11,017 of 11,017 |
| Whole record equal, raw | 1,436 of 11,017 (13%) |
| Equal after stripping VulnCheck additions (below) | 8,833 of 11,017 (80%) |
| `vulnStatus` equal | 10,459 of 11,017; 558 differ (5.1%) |

VulnCheck additions and changes, with record counts out of 11,017:

- `vcConfigurations` on 9,052 records and `vcConfigurationsWithPlatforms` on 346;
  `vcConfigurations` carries an empty `matchCriteriaId`. The `configurations`
  key itself passes NVD's data through, `matchCriteriaId` included. On the
  compatible endpoint `nodes[].negate` is present on all 50,216 nodes of the 59,215-record drain
  (all `false`; no sampled node in either source was `true`, so whether a `true`
  survives was not tested). The earlier 24-record read of the native index
  showed `negate` absent there. The flag matters: the reducer excludes a match
  inside a negated configuration or node from the affected index (#9696), so a
  dropped `true` would turn an excluded match into a false affected result.
- Multi-source `metrics` and `weaknesses`: VulnCheck adds a `Primary` entry from
  its own source and demotes NVD's to `Secondary`, so `type` flips on 1,149
  CVSS 3.1 entries and 1,173 weakness entries. `cvssMetricV40` has an extra
  entry on 1,418 records, and extra weakness entries appear on 471 plus 100
  records (`weaknesses` was absent on NVD for another 256).
- `cvssData` gains `temporalScore: 0` and `environmentalScore: 0`.
- `references[].tags` appear only on VulnCheck for 445 records.
- 2,184 normalized mismatches remain; the categories above explain most but I
  did not finish classifying every one. Read the 80% as a floor for what an
  adapter must reconcile, not a settled mapping.

**`vulnStatus` is the blocker.** 549 records are `Undergoing Analysis` at NVD and
`Awaiting Analysis` at VulnCheck, 8 go the other way, 1 is `Analyzed` at
VulnCheck against `Undergoing Analysis` at NVD, all with the same `lastModified`.
By status, the mismatch is 550 of 815 NVD `Undergoing Analysis` records (67%) and
8 of 2,939 `Awaiting Analysis`, and 0 for every other status. By `lastModified`
day it clusters on 11-08 (40 of 291) and 11-10 (302 of 2,765) and is 0 of 250 on
11-14, so it is bulk-clustered, not shown to sit at the edge. The live NVD API
was read for 8 mismatch records: 6 answered and all 6 returned the NVD feed's
value (`Undergoing Analysis`); 2 failed after 3 attempts. So VulnCheck, not the
feed, is the outlier. The satellite uses `vulnStatus` for deferral and severity
decisions, so taking it from VulnCheck would change projected truth silently.

## M8. Terms, from the published pages only

Not legal advice, and not verified as binding on a Community token:

- The NVD++ attribution notice allows free production use but requires
  prominent public attribution and labelling the data "VulnCheck NVD++" when it
  is shown to a user.
- The Service Terms (revised 2026-04-20) restrict making the data available free
  or under an open licence, and bar training or fine-tuning AI models on free
  offerings without a paid subscription. They are silent on storage and caching.
- No uptime or freshness commitment exists for the Community index; an SLA
  applies to the licensed tier only.

## Arbiter ruling

See [10720-vulncheck-ruling-2026-11-15.md](../arbiter/10720-vulncheck-ruling-2026-11-15.md).
Backup at rank 3 for the fresh-edge window class only, never primary; VulnCheck
content must not enter an NVD-labelled fact, so the only acceptable shape is CVE
id and `lastModified` discovery with content fetched from NVD; implementation is
blocked on written vendor answers, and the follow-up issue is deferred.

## What an adapter would have to do

The ruling above rejects a content adapter. For the record, a content-carrying
VulnCheck transport could only be safe if it (a) keeps only `nvd@nist.gov` and
other upstream-source metric and weakness entries and drops entries authored by
VulnCheck, (b) ignores `vc*` fields, `type` rewrites and the zero temporal and
environmental scores, (c) does not trust `vulnStatus` (carry it flagged), (d)
verifies that a `true` `negate` survives, which this note did not test, (e)
trims the half-open window, (f) labels every fact with its transport so rank and
audit can see it, and (g) stays behind the same cursor CAS and claim rules as the feed transport. That is
a design item with its own regression plan, not a quick adapter.

## Commands

All read-only. VulnCheck: `GET /v3/index`, `GET /v3/index/nist-nvd2` (limit,
page, cursor, `cve`, date windows) and `GET /rest/json/cves/2.0` (`resultsPerPage`,
`cursor`, `lastModStartDate`, `lastModEndDate`); the NVD Modified feed `.meta`
and `.json.gz`; NVD `GET /rest/json/cves/2.0?cveId=` for 8 records. Python was
run with `python3 -I`; downloaded data stayed in a scratch directory.

## NOT_CHECKED

- Whether the Service Terms bind a Community token, and whether storage,
  republishing, or feeding a downstream model is allowed. Needs a written answer
  from the vendor.
- Update cadence over more than one afternoon; one freshness sample.
- The 120-day window cap and sort order on the compatible endpoint; the 28-day
  drain did not hit a cap.
- Behaviour when records change during a cursor walk.
- `vulnStatus` mismatch cause (VulnCheck-derived status or an older snapshot);
  only the direction against NVD is measured.
- The remaining 2,184 normalized mismatches, and `cvssMetricV40` and
  `weaknesses` handling beyond counts.
- Whether the Community token is rate-limited per token or per IP, and any daily
  quota.
- NVD API behaviour for the 2 records that failed three times, and the NVD side
  of M4 at all beyond 8 single reads; the script for those reads was not kept.
- The 12 VulnCheck rows absent from the Modified feed, and whether a `negate:
  true` node survives the compatible endpoint (no sampled node was negated).
- Whether VulnCheck tracks the live NVD API or the feed; freshness was compared
  only against the feed.
