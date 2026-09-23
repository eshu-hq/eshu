# AGENTS.md — storage/postgres/fake guidance

## Read first

1. `go/internal/storage/postgres/fake/doc.go` -- why this package exists and
   what it deliberately does not know (Eshu query strings).
2. `go/internal/storage/postgres/fake/README.md` -- ownership boundary and
   exported surface.
3. `go/internal/storage/postgres/db/contracts.go` -- the interfaces
   `ExecQueryer` satisfies.
4. `docs/internal/design/6693-postgres-target-tree.md`, decision D6 -- why
   the shared fake became a real package instead of staying test-file code.

## Invariants

- No built-in knowledge of any Eshu query string or table. A caller that
  needs query-shape-specific behavior stages it via `Routes`, not by adding
  a case here.
- `ExecQueryer`, `Rows`, and `Result` stay safe for concurrent use --
  `ExecQueryer` guards every field with its internal mutex. Do not add a
  field that bypasses it.
- `Rows.Scan`'s destination-type switch mirrors real store `Scan` call
  sites. Add a case only when a real (non-test-only) destination type needs
  it, and add the same coverage in `rows_test.go`.
- Exported identifiers do not stutter the package name (`fake.ExecQueryer`,
  not `fake.FakeExecQueryer`).

## Common changes

- Adding a new domain's fixture behavior is a `Route` in that domain's own
  test file, not a change to this package.
- A new `db` contract method this fake must satisfy goes in a new file here
  (mirroring `transaction.go`'s pattern) plus the matching test file; keep
  each concern (exec/query, rows, transactions) in its own file per the
  500-line cap.
- After touching this package, run
  `go test ./internal/storage/postgres/fake/... -race -count=1` and
  `go vet ./internal/storage/postgres/fake/...` from `go/`, then
  `bash scripts/verify-package-docs.sh` from the repo root.

## Failure modes

- Reintroducing query-constant matching here (instead of `Routes`) recreates
  the private-constant coupling D6 exists to remove -- a caller package
  cannot see another package's unexported query constants, so that logic
  would only work for callers still inside the postgres root.
- Returning a `Rows` value with `FailWith` set directly to a caller (instead
  of having `QueryContext`/`Route` handling translate it into a returned
  error) would let a test iterate rows that were meant to represent a
  failure.
