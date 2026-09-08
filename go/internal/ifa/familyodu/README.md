# familyodu

Ifá's family Odù fixtures, split out of `go/internal/ifa` (#6594 P1) when
that package reached its directory file cap.

## Purpose

One file per materialized-edge family (`*_family_odu.go`) beside its guard,
each exporting a constructor that returns a cataloged `CatalogOdu`, plus the
shared envelope types (`types.go`), wire-kind literals (`fixturekinds.go`),
and cassette loader (`cassette_envelopes.go`). The remaining waived #6228
families land here beside their siblings.

## Ownership boundary

- This package builds fixtures. It never imports the parent `ifa` package
  (that edge is a cycle: `ifa` imports `familyodu`).
- Catalog registration (`*_family_catalog.go`), the catalog seed, coverage
  reconciliation, and canonicalization stay in the parent package.
- The parent re-exports this package's surface through
  `go/internal/ifa/family_fixture_compat.go`, so external callers (`cmd/ifa`,
  `throughput`, `materializededges`, `reducer`, `replay/offlinetier`,
  `storage/postgres`) keep resolving the same `ifa.*` names across the move.
- New family files follow the existing shape: cassette path const, `OduName`
  const, `Load<Family>Odu` projector, `<Family>Odu()` catalog constructor,
  cassette-path helper, all with doc comments (no identifier-only repeats).
