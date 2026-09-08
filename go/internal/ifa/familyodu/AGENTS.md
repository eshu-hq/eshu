# AGENTS.md - internal/ifa/familyodu guidance

## Read first

1. `README.md` - package purpose, ownership boundary, exported surface.
2. `doc.go` - godoc contract and the no-parent-import boundary.
3. `types.go` - the `Odu` / `CatalogOdu` envelope types.
4. `fixturekinds.go` - shared wire-kind literals.
5. The `go/internal/ifa` root `AGENTS.md` and `README.md` - the parent
   package's Odù catalog, coverage manifest, and CI gate contract this
   package plugs into. Everything there still applies here.

## Invariants

- Never import the parent `ifa` package from here (import cycle). Catalog
  `*_family_catalog.go` files stay in the parent for the same reason.
- One file per family beside its siblings; new waived #6228 families land
  here, not in the parent.
- Keep this package deterministic: no wall-clock time, randomness, network,
  or storage side effects inside fixture builders.
- Every new or touched exported type, function, and const needs a useful Go
  doc comment; identifier-only repeats are not acceptable.
- External callers resolve these names through the parent's
  `family_fixture_compat.go` aliases. When adding a family export that
  callers use as `ifa.*`, add the matching alias there too.
