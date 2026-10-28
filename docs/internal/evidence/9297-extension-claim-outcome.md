# Extension claim completion status (#9297)

The SDK returns complete, partial, or unchanged with bounded status counts. The
extension host attaches that validated result to the collected generation.
`ClaimedService` hands it to `CompleteClaim` only after committing facts or
resolving an unchanged generation. The existing fenced completion statement
writes result state, partial flag, and warning count on the work item while it
marks that item and its claim complete. A stale lease updates neither row.

The status read keys both completed and active probes by the enabled,
registered extension instances already present in the coordinator snapshot.
For each instance it selects the newest completed result per scope, then
aggregates by instance. Disabled, deactivated, unregistered, and in-tree
instances are omitted because their SDK outcome cannot alter runtime health.
A newer clean completion supersedes an old partial result for that scope.
An in-flight claim makes the instance partial while its fact commit and claim
completion may be in separate transactions. Terminal failures retain their
existing higher-priority status path. Provider payloads and scope IDs do not
leave the aggregate read.

Performance Evidence: NOT_PROVEN. The change adds one bounded-result status
query over completed SDK outcomes and active claims for registered extension
instances. The completed branch has a partial index ordered for its
newest-per-scope selection; the active branch uses the claimable index key
prefix. Query latency and write amplification require a
matched before/after run on a representative workflow table before promotion.

Observability Evidence: The existing
`eshu_dp_status_snapshot_read_duration_seconds` records the new
`extension_claim_outcomes` read stage and error outcome. The existing claim
run duration and failure-class signals cover the completion mutation; no
provider values are metric labels.
