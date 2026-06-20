// Package catalog is Ask Eshu's self-knowledge of every implemented API route
// and MCP tool it can call to answer a question.
//
// The catalog spine is the canonical surface inventory artifact
// (go/internal/capabilitycatalog/data/surface-inventory.generated.json), which
// is generated and drift-gated on every surface change. The catalog parses that
// inventory, keeps only implemented read-only api_route and mcp_tool surfaces,
// excludes curated mutating admin/recovery routes, and joins a curated
// annotation overlay that records each surface's backend (NornicDB, Postgres,
// both, or the embedded inventory) and a coarse cost class. The backend and cost
// signals let the Ask Eshu planner prefer the cheapest correct retrieval path.
//
// Backend and cost are NOT carried by the surface inventory; they are a curated
// overlay in this package. A coverage check (Catalog.Unannotated) reports any
// implemented read surface that lacks an annotation, and mutating-route tests
// ensure side-effecting admin surfaces are explicitly accounted for instead of
// silently vanishing. The package is pure: it reads the embedded artifact only
// and never queries Postgres, a graph backend, or live runtime state.
package catalog
