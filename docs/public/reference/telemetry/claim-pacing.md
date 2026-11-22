# Claim-Pacing Signals

Claim issuance paces grants when queue depth grows (#8470): per-domain
pending depth first, with global pending depth as the ceiling, under one
hysteresis latch. A paced grant issues no claim; the runner waits out the
grant delay and claims again. Five signals make the throttle legible at
3 AM.

`pacing_signal` and `domain` are pacing-series labels, not the
collector-level `collector_kind` taxonomy: `pacing_signal` is the closed
`domain`/`global`/`reducer_source` signal set, and `domain` names one
saturated family as `collector_kind/collector_instance_id`, set only on the
domain signal. The global ceiling carries no `domain` label.

Per-source reducer backpressure (#10882) reuses the same series with
`pacing_signal="reducer_source"` and `domain=<source_system>`: a source
slows when at least `ESHU_SOURCE_BACKPRESSURE_DEPTH_FLOOR` of its
claimable reducer rows are older than `ESHU_SOURCE_BACKPRESSURE_ENGAGE_AGE`,
and resumes once fewer than that many rows are older than
`ESHU_SOURCE_BACKPRESSURE_RELEASE_AGE`. The depth gauge is not emitted for
this signal; the trigger observation is the queue observer's
`eshu_dp_queue_source_oldest_age_seconds` and
`eshu_dp_queue_source_depth`. Ingester admission deferrals for a slowed
source land on `eshu_dp_reducer_admission_deferrals_total` with
`reason="source_behind"` and a `source_system` label. The coordinator
status surface publishes the same state as `source_pacing` rows (slowed,
reason, oldest claimable age, claimable depth, engaged since, stuck). A
consult the admission writer cannot read fails open: the group is
admitted, one warn log carries the source and error, and
`eshu_dp_reducer_admission_source_consult_failures_total{source_system}`
counts it.
`eshu_dp_reducer_admission_source_consult_duration_seconds` times every
enabled consult (#10882): the signal read or cache hit, the latch read and
any latch write, by `outcome` (`ok`, `error`) and `source_system` (the
per-source consult) or `collector_kind` (the family consult at a claim
grant). Read its p50 and p95 per `ESHU_SOURCE_BACKPRESSURE_SIGNAL_CACHE_TTL`
setting: the enablement gate needs p50 at or below 100 ms, and the alert fires
when p95 stays above 100 ms. A disabled leg records nothing.
Stuck marks an engagement older than
`ESHU_SOURCE_BACKPRESSURE_MAX_ENGAGEMENT` with the queue still behind:
still slowed, and loud. The claim-hot consult path probes one source at
a time (bounded per-source read, S0 verdict); the grouped scan serves
status publication only.

- `eshu_dp_workflow_claim_pacing_engaged_total` — counter of latch
  engagements, labeled `pacing_signal` and, for the domain signal,
  `domain`. Transitions
  matter: a gauge sampled between engagements reads as "fine".
- `eshu_dp_workflow_claim_pacing_active` — up/down gauge of whether pacing
  is on right now, labeled `pacing_signal` and, for the domain signal,
  `domain`. The counter alone cannot answer
  that: it holds the same value during an engagement and after it ends.
- `eshu_dp_workflow_claim_pacing_stuck_total` — counter of verdicts that
  stayed engaged past the engagement bound with depth still above the
  release watermark, labeled `pacing_signal` and, for the domain signal,
  `domain`. A stuck throttle stays loud; it is never released
  blindly.
- `eshu_dp_workflow_claim_pacing_delay_seconds` — histogram of the grant
  delay a paced claim carries, labeled `pacing_signal` and, for the domain
  signal, `domain`.
- `eshu_dp_workflow_claim_pacing_depth_pending` — gauge of the pending
  depth behind one pacing consult (#8472): the claiming family's depth with
  the `domain` label, the system-wide depth without it. Recorded from the
  same observation that feeds the verdict, so the depth that tripped the
  latch is visible next to the engagement.

Without these, a paced system is indistinguishable from a slow one:
throughput drops and every component-side metric looks healthy. A failed
depth query never paces (fail open), so pacing on a missing reading cannot
convert a metrics outage into an intake outage. The fail-open consult emits
only a debug record (`pacing consult failed open`, naming the error), so a
dead depth signal stays diagnosable without paging.

The active gauge is process-local: after a restart it starts at 0, so a
release with no matching engagement in this process reads -1 until the next
transition. Latch truth stays correct in the shared row; only the gauge
needs one transition to converge.

Per-component quota throttle signals (`quota_exceeded`, floor-rate
fallback) are not pacing signals: they belong to #8471, merged in #8557
and documented in [Claim Quota Signals](claim-quotas.md). They share the
`collector.claimed_run` coverage row with the pacing series above.
