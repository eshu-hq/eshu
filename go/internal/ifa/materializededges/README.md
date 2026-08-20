# ifa/materializededges

## Purpose

`materializededges` owns Ifá's materialized-edge coverage contract
(#5351): one pure vacuity guard per reducer-materialized graph edge family,
and the `replaycoverage.Resolver` that dispatches a coverage-manifest row to
the right guard by family name. It backs the `materialized_edges:<family>`
surface the `ifa-determinism` and `ifa-fault-injection` CI proof gates
require a baseline/fault row for.

See `doc.go` for the full design rationale — what a guard proves versus what
it deliberately does not (the live-write half), and why the package boundary
sits where it does.

## Ownership Boundary

This package split out of `go/internal/ifa` (#6053) preemptively, to buy
headroom under the repository's directory file-count gate (`go-dir-gate`)
before the families still queued consumed it. ifa was under the cap when the
split was taken, not over it. It owns:

- The seven family guards: SQL relationships (`materialized_edges_sql.go`),
  documentation edges (`materialized_edges_documentation.go`), code calls
  (`materialized_edges_code_calls.go`), rationale edges
  (`materialized_edges_rationale.go`), codeowners ownership
  (`materialized_edges_codeowners.go`), and deployable-unit edges
  (`materialized_edges_deployable_unit.go`), and repository dependencies
  (`materialized_edges_repo_dependency.go`).
- The shared dispatch/coverage-reconciliation machinery
  (`materialized_edges.go`), the waiver manifest loader
  (`materialized_edges_manifest.go`), and the shared expected-edge fixture
  loader (`materialized_edges_assert.go`).
- Every test that exercises a moved guard, including the family's
  compiled-catalog-vs-cassette lockstep test (moved with its guard from
  `ifa/<family>_family_odu_test.go` for the same reason: it can only reach
  the guard's unexported internals from this side of the package boundary).

It does NOT own: the Odù catalog itself (`Odu`, `Catalog`, `CatalogByName`,
`DiscoveredEvidence` all stay in `ifa`), the per-family fixture builders that
seed the catalog (`*_family_catalog.go`, `*_family_odu.go` stay in `ifa`
because `ifa`'s own `catalog_seed.go` calls them at production registration
time — moving them would create a production import cycle), or any test that
proves a genuinely cross-family invariant spanning fixtures outside this
package's family list (e.g. `TestIFALiveMatrixGenerationIDsAreUniqueAcrossScopes`
stays in `ifa`).

## Exported Surface

- `ExpectedEdge{RelationshipType, SourceEntityID, TargetEntityID, Identity}`
  and `LoadExpectedEdges(path, family)` - the shared hand-derived
  expected-edge-set shape and loader every guard reads its fixture through.
- `MaterializedEdgeDomainEdgeTypes(family)` - the family's registered
  live-write edge-type registry, used to prove a fixture is exhaustive, not
  just non-empty.
- `MaterializedEdgeOduResolver{Catalog, RepoRoot}` and its `Resolve` method -
  implements `replaycoverage.Resolver`; dispatches a
  `materialized_edges:<family>` coverage entry to the family's own guard.
- `MaterializedEdgeWaiver`, `MaterializedEdgeCoverageInputs`,
  `RunMaterializedEdgeCoverage`, `EnumerateMaterializedEdgeSurfaces`,
  `LoadMaterializedEdgeWaivers` - the coverage-manifest reconciliation surface
  that mirrors `ifa.RunCoverage`'s shape (`coverage.go`) with one addition:
  per-(surface, proof_gate) waivers for a family deliberately left RED with a
  tracked child issue.
- `RegistryMaterializedEdges`, `MaterializedEdgeSurfacePrefix`,
  `MaterializedEdgeManifestFileName` - the registry key, surface-key prefix,
  and manifest filename constants the reconciliation surface above uses.
- `RationaleExpectedNodeRecord`, `RationaleExpectedEdgeRecord`,
  `LoadRationaleExpectedEdgeRecords` - the rationale family's richer
  expected-edge record shape (it carries node identity alongside the edge, for
  `Rationale` node property assertions the plain `ExpectedEdge` shape can't
  express).

Each family's guard function
(`resolve<Family>MaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string)`)
stays unexported: it is reached only through `MaterializedEdgeOduResolver.Resolve`'s
dispatch or this package's own tests, so widening it to exported API surface
would only grow the contract without a real caller.

## Dependencies

`github.com/eshu-hq/eshu/go/internal/ifa` for `Odu`, `Catalog`,
`CatalogByName`, `DiscoveredEvidence`, and the exported per-family catalog
identifiers and cassette loaders (`ifa.CodeownersFamilyOduName`,
`ifa.LoadCodeCallFamilyOdu`, `ifa.RationaleFamilyRepositoryFact`, etc. — see
each family guard file's own duplication/export comments for the exact list
and why each crossed the boundary as an export versus a duplicated pure
constant). `reducer` for every family's production extraction/resolution
seam. `replaycoverage`, `cigates`, `goldengate` for the coverage-manifest
reconciliation contract. `facts`, `storage/cypher`, `relationships` for
family-specific fixture and identity-property lookups.

## Telemetry

None. Every function here is a pure, in-memory guard or coverage
reconciliation over already-loaded fixtures — no Postgres, graph, or network
I/O, so there is no runtime signal to emit. Operator-facing coverage results
surface through the CI proof gates' own reporting (`ifa-determinism`,
`ifa-fault-injection`), not a metric this package mints.

## Gotchas / Invariants

- **The Resolve dispatch switch is the actual registration point, not
  `init()`.** A new family's guard function existing and being fully unit
  tested proves nothing about coverage until
  `MaterializedEdgeOduResolver.Resolve` (materialized_edges.go) grows a
  `case` for it — the #5993 defect class this repository treats as a
  cautionary tale.
- **Duplicated small pure constants are marked, not accidental.** Several
  family guard files carry a one-off duplicate of an `ifa`-package unexported
  constant (e.g. `sqlFamilyOduName`, `repositoryFactKind`) with a doc comment
  naming the `ifa` source and why it could not simply be exported and imported
  instead. When editing one of those values in `ifa`, grep this package for the
  duplicate too. A duplicate is only safe where a stale copy fails CLOSED --
  identities on the reference side of an assertion, and extractor inputs the
  extractor stamps through without filtering on, fail OPEN and are exported
  from `ifa` instead. Successive review passes each found one more of those, so
  re-derive it by mutation rather than trusting this list -- BOTH directions,
  suffix and substitution, as AGENTS.md spells out. A suffix alone cannot see a
  path re-pointed at a file that exists.
- **Never duplicate guard, loader, or assertion logic itself** — only pure
  data constants. A second copy of a family's cassette decoder, catalog
  builder, or extraction comparison would drift from the original silently,
  which is exactly the false-green class this whole coverage contract exists
  to catch. Where a moved test needed a loader or builder function
  (`LoadCodeCallFamilyOdu`, `RationaleFamilyRepositoryFact`, etc.), the
  function was exported from `ifa` instead of copied.
- **`repoRootDir` is duplicated, at one extra `..` hop.** This package's test
  files sit one directory deeper than `ifa`'s
  (`go/internal/ifa/materializededges/` vs `go/internal/ifa/`), so the
  repo-root-relative walk needs four `..` segments here, not three.

## Related Docs

- `doc.go` - full design rationale for what a guard proves and the package
  boundary.
- `go/internal/ifa/README.md` - the parent package's Odù catalog, coverage
  manifest, and CI gate contract this package's guards plug into.
- `specs/ci-gates.v1.yaml` - the `ifa-determinism`/`ifa-fault-injection` gate
  definitions this package's coverage rows back.

## Performance and observability

No-Regression Evidence (#6053): this change relocates offline vacuity guards
between packages, exports identifiers so a single value is read rather than
copied, and repoints gate wiring. It adds no runtime path. The moved guards
execute no Cypher and open no graph session -- they READ the writer registries
(`cypher.MaterializedEdgeIdentityProperties`,
`cypher.SQLRelationshipMaterializedEdgeTypes`) to derive what a family must
produce, then compare a hand-derived fixture against rows extracted through the
pure reducer seams. The only production `.go` file in the diff is
`go/internal/reducer/deployable_unit_correlation_edges.go`, and its diff is one
comment line repointing a moved path; every other reducer and storage entry is
a `.md`.

No-Observability-Change: no worker, queue, lease, batch size, transaction
boundary, emitted query, span, metric or log is added or altered, so there is
nothing new for an operator to see and nothing existing that changes shape. The
handlers these guards describe are untouched.

The evidence that the guards still work is behavioural rather than numeric, and
it is the point of the change: each relocated guard was proven able to FAIL by
mutating the tree and observing the red, then restored and re-verified
byte-identical. Several were found to have stopped being able to fail when their
constants were copied alongside them -- a copy on the reference side of an
equality assertion is a disabled check, not a duplicate. Reading a guard does
not establish that it can fail.

## Family vacuity guards and their live-gate proof

Restored from `go/internal/ifa/README.md` when these guards moved into this
subpackage. The text describes the guards themselves, so it belongs beside them;
it was dropped rather than moved during the extraction and had to be restored in
a later commit. If you change what a guard proves, change this text in the same
commit -- `TestMaterializedEdgeLiveProofDocumentationMatchesWiring` in
`go/internal/ifa/code_call_live_documentation_test.go` pins these passages by
exact wording and fails if they drift.

- `RegistryMaterializedEdges`, `MaterializedEdgeSurfacePrefix`,
  `MaterializedEdgeManifestFileName`, `EnumerateMaterializedEdgeSurfaces`,
  `MaterializedEdgeOduResolver`, `MaterializedEdgeWaiver`,
  `LoadMaterializedEdgeWaivers`, `MaterializedEdgeCoverageInputs`,
  `RunMaterializedEdgeCoverage` (`materialized_edges.go`,
  `materialized_edges_manifest.go`, #5351) - the `materialized_edges:<domain>`
  exhaustiveness gate: binds an Odù expectation to each
  `reducer.MaterializedEdgeFamilies()` entry, mirroring `RunCoverage`'s shape
  with one addition — a `waivers:` section (parsed separately from the
  standard `replaycoverage.Manifest` `coverage:`/`scenario_requirements:`
  rows) that softens an otherwise-required uncovered row into an advisory
  finding naming a tracked child issue, instead of silently exempting it.
  Waivers are keyed per `(surface, proof_gate)` — equal to the reconciled row
  key — so waiving a family's `fault` (`ifa-fault-injection`) row never revokes
  credit for a proven `baseline` (`ifa-determinism`) row. The manifest is a
  CLAIMS LEDGER, not a roadmap: each required `(surface × scenario_type)` row
  must name the proof gate that runs it. SQL relationships has proven baseline
  and `delta_tombstone` rows under `ifa-determinism`; the matrix drives gen 2
  after gen 1 and checks the accumulated exact edge set. Its `fault` row is
  also proven (#5555 closed): `scripts/lib/ifa_fault_injection_sql_cells.sh`'s
  `cell_killworker_sql` and `cell_failgraphwrite_sql` provably target the
  `sql_relationship_materialization` / `sql_relationships` work item (a
  domain-scoped claimed-row precondition and a SQL edge MERGE anchor, not
  `CloudResource`).
- `materialized_edges_sql.go`'s `resolveSQLRelationshipMaterializedEdges` is
  the first family vacuity guard (`sql_relationships`): it asserts the
  hand-derived expected-edge-set fixture covers every
  `cypher.SQLRelationshipMaterializedEdgeTypes()` key, then reproduces it
  exactly by running the Odù's facts through the pure
  `reducer.ExtractSQLRelationshipRows` seam. The SQL-family Odù now proves all
  nine writer-registry types, including table-to-table `REFERENCES_TABLE` and
  routine-to-table `WRITES_TO`, in both its baseline and accumulated delta set.
  Because the expected type inventory is registry-derived, adding a tenth type
  without extending the Odù fails closed instead of preserving a stale count.
- `codeCallFamilyOdu` (`code_call_family_catalog.go`, #5991, unexported) - is the
  compiled, binary-portable Odù for the code-call family. Its facts and five
  hand-derived expected edges are pinned to the committed
  `testdata/cassettes/codecalls/` fixtures by a strict equality test, so changing
  either side alone fails closed. The determinism gate replays the cassette and
  exact-asserts all five live edges at N=1/2/4. The fault gate repeats that
  assertion after domain-scoped worker-kill recovery and a once-then-succeed
  graph-write fault. Together those gates prove all four code-call writer types
  and satisfy the family's baseline and fault manifest rows without waivers.
- `ExpectedEdge`, `LoadExpectedEdges`, `MaterializedEdgeDomainEdgeTypes`
  (`materialized_edges_assert.go`, #5351) - the exported surface `cmd/ifa`'s
  `assert-edges` verb uses for the LIVE, set-exact non-vacuity assertion: it
  loads the SAME hand-derived expected-edge-set fixture the pure vacuity guard
  consumes (so the live gate and the pure `go test` guard cannot drift on the
  format) and returns the family's registry edge types
  (registry-derived: each family's set comes from its own writer registry in
  `internal/storage/cypher`, never hand-listed here) so a live graph read knows
  which edges belong to the family. All fourteen umbrella families resolve as of
  #5543 - the multi-type ones (`sql_relationships`, `code_calls`,
  `inheritance_edges`, `repo_dependency`) through explicit arms, the rest from
  cypher's shared single-type table. An unregistered family returns an error
  rather than an empty set, so a caller fails closed instead of asserting
  nothing. This is what backs
  the `materialized_edges:sql_relationships`,
  `materialized_edges:code_calls`, `materialized_edges:documentation_edges`,
  and `materialized_edges:rationale_edges`
  manifest rows' `proof_gate` claims from inside the `ifa-determinism` and
  `ifa-fault-injection` live gates — digest equality
  across worker counts cannot catch a family silently
  empty in all cells; the absolute expected set can. The determinism gate
  asserts each baseline, drives the SQL and rationale generation-2 cassettes,
  and checks both delta outcomes before comparing N=1/2/4 graph digests.
  `ExpectedEdge` additionally carries `Identity map[string]string`: the
  relationship-property values, beyond the endpoint pair, that participate in
  an edge's MERGE identity for `codeowners_ownership_edges` and
  `submodule_pin_edges` (`cypher.MaterializedEdgeIdentityProperties`) —
  `Key()` appends them in sorted property-name order and is byte-identical to
  the pre-`Identity` key when `Identity` is empty, so every family with no
  declared identity needs no re-proof. `LoadExpectedEdges` now takes a
  `family` argument and validates every loaded edge's `Identity` key set
  matches the family's declaration exactly (a missing declared key, an
  undeclared key, or any `Identity` on a declared-empty family is a fixture
  error), non-blank identity-triple fields, and rejects an unknown JSON field
  in the fixture (`DisallowUnknownFields`). `assertMaterializedEdges`
  (`cmd/ifa/assert_edges.go`) mirrors the same validation against the LIVE
  graph: a declared identity property missing, non-string, or blank on a live
  edge is a loud identity defect, never silently keyed as `""`.

- `RationaleExpectedNodeRecord`, `RationaleExpectedEdgeRecord`, and
  `LoadRationaleExpectedEdgeRecords` (`materialized_edges_rationale.go`, #5998)
  extend the single rationale expected fixture with the complete source node,
  EXPLAINS relationship, and target node record used by the live CLI assertion.
  The loader rejects an empty or mixed repository scope and any disagreement
  between the fixture's top-level identities and nested raw properties before
  the graph backend is opened. Both live matrices drive the rationale cassette
  and exact-assert its full EXPLAINS records; the determinism matrix also drives
  generation 2 and checks the exact one-record survivor.

- `LoadDocumentationExpectedEdges` (`materialized_edges_documentation.go`,
  #5994) loads the exact three-edge DOCUMENTS set used by the live CLI
  assertion. Both live matrices drive the documentation cassette and
  exact-assert its three DOCUMENTS edges in baseline and domain-scoped recovery
  cells; the fault delta cell keeps those full graph records exact through its
  collateral comparison rather than a separate documentation assertion.
