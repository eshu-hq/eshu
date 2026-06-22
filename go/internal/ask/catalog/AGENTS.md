# AGENTS.md — internal/ask/catalog guidance for LLM assistants

## Read first

1. `go/internal/ask/catalog/catalog.go` — `Backend`, `CostClass`, `SurfaceKind`,
   `Entry`, `Catalog`, `Parse`, `Annotate`, `Unannotated`, `Lookup`, `ByBackend`,
   `CheapestFirst`; understand all types and methods before editing.
2. `go/internal/ask/catalog/annotations.go` — `Annotation` type and the
   `annotations()` merger; the overlay is split across two files for size.
3. `go/internal/ask/catalog/annotations_routes.go` — curated annotations for the
   implemented retrieval-capable HTTP API routes; read owning handlers in
   `internal/query/` before adding or changing entries.
4. `go/internal/ask/catalog/annotations_tools.go` — curated annotations for the
   147 implemented MCP tools; read owning handlers in `internal/mcp/` before
   adding or changing entries.
5. `go/internal/ask/catalog/planner_exclusions.go` — admin/auth/session control
   surfaces that must never enter the Ask Eshu planner catalog.
6. `go/internal/capabilitycatalog/data/surface-inventory.generated.json` —
   the canonical surface inventory; never hand-edit it; it is produced by
   `cmd/capability-inventory`.

## Invariants this package enforces

- **Overlay completeness** — every implemented `api_route` and `mcp_tool` surface
  in the inventory must be either a retrieval catalog entry in
  `askRouteAnnotations()` / `askToolAnnotations()` or an explicit planner
  exclusion in `planner_exclusions.go`. `TestOverlayCoversInventory` enforces
  retrieval-overlay coverage, and
  `TestEveryImplementedSurfaceIsReadOrPlannerExcluded` prevents silent drops.

- **Retrieval-only planner** — side-effecting admin/recovery routes and
  auth/session control routes must not appear in parsed catalog entries. Add
  newly implemented non-retrieval surfaces to `planner_exclusions.go`, not to the
  planner overlay.

- **No BackendUnknown or empty Backend in the overlay** — `annotations()` must
  never return an entry with `Backend == BackendUnknown` or `""`. Enforced by
  `TestAnnotationOverlayHasNoUnknownBackends`.

- **Cost must be one of low/moderate/high** — `CostClass` values outside the
  three constants are invalid. The same test rejects any overlay entry that uses
  an unlisted cost string.

- **Pure package** — no import of `database/sql`, `net/http` client code, graph
  driver packages, or any live-backend adapter. The package reads only the
  committed inventory artifact (in tests via `os.ReadFile`). Enforced by the
  absence of those imports; verify with `go vet`.

- **File size** — `annotations_routes.go` and `annotations_tools.go` are the
  natural split point; keep each annotation file under 500 lines. If the
  inventory grows, split further by surface family (e.g.
  `annotations_routes_code.go`).

## Adding a new implemented surface

1. Identify the surface category (`api_route` or `mcp_tool`) and name from the
   inventory.
2. Read the owning handler (in `internal/query/` for routes, `internal/mcp/` for
   tools) to determine:
   - whether it is a retrieval surface or a side-effecting/control action. If it
     mutates state or only manages caller/session/control-plane state, add it to
     `planner_exclusions.go` instead of the planner overlay.
   - `Backend`: which store(s) the handler reads from (`nornicdb`, `postgres`,
     `both`, or `embedded` for static/generated data reads).
   - `Cost`: `low` for bounded/indexed reads, `moderate` for scoped fan-out,
     `high` for broad or denormalized reads.
3. Add the entry to the appropriate annotation file.
4. Run `go test ./internal/ask/catalog -count=1` and confirm all tests pass,
   including `TestOverlayCoversInventory`.

## Changing Backend or Cost for an existing surface

The planner relies on these values for path selection. Changing them affects
query routing decisions. Before changing:

1. Read the handler again to confirm the new backend or cost is accurate.
2. Update the annotation.
3. Run the full catalog test suite.
4. Note the change in the PR description so reviewers can verify the handler
   reading.

## Verification commands

```bash
cd go
gofmt -l ./internal/ask/catalog    # must print nothing
go vet ./internal/ask/catalog
go test ./internal/ask/catalog -count=1 -v
```
