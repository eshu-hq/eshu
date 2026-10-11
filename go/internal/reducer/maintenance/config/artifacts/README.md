# Config Artifact Walker (skeleton)

## Purpose

Defines the shape of the reducer walker that repairs and backfills the
config-artifact read model (#7126). The walker finds repositories whose
stored `config_paths` rows are missing or stale, then walks every repository
from a persisted cursor, and derives each one with the same protocol as a
content Write. See the decision record
`docs/internal/design/7126-ingest-time-config-artifacts.md` and its protocol
companion, sections 3 and 4.

PR3b-1a ships the skeleton only. No code here runs a derive, and
`cmd/reducer` does not wire it yet.

## Ownership boundary

Owns the walker's configuration, its ports, and the in-process work split.
Does not own the tables, the walk cursor SQL, the dirty join, or the derive
itself: those live in `internal/storage/postgres/config/artifacts`, and
`cmd/reducer` will adapt them to the ports here.

## Exported surface

- `Config` and its defaults: `DefaultWorkers` (K = 4),
  `DefaultSkipAfterFailures` (5), `DefaultPageBudget`, `DefaultPollInterval`
- `Claimer` — claims the next page of the shared walk
- `Deriver` — runs the derive protocol for one repository (PR3b-2)
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

None yet. The walker emits nothing until PR3b-2 wires it. The record names
`eshu_dp_config_artifact_derives_total{trigger,outcome}`,
`eshu_dp_config_artifact_reconcile_total{outcome}` and
`eshu_dp_config_artifact_stale_repos{reason}` for that PR.

## Related docs

- `docs/internal/design/7126-ingest-time-config-artifacts.md`
- `docs/internal/design/7126-ingest-time-config-artifacts-protocol.md`
- `docs/public/observability/telemetry-coverage.md`
