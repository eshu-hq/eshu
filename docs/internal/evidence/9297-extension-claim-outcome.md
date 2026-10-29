# Extension claim completion status (#9297)

The SDK returns complete, partial, or unchanged with bounded status counts. A
complete or unchanged result reaches `ClaimedService`, which calls
`CompleteClaim` after committing facts or resolving an unchanged generation.
The fenced completion statement stores result state, partial flag, and warning
count while marking the work item and claim complete.

A partial result, or any partial status within a result, is withheld before
fact mapping and snapshot commit. The fenced retryable or terminal failure
mutation stores `partial_withheld`, the partial flag, and bounded warning count
on the work item. It schedules a retry while attempt budget remains; an
exhausted attempt is terminal. A stale lease updates neither the work item nor
its claim. No withheld result reaches the projector queue.

The status read keys both completed and active probes by the enabled,
registered extension instances already present in the coordinator snapshot.
For each instance the outcome arm combines completed results with
`partial_withheld` rows in pending, claimed, or terminal state. It selects the
newest row per scope by update time and work item ID, then aggregates scope,
partial, and warning counts by instance. A newer clean completion supersedes
an older withheld partial; a newer withheld partial supersedes an older clean
completion. A separate claimed-row arm counts in-flight scopes. A third arm
gets `MAX(updated_at)` from completed results only, so a withheld attempt
updates status time without advancing `last_observed_at`.

Disabled, deactivated, unregistered, and in-tree instances are omitted because
their SDK outcome cannot alter runtime health. An in-flight claim makes the
instance partial while fact commit and claim completion may be in separate
transactions. Terminal failures retain their existing higher-priority status
path. Provider payloads and scope IDs do not leave the aggregate read.

Performance Evidence: NOT_PROVEN. The status query is keyed by registered
extension instances, but each instance can have many workflow rows. Its
outcome arm unions completed and withheld rows before newest-per-scope
selection; the other arms count claimed rows and compute the completed-only
`MAX(updated_at)`. The completed-result partial index and the existing
claimable index may help these reads, but no representative PostgreSQL plan
or timing has established their use or bounded the added work. Query latency
and write amplification need a matched before/after run on a representative
workflow table before promotion.

Observability Evidence: The existing
`eshu_dp_status_snapshot_read_duration_seconds` records the new
`extension_claim_outcomes` read stage and error outcome. The existing claim
run duration and failure-class signals cover the completion mutation; no
provider values are metric labels.
