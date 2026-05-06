# Go Write Plane Conversion Cutover

## What This Is

Complete the Python-to-Go platform conversion for Eshu's write plane. The proof milestones (0-5) are done; this project covers the actual ownership flip — making deployed services Go-owned, deleting Python finalization bridges, and removing Python runtime ownership from the normal platform flow.

## Core Value

No deployed runtime or write service starts from Python runtime entrypoints. The merge bar must be satisfied before any new ingestor work begins.

## Requirements

### Validated

- ✓ Go data plane substrate (collectors, facts, projectors, reducers, canonical writes) — Milestone 1
- ✓ Scope-first ingestion (ingestion_scopes, scope_generations) — Milestone 2
- ✓ Truth layers and reducer ownership — Milestone 3
- ✓ Post-commit bridge cutover — Milestone 4
- ✓ Documentation and operator guidance — Milestone 5
- ✓ Doc rebaseline (conversion marked incomplete) — Chunk 1

### Active

- [ ] Go-owned deployed runtime services (Chunk 3)
- [ ] Python finalization and recovery replacement (Chunk 4)
- [ ] Python runtime ownership removal (Chunk 5)
- [ ] Native parser and collector integration (Chunk 2 — Codex owns)

### Out of Scope

- New collectors (AWS, Kubernetes, GCP) — blocked until merge bar satisfied
- MCP/HTTP read-plane rewrite — separate effort
- Collector feature expansion — no new features during conversion

## Context

- Branch: `codex/go-data-plane-architecture` (long-running)
- Codex agent owns Chunk 2 (parser matrix parity, native selection/snapshot, bridge deletion)
- This agent owns Chunks 3, 4, 5 — with Chunk 3 first, then 4+5 after Chunk 2 lands
- Execution plan: `docs/superpowers/plans/2026-04-13-go-write-plane-conversion-cutover.md`
- Architecture PRD: `docs/superpowers/specs/2026-04-12-go-data-plane-rewrite-prd.md`
- SOW: `docs/superpowers/plans/2026-04-12-go-data-plane-rewrite-sow.md`

## Constraints

- **Branch policy**: No direct commits to main, no feature branching inside the rewrite
- **Merge bar**: 6 conditions must all be true before branch can merge (see SOW)
- **Dependency**: Chunks 4+5 depend on Chunk 2 completing first
- **TDD**: All Go code must be test-first
- **Telemetry**: Operator/admin status contract must be preserved in Go services

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Codex owns Chunk 2, this agent owns 3/4/5 | Parallel execution, no file conflicts | — Pending |
| Chunk 3 first, then 4+5 after Chunk 2 | Finalization removal depends on bridge deletion | — Pending |
| Use existing cutover plan as-is | Extensively documented, no replanning needed | ✓ Good |

---
*Last updated: 2026-04-13 after initialization*
