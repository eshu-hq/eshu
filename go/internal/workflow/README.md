# Workflow

## Purpose

`internal/workflow` defines storage-neutral contracts for the workflow control
plane: runs, work items, claims, collector instances, completeness states,
claim fairness, collector-family configuration validation, and reducer phase
requirements.

## Ownership boundary

Workflow code owns value types and pure derivation logic. It does not open
database connections, claim work from providers, emit facts, or write graph
truth. Postgres persistence lives in `internal/storage/postgres`, runtime
planning lives in `internal/coordinator`, and fact emission belongs to
collector runtimes.

The main flow is:

```text
DesiredCollectorInstance.Materialize
  -> ControlStore.ReconcileCollectorInstances
  -> ControlStore.EnqueueWorkItems
  -> ControlStore.ClaimNextEligible
  -> claim mutation with FencingToken
  -> ReconcileRunProgress
  -> CompletenessState rows
```

## Exported surface

See `doc.go` and `go doc ./internal/workflow` for the full godoc contract. The
main package contracts are:

- Status enums: `CollectorMode`, `TriggerKind`, `RunStatus`,
  `WorkItemStatus`, and `ClaimStatus`.
- Durable value types: `Run`, `WorkItem`, `Claim`,
  `DesiredCollectorInstance`, `CollectorInstance`, and `CompletenessState`.
- Store types: `ControlStore`, `ClaimSelector`, `ClaimMutation`, and
  `ClaimedWorkItem`.
- Reducer phase contracts: `CollectorContract`, `CollectorContractFor`,
  `CanonicalKeyspacesForCollector`, `RequiredPhasesForCollector`,
  `PhaseRequirement`, and `PhasePublicationKey`.
- Progress types: `CollectorRunProgress`, `RunProgressSnapshot`, and
  `ReconcileRunProgress`.
- Fairness types: `FairnessCandidate`, `ClaimTarget`,
  `FamilyFairnessScheduler`, `NewFamilyFairnessScheduler`, and
  `FairnessCandidatesFromCollectorInstances`.
- Config validators:
  `ValidateTerraformStateCollectorConfiguration`,
  `ValidateOCIRegistryCollectorConfiguration`, and
  `ValidatePackageRegistryCollectorConfiguration`.

## Dependencies

- `internal/reducer` for graph projection keyspace and phase identifiers.
- `internal/scope` for collector-kind identity.

There is no telemetry dependency and no storage dependency.

## Telemetry

None. The coordinator and Postgres storage layers emit telemetry around these
contracts.

## Gotchas / invariants

- Every `Validate` method rejects blank identifiers, unknown enum values, and
  invalid timestamps. Treat invalid stored rows as corruption.
- Claim mutation uses `FencingToken` for optimistic concurrency. Do not mutate a
  claim or work item without the current fence.
- `ReconcileRunProgress` is pure and deterministic. It can return
  `collection_pending` for an empty collector slice; that is valid early run
  state.
- Missing `PhasePublicationKey` counts as zero published items and keeps the
  corresponding completeness row pending.
- Terraform-state completion currently requires Terraform resource and module
  `canonical_nodes_committed` checkpoints, not DSL anchor readiness.
- AWS workflow completion is fact-backed until a live AWS graph-readiness
  publisher exists; do not require future `cloud_resource_uid` phase rows.
- `FamilyFairnessScheduler.Next` mutates scheduler state and is not safe for
  concurrent use without external synchronization.
- Unknown collector kinds return no phase requirements unless registered in
  `collectorContracts`; callers must handle that explicitly.

## Verification

Use the smallest command that proves the changed contract:

```bash
cd go
go test ./internal/workflow -count=1
go vet ./internal/workflow
go doc ./internal/workflow
go run ./cmd/eshu docs verify ../go/internal/workflow --limit 1000 \
  --fail-on contradicted,missing_evidence
```

Changes to `ControlStore` or claim/completeness behavior usually also need the
matching `internal/storage/postgres` and collector/coordinator tests.

## Related docs

- `docs/public/architecture.md`
- `docs/public/deployment/service-runtimes.md`
- `go/internal/coordinator/README.md`
- `go/internal/storage/postgres/README.md`
