# Arbiter ruling: #10817 canary Bar1 read-rate bar recalibration (2026-11-23)

Recorded by the coordinator; the arbiter does not edit files. Arbiter:
`arbiter-eshu`. Goal: #10671. Origin: the #10817 QA canary re-run
(2026-11-23, host `0772ee6913`, satellite `8b24141e`), where the
three-seed-windows-at-8-MiB/s bar failed at ~0.015 MiB/s observed. Design:
[10671-nvd-feed-transport-fallback.md](../design/10671-nvd-feed-transport-fallback.md).
Issue: [#10817](https://github.com/eshu-hq/eshu/issues/10817) (canary
paragraph edited to this ruling in the same drive).

Ruling (the arbiter's headline): recalibrate Bar1; do not keep it as-is and
do not wait for natural multi-MB feed windows. The 4 MiB/s figure was only
the deadline-budget divisor, set ~12x below the measured 47-51 MB/s
bucket-read rate. Reused as an observed end-to-end rate it is disproved by
the QA data: a 67,303 B bucket behind flat ~4.2 s satellite overhead caps at
~0.015 MiB/s at infinite disk speed. Keeping the bar also gates enablement
on lagging feed windows the API-primary design suppresses while the planner
cursor sits at head, so it may never fire.

## Decisions

1. Bar1 rate is the collector bucket-read segment alone (open + gunzip +
   scan, `nvd_seed_read_ms`), excluding satellite latency and core upsert.
   Bar: >= 8 MiB/s only where the touched bucket bytes total >= 1 MiB;
   below that, the bar is an absolute read duration <= 2 s.
2. Satisfy the >= 1 MiB leg with one forced QA canary: a deliberately
   lagging, seed-covered window (or a replayed claim against a multi-MB day
   bucket), not natural traffic. Three seed-served windows and one paged
   group are still required; at least one window must be forced.
3. Add deadline proof: every canary window's actual read duration must fit
   `bucket_bytes/4MiB/s + 75s`, the floor it was admitted under.

## Context

The canary observed the three latest feed windows touching only the
2026-11-22 day bucket (67,303 B on the seed volume). Satellite
`source_latency_ms` is flat at ~4.2-4.4 s across 136-644 facts, so fixed
overhead dominates and small-bucket windows structurally cannot pass an
8 MiB/s end-to-end bar — only windows touching multi-MB buckets could. The
planner cursor sat at head the whole run (no new feed claims expected until
fresh NVD data arrives), and zero snapshots carry
`nvd_window_transport=feed`, so the feed path is still unexercised and
waiting does not exercise it. The `nvd_seed_read_ms` / `nvd_seed_buckets` /
`nvd_seed_bytes` timing already ships in the deployed satellite pin
(`nvd_feed_seed_serve.go`, in `8b24141e`), so no instrumentation issue was
filed. Baseline bars are unaffected: 0/4 chunks flowed because satellite
#199 (record-atomic chunker) is still open; that blocker is recorded on
[#10728](https://github.com/eshu-hq/eshu/issues/10728).

## Consequences

Good: the bar measures what the floor prices (bucket reads), with a
defined small-bucket leg and a forcing rule that works under the
API-primary design instead of waiting on traffic that design suppresses.

Bad: a forced lagging window is a deliberate QA mutation outside the
read-only canary already run; it needs its own reviewed step. The
multi-MB-bucket leg still depends on real NVD catch-up flow, not on the
canary runner alone.

## Acceptance criteria

- [ ] One forced seed-covered window claim is served on QA and its
  `nvd_seed_read_ms` / `nvd_seed_bytes` pair is recorded.
- [ ] The recorded pair passes decision 1 (>= 8 MiB/s at >= 1 MiB, else
  read <= 2 s) and decision 3 (fits `bucket_bytes/4MiB/s + 75s`).
- [ ] `min_chunk_interval` stays at the 30-minute default until Bar1 and
  the baseline-chunk bar both pass.

<details>
<summary>Evidence and measurements</summary>

Canary workflow report, QA 2026-11-23 12:12Z-14:23Z: W1/W2/W3
0.015 MiB/s each (67,303 B bucket / ~4.2-4.4 s satellite latency);
upsert-basis variants 0.049/0.098/0.026 MiB/s. Planner cursor
1795435993322000 at head, updated 12:13:53Z. Store census: 13,707
`vulnerability.source_snapshot` rows; 46 with `nvd_transport=feed` (all
predating the satellite pin, none carrying the `nvd_window_transport` key);
exactly one row with `nvd_window_transport=api`. Paged group confirmed
(batch_count=2). Inflow 920/hr trailing-60m; collector RSS peak 28 MiB.
Full per-bar verdicts are on
[#10817](https://github.com/eshu-hq/eshu/issues/10817#issuecomment-7103957413).
</details>
