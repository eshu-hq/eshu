# Requeue Reasons

Every reducer requeue path stamps `fact_work_items.last_requeue_reason` from
a closed vocabulary (#11615) and counts the transition. No `domain` label:
per-domain breakdown is a runbook SQL `GROUP BY domain,
last_requeue_reason`; NULL reads as `legacy_unclassified` (never requeued,
or predating the column) and no writer may stamp that label or `unknown`.

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_reducer_requeue_total` | counter | Reducer rows returned to an outstanding state, by `reason` (`retry_backoff`, `readiness_wait`, `waiting_wake`, `maintenance_reopen`, `producer_activation_settle`, `completion_wake`, `cross_scope_replay`, `fanout_holder`, `retouch_new_evidence`, `admin_replay`, `admin_reopen`, `recovery_redrive`). Claimed or running rows newly flagged for replay debt count at schedule time; the database ACK-trigger conversion they later cause is never double-counted. A rolled-back transaction never counts: the `waiting_wake` reason (#11624) is emitted after the owning transaction commits, so it can run below `eshu_dp_reducer_waiting_wake_total{outcome="reopened"}`, which counts wake statements and still records a wake whose transaction later rolled back. Touches that coalesce into an already-scheduled run (a retouch on a pending or retrying row) create no execution and are neither stamped nor counted. |
| `eshu_dp_reducer_requeue_depth` | gauge | Live reducer queue depth by `stage`, `status`, and `reason`, on the same poll as queue depth/age. The `legacy_unclassified` series is the rollout-ramp proof: it must trend to ~0 for reducer rows as the fleet rolls, and any plateau is a missed writer. |
| `eshu_dp_reducer_requeue_oldest_age_seconds` | gauge | Age in seconds of the oldest outstanding reducer queue item by `reason`. A climbing `readiness_wait` age with a flat counter means waits park and never resolve; a climbing `recovery_redrive` age means the same rows re-drive without converging. |
