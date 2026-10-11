# Retention Relock Re-checks Uncovered Coverage (#7903)

The #7472 fix excluded uncovered writers at selection, but the targeted
re-lock re-checked status, superseded_at, active id, live work, and rank —
not coverage. Coverage is horizon-relative, so a full activating between
select and relock flips a covered candidate to uncovered, and the relock
still locked and pruned it: the exact #7472 harm through a narrow window.

The targeted query now carries the probe-mirrored uncovered definition over
a scope-bounded (`$4`) `last_activated_full_write` horizon. The locked leg
excludes flipped members without locking them; a lock-free `UNION ALL` leg
reports them and the store counts them as `Skipped["uncovered_writer"]`, so
they surface in the cycle log/counter instead of vanishing. One statement
classifies and locks under one snapshot: a member is either locked
(covered) or reported (uncovered), never both. Members missed for the old
reasons (taken, re-activated, work started) stay silent, as
`TestGenerationRetentionRelockDropsMissedMembersAndPrunesTheRest` pins.

Residual, accepted: a full committing after the re-lock statement's
snapshot still flips a locked member before the prune commits. The horizon
depends on other rows, so no lock-time check sees a concurrent activation
commit; closing it would serialize activation against retention, which the
repo forbids. The re-check narrows the window from select-to-commit to
relock-statement-to-commit. No worker, batch, lease, or retry knob changes.

`relockRetentionSelection` moved code-identically (plus the skip partition)
into `generation_retention_relock.go` because the re-check pushed
`generation_retention_events.go` over the 500-line cap; the dirgate ledger
re-pins 338 to 339 with its regenerated mirror.

No-Regression Evidence (#7903): baseline is the base-revision targeted
query (HEAD `ba95c9a97`, exact shipped text) planned on a seeded corpus (1
scope, 1 activated full, 50 writer candidates; params: soft cutoff now-7d,
min-newer 0, batch limit 100, hard ceiling now-90d) on local PostgreSQL 18
(postgres:18 Docker): LockRows over all 50 rows, 1.200 ms execution, 231
shared hits. After is the new query on the identical corpus and params:
Append over 1 locked plus 49 reported rows, 0.219 ms execution, 28 shared
hits. The corpus is uncovered-heavy (49 of 50 writers postdate the full),
so the wall-time delta is not a speedup claim; the structural delta is one
scope-bounded horizon scan+sort plus the report leg, and the correlated
rank subquery now runs only for covered survivors instead of every
provisional member. Lock ordering is unchanged (Sort on
`(scope_id, generation_id)` below LockRows, still pinned by
`TestGenerationRetentionTargetedLockPlanShapeLive`), `fact_work_items` is
still read once, and every generation re-check is still a primary-key
probe. No new index: the horizon's `(scope_id, ingested_at DESC,
generation_id DESC)` access matches
`scope_generations_scope_latest_lookup_idx`, and #7472 already accepted the
unscoped variant of this CTE without an index. The regression test pins
terminal counts on a 4-row flip fixture: flipped candidate retained and
skipped (`Skipped["uncovered_writer"]=1`), bystander pruned,
`GenerationsPruned=1`. Full retention family green
(`go test ./internal/storage/postgres -run TestGenerationRetention`,
97.976s), including the EPQ raced-member drops and the #7472 keeps.

Observability Evidence (#7903): no new instrument. Relock-time flips reuse
the existing `Skipped["uncovered_writer"]` tally, which the
`generation retention cycle completed` log carries in `skipped_by_reason`
and `eshu_dp_generation_retention_skipped_total{reason="uncovered_writer"}`
counts; the metrics reference now names the re-lock as the second
reporting site in the same PR. Post-deploy, an `uncovered_writer` skip on a
pass whose selection reported none means a full activated mid-pass; the
flipped generation retries on the next pass once a newer full covers it, or
stays held while the probe still needs it.
