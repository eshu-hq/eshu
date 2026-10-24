# Security Alert Reconciliation Impact Wait

`security_alert_reconciliation` can only match a provider alert once the
`supply_chain_impact` work of the same scope generation has written its
finding. The projector enqueues both from the same generation. Until #8827 the
reconciliation spent one of its three counting attempts every time it ran
while the producer was still pending or retrying, and after the third it wrote
`unmatched` decisions with no signal. These signals show the wait and the
give-up.

## Counter

`eshu_dp_security_alert_reconciliation_impact_wait_total` counts one decision
per reconciliation pass that had a decision waiting on impact evidence. Every
label is a closed set:

| Label | Values |
| --- | --- |
| `domain` | `security_alert_reconciliation` |
| `outcome` | `deferred_in_flight`, `gave_up_unmatched`, `gave_up_bound_exceeded` |

| Outcome | Meaning | Spends an attempt |
| --- | --- | --- |
| `deferred_in_flight` | The same scope generation's `supply_chain_impact` work is pending, retrying, claimed, or running, and its newest sign of life (claim, retry schedule, or lease heartbeat) is under 30 minutes old. The pass deferred with `failure_class=security_alert_reconciliation_impact_not_ready` and wrote nothing. | No: the queue keeps `attempt_count` for that class. |
| `gave_up_unmatched` | The handler reached its cap of 3 attempts with no impact work in flight (it finished, dead-lettered, or never existed) and wrote `unmatched` decisions. | n/a, the attempt records `succeeded` |
| `gave_up_bound_exceeded` | The handler reached its cap while impact work was in flight but had shown no sign of life for 30 minutes, and wrote `unmatched` decisions. | n/a |

It never carries a scope id or a generation id; those are on the logs.

## Logs

| Message | Level | Fields |
| --- | --- | --- |
| `security alert reconciliation deferred: supply chain impact in flight for the same scope generation` | INFO | `domain`, `scope_id`, `generation_id`, `impact_wait_outcome`, `failure_class`, `impact_pending`, `impact_retrying`, `impact_claimed`, `impact_running`, `producer_max_wait`, `producer_elapsed` |
| `security alert reconciliation gave up waiting for impact evidence and wrote unmatched decisions` | WARN | `domain`, `scope_id`, `generation_id`, `impact_wait_outcome`, `attempt_count`, `handler_attempt_cap` |

The defer line never carries `attempt_count`, which the non-counting class
freezes. `producer_elapsed` is measured from the newest in-flight impact row's
last-workable time and is negative while a retry backoff or a live lease lies
in the future. On a give-up the attempt's success line also carries
`sub_signal_impact_wait_gave_up=1`.

## What to do

See [Deferred reconciliation](../../operate/3am-operator-runbook.md) in the 3 AM
runbook. A rising `gave_up_*` rate means alerts are resolving without impact
evidence; look at `supply_chain_impact` for those scopes.
