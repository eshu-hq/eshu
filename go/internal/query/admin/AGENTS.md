# Admin Leaf — Agent Instructions

Scope: `go/internal/query/admin/` (package `admin`) plus its nested leaves
`audit/`, `store/`, `identity/`, and `provider/config/`.

## Ownership

This tree owns the admin-handler family (#6060, lane B): recovery,
work-item inspection, dead-lettering, replay, backfill, replay events,
decisions, and input-invalid facts, with the tenant identity and
provider-config surfaces in their nested leaves.

- Do not add routes here for other families. New families get their own
  leaf; only the root `APIRouter` wires them.
- The OpenAPI fragments for these routes stay in the query root
  (`openapi_paths_auth_admin_*.go`); `verify-openapi.sh` requires every
  family's fragments at the top level.
- `queryplan` manifests: the admin family has no entries. Keep it zero.

## Import Direction

- These packages import `queryauth`, `querycontract`, `governanceaudit`,
  `recovery`, `telemetry`, and `storage/postgres`. They MUST NOT import the
  query root (cycle through `handler.go` / `admin_alias.go`).
- Children (`identity`, `provider/config`, `store`) may import the parent
  `admin` package and the sibling `audit` package. The parent MUST NOT
  import its children.
- Shared audit/permission glue lives in `audit/` exactly once, each helper
  citing its root source. Do not copy those helpers into more packages.

## Naming

`docs/internal/naming.md` is law: no `admin_` file prefixes, no
`admin/admin.go`, exported identifiers lose the family stutter (`Handler`,
not `AdminHandler`). Interface method names stay stable for the cmd/api
adapters; the root `admin_alias.go` keeps every old exported spelling.
