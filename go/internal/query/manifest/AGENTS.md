# manifest — agent instructions

## Read first

`doc.go` for the route contract, `README.md` for layout and telemetry.

## Invariants

- The route stays bounded: page cap 200, one probe row for truncation, and a
  10-second read timeout. Do not add an unanchored fleet-wide scan.
- Runtime dependencies are the default; dev and test dependencies appear only
  with the `include_dev_dependencies` opt-in. The filter is a SQL predicate on
  the already scope-probed rows, never a new index condition.
- Each row's `scope` field normalizes to `runtime`, `dev`, or `test` from the
  fact's `development_dependency` and `dependency_scope` signals; a missing
  signal never invents a dev claim.
- The read serves active-generation rows through the same delta-overlay
  snapshot the reducer uses. A refused delta base answers 503; never serve
  delta-only rows as the repository's complete manifest.
- The truth envelope says declared, not resolved. No version resolution runs
  here, and no repository ownership beyond the scope anchor is asserted.
- Grant-denied callers receive the same empty page as an empty repository
  without touching the store (no existence oracle).
- `ecosystem` normalizes the stored package manager via
  `packageidentity.NormalizeEcosystem`, with the sbt-to-maven fallback the
  mapping test pins. A manager with no spelling yields no ecosystem; the
  exact `package_manager` is always present.
- Change the capability row only in `capability.go`. The matrix and
  `main_test.go` both call `Support()`; do not reintroduce a literal copy.
- This package must not import the root query package or another handler
  family. Shared types come from `querycontract`. The handler holds the
  `ManifestDependencyStore` interface, never a concrete store.
- A failed store read never echoes the backend error into the response body
  (#7674).

## Anti-patterns

- Returning resolved versions or cross-repository ownership from this route.
- Emitting an ecosystem value the normalization does not produce.
- Adding a fleet-wide browse mode without an anchor and a measured plan.
