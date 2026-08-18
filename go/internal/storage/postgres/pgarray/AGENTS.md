# AGENTS.md — internal/storage/postgres/pgarray guidance for LLM assistants

## Read first

1. `README.md` in this directory — why `lib/pq` was removed and how the
   byte-identical encoding was proven
2. `pgarray.go` — `StringArray`, `Float64Array`, `Array`, `QuoteIdentifier`
3. `parse.go` — the one-dimensional text-array parser `Scan` uses
4. `pgarray_test.go` — the frozen encoding, identifier and scan tables; the
   literals there were captured from `lib/pq` v1.10.9 and are the contract

## Invariants you must not break

- **Never change a driver value in the tables without a wire-format story.**
  These literals are what production sends to Postgres today. A change is a
  data-format migration, not a refactor.
- **Never quote selectively.** Every string element is quoted. A "smart" quoter
  needs a table of special characters that can drift from the server's parser.
- **Never accept a new element type silently.** `Array` returns an erroring
  wrapper for anything but `[]string`, `*[]string`, `[]float64`, `*[]float64`.
  Add a type by adding a typed array with its own table rows, never by
  reflection.
- **Never drop a NULL element on scan.** It is an error for both types.
- **Never flatten a nested array.** Multi-dimensional input is an error.
- **Do not add a dependency.** This package is stdlib-only and
  `doc_lockstep_test.go` pins the import set. Reaching for `pgtype` or any
  third-party array helper defeats the reason the package exists.
- **Do not import the parent `postgres` package.** The dependency runs one
  way: `postgres`, `query`, `reducer` import `pgarray`.

## When you change the encoder or parser

Add the table row first, watch it fail, then change the code. If you must
prove a guard bites, edit the source ON DISK and restore it: the lockstep test
reads files with `parser.ParseFile`, so a `go test -overlay` mutation passes
vacuously.

## Verification expected on any change here

```bash
cd go && go test ./internal/storage/postgres/pgarray -count=1
cd go && go test ./internal/storage/postgres/... ./internal/query/... -count=1
```

The second line is the blast radius: every `pgarray.Array` call site in
storage and query. Run it, do not reason about it.
