# compare — agent instructions

## Read first

`doc.go` for the route contract, `README.md` for the queryplan registration of
the two graph reads.

## Invariants

- `workload_id`, `left` and `right` stay required.
- A scoped caller who cannot see the workload gets the same response as a
  workload that does not exist.
- A failed read answers through `writeCompareReadFailure` (`failure.go`)
  with its own fixed message; never write `err.Error()` into a 5xx body, and
  never add a private status-taking writer (#7674).
- Change the capability row only in `capability.go`.
- This package must not import the root query package.

## Common changes

Editing `fetchWorkload` or `environmentSnapshot`, signature or body: rerun
`go test ./internal/queryplan/...`, re-audit the read, and update its
`non_hot` entry in `internal/queryplan/testdata/query-source-coverage.yaml`
with the new `source_sha256`.
