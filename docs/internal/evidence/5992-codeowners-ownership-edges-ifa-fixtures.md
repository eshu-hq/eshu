# #5992: codeowners_ownership_edges Ifá exhaustiveness fixtures

## Scope

Issue #5992 (child of the #5543 umbrella) builds the family-specific Ifá
conformance-platform artifacts that prove `materialized_edges:
codeowners_ownership_edges` exhaustively: a compiled Odù
(`go/internal/ifa/codeowners_family_catalog.go`), its cassette projector
(`go/internal/ifa/codeowners_family_odu.go`), the family's own vacuity guard
(`go/internal/ifa/materialized_edges_codeowners.go`), a checked-in cassette
(`testdata/cassettes/codeowners/ifa-codeowners-family.json`), a hand-derived
expected-edge-set fixture
(`go/internal/ifa/testdata/codeowners/ifa-codeowners-family-expected-edges.json`),
their focused tests, and two draft (unwired, unrun) shell helpers for a
future live-gate phase (`scripts/lib/ifa_codeowners_live.sh`,
`scripts/lib/ifa_fault_injection_codeowners_cells.sh`).

This gate fires content-based on `codeowners_family_catalog.go` and
`materialized_edges_codeowners.go`: both files' Go doc comments quote the
literal word "MERGE" while explaining, in prose, the EXISTING production
write template's relationship-MERGE-key shape
(`go/internal/storage/cypher/canonical_codeowners_edges.go`'s
`batchCanonicalCodeownersOwnershipEdgeCypher`, unchanged by this PR) so a
reader of the test fixture understands why RULE A/B/C in the Odù produce
three distinct edges to one team. Neither file contains executable Cypher,
a graph write call, a worker, a queue, a lease, or a runtime knob.

## No-Regression Evidence:

No production code path changed. Every file this PR adds or touches lives
under `go/internal/ifa/` (a hermetic, backend-free conformance-platform
package: pure Go structs, JSON fixture loaders, and multiset comparisons —
see `go/internal/ifa/README.md`'s "Telemetry" section, "the package is a
pure local conformance helper with no worker, queue, or deployed-service
path"), `go/cmd/ifa/` (a new hermetic unit test file, no CLI behavior
change), `testdata/cassettes/codeowners/` (a fixture), and
`scripts/lib/` (two new, NOT-YET-SOURCED shell function libraries — neither
is invoked by any script that runs today; `scripts/verify-ifa-determinism.sh`
and `scripts/verify-ifa-fault-injection.sh` do not yet reference the
`codeowners_*` cassette, expected-edge path, or cell functions these files
define, so they execute zero times in CI or `make pre-pr` until a future,
separately-reviewed change wires them in).

The production reducer handler
(`go/internal/reducer/codeowners_ownership_materialization.go`), its pure
extraction seam (`ExtractCodeownersOwnershipEdgeRowsWithQuarantine`), and
the production Cypher writer
(`go/internal/storage/cypher/canonical_codeowners_edges.go`) are all
byte-identical to `origin/main` in this PR's diff — this PR only calls the
existing extractor from new test code, it does not modify it.

`cd go && go test ./internal/ifa/... ./cmd/ifa/... -count=1` passes with no
red tests, and `go build ./...` is clean.

An earlier revision of this branch carried three deliberately-red tests: two
proving that the shared `ifa.ExpectedEdge` triple could not tell two
codeowners rules apart, and one proving the family was not yet cataloged.
#6137 landed the relationship-identity properties on `ExpectedEdge`, so the
first gap is closed and this branch now reads the fixture through the shared
`ifa.LoadExpectedEdges` loader and `ExpectedEdge.Key()` instead of a
family-local struct, loader, and key. The two gap tests were replaced by
positive proofs of the closed mechanism
(`TestExpectedEdgeKeyDistinguishesADroppedRuleFromAnUnrelatedDuplicate`,
`TestCodeownersOwnershipIdentityExcludesOrderIndex`), and the family is now
spliced into `catalogSeed` and `MaterializedEdgeOduResolver.Resolve` the same
way `rationale_edges` is: cataloged and resolvable, still waived in
`specs/ifa-materialized-edge-coverage.v1.yaml` because no live gate has proven
it yet.

## No-Observability-Change:

This PR adds no metric, span, log field, status field, queue table, worker,
lease, batch, or runtime knob of any kind. It adds no route, graph query
shape, or graph write. The two new shell files
(`scripts/lib/ifa_codeowners_live.sh`,
`scripts/lib/ifa_fault_injection_codeowners_cells.sh`) are draft function
libraries for a FUTURE live-gate wiring phase that this PR does not perform
— they are sourced by nothing today, so they cannot change any operator-facing
signal. Operators diagnosing the Ifá conformance platform continue to use the
exact existing surfaces documented in `go/internal/ifa/README.md`'s
Telemetry section: `go test`, `ifa coverage`'s JSON report and stdout
summary, and (once a future PR wires this family into the live gates) the
existing `ifa assert-edges`/`ifa drive` CLI output — no new diagnostic
surface is introduced by this change.
