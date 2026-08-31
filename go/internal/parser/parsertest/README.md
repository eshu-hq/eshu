# Parser test helpers

## Purpose

`parsertest` holds fixture and payload assertions shared by external parser
test packages. It keeps black-box Engine tests from copying assertion logic as
language-owned tests move out of the parent parser directory.

## Ownership boundary

This package supports tests only. It may import the parent `internal/parser`
package to exercise the public Engine API. Production packages and same-package
parser tests must not import it.

## Exported surface

- `WriteFile` creates an owner-only fixture file.
- `MustParsePath` runs the public default Engine path.
- `AssertNamedBucketContains`, `AssertBucketItemByName`,
  `AssertBucketContainsFieldValue`, and `AssertFunctionByNameAndClass` check
  map-shaped parser buckets. `AssertFunctionByNameAndClass` matches on both
  `name` and `class_context`, for languages where a method name repeats across
  conformances, extensions, overrides, or implementations.
- `AssertStringSliceContains`, `AssertStringSliceNotContains`, and
  `AssertStringSliceEquals` check an exact `[]string` field without sorting or
  converting its values.
- `AssertIntFieldValue` checks an exact `int` field.
- `AssertPrescanContains` checks that a prescan path list carries an exact
  entry.
- `AssertFrameworksEqual`, `AssertNestedStringSliceEqual`, and
  `AssertNestedRouteEntriesEqual` check framework semantics without weakening
  ordering or type assertions.

See [doc.go](doc.go) for the godoc contract.

## Dependencies

- `internal/parser` provides the public Engine exercised by `MustParsePath`.
- The Go standard library provides filesystem, reflection, and test support.

## Telemetry

None. Test helpers do not run in Eshu services.

## Gotchas / invariants

- Keep assertion failure text and ordering checks stable when migrating a test.
- Call every helper with the caller's `testing.T`; each helper marks itself
  with `t.Helper()` so failures point to the external test.
- Do not add production imports of this package.

## Related docs

- `docs/public/contributing-language-support.md` — parser test ownership and
  the external test-package migration path
