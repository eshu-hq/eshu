# Terraform State Reducer Contract

## Purpose

`internal/reducer/tfstate` records the reducer-facing contract for Terraform
state-derived canonical projection. It names projector components and readiness
checkpoints that fixtures, docs, and downstream domains depend on.

## Ownership Boundary

This package owns contract values only. It does not collect Terraform state,
emit facts, enqueue work, write graph rows, or publish phase rows at runtime.
Live source-local projection belongs to `internal/projector`.

## Exported Surface

See `doc.go` and `go doc ./internal/reducer/tfstate` for the contract. The
stable anchors are the runtime contract, published checkpoints, validation, and
defensive-copy helpers.

## Telemetry

None. Runtime telemetry for Terraform-state graph writes follows projector,
queue, and canonical-write instrumentation.

## Gotchas / Invariants

- The accepted checkpoints are `terraform_resource_uid` and
  `terraform_module_uid` at `canonical_nodes_committed`.
- Helpers return defensive copies; do not mutate package defaults through
  shared slices.
- `Validate` checks blank contract metadata, not the existence of concrete
  projector implementations.
- Domains that consume resolved relationships still need the facts-first
  post-Phase-3 reopen path outside this package.

## Focused Tests

```bash
cd go
go test ./internal/reducer/tfstate -count=1
go doc ./internal/reducer/tfstate
go run ./cmd/eshu docs verify ../go/internal/reducer/tfstate --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related Docs

- `go/internal/reducer/README.md`
- `go/internal/projector/README.md`
- `docs/public/architecture.md`
