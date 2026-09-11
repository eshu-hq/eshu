# internal/reducer/intents/phase/repair

## Purpose

Drains the exact graph-projection-phase repair queue and republishes missing
readiness rows when their bounded generation is still authoritative (issue
#6061). Phase publications and the graph writes they follow are not atomic;
when a write commits but its readiness publication fails, the row lands here
for retry instead of leaving the graph write's readiness signal permanently
missing.

## Ownership boundary

**Owns:** the repair queue drain loop (`Repairer.Run`/`RunOnce`), its
polling/backoff config (`Config`), and the `StateLookup` port it checks
before replaying a row.

**Does not own:** the repair SHAPE (`PhaseRepair`, `PhaseRepairQueue`,
`PhaseRepairsFromStates`) or the publish-with-repair enqueue path
(`PublishIntentGraphPhaseWithRepair`, `PublishPhaseStatesWithRepair`) — those
are plain data and pure builders that live in `internal/reducer/gpphase`
instead, alongside the rest of that package's readiness vocabulary.

## Exported surface

| symbol | what it is |
|---|---|
| `Repairer` | the repair-queue drain loop |
| `Config` | polling interval, batch limit, retry delay |
| `StateLookup` | resolves whether one exact readiness phase is already published |

The reducer root keeps `GraphProjectionPhaseRepairer` (aliased to `Repairer`)
and `GraphProjectionPhaseRepairerConfig` (aliased to `Config`) so
`cmd/reducer`'s construction (`main_helpers.go`, `config_projection.go`) and
`internal/reducer/service.go`'s field keep their existing spelling.

## Dependencies

`internal/reducer/gpphase` (the repair shape and phase/keyspace vocabulary),
`internal/reducer/sharedintent` (the accepted-generation lookup and
acceptance key), `internal/telemetry`, `go.opentelemetry.io/otel/metric`, and
the standard library.

**This package must never import `internal/reducer`**, directly or
transitively.

## Telemetry

Registers no instruments of its own; records through the caller-supplied
`*telemetry.Instruments.CanonicalWrites` counter and structured logs
(`graph projection readiness repair cycle failed/completed`, `graph
projection readiness repair publish failed`, `graph projection readiness
repaired`), unchanged by this move since the instrument and log lines
followed their call sites.

## Gotchas / invariants

**A repair row whose generation is no longer accepted is deleted, not
replayed.** `repairNeedsAcceptedGeneration` exempts
`PhaseWorkloadMaterialization` rows keyed under `KeyspaceServiceUID`: those
rows deliberately use `generation_id` as `source_run_id` so code-stage
symbol-runtime rows can find them across the workload/code source-run
boundary, and may have no matching shared-projection-acceptance row even
though the generation-scoped phase key is still safe to replay.

**One-way import rule.** This package must never import `internal/reducer`.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/gpphase/README.md` — the repair shape and phase/keyspace vocabulary this package drains
- `docs/internal/design/package-restructure.md` — the #6061 restructure
