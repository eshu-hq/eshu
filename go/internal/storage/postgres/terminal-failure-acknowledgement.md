# Collector terminal failure acknowledgement (#8830)

`TerminalFailureAckStore` (`collector_terminal_failure_ack.go`) records an
operator acknowledgement of a collector terminal failure in
`collector_terminal_failure_acknowledgements` (migration 231), primary key
`(collector_kind, collector_instance_id, scope_id)`, no foreign key. The status
read `workflowCollectorTerminalFailuresQuery`
(`workflow_terminal_failure_status.go`) joins the rows back so an acknowledged
failure is returned as acknowledged instead of degrading the instance.

## Write

`ResolveTarget` reads the instance (kind match, enabled, not deactivated) and the
newest unresolved `failed_terminal` row on the scope inside the instance's window
(3 x `scan_interval`, 30 minute floor). `Acknowledge` repeats that read inside one
READ COMMITTED transaction and upserts the row it saw: `failure_at` is that row's
`updated_at`, `work_item_id` its id, and `failure_class` its raw class. It
refuses an inactive or mismatched instance, a scope with no unresolved failure
inside the window, and (when the caller names one) a bounded class that differs.
The write touches only the acknowledgement row, so it shares no lock with the
failure writers, which update claimed work items. The caller's idempotency
ledger (`admin_replay_requests`) serializes retries; two different keys both
commit and the later row stands.

`ListTerminalFailureAcks` is the admin read and the only one that returns the
reason.

## One covering rule

`workflowTerminalFailureAckCovers` is the only definition of when an
acknowledgement `k` covers a `failed_terminal` row `t` of the same target: `t` is
not later than `k.failure_at`, or `t` has the raw class `k.failure_class` (NULL
and empty agree) and no completed row of the target landed between
`k.failure_at` and `t.updated_at`. Both laterals of the status read use it. It
reads only `updated_at` values written by one clock family, never `now()`. Never
copy it into another query.

The acknowledgement stores the failure it saw, not a reference to the work item,
so workflow work-item retention (#8829) can delete the failed row without
touching it. There is no expiry and no sweeper.

## Status read

The `u` lateral returns the newest unresolved failure no acknowledgement covers.
A scope pre-filter (`t.scope_id <> ALL(acknowledged scopes of the instance)`)
keeps a row on an unacknowledged scope from probing the acknowledgement table and
keeps the planner from running the completed probe on a covered row first. The
`a` lateral returns the newest covered failure and sits behind a One-Time Filter
for an instance with no acknowledgement. Both keep the
`workflow_work_items_failed_terminal_instance_updated_idx` seek in index order
with no Sort.

Plan and cost: `docs/internal/evidence/8830-collector-terminal-failure-acknowledgement.md`.
