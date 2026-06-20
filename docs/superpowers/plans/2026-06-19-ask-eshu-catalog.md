# Ask Eshu Catalog Implementation Summary

This note records the bounded implementation plan for the Ask Eshu catalog
foundation in `go/internal/ask/catalog`. It intentionally stays short enough to
belong in the repository; detailed task checklists and scratch execution notes
belong outside committed docs.

## Goal

Build Ask Eshu's local self-knowledge of callable API and MCP surfaces:

- parse the committed surface inventory;
- keep implemented API routes and MCP tools;
- join a curated backend and cost overlay;
- expose stable lookup and cheapest-first planner queries;
- fail tests when the overlay drifts from the implemented inventory.

## Package Contract

The package is pure and read-only. It must not import live storage clients, HTTP
clients, graph drivers, or runtime wiring. Tests may read the committed inventory
artifact from `go/internal/capabilitycatalog/data/surface-inventory.generated.json`.

The catalog has two layers:

- the generated inventory is the source of truth for which surfaces exist;
- the curated overlay records backend and cost information that the inventory
  does not carry.

Side-effecting admin/recovery routes are not planner retrieval paths. They are
accounted for in a curated mutating-surface registry so drift tests can prove
that every implemented surface is either a read catalog entry or an explicitly
excluded write action.

## Implementation Slices

1. Create package docs and core value types:
   `Backend`, `CostClass`, `SurfaceKind`, `Entry`, and `Catalog`.
2. Parse inventory JSON into sorted implemented API route and MCP tool entries.
3. Add the route/tool annotation overlay and apply it with `Catalog.Annotate`.
4. Add planner queries:
   `Lookup`, `ByBackend`, and `CheapestFirst`.
5. Add drift gates:
   overlay covers all read surfaces, overlay keys are live implemented surfaces,
   and mutating admin routes stay excluded from planner entries.
6. Document maintenance rules in `README.md` and `AGENTS.md`.

## Review Checklist

- Every changed Go file has focused tests.
- Every implemented read surface is annotated.
- Every mutating admin/recovery route is explicitly excluded.
- Overlay keys cannot go stale silently.
- Planner ordering is stable and deterministic.
- Package docs explain how to classify future surfaces.
- `go/internal/ask/catalog` files remain below the 500-line limit.

## Verification

Run from the repository root unless noted:

```bash
cd go && gofmt -l ./internal/ask/catalog
cd go && go vet ./internal/ask/catalog
cd go && go test ./internal/ask/catalog -count=1
cd go && golangci-lint run ./internal/ask/catalog/...
scripts/verify-package-docs.sh
git diff --check
```
