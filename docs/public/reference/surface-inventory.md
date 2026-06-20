# Surface Inventory

The surface inventory is a generated, machine-readable record of every platform
surface Eshu ships, so an operator or contributor can see the full fleet from one
artifact — and so a new surface cannot silently bypass capability truth.

It complements the [Capability Catalog](capability-catalog.md): the catalog
answers "what can Eshu do, and how mature is each capability"; the surface
inventory answers "what surfaces exist across the runtime, and what readiness
lane is each in". Both are generated and drift-gated, never hand-maintained.

## Categories

The inventory tracks six surface categories, each enumerated from an
authoritative live source so the inventory stays in lockstep with code:

| Category | Live source |
| --- | --- |
| `command` | command binaries under `go/cmd` |
| `collector` | collector families (`scope.AllCollectorKinds`) |
| `reducer_domain` | reducer domains (`reducer.AllDomains`) |
| `api_route` | HTTP API routes (one per method+path operation in the OpenAPI spec) |
| `mcp_tool` | read-only MCP tools (`mcp.ReadOnlyTools`) |
| `console_page` | console page components under `apps/console/src/pages` |

## Readiness lanes

Every surface carries a readiness lane — the canonical, static classification of
what the surface does today. The lanes are ordered from most to least
production-ready:

| Lane | Meaning |
| --- | --- |
| `implemented` | Built, charted where applicable, and provable end to end. The only lane that asserts production readiness, so it requires linked promotion proof. |
| `partial` | Evidence exists but the implemented contract is unmet (readback pending, claims inactive, runtime-proof gap). |
| `gated` | Built but intentionally withheld from a public lane pending a missing gate (a sanitized live smoke, a public chart, an operator opt-in). |
| `foundation_only` | Code structure exists but no hosted runtime, claim-driven path, reducer projection, or chart yet. |
| `fixture_only` | Proven only against fixtures; never reaches implemented without live provider proof. |
| `research_only` | Design or research only; no production code lane. |
| `not_implemented` | Declared or referenced but not implemented. |
| `unsupported` | Known family with no configured or shipped instance. |

These lanes are deliberately distinct from the per-instance runtime
`promotion_state` reported by `/admin/status` (see
[Collector And Reducer Readiness](collector-reducer-readiness.md)): a lane
describes a surface's development maturity in source, while a promotion state
describes one configured instance's observed health right now. They share the
common vocabulary (`implemented`, `partial`, `gated`, `unsupported`) so docs,
status, and the inventory never contradict each other.

## Editorial overlay

The inventory is generated from code, but the readiness lane, owner, promotion
proof, and notes for each surface cannot be derived from code. They live in the
editorial overlay `specs/surface-inventory.v1.yaml`, keyed by category and name.
Surfaces with the category default lane (`implemented` for everything except
collectors) need no overlay row, so the overlay stays small. Collectors have no
default lane and **must** be classified — an unclassified collector fails the
drift gate so a new collector cannot silently claim production readiness.

## Drift gate

`go run ./cmd/capability-inventory -mode verify` is the CI gate. It fails when:

- a live surface is missing from the committed artifact (a surface added in code
  without regenerating), or
- an overlay row references a surface absent from code (a surface removed or
  renamed without updating the overlay), or
- a collector is unclassified, an `implemented` collector links no proof, or an
  overlay lane is invalid.

Regenerate the committed artifact after any surface change:

```bash
cd go
go run ./cmd/capability-inventory -mode generate
go run ./cmd/capability-inventory -mode verify
```

The committed artifact is `go/internal/capabilitycatalog/data/surface-inventory.generated.json`.

## Product surfaces

The same embedded artifact is served, read-only and bounded, across three
surfaces that stay in parity because they all read it:

- **API**: `GET /api/v0/surface-inventory?category=&readiness=&limit=&offset=`
  returns the readiness rows with owner, proof, docs, and notes, with a fresh,
  exact truth envelope in every profile.
- **MCP**: the `get_surface_inventory` tool summarizes the inventory for
  assistants with the same `category`/`readiness` filters and bounded paging.
- **Console**: the Surface Inventory page groups surfaces by category and shows
  each readiness lane honestly — only `implemented` is styled as production-ready.

Because the inventory is a compiled-in artifact, the read is static and exact in
every profile and carries no tenant- or source-scoped data, so the three
surfaces never disagree.

## Related

- [Capability Catalog](capability-catalog.md)
- [Collector And Reducer Readiness](collector-reducer-readiness.md)
- [Capability Conformance Spec](capability-conformance-spec.md)
