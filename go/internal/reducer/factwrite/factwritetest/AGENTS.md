# Agent instructions: internal/reducer/factwrite/factwritetest

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

Test support for `factwrite`'s batched-insert contract: a fake `Execer`
(`FakeExecer`) and a decoder for the calls it records
(`DecodeBatchedFactCalls`), importable from any reducer family's `_test.go`
file (issue #6061). See `README.md` for the full purpose and usage.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/factwrite/factwritetest/README.md`
- `go/internal/reducer/factwrite/README.md`

## Invariants

- **This package is test support, not production code.** Import it only
  from a `_test.go` file. It has no production call path and must not gain
  one.
- **No import of the reducer root, ever**, and no import of any family
  subpackage — only `factwrite` and the standard library.
- **The 16-column decode order in `DecodeBatchedFactCalls` must track
  `factwrite.ChunkArgs`'s column order exactly.** It is a positional decode
  with no field-name binding; if `factwrite.ChunkArgs` adds, removes, or
  reorders a column, this package's `decodeBatchedFactCall` must change in
  the same commit or every caller's decoded rows silently misattribute
  values to the wrong field.
- **The reducer root's own equivalent
  (`reducer_fact_batch_insert_test_helpers_test.go`,
  `fakeWorkloadIdentityExecer`/`decodeBatchedFactCalls`) is a SEPARATE,
  untouched copy**, not a forwarder to this package and not deleted. Do not
  "consolidate" it into an alias without first confirming every one of its
  ~30 root callers compiles against the change — that migration was
  deliberately left out of scope for issue #6061's `cicdrun` split.

## Common changes

Adding the versioned batched-insert decode
(`factwrite.BatchInsertVersionedQuery`): mirror
`BatchedFactRow`/`DecodeBatchedFactCalls`/`decodeBatchedFactCall` with the
17-column order the root's `decodedBatchedVersionedFactRow`/
`decodeBatchedVersionedFactCalls` already establish
(`reducer_fact_batch_insert_test_helpers_test.go`), only when a family
subpackage actually needs to assert on a versioned batched write — do not
port it speculatively.

## Failure modes to avoid

- Letting this package's column order drift from `factwrite.ChunkArgs`
  after a `factwrite` change.
- Deleting or aliasing the reducer root's own copy without migrating all
  ~30 of its current callers in the same change.
- Adding a production (non-test) import of this package anywhere.
