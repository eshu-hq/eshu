# internal/reducer/code/call/projection

## Purpose

Runs the controlled code-call projection lane: drains code-call
shared-projection intents one repo/run at a time, claiming a partition
lease, selecting one authoritative acceptance unit's pending rows,
retracting stale edges when needed, and writing the active rows through the
shared edge writer (issue #6061).

`Runner.Run` drives partitions concurrently or sequentially depending on
`RunnerConfig.Workers`/`PartitionCount`, backing off on empty or
readiness-blocked cycles. Selection resolves one accepted, ready, unfenced
acceptance unit through the generic worker's shared readiness/acceptance
machinery (`intents/shared/worker`); the refresh fence
(`codeCallProjectionRowBlockedByRepoFence`) prevents a file-scoped write from
racing a covering whole-repo or file-refresh row in the same batch.

## Ownership boundary

**Owns:** the code-call projection cycle (`Runner`), its partition-key
classification (legacy/whole-repo/file-scoped), its retract/write row
building, and its lease-heartbeat loop.

**Does not own:** the shared-projection worker/runner machinery this
package's selection and readiness logic calls into
(`intents/shared/worker`), the code-call extraction/evidence-source
vocabulary (`codecall`), or the graph-projection phase/readiness vocabulary
(`gpphase`).

## Exported surface

| symbol | what it is |
|---|---|
| `Runner` / `RunnerConfig` | the projection cycle and its tunables |
| `ReducerGraphDrain` | the optional gate that pauses local single-backend runners while reducer graph-writing domains are active |
| `IntentReader` / `PartitionIntentReader` / `PartitionCandidateReader` / `UnhashedCandidateReader` | the intent-listing ports, from broad domain scans down to partition-hashed candidate reads |
| `HistoryLookup` / `CurrentRunHistoryLookup` / `CurrentRunPartitionHistoryLookup` / `CurrentRunRefreshHistoryLookup` | optional completion-history ports that let a durable store skip a proven no-op retract |
| `RefreshFenceLookup` | the optional bounded refresh-fence check a durable store can implement instead of loading the whole acceptance unit |
| `DefaultLeaseOwnerPrefix` / `DefaultAcceptanceScanLimit` / `FilePartitionKeyPrefix` | the runner's defaults and the file-scoped partition-key prefix external stores match on |

The reducer root wires `Runner` on `Service.CodeCallProjectionRunner`,
keeping the `reducer.CodeCallProjectionRunner`/
`reducer.CodeCallProjectionRunnerConfig`/`reducer.ReducerGraphDrain`/
`reducer.DefaultCodeCallProjectionLeaseOwnerPrefix`/
`reducer.DefaultCodeCallAcceptanceScanLimit`/
`reducer.CodeCallProjectionFilePartitionKeyPrefix`/
`reducer.CodeCallProjectionPartitionCandidateReader`/
`reducer.CodeCallProjectionUnhashedCandidateReader`/
`reducer.CodeCallProjectionRefreshFenceLookup` spellings through the
code-call stanza of `compat_projection.go`, since cmd/reducer's wiring and
internal/storage/postgres' compile-time interface assertions and
partition-key-prefix helper all still name them that way.

## Dependencies

`internal/reducer/code/call` (`codecall`: evidence sources, partition-key
version), `internal/reducer/contract` (`Domain*`, `IsRetryable`),
`internal/reducer/gpphase` (`Phase*`, `Keyspace*`, `ReadinessLookup`/
`ReadinessPrefetch`), `internal/reducer/intents/shared/worker` (the shared
partition-processing/selection/telemetry machinery this runner drives),
`internal/reducer/sharedintent` (`Row`, `AcceptanceKey`, `EdgeWriter`,
`PartitionLeaseManager`, `AcceptedGenerationLookup`/`Prefetch`,
`PartitionForKey`, `UniqueRepositoryIDs`), and `internal/telemetry`
(`Instruments`). No dependency on the reducer root.

## Telemetry

`Instruments` (when wired) records queue-claim duration, canonical-write
duration/count, and shared-projection intent-wait/processing durations, each
tagged with the `code_calls` domain. `Logger` (when wired) emits "code call
projection cycle completed"/"code call projection cycle failed" structured
logs with the full phase-duration breakdown
(`selection_*_duration_seconds`, `lease_claim_duration_seconds`,
`retract_duration_seconds`, `write_duration_seconds`,
`mark_completed_duration_seconds`) and a lease-heartbeat failure log.

## Gotchas / invariants

- **`processPartitionOnce` always releases the claimed lease with the
  caller's context, not the derived lease-heartbeat context.** Stopping the
  heartbeat cancels the derived context before the deferred release runs;
  releasing with a canceled context would silently fail the release.
- **The refresh fence MUST be checked before a file-scoped row is written**,
  so a per-file write never races a covering whole-repo or wider-file-set
  refresh landing in the same partition batch.
- **`FilePartitionKeyPrefix()` and the legacy/whole/file partition-kind
  classification are a durable wire contract** — `internal/storage/postgres`
  matches on this exact prefix; changing it is a data-shape change, not a
  refactor.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/code/call/README.md` — the `codecall` sibling this package reads evidence sources and partition-key versioning from
- `go/internal/reducer/intents/shared/worker/README.md` — the shared partition-processing machinery this runner drives
- `go/internal/reducer/code/README.md` — the `code/` namespace parent
- `docs/internal/design/reducer-target-tree.md` — the #6061 restructure

No-Regression Evidence: #6061 moves the code-call projection runner out of
the reducer root into this new package, without changing any field,
exported behavior, wire string, or call order. `CodeCallProjectionRunner`
and its supporting reader/lookup interfaces dropped the
`CodeCall(Projection)` stutter per `docs/internal/naming.md`
(`CodeCallProjectionRunner` -> `Runner`, `CodeCallProjectionRunnerConfig` ->
`RunnerConfig`, and so on for the interface types); the reducer root keeps
every spelling with an external caller through the code-call stanza of
`compat_projection.go`, so no external caller needed a source change.
Measured from `go/`, with `GOROOT` unset: `go build ./...`, `go vet
./internal/reducer/... ./cmd/reducer/...`, and `go test
./internal/reducer/... ./cmd/reducer/... ./internal/storage/postgres/...
./internal/replay/... -count=1` all exited 0 on this branch. `git diff
--check` exited 0.

No-Observability-Change: this move adds no queue domain, worker, lease,
graph, or Postgres operation, runtime setting, metric instrument, metric
label, span, or status surface. The structured-log messages and metrics
listed under Telemetry above are unchanged; only the package that owns the
code moved.
