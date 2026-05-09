# Workflow

## Purpose

`internal/workflow` defines the durable contracts for the workflow control
plane: runs, work items, claims, collector instances, completeness states, the
`ControlStore` surface, and the reducer-facing phase contract per collector
family. All types are storage-neutral value types with `Validate` methods;
this package never opens database connections.

## Where this fits in the pipeline

```mermaid
flowchart LR
  COORD["internal/coordinator\nService.Run"] --> WF["internal/workflow\nStore interface + types"]
  ING["ingester collectors\nClaimNextEligible / CompleteClaim"] --> WF
  WF --> PG["internal/storage/postgres\nWorkflowControlStore"]
  PG --> RUNS["workflow_runs / work_items\n/ claims / collector_instances\n(Postgres)"]
  RED["internal/reducer"] --> WF2["internal/workflow\ncollector_contract.go\nphase requirements"]
```

## Internal flow

```mermaid
flowchart TB
  A["DesiredCollectorInstance\n(declarative config)"] --> B["Materialize(observedAt)\nproduces CollectorInstance"]
  B --> C["ControlStore.ReconcileCollectorInstances\nPersist durable row"]
  D["ControlStore.ClaimNextEligible\nClaimSelector + lease duration"] --> E["WorkItem + Claim\n(one ownership epoch)"]
  E --> F["ControlStore.HeartbeatClaim\n/ CompleteClaim / ReleaseClaim\n/ FailClaimRetryable / FailClaimTerminal"]
  G["RunProgressSnapshot\n(Run + []CollectorRunProgress)"] --> H["ReconcileRunProgress(snapshot, observedAt)"]
  H --> I["updated Run (status)\n+ []CompletenessState"]
  J["[]CollectorInstance (enabled + claims)"] --> K["FairnessCandidatesFromCollectorInstances"]
  K --> L["NewFamilyFairnessScheduler(candidates)"]
  L --> M["FamilyFairnessScheduler.Next()\nClaimTarget"]
```

## Lifecycle / workflow

Types in this package flow through four phases of the workflow control plane:

1. **Collector registration** — `DesiredCollectorInstance` represents declarative
   configuration. `Materialize(observedAt)` binds it to a timestamp and produces
   a `CollectorInstance` suitable for `ControlStore.ReconcileCollectorInstances`.

2. **Work intake** — a caller creates a `Run` and calls
   `ControlStore.EnqueueWorkItems` with a slice of `WorkItem` rows. Each
   `WorkItem` carries identity (`WorkItemID`, `RunID`, `CollectorKind`,
   `CollectorInstanceID`), a `FairnessKey` for cross-instance routing, and a
   `WorkItemStatus` lifecycle value.

3. **Claim lifecycle** — collector actors call `ControlStore.ClaimNextEligible`
   with a `ClaimSelector` to acquire a `WorkItem` and `Claim`. They advance the
   claim via `HeartbeatClaim`, `CompleteClaim`, `ReleaseClaim`,
   `FailClaimRetryable`, or `FailClaimTerminal` using a `ClaimMutation` carrying
   the `FencingToken` for optimistic concurrency. The coordinator's
   `ControlStore.ReapExpiredClaims` reclaims ownership of claims whose
   `LeaseExpiresAt` has passed.

4. **Run progress and completeness** — reducer phases publish checkpoints that
   the coordinator observes. `ReconcileRunProgress(snapshot, observedAt)` takes
   a `RunProgressSnapshot` (a `Run` plus per-collector counts and published phase
   counts) and derives an updated `Run.Status` and a sorted slice of
   `CompletenessState` rows. Terminal collector failures produce blocked
   completeness rows before downstream phases can be marked ready. The
   transition table:

   | Condition | `RunStatus` |
   |---|---|
   | any `failed_terminal` work items | `failed` |
   | all collection complete + all required phases ready | `complete` |
   | all collection complete, some phases pending | `reducer_converging` |
   | any claimed or mix of pending/completed | `collection_active` |
   | no collectors yet | `collection_pending` |

## Exported surface

**Status enums** (all carry `Validate` methods):
- `CollectorMode` — `continuous`, `scheduled`, `manual`
- `TriggerKind` — `bootstrap`, `schedule`, `webhook`, `replay`,
  `operator_recovery`
- `RunStatus` — `collection_pending`, `collection_active`,
  `collection_complete`, `reducer_converging`, `complete`, `failed`
- `WorkItemStatus` — `pending`, `claimed`, `completed`, `failed_retryable`,
  `failed_terminal`, `expired`
- `ClaimStatus` — `active`, `completed`, `failed_retryable`,
  `failed_terminal`, `expired`, `released`

**Durable value types** (all carry `Validate` methods):
- `Run` — root record for one workflow execution
- `WorkItem` — bounded collector slice unit; carries `FencingToken` and
  `CurrentClaimID`
- `Claim` — one ownership epoch for a `WorkItem`; carries `FencingToken` for
  safe concurrent mutation
- `DesiredCollectorInstance` — declarative config shape; `Materialize` binds to
  a timestamp
- `CollectorInstance` — durable row; adds `LastObservedAt`, `DeactivatedAt`
- `CompletenessState` — one reducer-phase checkpoint row per collector kind per
  keyspace per phase

**Store surface**:
- `ControlStore` — the full durable surface (thirteen methods) implemented by
  `storage/postgres`
- `ClaimSelector`, `ClaimMutation`, `ClaimedWorkItem` — claim operation
  arguments and return shapes

**Phase contract**:
- `CollectorContract`, `CollectorContractFor`, `CanonicalKeyspacesForCollector`,
  `RequiredPhasesForCollector` — lookup table of required reducer phases per
  collector family; registered entries: `CollectorGit`,
  `CollectorTerraformState`, `CollectorAWS`, `CollectorWebhook`
- `PhaseRequirement`, `PhasePublicationKey` — per-phase requirement and
  publication checkpoint key types

**Run progress**:
- `CollectorRunProgress`, `RunProgressSnapshot` — inputs to `ReconcileRunProgress`
- `ReconcileRunProgress(snapshot, observedAt)` — pure derivation of `Run` status
  and completeness rows
- `CompletenessStatusPending`, `CompletenessStatusReady`,
  `CompletenessStatusBlocked` — status constants

**Fairness scheduling**:
- `FairnessCandidate`, `ClaimTarget` — input and output of the scheduler
- `FamilyFairnessScheduler`, `NewFamilyFairnessScheduler` — deterministic
  weighted round-robin across collector families; rotation within each family
- `FairnessCandidatesFromCollectorInstances` — extracts claim-enabled durable
  instances into `FairnessCandidate` slices

`DesiredCollectorInstance.Validate` applies a stricter configuration check for
`terraform_state` instances. A Terraform state collector must declare a
`discovery` block with graph discovery, explicit seeds, or local repo limits.
S3 seeds must include bucket, key, and region; any S3 seed also requires
`aws.role_arn`. This keeps unsafe or incomplete state-reader config out of the
durable `collector_instances` table.

**Defaults**:
- `DefaultClaimLeaseTTL()` — 60s
- `DefaultHeartbeatInterval()` — 20s
- `DefaultReapInterval()` — 20s
- `DefaultExpiredClaimLimit()` — 100
- `DefaultExpiredClaimRequeueDelay()` — 5s

## Dependencies

- `internal/reducer` — `GraphProjectionKeyspace` and `GraphProjectionPhase`
  identifiers used in the phase contract and `CompletenessState`
- `internal/scope` — `CollectorKind` used throughout

## Telemetry

None. The coordinator (`internal/coordinator`) and storage
(`internal/storage/postgres`) layers own telemetry around these contracts.

## Operational notes

- `ReconcileRunProgress` is a pure function. Feed it a fresh
  `RunProgressSnapshot` from the store and compare the returned `Run.Status`
  to the current durable row to determine whether an update is needed.
- `CollectorRunProgress.PublishedPhaseCounts` must be keyed by
  `PhasePublicationKey` values from `RequiredPhasesForCollector`. A missing key
  counts as zero published items and keeps the phase in `pending`.
- `CompletenessState` rows from `ReconcileRunProgress` are sorted by
  `CollectorKind`, `Keyspace`, `PhaseName` — callers can compare slices
  element-by-element for drift detection.

## Extension points

- **Add a new collector family** — add an entry to `collectorContracts` in
  `collector_contract.go` with the required `PhaseRequirement` rows. The
  coordinator and storage layers consume the contract via
  `RequiredPhasesForCollector` lookups; no changes to their code are needed.
- **Add a new `RunStatus` transition** — edit `ReconcileRunProgress` in
  `progress.go`; add the new `RunStatus` constant in `types.go` with a
  `Validate` entry; update `progress_test.go` coverage.
- **Add a new claim operation** — add the method to `ControlStore` in
  `store.go`; implement it in `storage/postgres`; no changes to the value types
  are needed unless the operation introduces new state.

## Gotchas / invariants

- Every `Validate` enforces non-blank identifiers, known status enum values,
  and `updated_at >= created_at`. Stored rows that fail `Validate` should be
  treated as corruption and not silently ignored.
- `FamilyFairnessScheduler.Next` mutates internal `currentWeight` state. A
  single scheduler instance is not safe for concurrent use without external
  synchronization.
- Adding a new collector family requires an entry in `collectorContracts` in
  `collector_contract.go`. The lookup returns `false` for unknown kinds;
  callers that do not check will silently get empty phase lists.
- `DesiredCollectorInstance.Materialize` always sets `CreatedAt` and
  `UpdatedAt` to the supplied `observedAt`. The storage layer is responsible
  for applying an upsert that preserves the original `CreatedAt` on repeat
  calls.
- `ReconcileRunProgress` returns `RunStatusCollectionPending` for a snapshot
  with no collectors, not an error. An empty `Collectors` slice is valid
  (early in run lifecycle).

## Related docs

- `docs/docs/architecture.md`
- `docs/docs/deployment/service-runtimes.md`
- `go/internal/coordinator/README.md`
- `go/internal/storage/postgres` — the Postgres WorkflowControlStore type
  implements `ControlStore`
