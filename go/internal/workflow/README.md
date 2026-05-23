# Workflow

## Purpose

`internal/workflow` defines storage-neutral workflow control-plane contracts:
runs, work items, claims, collector instances, completeness states, claim
fairness, collector-family config validation, and reducer phase requirements.

## Ownership Boundary

Workflow code owns value types and pure derivation logic. It does not open
database connections, claim provider work, emit facts, or write graph truth.
Postgres persistence, coordinator planning, and collector fact emission live in
their own packages.

## Exported Surface

See `doc.go` and `go doc ./internal/workflow` for the contract. Keep the
value-type and validator list in godoc; this README should stay focused on the
control-plane invariants.

## Telemetry

None. Coordinator and storage layers emit telemetry around these contracts.

## Gotchas / Invariants

- Validators reject blank IDs, unknown enum values, and invalid timestamps.
- Claim mutation uses `FencingToken`; stale claim writers must not update work.
- `ReconcileRunProgress` is pure and may return `collection_pending` for an
  empty collector slice.
- Missing phase-publication rows keep completeness pending.
- Terraform-state completion currently waits on resource and module canonical
  node checkpoints.
- `FamilyFairnessScheduler.Next` mutates state and needs external
  synchronization for concurrent callers.

## Focused Tests

```bash
cd go
go test ./internal/workflow -count=1
go vet ./internal/workflow
go doc ./internal/workflow
go run ./cmd/eshu docs verify ../go/internal/workflow --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related Docs

- `docs/public/architecture.md`
- `docs/public/deployment/service-runtimes.md`
- `go/internal/coordinator/README.md`
