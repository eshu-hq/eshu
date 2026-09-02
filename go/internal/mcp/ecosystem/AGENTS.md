# AGENTS.md — MCP ecosystem registration guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `../types.go` for the ordered assembly position.
4. `../dispatch_ecosystem.go`, `../dispatch_repositories.go`,
   `../dispatch.go`, `../dispatch_infra_search.go` (the adapter over
   `../infrasearch`), and `../dispatch_impact.go` (the adapter over
   `../impact`) for split route ownership.
5. `../toolcontract/README.md` for the dependency-neutral definition contract.
6. `../../query/AGENTS.md` before changing ecosystem query behavior.

## Invariants

- Keep this package registration-only. Routing and argument mapping stay in the
  parent MCP package, except where a family has been extracted to its own route
  selector, each reached from a thin adapter arm the parent still owns:

  | Selector | Tools |
  | --- | --- |
  | `../infrasearch` | 1 |
  | `../impact` | 9 |
  | `../codeflow` | 4 |
  | `../codeintel` | 8 |
  | `../deadcode` | 3 |
  | `../codequality` | 3 |
  | `../entityresolution` | 3 |

  Counts are derived from each selector's own route table, not from the tool
  registration list here. Add a row when a family is extracted -- this list went
  stale by three extractions once, and a contributor following it edits the
  wrong package. Validation and reads stay in `internal/query`.
- Keep the package clause as `package ecosystemtools`; the root imports it with
  an explicit alias.
- Preserve all 23 tool names, descriptions, schemas, and their local order.
- Keep one ordered `Tools` assembly built from seven private definition slices
  and seven single-definition constructors. `package_registry_tools.go` owns
  the two package-registry definitions; the change-surface sibling owns
  `find_change_surface` and `investigate_change_surface`.
- Return fresh definitions on every call, including independent nested maps and
  slices.
- Preserve root positions 41–63 and the complete 162-tool order.
- Keep the existing split routers separate in the parent package.

## Common changes

- Change a schema only with the root route tests, query-handler contract,
  public API documentation, and the golden-corpus proof that replays saved
  inputs and checks the returned query shape.
- Update the serialized-definition hash in package tests only for an approved
  wire-contract change.
- Add a tool only with an explicit decision about its root registry position
  and owning router.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `toolcontract` only.
- Leaving one of the helper definitions in the root package creates a lateral
  dependency that the child cannot import.
- Moving a router here would expose root-only route and argument helpers and
  blur registration ownership.
- Reusing package-level maps or slices lets caller mutation leak into later
  `tools/list` responses.
- A set-only test misses local or root registration reordering.

## Anti-patterns

- Do not add route, HTTP, query, storage, authorization, transport, or telemetry
  helpers.
- Do not register tools through `init` functions.
- Do not weaken the serialized-definition or root order guards.
- Do not split the 23-definition family across packages or reorder the single
  `Tools` assembly. Source helpers may live in sibling files when the ordered
  assembly and serialized-definition guard stay unchanged.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/ecosystem ./internal/mcp -count=1
go vet ./internal/mcp/ecosystem ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
ecosystem query-shape change also requires the golden-corpus gate, which replays
saved inputs and checks the returned shape.
