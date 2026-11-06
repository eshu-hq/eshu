# Coordinator reachability planning

## Purpose

`reachability` plans Go reachability scanner work. It decodes the
`derive_from_go_modules` section of an activation's config block and turns one
rotation page of repositories into one run and one work item per repository.

## Ownership boundary

This package owns the derive decode, skip reasons, run id, work-item identity, and
the `requested_scope_set` document. It opens no connection and never imports the
coordinator root. The root owns the egress decision, clock, page read, admission,
and telemetry. `go/internal/storage/postgres/gomodules` owns the read.
`scannercomponent` owns the value types the three share.

## Exported surface

- `Derivation`, `GoModulesSection`, `DerivationFromConfig`, `DefaultScanInterval`
  decode the config block and its cadence.
- `PlanRequest`, `Plan`, `RunID` plan one page and name its run.
- `SkipInvalidRepositoryID`, `SkipDuplicateRepository`, `SkipUnsupportedRemote`,
  `SkipUnsupportedRemoteWithEvidence`, `SkipMissingCommit` are the closed skip
  reasons.

## Dependencies

`collector/scannercomponent`, `coordinator/planner/contract`, `coordinator/schedule`,
`facts`, `scope`, and `workflow`.

## Telemetry

None of its own. The root (`activation_go_modules.go`) records these from `Plan`'s
results, and none holds a repository id or URL:

- `eshu_dp_workflow_coordinator_activation_derived_scopes_planned_total` with
  `collector_kind="scanner_worker"` and `ecosystem="go"`.
- One INFO `workflow coordinator planned derived scopes for component activation`
  line per page read, which repeats each tick while a page has no run: `candidates`, `total_candidates`, `slot`, `slot_count`,
  `evidence_only_repositories` (planned only to retract Go evidence after losing a
  `go.mod`), and one `skipped_<reason>` count per reason.
- One ERROR `workflow coordinator refused derived scopes for component activation`
  line for a mislabeled host. Egress denial uses the existing skip log and audit
  event.
- One WARN per plan bucket when the rotation period is longer than half the generation
  retention soft cutoff, `reason=go_rotation_exceeds_half_retention_cutoff`. The root emits
  it because the store and the cutoff live there.
- The page read and the fence report on
  `eshu_dp_postgres_query_duration_seconds{store="go_module_repositories"}`.
- `eshu_dp_workflow_coordinator_activation_derived_scopes_fenced_total` with
  `collector_kind`, `ecosystem`, and `reason="stale_evidence"`, for generations actually
  retracted. The planned line adds `fenced_stale_evidence`, `fence_holders_reopened`,
  `fence_candidates_skipped_no_commit`, and `fence_history_pruned`.

## Gotchas / invariants

- The commit is part of work-item identity, so a new commit is a new generation of
  the same scope. The open-target guard holds it back only while the earlier item's
  run is not yet `complete` or `failed`. This is not handled: one terminal item sets
  the whole run `failed` while its siblings stay claimable, and the guard stops counting
  them as open, so a later plan can add a second item for such a scope
  (eshu-hq/eshu#10091). The activation stays at replicas 0 until the commit-skew fence
  (eshu-hq/eshu#10055), eshu-hq/eshu#10054, eshu-hq/eshu#10077 and #10091 merge.
- The repository id and scope id are the only repository values in any identity.
  The clone URL lives only in `requested_scope_set`.
- One unplannable repository skips with a reason. It never fails the page, or one
  bad row would stall that page's whole slot of the rotation.

- The cadence belongs to the derive section. An activation has no top-level
  `scan_interval`, so the instance's interval is the reconcile tick, and bucketing
  on it would rescan repositories almost continuously.

- `skipped_unsupported_remote_with_evidence` is an invariant alarm, not a lifecycle
  state. A stored remote keeps one form for a scope's life, so a non-zero value means
  identity normalization or the remote stamp changed. `ValidCloneURL` checks syntax
  only: a dead or revoked remote is planned and claimed, its claim fails, and the
  evidence of its last commit stays active until the fence in eshu-hq/eshu#10055.
- `StaleEvidence` retracts Go evidence when the repository left the commit it measured more
  than two rotation periods ago. One repository is visited once per `slot_count` x
  `scan_interval`, so a bound in intervals would retract healthy repositories on a large
  fleet. The age is measured from the repository's own history, so a quiet repository is
  never retracted by age.
- A retraction changes which facts are active, not any finding. The store reopens each holder in
  the same transaction, which changes no verdict until eshu-hq/eshu#10095 lets passes read Go
  evidence across scopes. See
  `docs/public/reference/standalone-vulnerability-scanner-boundary.md`.

## Related docs

- `docs/public/reference/standalone-vulnerability-scanner-boundary.md`
- `go/internal/coordinator/README.md`
