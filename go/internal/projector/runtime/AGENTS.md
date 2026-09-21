# AGENTS.md — projection runtime guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../canonical/README.md`, `../decode/README.md`, `../stage/README.md`,
   `../failure/README.md` — this package composes all four.
4. `../service.go` for the claim/lease loop that calls `Runtime.Project`.

## Invariants

- Admission precedes every writer call. A fact with an unsupported schema
  version or a mismatched `generation_id` fails the whole projection; it is
  never written partially. A half-written generation is wrong graph truth.
- Fact payloads are **borrowed**. Do not mutate one, and do not reintroduce a
  defensive clone without measuring it —
  `TestBuildProjectionDoesNotMutateInputFactPayloads` pins the borrow and
  `BenchmarkProjectionCloneRemovalProof` is the measurement it replaced a
  clone with.
- `PackageRegistryIdentityLocker` brackets the package-registry identity
  writes. Concurrent projections of different scopes share that keyspace;
  dropping the bracket is a concurrency defect. `package_registry_lock_test.go`
  pins both the locked and the no-rows-so-no-lock path.
- `scope_generation_intents.go` is the single dispatcher for the per-family
  reducer-intent probes. Probe order is pinned by the fan-out parity fixture —
  reordering changes anchor `FactID`s.
- This package owns no queue, claim or lease. Those belong to the root
  `projector` service.

## Why the projection-level test suites live here

A test that drives `Runtime.Project` or `buildProjection` cannot live in
`../canonical` or `../decode`: this package imports both, so the test would
close an import cycle. The `*_projection_test.go`, `*_input_invalid_test.go`
and `*_cassette_*_test.go` suites are therefore here even where they assert on
a canonical or decode behavior. Put a new one here when it needs a projection;
put it in the owning package when it does not.

`scope_fixture_test.go` duplicates the `testScope` / `testGeneration` fixture
that `../canonical/builder_test.go` also defines. A Go test helper cannot
cross a package boundary; both suites must assert against the same scope
identity, so the two copies must stay byte-equal in behavior.

## Verification

```bash
cd go && go test ./internal/projector/runtime/... -count=1
cd go && go test ./internal/projector/... -count=1
```

Queue, lease and retry changes additionally need `concurrency-deadlock-rigor`.
