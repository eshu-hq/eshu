# Supply Chain Impact Attachment Wait

The projector enqueues `sbom_attestation_attachment` and `supply_chain_impact`
from the same scope generation. The impact pass reads the attachment fact
that its sibling writes. Nothing re-runs the pass when the attachment commits
later. Before #11105 an impact pass could run while its own attachment was
retrying (a backoff with a future `visible_at`). It then published findings
without the attachment-backed ones, and they stayed missing until an
unrelated trigger.

Now the impact pass reads its own generation's attachment work before it
issues a fencing token or loads any evidence. While that work is in flight,
the pass defers and spends no attempt. The attachment's terminal Ack reopens
it at once.

## Counter

`eshu_dp_supply_chain_impact_attachment_wait_total` counts one decision per
impact pass that deferred on its attachment, or that ran without one. A pass
records nothing when its generation has no attachment work, or when every
attachment row succeeded or was superseded. Every label is a closed set:

| Label | Values |
| --- | --- |
| `domain` | `supply_chain_impact` |
| `outcome` | `deferred_in_flight`, `producer_failed`, `bound_exceeded` |

| Outcome | Meaning | Spends an attempt |
| --- | --- | --- |
| `deferred_in_flight` | The same scope generation's `sbom_attestation_attachment` work is pending, retrying, claimed, or running. Its newest sign of life (a claim, a retry schedule, or a lease heartbeat) is under 30 minutes old. The pass deferred with `failure_class=supply_chain_impact_sbom_attachment_not_ready` before it loaded evidence, and wrote nothing. | No: the queue keeps `attempt_count` for that class. |
| `producer_failed` | No attachment work is in flight, but at least one attachment row dead-lettered or failed. The pass ran without that attachment's evidence. | n/a, the pass runs |
| `bound_exceeded` | Attachment work is in flight but has shown no sign of life for 30 minutes. The pass stopped waiting and ran without it. | n/a, the pass runs |

The counter never carries a scope id or a generation id. Those are on the
logs.

## Logs

| Message | Level | Fields |
| --- | --- | --- |
| `supply chain impact deferred: sbom attestation attachment in flight for the same scope generation` | INFO | `domain`, `scope_id`, `generation_id`, `attachment_wait_outcome`, `producer_domain`, `attachment_pending`, `attachment_retrying`, `attachment_claimed`, `attachment_running`, `attachment_succeeded`, `attachment_failed`, `attachment_total`, `producer_max_wait`, `failure_class`, `producer_elapsed` |
| `supply chain impact ran without a dead-lettered or failed sbom attestation attachment` | WARN | the same fields, without `failure_class` |
| `supply chain impact stopped waiting for an sbom attestation attachment past the bound and ran without it` | WARN | the same fields, without `failure_class` |

`producer_elapsed` is measured from the newest in-flight attachment row's
last-workable time. It is negative while a retry backoff or a live lease lies
in the future, and it is absent when no row is in flight. The defer line
never carries `attempt_count`, which the non-counting class freezes. The
deferral also records
`eshu_dp_reducer_executions_total{domain="supply_chain_impact",status="deferred"}`
and the reducer's WARN `reducer execution deferred` line.

## Queue behavior

The deferral parks a `producer_domain_terminal` wait keyed on the pass's
`scope_id`, `generation_id`, and `producer_domain=sbom_attestation_attachment`.
The attachment's terminal Ack reopens it in the same post-commit wake that
other narrowed producer waits use. A dead-lettered attachment does not Ack,
so the waiting pass comes back through the waiting audit or the 10-minute
backstop, then records `producer_failed`. An attachment Ack in another scope
does not reopen the pass (#11114): the wait declares a same-scope-generation
provider, so only its own scope generation's Ack wakes it.

A probe that cannot read the attachment state is a counting failure. It is
never read as ready or not ready, so an unreadable store dead-letters on the
retry budget and does not retry forever.

## What to do

- **Do nothing** while `deferred_in_flight` is small and the attachment
  backlog drains. The pass comes back on the attachment's Ack.
- **Worry** when `deferred_in_flight` keeps rising. The counter carries no
  scope label, so find the scope from the `scope_id` and `generation_id` on
  the INFO `supply chain impact deferred: sbom attestation attachment in
  flight for the same scope generation` lines. A scope that keeps logging it
  has an attachment that is not finishing. Find the attachment holder with
  `eshu_runtime_domain_oldest_age_seconds{domain="sbom_attestation_attachment"}`.
- **Act** on `producer_failed` or `bound_exceeded`. The pass published without
  the attachment, and it does not re-run when a replayed attachment commits
  later in the same generation. After you replay the dead-lettered attachment
  item, also replay that scope's `supply_chain_impact` item. Otherwise the
  attachment-backed finding appears only with the next generation.
