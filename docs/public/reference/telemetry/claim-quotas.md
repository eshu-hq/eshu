# Claim Quota Signals

A component's `spec.quotas.maxGenerationsPerHour` bound is enforced per
(component, scope) per hour when the workflow coordinator grants a claim
(#8471): each scope row independently allows up to the limit, so a component
with items in N scopes can grant up to N times the limit per hour. An
over-quota scope is throttled even on an idle system, and the reservation
shares the grant transaction so concurrent coordinators cannot overshoot.

The coordinator builds its claim-path lookup at startup from the admitted
component manifests (#8561): one manifest fans out to every collector kind
it declares, and one kind gets one budget — conflicting declarations for
the same kind fail startup with the kind named. The uniform operator
override wins per field over every admitted manifest:
`ESHU_COMPONENT_QUOTA_MAX_GENERATIONS_PER_HOUR` (and
`ESHU_COMPONENT_QUOTA_MAX_FACTS_PER_GENERATION`, declared and validated
today but not runtime-enforced). Blank keeps the manifests; a set-but-invalid
value fails startup. Manifest or override changes need a coordinator
restart, since the lookup is built once at startup.

Two signals make a throttled system distinguishable from a slow one.

- `eshu_dp_workflow_claim_quota_exceeded_total` — counter of claims that found
  eligible work but granted nothing because the quota blocked them. Labels:
  `collector_kind`, `quota_kind` (`generations_per_hour`, the only
  runtime-enforced kind today). A climbing series beside a flat grant rate
  means the scope hit its hourly budget; the budget releases when the
  database-side hour window rolls.
- `eshu_dp_workflow_claim_quota_unreadable_fallback_total` — counter of
  admitted floor-rate fallback calls because the quota state was unreadable.
  Labels: `collector_kind`. A nonzero rate means quota protection is degraded
  to the floor budget (60 grants per component per coordinator-hour by
  default): healthy collectors keep moving, but a runaway is no longer
  precisely bounded. Each tick also logs a WARN line with
  `quota.collector_kind` and `quota.limit`. Calls denied by an exhausted
  floor throttle quietly and tick nothing.

`scope_id` never appears on either label — per-operator scope values are
unbounded — and no log line carries it today: the floor fallback never
learns the scope, so the registered `quota.scope_id` key is reserved for
future use.
