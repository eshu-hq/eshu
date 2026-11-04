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

- `Derivation`, `GoModulesSection`, `DerivationFromConfig` decode the config block.
- `PlanRequest`, `Plan`, `RunID` plan one page and name its run.
- `SkipInvalidRepositoryID`, `SkipDuplicateRepository`, `SkipUnsupportedRemote`,
  `SkipMissingCommit` are the closed skip reasons.

## Dependencies

`collector/scannercomponent`, `coordinator/planner/contract`, `coordinator/schedule`,
`facts`, `scope`, and `workflow`.

## Telemetry

None of its own. The root (`activation_go_modules.go`) records these from `Plan`'s
results, and none holds a repository id or URL:

- `eshu_dp_workflow_coordinator_activation_derived_scopes_planned_total` with
  `collector_kind="scanner_worker"` and `ecosystem="go"`.
- One INFO `workflow coordinator planned derived scopes for component activation`
  line per planned page: `candidates`, `total_candidates`, `slot`, `slot_count`,
  and one `skipped_<reason>` count per reason.
- One ERROR `workflow coordinator refused derived scopes for component activation`
  line for a mislabeled host. Egress denial uses the existing skip log and audit
  event.
- The page read reports on
  `eshu_dp_postgres_query_duration_seconds{store="go_module_repositories"}`.

## Gotchas / invariants

- The commit is part of work-item identity, so a new commit is a new generation of
  the same scope. The open-target guard still holds it back while one is open.
- The repository id and scope id are the only repository values in any identity.
  The clone URL lives only in `requested_scope_set`.
- One unplannable repository skips with a reason. It never fails the page, or one
  bad row would stall that page's whole slot of the rotation.

## Related docs

- `docs/public/reference/standalone-vulnerability-scanner-boundary.md`
- `go/internal/coordinator/README.md`
