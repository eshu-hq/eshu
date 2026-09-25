# internal/reducer/intents/shared/worker

## Purpose

The shared-projection substrate: the generic partition worker, runner,
lease heartbeat, batch selection, readiness/presence gating, and
repo-refresh-fence machinery every generic shared-projection domain
(code_calls, repo_dependency, handles_route, runs_in, rationale,
inheritance, sql_relationships, shell_exec, documentation, codeowners,
submodule pins, workload_dependency, deployable_unit_edges) is drained
through (issue #6061).

It exists so this machinery can live outside the reducer root without
importing it. The root imports the families that need this substrate, so
this package staying in the root package was the single largest blocker to
splitting those families into subpackages: 23-plus domains and the dedicated
projection runners (code-call and repo-dependency) depend on it.

## Ownership boundary

**Owns:** `ProcessPartitionOnce` (claim lease, select batch, retract/write
edges, mark completed, release lease), `SelectPartitionBatch`,
`Runner` (the long-lived polling loop across domains and
partitions), the lease heartbeat, the indexed/legacy partition candidate
readers, batch dedup and authoritative-generation filtering, readiness and
property-keyed presence gating (`FilterRowsByReadiness`,
`filterRowsByTargetPresence`), the repo-wide-retract fence
(`PlanRepoWideRetractWork` and its helpers), the runner's env-var
configuration, and its telemetry.

**Does not own:** intent row shape and identity (`sharedintent`), readiness
phase/keyspace vocabulary and presence-key derivations (`gpphase`), the
Domain catalog (`contract`), or the dedicated projection runners that
drive it: `code/call/projection`, and the repo-dependency runner, which
still lives at the reducer root.

## Exported surface

See [doc.go](doc.go) for the full list. The headline entry points are
`ProcessPartitionOnce`, `SelectPartitionBatch`, `Runner`, and
`LoadConfig`. This package's own exported names drop the Shared/SharedProjection
prefix that stuttered against its own `intents/shared/worker` path (issue
#6061's naming pass): `Runner`, `RunnerConfig`, `LoadConfig`, `IntentReader`,
`PartitionCandidateReader`, `UnhashedCandidateReader`,
`SupersededGenerationReader`, `RefreshFenceLookup`,
`ReadinessPhase`, `Domains`, `AcceptanceTelemetry`, `AcceptanceLookupEvent`,
`RecordStepDurations`, `MaxIntentWaitSeconds`, `DefaultPollInterval`,
`DefaultLeaseOwnerPrefix`, `ReadinessKeyspace`, and
`GraphProjectionPhaseKeyForRow`. `LatestIntentsByRepoAndPartition` and
`FilterAuthoritativeIntents` do not stutter and keep their names. The
reducer root keeps a type alias or thin forwarder under its ORIGINAL
(pre-#6061) spelling only for the names that still have a caller outside
this package's own tests — e.g. `SharedProjectionRunner` aliases `Runner`,
`LoadSharedProjectionConfig` forwards to `LoadConfig`. The H5 root-remnant
fold (issue #6061 decision D12) deleted the root forwarders for
`SelectPartitionBatch`, `FilterRowsByReadiness`, `ReadinessPhase`,
`ReadinessKeyspace`, `RowUsesRefreshFence`, `PlanRepoWideRetractWork`, and
`GraphProjectionPhaseKeyForRow`: their only callers were the reducer
root's own tests and one production file
(`repo_dependency_projection_concurrency_proof.go`), so those now name
this package directly instead of going through a forwarder.

## Dependencies

`internal/reducer/sharedintent`, `internal/reducer/gpphase`,
`internal/reducer/contract`, `internal/reducer/payloadcore`,
`internal/reducer/inheritance` (one evidence-source constant),
`internal/cpubudget`, `internal/telemetry`, `go.opentelemetry.io/otel/{trace,metric}`,
and the standard library. **This package must never import
`internal/reducer`**, directly or transitively — that would recreate the
cycle this package exists to break.

## Telemetry

Registers no instruments of its own; it records through the
`*telemetry.Instruments` a caller supplies (`SharedProjectionIntentWaitDuration`,
`SharedProjectionProcessingDuration`, `SharedProjectionCycles`,
`CanonicalWriteDuration`, `SharedProjectionPartitionProcessingDuration`,
`SharedProjectionIntentsCompleted`, `SharedProjectionStepDuration`,
`SharedProjectionPartitionHeartbeatMissed`, `SharedAcceptanceLookupDuration`,
`SharedAcceptanceLookupErrors`, `SharedProjectionStaleIntents`), unchanged by
this move since the instruments followed their call sites.
`SharedProjectionStaleIntents` carries a closed `reason` attribute:
`acceptance_mismatch` or `generation_superseded` (#7121).

## Gotchas / invariants

**One-way import rule.** This package and `internal/reducer/intents/phase/repair`
must never import `internal/reducer`. `go list` should show no edge back to
the root; a `go list -f '{{join .Imports " "}}'` grep for the root import
path is the check.

**The heartbeat/release ordering is load-bearing.** `ProcessPartitionOnce`'s
`stopHeartbeat()` must run before `ReleasePartitionLease`, and the release
call uses the pre-heartbeat context, not the heartbeat-derived one
`stopHeartbeat` cancels — releasing through the cancelled context silently
fails and leaves the lease held until its own TTL. See the inline comment on
`ProcessPartitionOnce` before reordering this.

**Readiness-blocked intents of a superseded generation drain (#7121).** When
the `IntentReader` also implements `SupersededGenerationReader`,
`SelectPartitionBatch` runs the acceptance filter, dedupe, and readiness gate
first, then makes one bounded lookup over the distinct generation ids of the
BLOCKED rows only (skipped when nothing is blocked) and moves the blocked rows
whose scope generation is `superseded` into `StaleIDs`, out of `BlockedRows`.
Such a generation was superseded before workload materialization ran, so its
`workload_materialization` phase row is never published and the gate would
block those rows forever. Ready rows (phase row published) and terminal rows
on a superseded generation are NOT drained and still project: a delta
successor (`scope_generations.is_delta`) carries only changed-file facts and a
file-scoped retract, so it never re-emits an untouched file's edge, and
draining a ready row would lose that edge permanently. Drained rows count as
progress, so a window full of orphans returns a batch instead of widening the
scan toward the cap. The predicate is the terminal
`superseded` status, not "not the active generation": a pending generation's
intents are selectable before it activates and must not be dropped. A reader
without the port keeps the old behavior; a lookup error fails the selection.
`PartitionBatchResult.SupersededGenerationCount` and
`PartitionProcessResult.SupersededGenerationIntents` carry the subset of the
stale count that came from this drain. Because the drained rows leave
`BlockedRows`, `blocked_count` and `blocked_intent_wait_seconds` describe only
generations that are not superseded, so a large blocked wait is a real
prerequisite-phase stall. The SQL does not yet enforce that `superseded` is
terminal on every writer; that gap is tracked in #7130.

**The repo-wide-retract fence only engages for the fenced domain set**
(`sharedintent.DomainHasRepoWideRetract`). A domain added to that set without
a matching change in `internal/storage/cypher`'s `wholeScopeRetractDomains`
table gets the #6166 over-delete.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/sharedintent/README.md` — the intent row shape and ports this package drains
- `go/internal/reducer/gpphase/README.md` — the readiness phase/keyspace vocabulary and presence-key derivations
- `docs/internal/design/package-restructure.md` — the #6061 restructure and this hoist's no-regression evidence
