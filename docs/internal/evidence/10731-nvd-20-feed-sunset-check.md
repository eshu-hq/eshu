# #10731 NVD 2.0 feed sunset check (2026-11-23)

## Claim

No NIST retirement date exists for the NVD 2.0 JSON data feeds as of
2026-11-23. The feeds are live and regenerating, and every retirement
notice found concerns the legacy 1.x feeds, the 1.0 API, RSS, or CVE
legacy formats — not the 2.0 JSON feeds the feed transport reads.

## First-Hand Evidence (fetched 2026-11-23, exit 0)

- `https://nvd.nist.gov/feeds/json/cve/2.0/nvdcve-2.0-modified.meta`
  (HTTP 200):
  `lastModifiedDate:2026-11-22T18:00:05-05:00`, zipSize 3306891,
  sha256 `38B20C19…F70678`.
- `https://nvd.nist.gov/feeds/json/cve/2.0/nvdcve-2.0-2024.meta`
  (HTTP 200): `lastModifiedDate:2026-11-21T03:01:54-05:00`.
- `https://nvd.nist.gov/feeds/json/cve/2.0/nvdcve-2.0-2025.meta`
  (HTTP 200): `lastModifiedDate:2026-11-22T03:01:26-05:00`.
- NVD API 2.0 `https://services.nvd.nist.gov/rest/json/cves/2.0/?resultsPerPage=1`
  (HTTP 200): `totalResults:430558`, `format:NVD_CVE`, `version:2.0`,
  `timestamp:2026-11-23T00:15:09.429`.

The year files regenerate roughly daily, which is the normal NVD rewrite
cadence, not a shutdown ramp.

## Second-Hand Evidence (search-index snippets of NIST pages)

- `https://nvd.nist.gov/general/news/change-timeline`: "The retirement
  timeline has been extended for the Legacy Data Feed Files until further
  notice." The page's retirement rows name legacy data feeds and the 1.0
  APIs only.
- `https://nvd.nist.gov/general/news/changes-to-feeds-and-apis`: the
  late-2023 retirement concerned data feeds superseded by the 2.0 APIs.
- 2026 NVD news (Apr 15 selective-enrichment model; Jun 17 SSVC/affected
  data added to feeds and APIs; Aug 26 history-feed link change) expands
  or reshapes feed content; none retires the 2.0 feeds.

## Explicitly Not Checked

`https://nvd.nist.gov/vuln/data-feeds` and
`https://nvd.nist.gov/general/news/change-timeline` render as Angular
app shells (HTTP 200, ~2.3 KiB JS shell, no server-rendered text), so
curl cannot read their current body text — the same limitation the
issue predicted. This lane has no browser tool; the page-body claims
above rest on the search index, not a render. A re-check with a real
browser render is recommended before the next scheduled watch, or sooner
if the feed-age trip line fires.

## Runbook Pointer

`docs/public/operate/nvd-feed-transport.md` does not exist on `main`;
it lives only on the #10716 lane branch
(`drive/10716-nvd-feed-switch-telemetry-prior-20261115`), whose
`## Feed retirement` section already reads: "A NIST notice retiring the
NVD 2.0 JSON data feeds is tracked in eshu-hq/eshu#10731, not on this
page." No lane-C edit to the runbook was needed or made; the pointer
arrives on `main` with the #10716 PR.

## Outcome

Per the issue: no date exists, so no transport-changing follow-up is
filed. This file is the check record. Next watch: on any NIST feeds
announcement, or when the feed-age guard trips.
