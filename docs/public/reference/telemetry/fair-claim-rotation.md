# Fair-Claim Rotation Signals

Claims from the workflow queue run priority-then-rotation (#8469): the lowest
`claim_priority` tier first, then a uniform rotation across that tier's
fairness keys. Three signals make the rotation legible at 3 AM.

- `eshu_dp_workflow_fair_claims_total` — counter of granted claims, labeled
  `collector_kind` and `claim_priority`. A tier whose share flatlines while
  its backlog grows is stalled.
- `eshu_dp_workflow_fairness_key_depth` — pending depth for the top 10
  fairness keys per collector family. One enormous key beside flat siblings
  means one account is behind; all ten climbing together means everything is.
- `eshu_dp_workflow_fairness_key_count` — distinct pending keys per family:
  the denominator of the round-robin wait bound.

The claimed-run span carries the raw `fairness_key` and `claim_priority`, so
a trace names the exact rotation slot. Raw keys never appear on any other
metric label: the depth gauge is capped at ten series per family at the
emission site, and every other fair-claim signal uses the closed 0-4
priority rank.
