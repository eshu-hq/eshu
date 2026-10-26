# GitHub Actions delivery inbox

The inbox stores bounded, immutable delivery identities keyed by
`X-GitHub-Delivery`; its caller must verify the GitHub HMAC before insertion.
The coordinator consumer is available, but the listener adapter follows in a
separate change. The existing listener does not yet enqueue workflow events.
The coordinator plans every configured
GitHub Actions scope that matches the delivery repository. It uses the existing
open-target admission guard, verifies the exact durable workflow item for each
scope, then ACKs the delivery with its current fencing token. Another open run
cannot satisfy the ACK check. A duplicate delivery cannot reopen completed work
or alter the event identity. No raw payload or credential is persisted.

The claim conflict domain is a delivery ID. Two coordinators use `SKIP LOCKED`
and fencing tokens to claim disjoint deliveries and reject stale ACKs. Each
claim takes at most 100 deliveries with a one-minute lease. Within one claim,
selected deliveries with the same exact `(repository, repository_id)` receive
one persisted `batch-v1:` handoff key. The key is assigned only while empty;
release, reap, and reclaim preserve it. A delivery inserted after that claim's
selection receives a fresh key on a later claim, even if an earlier group is
still collecting. Concurrent coordinators can select separate groups for the
same repository, so one group per repository is a bound within a selected
claim, not a promise for an entire burst.

The coordinator plans one full-snapshot work item per configured scope in a
group. Its `webhook-group-v1-` plan key hashes the persisted handoff key,
scope, and normalized tenant boundary. This namespace is distinct from the
older per-delivery plan keys, so exact work for a legacy anchor delivery cannot
cover the other group members. Each scope has an independent run identity:
if one scope finishes while another is blocked, the missing scope can still be
admitted on retry. Every delivery is ACKed separately with its current fence
only after the group's exact durable work exists for every scope. Admission errors
retry up to eight claims and then fail terminally; expired claims are reaped in
batches of 100. A reap returns bounded requeued and exhausted counts from the
updated row statuses. The coordinator records each count in
`eshu_dp_ci_cd_run_webhook_reaps_total` and warns with aggregate counts on
exhaustion; reaps are separate from handoff attempts. Benign blocked or
inactive-target releases wait 30 seconds
without consuming that attempt budget. The queued/claimed repository index lets
scheduled GitHub fallback defer while any delivery is pending, including a
blocked delivery whose next claim is not visible yet. GitLab scheduling retains
its existing path. The bootstrap DDL and migration 236 must match.

## No-Regression Evidence

- `TestActionsDeliveryLeaseAndIdentityLive` uses PostgreSQL to prove conflicting
  identities fail, stale ACK is fenced, a duplicate terminal delivery remains
  terminal, and a distinct delivery survives another claim.
- `TestActionsDeliveryConcurrentClaimsAreDisjointLive` and
  `TestActionsClaimSkipsHeldRowLocksLive` prove concurrent selection and that a
  held row lock does not stall claim or reap of another delivery. The same
  tests verify mixed requeue/exhaustion counts, durable failed statuses,
  repeat idempotency, and preservation of an unexpired lease.
- `TestActionsClaimPersistsBoundedRepositoryBatchLive` proves 100 selected rows
  share one key, a late arrival and different repository identities stay
  separate, and a partially released row keeps its key on reclaim.
- `TestActionsDeliveryBlockedBudgetAndFailedRetryCapLive` proves blocked
  retries survive beyond the error cap, while eight admission errors fail
  terminally.
- `TestPendingRepositoryIncludesDeferredDeliveryLive` proves blocked work stays
  pending until retry, ACK clears its prior failure class, and completed work
  no longer blocks fallback.
- `TestActionsDeliveryMigrationMatchesBootstrapSchema` compares migration 236
  with the DDL applied by `EnsureSchema`.
- `TestCICDActionsAdmissionBeforeACKCrashUsesExactDurableWorkLive` exercises the
  production planner, guarded workflow admission, exact item lookup, expired
  lease reaping, retry, and ACK crash recovery against PostgreSQL.
- `TestCICDMixedProviderScheduledGroupsShareDurableRunLive` checks the storage
  contract for incremental admission into a shared, nonterminal run; it bypasses
  the Service scheduler. Production uses separate run identities per provider.
  `TestCICDRunMixedProviderAdmissionsSurviveFirstGroupTerminalizing` exercises
  that scheduler and proves an early terminal run cannot suppress the other provider.
- `TestCICDServiceClaimGroupBurstAndLateArrivalLive`,
  `TestCICDServiceConcurrentCoordinatorsShareClaimGroupLive`,
  `TestCICDServiceClaimGroupPartialACKCrashRecoveryLive`, and
  `TestCICDServiceGroupRecoversPartiallyAdmittedScopesLive` cover production
  Service handoff against PostgreSQL: a selected burst, a late group, parallel
  coordinators, partial ACK crash recovery, and a blocked second scope after
  an earlier scope turns terminal.
- Before adding the pending repository index, a 100,000-row local PostgreSQL
  fixture scanned 637 buffers for a rare target. The identical predicate with
  the partial `(repository)` index used an index-only scan and 3 buffers.
  Applying migration 236 to an isolated 100,000-row schema and explaining the
  production lookup also used `cicd_actions_deliveries_pending_repo_idx` with
  3 buffers. These
  are query-shape observations, not an end-to-end throughput claim.

## Observability Evidence

The coordinator records `eshu_dp_ci_cd_run_webhook_reaps_total` by `requeued`
or `exhausted` for successful reap calls and emits an aggregate warning for
exhaustion. It records `eshu_dp_ci_cd_run_webhook_handoffs_total` by bounded
outcome for each delivery, even when several deliveries share one work item,
and `eshu_dp_ci_cd_run_webhook_lag_seconds` from receipt to verified ACK. The
queue status, failure class, and retry attempts remain per delivery. Newly inserted scheduled GitHub work increments
`eshu_dp_ci_cd_run_poll_fallback_total`; GitLab admissions do not.

Focused proof: `go test ./internal/storage/postgres/webhook/actions ./internal/storage/postgres -run 'Test(ActionsDelivery|ActionsClaim|PendingRepository|CICDActionsAdmission|CICDMixedProvider)' -count=1` with local fixture DSN variables; exit 0.

Performance Evidence: the same 100-delivery fixture at a 100-row claim limit
and one-minute lease left 1 delivery handed off and 99 pending after the first
pre-group Service tick (the open-target guard held the rest). With persisted
groups, the production Service handed off all 100 for one admitted work item.
The locally served GitHub App fixture then observed one token mint, 22 provider
GETs (21 Actions and one empty deployment response), 10 `ci.run` facts, and
11 total facts for that one full snapshot. A 101st delivery inserted after
selection stayed in a fresh group while the first work was open, then admitted
one later snapshot after that work turned terminal. In one two-coordinator
sample, the disjoint claims admitted one work item; scheduling can make two
selected groups, so this does not establish a universal one-work burst bound.
These are event and work counts, not a wall-time speedup claim.

Observability Evidence: per-delivery inbox status, failure class, attempt
count, webhook handoff outcome and receipt-to-ACK lag show blocking and retry
progress. The existing provider request counter and emitted-fact counter show
whether a selected group causes a bounded collector snapshot or repeated work.
