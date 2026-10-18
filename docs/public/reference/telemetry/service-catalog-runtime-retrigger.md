# Service Catalog Runtime Re-trigger

A service generation can commit with an incomplete runtime family: the
runtime readiness fence answers ready when a repository has no workload
producer yet, and it commits what it can read once every in-flight producer is
past its 30-minute bound. A `workload_materialization` ACK repairs that. In
the same statement as the ACK, it reopens or flags the
`service_catalog_correlation` rows whose decisions name the acked repository
(#8169). These signals show what each ACK re-triggered.

## Counter

`eshu_dp_service_catalog_runtime_retrigger_total` counts the catalog rows a
succeeded producer ACK touched. Every label is a closed set:

| Label | Values |
| --- | --- |
| `domain` | the producer domain whose ACK re-triggered: `workload_materialization` |
| `outcome` | `reopened` (a succeeded catalog row returned to pending; adds the row count), `replay_flagged` (a claimed or running catalog row got `cross_scope_replay_required`, so its own ACK returns it to pending; adds the row count), `none` (the ACK matched no catalog row; adds 1 per ACK statement) |

It never carries a scope id, a repository id, or a service id.

A steady `none` with a rare `reopened` is normal: most workload ACKs belong to
repositories that no catalog decision names. A `reopened` rate close to the
workload ACK rate means many catalogs name the repositories being
re-materialized. Each reopen costs one catalog handler run; an unchanged
repository produces no new service generation (the generation id is the
evidence fingerprint).

## Log

One info line per ACK statement that reopened or flagged a row:
`service catalog correlation re-triggered by workload materialization ack`,
with `domain` (`service_catalog_correlation`), `producer_domain`, `scope_id`
(the acked repository scope; empty for a batch ACK), `acked_count`,
`reopened_count`, and `replay_flagged_count`.

The runtime readiness fence's defer and bound-overrun lines carry
`producer_kind` (`workload_materialization`, `repo_dependency`, or both) and
`runs_on_intents_pending`, the pending `repo_dependency` RUNS_ON intents of
the correlated repositories. See
[Reducer And Storage Metrics](metrics-reducer-storage.md) for
`eshu_dp_service_catalog_runtime_readiness_total`.
