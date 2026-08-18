# internal/storage/postgres/pgarray

Postgres text-array encoding and identifier quoting for the storage layer.
This is the in-repo replacement for the four `github.com/lib/pq` symbols Eshu
used -- `pq.Array`, `pq.StringArray`, `pq.Float64Array`, `pq.QuoteIdentifier`.

## Why this package exists

`lib/pq` was linked into every production binary only for these helpers; the
actual driver has been `pgx` for a long time. In August 2026 govulncheck, a
blocking gate, began reporting five advisories against `lib/pq@v1.10.9`
(GO-2026-6166, -6168, -6170, -6171, -6172), all `Fixed in: N/A`. There is no
version to bump to, so the dependency had to go. `pgx/v5`'s `pgtype` package
has array types but none of them implements `driver.Valuer`/`sql.Scanner`, and
adding a fresh third-party module to replace one being dropped for supply-chain
reasons was not an option. Roughly 350 call sites depend on the encoding, so
the replacement had to be a drop-in with the same bytes on the wire.

## Exported surface

- `StringArray` -- `[]string` that is a `driver.Valuer` (renders `{"a","b"}`)
  and a `sql.Scanner` (parses the text form Postgres emits for `text[]`).
- `Float64Array` -- the same pair for `double precision[]`, used by the
  semantic-search vector tables.
- `Array(v any)` -- wraps `[]string`, `*[]string`, `[]float64` or `*[]float64`
  in the matching type. A slice value is copied (so a nil slice still encodes
  as SQL NULL); a pointer is aliased so `Scan` writes through. Any other Go type
  gets a wrapper whose `Value`/`Scan` fail with a typed error naming it.
- `QuoteIdentifier(name)` -- double-quotes a schema/table name and doubles any
  embedded `"`. Used by the live tests that create per-run schemas.

## Encoding contract

| Input | Driver value |
| --- | --- |
| `[]string(nil)` | SQL `NULL` |
| `[]string{}` | `{}` |
| `[]string{"a", `c:\x`, `q"`}` | `{"a","c:\\x","q\""}` |
| `[]float64{0.1, 3, 1e-7}` | `{0.1,3,0.0000001}` |

Every string element is quoted, always. Postgres accepts that, and it removes
the need for a which-characters-need-quoting table that could drift from the
server's parser. Comma, braces, whitespace, the empty string, the word `NULL`
and non-ASCII text all round-trip as data.

## How the encoding was proven

The change landed in two steps. First this package was added alongside
`lib/pq` with a differential test (`pq_differential_test.go`, since deleted)
that, in one process, asserted for every row of the tables in
`pgarray_test.go` that `pgarray`'s driver value equalled `pq`'s, that the
frozen literal in the table equalled `pq`'s output, and that `Scan` agreed
with `pq` on both decoded values and error/no-error for malformed input. The
test was then shown to fail: an on-disk edit dropping backslash escaping and
identifier-quote doubling turned the backslash, mixed-escape and
quote-injection rows red in both the differential and the frozen tables; a
second edit breaking bare-`NULL` detection in the parser turned the NULL scan
rows red. Both edits were restored. Only after that did the second step swap
every call site, delete the differential file, and `go mod tidy` the
dependency out.

## Invariants

- The driver value for a given slice must not change without a migration
  story: these literals are what the wire carries today.
- Only one-dimensional arrays. A nested `{` is an error, never flattened.
- A NULL element is an error on scan for both types. Dropping it would shift
  every later element by one position.
- Standard library only, no reflection. `doc_lockstep_test.go` pins the import
  set and the `fmt`/`strconv`/`strings` selectors so the next dependency or
  reflective fallback is a deliberate edit, not drift.

## Verification

```bash
cd go && go test ./internal/storage/postgres/pgarray -count=1
```

## One deliberate divergence from lib/pq

`Scan` rejects a nested-empty literal such as `{{}}` or `{{{}}}`, where `lib/pq`
returned an empty slice. This is deliberate rather than an oversight: Postgres
normalises an empty array to `{}`, so a server cannot emit the nested form, and
nothing in this repository constructs it. Rejecting it keeps the parser's
one-dimensional contract honest instead of silently flattening a shape it does
not model.

It is recorded here so the difference is a decision someone made, not a
discovery someone repeats.
