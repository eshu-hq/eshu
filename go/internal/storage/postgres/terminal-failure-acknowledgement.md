# Collector terminal failure acknowledgement (#8830)

`TerminalFailureAckStore` (`collector_terminal_failure_ack.go`) records an
operator acknowledgement of a collector terminal failure in
`collector_terminal_failure_acknowledgements` (migration 231), primary key
`(collector_kind, collector_instance_id, scope_id)`. The status read
`workflowCollectorTerminalFailuresQuery` (`workflow_terminal_failure_status.go`)
joins the rows back so an acknowledged failure is returned as acknowledged
instead of degrading the instance.

## Write

`Acknowledge` reads the newest unresolved `failed_terminal` row of the instance
and scope, then upserts the failure it saw:

- It refuses an unknown, disabled, or deactivated instance
  (`ErrTerminalFailureAckInstanceNotFound`), a scope with no unresolved failure
  (`ErrTerminalFailureAckNoFailure`), and a failure whose bounded class differs
  from the one the caller named (`ErrTerminalFailureAckClassChanged`).
- A failure an earlier acknowledgement already covers returns `Unchanged` and
  writes nothing, so the first reason and actor stay on record.
- The read and the write are two autocommit statements. The row stores the
  class and `updated_at` of the failure the read saw, so a failure landing in
  between is either the same episode (still covered) or a different class
  (reported); it is never suppressed unseen.

## One covering rule

`workflowTerminalFailureAckCovers` is the only definition of when an
acknowledgement `k` covers a `failed_terminal` row `t` of the same target: `t`
is older than `k.failure_at` (history the acknowledged failure superseded), or
`t` has class `k.failure_class` and no completed row of the target is newer than
`k.failure_at`. The status read's two laterals and the write's "already
covered" check all use it. Never copy it into another query.

The acknowledgement stores the failure it saw (class and `failure_at`), never a
work-item id, so workflow work-item retention (#8829) can delete the failed row
without touching it.

## Status read

The `u` lateral returns the newest unresolved failure no acknowledgement
covers; it keeps the `workflow_work_items_failed_terminal_instance_updated_idx`
seek with no Sort. Only when `u` finds nothing does the `a` lateral return the
newest covered one, behind a One-Time Filter, so an acknowledged failure never
hides an unacknowledged failure on a sibling scope.

Cost: at 542,800 work items, the read is 24.2 ms before and 25.0 ms after with
no acknowledgement, and each acknowledged scope with failures inside the window
adds about 0.45 ms (all 460 scopes of one instance acknowledged: 208.9 ms). A
partial index on completed rows would cut it; it was measured and not shipped.
Evidence: `docs/internal/evidence/8830-collector-terminal-failure-acknowledgement.md`.
