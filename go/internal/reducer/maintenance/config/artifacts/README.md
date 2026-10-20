# Config Artifact Walker

## Purpose

Runs the reducer walker that repairs and backfills the config-artifact read
model (#7126, #8155). Each cycle derives the dirty join's repositories with
trigger `repair`, then derives one claimed walk page with trigger `backfill`,
over K workers that partition by `repo_id`. When a claimed page reaches the
end of the walk and nothing is dirty anymore, the cycle records the backfill
marker: the first full walk is done. See the decision record
`docs/internal/design/7126-ingest-time-config-artifacts.md` and its protocol
companion, sections 3 and 4.

`cmd/reducer` wires the `Runner` into the reducer service; the walker's
environment variables (`ESHU_CONFIG_ARTIFACT_WALKER_ENABLED`,
`ESHU_CONFIG_ARTIFACT_WALKER_WORKERS`) are read once at reducer start.

## Ownership boundary

Owns the walker's configuration, its ports, and the in-process work split.
Does not own the tables, the walk cursor SQL, the dirty join, or the derive
itself: those live in `internal/storage/postgres/config/artifacts`, and
`cmd/reducer` will adapt them to the ports here.

## Exported surface

- `Config` and its defaults: `DefaultWorkers` (K = 4),
  `DefaultSkipAfterFailures` (5), `DefaultPageBudget`, `DefaultPollInterval`
- `Claimer` — claims the next page of the shared walk
- `Deriver` — runs the derive protocol for one repository and returns its
  outcome label
- `Lister`, `DirtyRepo` — the dirty join's work items (reason, skipped flag)
- `Marker` — reads and records the backfill completion marker
- `Runner`, `Batch` — the cycle loop; what one bounded cycle did
- `TriggerRepair`, `TriggerBackfill` — the derive trigger labels, pinned
  equal to the storage constants by the `cmd/reducer` parity test
- `DeriveOutcomeOK`, `DeriveOutcomeSuperseded`,
  `DeriveOutcomeEncodingUnsupported`, `DeriveOutcomeFenceMissing` — the
  derive outcomes the reconcile mapping branches on, pinned equal to the
  storage constants by the same parity test; any other outcome maps to
  reconcile `error`
- `StaleReasons` — the closed dirty-join reason set the stale gauge covers
- `Assign` — splits a page over K workers by a stable FNV-1a hash of
  `repo_id`; a repeated id is assigned once

## Concurrency model

- In one process, `Assign` gives each repository to exactly one worker, so
  two workers never derive one repository at the same time.
- Across replicas, `ClaimPage` serializes claims on the cursor row lock, so
  replicas take disjoint pages. The claim is not a lease. A repository can
  still be derived by two processes (a wrap, or a content Write's own
  derive). The derive's repository advisory lock and advance-only write keep
  that correct; the claim only spreads the work.

## Dependencies

Standard library only. Never `internal/reducer`.

## Telemetry

Each derived repository emits one `config_artifacts.derive` span and one
`eshu_dp_config_artifact_derives_total{trigger,outcome}` point plus a
`eshu_dp_config_artifact_derive_duration_seconds{trigger}` observation, with
trigger `repair` (dirty join) or `backfill` (cursor walk), and one
`eshu_dp_config_artifact_reconcile_total{outcome}` point (`repaired`,
`match`, `superseded`, `error`, `skipped`). Each cycle sets
`eshu_dp_config_artifact_stale_repos{reason}` for every dirty-join reason,
including zero. A derive the shutdown cancels records nothing. Cycle failures
log `config_artifacts.walker.cycle_failed` at ERROR; the backfill marker logs
at INFO; routine cycles log at DEBUG.

## Related docs

- `docs/internal/design/7126-ingest-time-config-artifacts.md`
- `docs/internal/design/7126-ingest-time-config-artifacts-protocol.md`
- `docs/public/observability/telemetry-coverage.md`
