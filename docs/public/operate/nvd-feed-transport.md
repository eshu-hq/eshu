<!-- docs-catalog
title: NVD feed transport govern and trip
description: Govern the NVD 2.0 JSON feed transport kill switch, read its transport-labeled metrics, and act on the paging trip lines.
type: operate
audience: operator
entrypoint: false
landing: false
-->

# NVD feed transport: governed enable and trip lines

Use this page when you operate the NVD 2.0 JSON feed transport (design:
`docs/internal/design/10671-nvd-feed-transport-fallback.md`, arbiter ruling
`docs/internal/arbiter/10671-cvelist-mirror-ruling-2026-11-15.md`, design PR
#10719).
The feed is a second transport for NVD window claims, not a replacement: the
API stays primary when healthy, and cutover back is automatic — once the cursor
catches up, every window fails feed coverage and the API walk resumes with no
operator action.

## The kill switch

The top-level `nvd_feed_transport` key in the vulnerability-intelligence
configuration gates the feed. It defaults to off until soaked; setting it to
off forces the API walk from the next claim and logs the reason. A value that
is not on or off reads as off (a present-but-unknown value warns), so a typo
can only subtract the feed, never enable it. Nothing flips the switch automatically: an alert
pages, the operator flips.

## What to read at 3 AM

The window claim counters, the duration histogram, and the lag gauge carry
a closed `transport` label (`api`, `feed`, `unknown`); the feed age gauge
(`source`), the guard trips (`failure_class`), and the deadline-short,
mismatch, and meta-unavailable counters carry their own labels. On the feed
path the lag head is the feed horizon, not the server clock.

Satellite-to-host contract (#11287): the satellite stamps `nvd_window_transport`
(`api` or `feed`) into the source snapshot payload; the host folds it onto the
closed set and an absent or non-string stamp reads as `unknown`, never `feed`.
A `feed` series therefore proves a satellite-stamped feed window. Verify with
`sum by (transport) (rate(eshu_dp_vulnerability_nvd_window_claims_total[5m]))`:
after feed-served windows a `transport="feed"` series appears; before the
satellite pin every window reads `unknown`.

- `eshu_dp_vulnerability_nvd_window_claims_total` (`transport`, `outcome`):
  which transport served each window and how it ended.
- `eshu_dp_vulnerability_nvd_window_cursor_advance_total` (`transport`,
  `outcome=advanced|held`): held means the facts committed and the verdict
  held the cursor.
- `eshu_dp_vulnerability_nvd_window_claim_duration_seconds` (`transport`,
  `outcome`): wall time of one window claim, claim start to final outcome.
- `eshu_dp_vulnerability_nvd_cursor_lag_seconds` (`source`, `transport`):
  stored head minus cursor; on the feed path a growing lag means the planner
  or claims are behind the feed horizon.
- `eshu_dp_vulnerability_nvd_feed_age_seconds` (`source`): scrape time minus
  the newest feed meta time a completed claim carried; it keeps growing while
  no claim carries one.
- `eshu_dp_vulnerability_nvd_feed_guard_trips_total`
  (`failure_class=nvd_feed_year_stale|nvd_feed_group_oversize|nvd_feed_probe_failed|other`):
  each trip routed its window to the API walk.
- `eshu_dp_vulnerability_nvd_feed_deadline_short_total`: windows routed to
  the API because under 150 s of claim deadline remained.
- `eshu_dp_vulnerability_nvd_feed_sha256_mismatch_total`: retryable claim
  failures whose feed body did not match its `.meta` digest.
- `eshu_dp_vulnerability_nvd_feed_meta_unavailable_total`: windows that found
  the meta file missing or failing and ran the API; a sustained rate means the
  feed is gone, and the alert on this counter is how you learn that.

## Trip lines: page, then flip

Flip the switch off when either line trips; both are pages, not automatic
flips. "NVD-proxy dirty rows" means dirty-log rows written after the enable
whose `advisory_id` starts with `CVE-` (OSV rows also carry CVE ids, so the
proxy over-counts and trips early — the safe direction).

- More than 15,000 NVD-proxy dirty rows an hour in two consecutive hourly
  samples.
- The oldest dirty-log row is over 96 h. Compare against the pre-enable
  baseline before acting: the OSV/GHSA backlog was ~58 h old at measurement,
  so compare against your own pre-enable baseline, not this number — a
  pre-existing backlog is not a trip.

Re-enable only when the oldest dirty-log row is under 48 h, then record six
hourly samples. The #10706 bars stay soak bars, and the 2026-11-12 soak bar
applies unchanged.

## Sunset watch

If NIST announces a retirement date for the 2.0 feeds, the transport's design
changes. That watch is tracked in [#10731](https://github.com/eshu-hq/eshu/issues/10731):
before enabling, check it for the latest read date and sources; if a date
exists, do not enable — write the follow-up that changes the transport first.
