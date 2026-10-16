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
- The `/api/v0/admin/*` routes are documented in
  `openapi/paths/status/admin.go`; the identity and provider-config routes
  (`/api/v0/auth/admin/*`) in `openapi/paths/auth/` (`admin_mutations.go`,
  `admin_provider_configs.go`, `admin_reads.go`). `verify-openapi.sh` scans
  the tree recursively.
- `queryplan` manifests: the admin family has no entries. Keep it zero.

## Import Direction

- These packages import `auth`, `querycontract`, `governanceaudit`,
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

## Failure Answers (#7674)

- A failed step never writes `err.Error()` or `%v` of an error into a
  response body. Use the helpers in `server_failure.go`, each with a fixed
  package constant per step: `writeInspectionFailure` for an unbounded read
  through `ReadStore` or the repository catalog (reader verdict under its
  capability-matrix key, then 500), `writeBoundedReadFailure` for a read
  under its own `timeout_ms` (reader verdict under its key, then 504, then
  500), and `writeMutationFailure` for a writer step. Every reader verdict
  names a registered capability-matrix key; a new admin read registers its
  key in `specs/capability-matrix.v1.yaml` first, and the root capability
  sweep fails an unregistered one. Keep 400 echoes of caller input, the 409/422 refusals, and the
  configuration 503s as they are.
- The replay, reopen, and recover-generations idempotency finalize and
  their accepted audit append run on `Handler.finalizeContext` (detached
  from the request cancel, bounded by `FinalizeTimeout`) and fail through
  `writeFinalizeFailure`, which is always a 500. Do not move them back onto
  `r.Context()`: a client cancel there strands the ledger row in_progress
  forever (no expiry exists). Do not detach anything before the commit.
- A mutation step's 499 does not mean nothing changed. When you add or move
  a mutation step, state in a comment beside the call what may already have
  committed when it fails, and keep `docs/public/reference/http-api/admin-failures.md`
  in step.
- `TestNoNewQueryErrorTextLeakSites` (root `query`) no longer lists any
  file here, so a new leak site fails it. The route tables in
  `server_failure_routes_test.go` pin each step's message, 499, and reader
  503; add a row for every new failing step.
