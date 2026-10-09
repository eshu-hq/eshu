# Storage Postgres Test Fixtures

## Purpose

Shares the fixture helpers the `postgres` and `activation_test` test
packages need in common. Created under #7648 by merging proven twin
helpers that each side had duplicated.

## Ownership boundary

Owns the merged twin helpers only. Does not own production storage
behavior, and production code must never import this package. Helpers
with a single consumer, or twins that differ semantically (kept as
separate copies by design), stay in their own test files.

## Exported surface

- `CatalogScope`, `CatalogGeneration` — catalog scope/generation
  builders
- `FactChannel` — closed fact-envelope channel
- `DSNForDeferredPartitionMemoProof` — disposable proof DSN or skip
- `SeedScope`, `SeedSupersededGeneration` — scope/generation seeders
- `ActivationRepositoryFact`, `AssertObligationStateToken` —
  activation obligation live-proof helpers

See `doc.go` for the full contract.

## Dependencies

- `internal/facts`, `internal/scope` — value shapes
- `internal/testutil/postgresproof` — disposable DSN helper

Never `internal/storage/postgres` or `internal/storage/postgres/activation`:
this leaf is imported by the root package's own internal test files, so
importing the parent would be an import cycle. Helpers that need parent
types stay duplicated per test package by design.

## Telemetry

None. Test helpers emit no signals.

## Gotchas / invariants

- Merge bar: byte-identical twins, or twins differing only in package
  qualification, whose merged form needs no parent-package type. A
  twin that differs semantically keeps both copies with the
  difference recorded (today: `openIsolatedBootstrapSchema`, whose
  root copy wraps the bootstrap error and whose activation copy
  returns it raw; and `openActivationObligationProofDB`, which
  depends on it). Proven twins that need parent types
  (`commitActivationRepository`, `claimActivationProjectorWork`,
  `newLiveActivationRunner`) also keep both copies: merging them
  would make this leaf import the parent, an import cycle for the
  root package's internal test files.
- Every helper takes `*testing.T` first and fails the test on setup
  errors; none returns an error except through the tested store call.

## Related docs

- `go/internal/storage/postgres/README.md`
- `go/internal/storage/postgres/activation/README.md`

No-Regression Evidence (#7648): this package merges proven test-fixture
twins duplicated between the root `postgres` and `activation_test` test
packages. Every merged helper was verified byte-identical with `diff`
before merging (seeders: same SQL, same args), except the three
qualification-only sweep twins that stay duplicated because merging
them would import the parent package into this leaf (an import cycle
for the root package's internal test files) and the one semantic
difference that keeps both copies (`openIsolatedBootstrapSchema`).
Merged callers keep identical behavior: same SQL, same arguments, same
failure messages. Measured: `go build ./...` exits 0, `go vet ./...`
exits 0, `go test -count=1` over `storage/postgres/...` passes with an
unchanged ok-set.

No-Observability-Change (#7648): test-only helpers imported by
`_test.go` files only. No production call path, no instrument, no
signal.
