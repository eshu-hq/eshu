# internal/workflow Agent Rules

This package is the storage-neutral workflow control-plane contract. Keep it
pure: no database calls, graph writes, collector I/O, goroutines, or telemetry
belong here.

## Read First

MUST read these before editing:

1. `README.md` and `doc.go`.
2. `types.go`, `store.go`, `progress.go`, `collector_contract.go`, and
   `fairness.go`.
3. Matching tests for the touched contract.

## Local Invariants

- `Validate` methods MUST reject blank identifiers, unknown enum values,
  invalid timestamps, negative counts, and invalid collector configuration.
  Invalid durable rows are corruption, not fallback input.
- `ControlStore` is the durable Postgres contract. Any signature change MUST be
  coordinated with `internal/storage/postgres` and all coordinator/collector
  callers.
- Claim mutations MUST preserve `FencingToken` optimistic concurrency. Do not
  mutate claims or work items without the current fence.
- `ReconcileRunProgress` MUST stay pure and deterministic. It may return
  `collection_pending` for an empty collector slice.
- Missing phase publication counts as zero. Terminal collector failures MUST
  block completeness and fail the run.
- Collector phase requirements MUST stay in `collectorContracts`; do not branch
  on collector kind in progress or fairness code.
- Unknown collector kinds currently return no required phases. Any new family
  MUST add a contract and progress tests before it can complete correctly.
- `FamilyFairnessScheduler.Next` mutates scheduler state and is not safe to
  share across goroutines without external synchronization.

## Change Rules

- New collector family: add `collectorContracts` entry, contract tests, and
  progress transition tests.
- New status or lifecycle field: update validation, storage scan/write paths,
  progress tests, and any status/query consumer.
- Timing default changes MUST preserve `HeartbeatInterval < ClaimLeaseTTL`.
- Terraform-state completion MUST keep resource and module
  `canonical_nodes_committed` checkpoints unless the reducer phase contract is
  deliberately redesigned.
- AWS completion is fact-backed until a live AWS graph-readiness publisher
  exists; do not require future `cloud_resource_uid` phase rows here.

## Proof

Run the focused package gate for any edit:

```bash
cd go
go test ./internal/workflow -count=1
go vet ./internal/workflow
go doc ./internal/workflow
```

Docs-only edits also need the package-doc verifier for this directory and
`git diff --check`.
