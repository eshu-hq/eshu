# terraform/drift — agent instructions

## Read first

`doc.go` for the route contract, `README.md` for layout and the name
collisions with `internal/storage/postgres` and `internal/reducer/tfconfigstate`.

## Invariants

- `scope_id` stays required and must be a `state_snapshot:` scope; limit is
  capped at 500.
- A scoped caller without a grant on the exact `scope_id` never reaches the
  store, and the grant stays bound onto `FindingFilter` for the SQL layer.
- Ambiguous-owner candidates and evidence `scope_id` values are filtered
  against the caller's grant independently of the finding's own grant check.
- Change the capability row only in `capability.go`.
- This package may import `internal/query/iac` for its paging helpers; it
  must not import the root query package.

## Common changes

Moving or renaming a file here: update
`internal/mcp/iac/management/AGENTS.md` and `README.md`, which cite
`handler.go` by path, then run `go test ./internal/mcp/...`.
