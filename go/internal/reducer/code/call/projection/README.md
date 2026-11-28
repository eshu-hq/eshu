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
| `ReducerGraphDrain` | the optional local-authoritative gate that checks reducer graph work and canonical-code quiescence |
| `CanonicalCodeQuiescenceChecker` | the backend/profile-independent gate wired when `ReducerGraphDrain` is disabled |
| `CanonicalCodeQuiescenceDescriber` | the optional port that names the scopes holding that gate; the runner calls it only when a blocked episode starts and at most once a minute after that |
| `BlockedReasonCanonicalCodeQuiescence` / `BlockedReasonReducerGraphWork` | the closed `reason` label values on `eshu_dp_shared_projection_lane_blocked_total` |
| `IntentReader` / `PartitionIntentReader` / `PartitionCandidateReader` / `UnhashedCandidateReader` | the intent-listing ports, from broad domain scans down to partition-hashed candidate reads |
| `HistoryLookup` / `CurrentRunHistoryLookup` / `CurrentRunPartitionHistoryLookup` / `CurrentRunRefreshHistoryLookup` | optional completion-history ports that let a durable store skip a proven no-op retract |
| `RefreshFenceLookup` | the optional bounded refresh-fence check a durable store can implement instead of loading the whole acceptance unit |
| `ShortfallStamper` | the optional durable first-shortfall anchor a store implements so the shortfall park can expire (#10947) |
| `DefaultLeaseOwnerPrefix` / `DefaultAcceptanceScanLimit` / `FilePartitionKeyPrefix` | the runner's defaults and the file-scoped partition-key prefix external stores match on |

The reducer root wires `Runner` on `Service.CodeCallProjectionRunner`,
naming `projection.Runner`/`projection.RunnerConfig`/
`projection.DefaultLeaseOwnerPrefix`/`projection.DefaultAcceptanceScanLimit`/
`projection.FilePartitionKeyPrefix`/`projection.PartitionCandidateReader`/
`projection.UnhashedCandidateReader`/`projection.RefreshFenceLookup`
directly (#6626 value-flow lane retired the code-call stanza of
`compat_projection.go`).

## Blocked-lane visibility

When a lane-wide gate holds the lane shut, every blocked cycle adds once
to `eshu_dp_shared_projection_lane_blocked_total{domain="code_calls",reason}`
(#10871: the gate is evaluated once per cycle and shared across partitions,
so the counter is per-cycle, not per-partition).
blocked.go rate-limits the rest per episode: a WARN `code call projection lane
blocked` line with `blocked_reason`, `blocked_seconds`, `blocking_scope_count`
and up to 10 `blocking_scope_ids`, plus the
`eshu_dp_shared_projection_lane_blocking_scopes` gauge, both refreshed at most
once a minute. The first open cycle after an episode logs `code call projection
lane released` and zeroes the gauge. A switch from one blocked reason to
another closes the replaced episode the same way: it zeroes that reason's gauge
and logs the same `lane released` line with the old `blocked_reason`, its
`blocked_seconds`, and `replaced_by` naming the new reason, then starts the new
episode at zero. The blocker sample is taken from the dependency the gate
consults (`ReducerGraphDrain` when wired, otherwise `CanonicalQuiescence`),
never from the other one. Before #7133 a blocked cycle emitted
nothing, and the lane sat shut in the QA environment for eight days without a signal.

## Dependencies

`internal/reducer/code/call` (`codecall`: evidence sources, partition-key
version), `internal/reducer/contract` (`Domain*`, `IsRetryable`),
`internal/reducer/gpphase` (`Phase*`, `Keyspace*`, `ReadinessLookup`/
`ReadinessPrefetch`), `internal/reducer/intents/shared/worker` (the shared
partition-processing/selection/telemetry machinery this runner drives),
`internal/reducer/sharedintent` (`Row`, `AcceptanceKey`, `EdgeWriter`,
`PartitionLeaseManager`, `AcceptedGenerationLookup`/`Prefetch`,
`PartitionForKey`, `UniqueRepositoryIDs`), `internal/reducer/crossscope`
(`ProducerReadinessMaxWait`, the shortfall park's elapsed bound), and
`internal/telemetry` (`Instruments`). No dependency on the reducer root.

## Telemetry

`Instruments` (when wired) records queue-claim duration, canonical-write
duration/count, and shared-projection intent-wait/processing durations, each
tagged with the `code_calls` domain. `Logger` (when wired) emits "code call
projection cycle completed"/"code call projection cycle failed" structured
logs with the full phase-duration breakdown
(`selection_*_duration_seconds`, `lease_claim_duration_seconds`,
`retract_duration_seconds`, `write_duration_seconds`,
`mark_completed_duration_seconds`) and a lease-heartbeat failure log.

## Shortfall park bound

An enforced CALLS write that matches fewer rows than submitted parks the
batch retryably under the shortfall failure class instead of completing it
(#10938). The park carries a 30-minute elapsed bound
(`crossscope.ProducerReadinessMaxWait`, the same named constant every other
elapsed wait uses) anchored on a durable per-intent stamp:
`shared_projection_intents.first_code_call_shortfall_at` (migration 314),
written COALESCE-once so the first park wins. Each parked cycle takes the
oldest stamp in the batch; a batch with no anchor yet keeps parking (NULL
defers), as does a reader without the `ShortfallStamper` port. Past the
bound the runner dead-letters one
`sharedintent.UnroutableRow` per active intent (bounded reason
`code_call_shortfall_wait_exceeded`) through `worker.UnroutableWriter`
BEFORE `MarkIntentsCompleted`, then completes the batch — a deleted endpoint
in a never-superseded generation terminates here instead of starving its
partition forever. No new probe runs: the bound is checked at the
`writeActiveRows` shortfall error return, and the once-per-cycle lane pin
stays as-is.

Three telemetry outcomes, one per terminal state: a fresh park keeps the
writer's WARN plus `eshu_dp_shared_edge_target_miss_total`, and the visit
result still carries the terminal submitted/matched counts onto
`eshu_dp_code_call_edge_submitted/matched_rows_total`; an expiry adds the
dead-letter count to `eshu_dp_shared_edge_unroutable_rows_total` under the
bounded reason plus a `eshu_dp_shared_projection_stale_intents_total` entry
under the same reason, with a WARN naming the dead-lettered count and the
elapsed wait.

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
