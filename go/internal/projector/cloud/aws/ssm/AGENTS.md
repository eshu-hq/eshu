# AGENTS.md — SSM projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../../AGENTS.md` and `../../../README.md` for projector-wide invariants.
3. `../../../intent/AGENTS.md` for the neutral builder contract.
4. `../../../runtime/scope_generation_intents.go` for root-owned assembly order.

## Invariants

- Import `internal/projector/intent` and `internal/reducer` (domain
  constants only), never the root projector package.
- `BuildHostInventoryMaterializationReducerIntent` must keep the
  `ssm_host_inventory_materialization:<scope>` entity key, shared with no
  other domain.
- Anchor on the earliest application fact with platform fallback, so the
  reducer claim stays stable across reprojections.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.
