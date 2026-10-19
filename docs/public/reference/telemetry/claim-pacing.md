# Claim-Pacing Signals

Claim issuance paces grants when queue depth grows (#8470): per-domain
pending depth first, with global pending depth as the ceiling, under one
hysteresis latch. A paced grant issues no claim; the runner waits out the
grant delay and claims again. Four signals make the throttle legible at
3 AM.

`pacing_signal` and `domain` are pacing-series labels, not the
collector-level `collector_kind` taxonomy: `pacing_signal` is the closed
`domain`/`global` signal set, and `domain` names one saturated family as
`collector_kind/collector_instance_id`, set only on the domain signal. The
global ceiling carries no `domain` label.

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
