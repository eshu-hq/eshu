# Agent instructions: internal/reducer/advisory/group

Scoped rules for this directory. The root `AGENTS.md` still applies.

## Read first

- `README.md` in this directory.
- `go/internal/reducer/AGENTS.md`.
- `go/internal/storage/cypher/edge/writer/advisory_group.go` for the Cypher
  this contract drives.
- `docs/internal/evidence/9209-3-2-advisory-group-cost.md` for the grouping
  cost basis.

## Invariants

- Never import the reducer root or any storage package from here. The root,
  the handler lane, and the writer all import this package.
- `Edge` identity is exactly (RepositoryID, PackageID). Do not add a mutable
  field to it.
- `Group` carries no evidence stamps. Changing that orphans the
  membership-only contract the writer implements.
- Keep every file under 500 lines.

## Common changes

Adding a field to the node: extend `Group` first, then the writer's `SET`
list, then the handler rows, in one change with the sibling lanes.
