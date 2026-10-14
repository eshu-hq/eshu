# 7349: Java package lift of file_import_cycles + 34k-edge enumeration tuning

## Decision

`file_import_cycles` answers Java at package level (`language=java`). A Java
import names a fully qualified class while a cycle node is a package, so both
endpoints derive as packages: the source from the projector-owned
`source_package` edge property, the target by trimming the imported class
name. The shared collapse rule and the shared walk run byte-unchanged. The
Java fetch collapses to distinct package pairs in the statement, which keeps
the flagship repository (3,387 files, 34,563 `IMPORTS` edges) under the
25,000-row scan bound. Unfiltered requests stay Python-only.

Scan-bound design decision (the still-required item from the #6851 sweep):
collapsed package-edge fetch in Cypher, not a bound raise. The 25,000-row
ceiling is untouched. A file-level fetch returns 34,563 rows on the flagship
repository and fails closed with `ErrImportDependencyScopeTooBroad`; the
collapsed fetch returns the distinct package pairs (3,903 on the committed
deterministic flagship model, 6.4x under the ceiling), so the flagship
answers. The remote `PROFILE` on the largest repo stays env-gated for CI
(no Docker on this host) and is reported `NOT_CHECKED`.

## Layering (mirrors #7347)

Parser identity (`package_name`, conditional) -> projector carriage
(`SourcePackage`, Java-only) -> writer edge property (`source_package`,
unconditional SET) -> collapsed fetch + package-keyed decode ->
Validation lift -> fixtures -> docs lockstep.

## Failing-first record

- `TestJavaPackageCyclesParserFlags`: failed on `package_name = <nil>`
  before the parser change; green after. The default-package absence test
  passed throughout and guards the conditional emit.
- Projector membership tests: failed to build (`ImportRow has no field
  SourcePackage`) before the mapping; green after.
- Writer test: failed on `source_package = <nil>` before the statement and
  parameter change; green after.
- `TestImportDependencyRequestAcceptsJavaFileImportCycles`: failed on
  `Validate() error ... want nil` before the lift; green after.
- `TestCollapseJavaPackageCycleRowsFoldsFileEdges`: failed twice during
  development — once on an on-demand double-trim in `javaTargetPackage`
  (fixed in both the Go derivation and the Cypher), once on the fold test
  itself; green after.
- Fixture truth test: would report no cycles pre-lift (the decoder dropped
  every non-Python edge); green after.

## Graph truth (three-way agreement)

`TestJavaPackageCyclesFixtureTruthAgreement` parses 26 real Java sources
with the production parser, projects with production `BuildMaterialization`,
and reads with production decode plus the response shaper. Deep requests
(`MaxCycleLength: 7`) report exactly lengths [2 3 5 7]; default requests
report exactly [2 3 5]; diamond and fan-in fixtures close nothing; rotation
starts at the smallest package; `query_shape` is
`java_package_import_cycle`; `cycle_java_edges_missing_package` is 0.
`TestCollapseJavaPackageCycleRowsChangesNoAnswer` pins collapse-location
invariance: file rows decoded directly and folded rows report identical
cycles. The env-gated live test
(`TestFileImportCyclesJavaTruthAgainstARealGraph`, `NOT_CHECKED` locally)
proves the collapsed statement end to end in CI, including the on-demand
leg, the static-member conservative miss, the diamond negative, and the
pre-filtered legacy edge.

## Tuning: theory first, then work

Theory record, stated before any tuning code: stage = in-process DFS over
the package-collapsed digraph at `maxLength` 7; metric = steps examined vs
the 250,000-step budget; baseline = walk code at the branch base;
worst-case partition = top-1 package fan inside a strongly connected
component; bar = 10x margin (worst <= 25,000 steps completes); stop = any
tuning that changes cycle output or serializes work is rejected.

Proven with a throwaway shim (deleted; numbers below), then committed as
gates:

- Length-batched (iterative-deepening) traversal: DISPROVEN. 1.76x worse
  than DFS on layered Java-scale models and it regressed a cap-stop into a
  budget-stop on the uniform stress. Never implemented. The 300/300
  agreement check it ran confirmed exactness would have held, but the
  performance theory failed, so no traversal change shipped. The shared
  walk is untouched.
- All realistic truncations are `cycle_cap` stops, never `step_budget`
  stops: the walk finds 1,000 cycles slowly on cycle-dense graphs; the
  250,000-step budget is never the binding constraint. Retuning the budget
  cannot manufacture completion where more than 1,000 cycles exist.
- Default depth 5 completes with a 14.1x margin at flagship scale
  (committed gate `TestImportCycleStepBudgetCoversJavaPackageScale`: worst
  17,694 steps over 30 placements; length 7 sees zero budget stops over 25
  placements). No walk change is needed for the default path; deep
  requests may cap-truncate with a named reason, by design.
- Fetch layer (committed gate
  `TestCollapseJavaPackageCycleRowsKeepsFlagshipScaleUnderTheScanBound`):
  34,563 file rows collapse to 3,903 package pairs, under the scan bound.

Before/after on identical boundaries (largest-java-repo shape, 34,563
file rows; 250,000-step budget; up to 7-node cycles):

| Claim | Command (at final HEAD) | Measured | Match |
|---|---|---|---|
| Java rejected before, accepted after | `go test ./internal/query/codemodel/ -run TestImportDependencyRequestAcceptsJavaFileImportCycles` | PASS (failed pre-lift) | yes |
| 34,563 file rows collapse under the scan bound | `go test ./internal/query/codemodel/ -run TestCollapseJavaPackageCycleRowsKeepsFlagshipScaleUnderTheScanBound -v` | 3,903 pairs (< 25,000) | yes |
| Default-5 walk completes with >= 10x margin | `go test ./internal/query/codemodel/ -run TestImportCycleStepBudgetCoversJavaPackageScale -v` | worst 17,694 steps (14.1x) | yes |
| Length-7 walk never budget-stops on layered shapes | same gate | 0 of 25 budget stops | yes |
| Fixture intent == graph truth == query truth | `go test ./internal/query/codequery/imports/ -run TestJavaPackageCyclesFixtureTruthAgreement -v` | lengths [2 3 5 7], negatives absent | yes |

## Benchmark Evidence

Benchmark Evidence: the committed walk-scale gate measures the default-5
walk at flagship scale on this host (worst 17,694 steps over 30 placements,
14.1x under the 250,000-step budget; length 7 sees zero budget stops over
25 placements), and the fetch-layer count gate measures 34,563 file rows
collapsing to 3,903 package pairs. No-Regression Evidence: the shared walk,
collapse, and budget are byte-unchanged, so Python reads keep their proven
behavior; the collapsed Java fetch returns fewer rows than the file fetch
on every input (distinct pairs vs file rows), so fetch-adjacent work only
shrinks; the statement adds one aggregation over the same repository
anchor, grant predicates, ordering, and `$scan_limit`. Baseline: walk and
fetch code at the branch base `460682360` (post-#8086 main); after: this
change; backend: no live backend on this host (in-process enumeration plus
row folding); terminal counts: 3,903 pairs, worst 17,694 steps, zero budget
stops. The counts were first measured on the pre-rebase tree and re-verified
on the rebased tree by the passing flagship scale tests
(`TestCollapseJavaPackageCycleRowsKeepsFlagshipScaleUnderTheScanBound`,
`TestCollapseJavaPackageCycleRowsFoldsFileEdges`,
`TestCollapseJavaPackageCycleRowsChangesNoAnswer`); the rebase union did not
touch the measured Java paths.
Wall-time profiling of the statement (`PROFILE`, DB hits, latency) is
env-gated for CI: no Docker on this host. The new Cypher uses only core
scalar functions (`count`, `min`, `substring`, `size`, `split`, `last`,
`right`, `CONTAINS`, `CASE`); `last`/`split`/`right` have no precedent in
the repo's graph queries, so the CI NornicDB legs are the dialect proof.
If a blocking NornicDB leg rejects the statement, the documented fallback
is a projector-resolved `target_package` edge property with a trim
fallback, which removes all string functions from the fetch.

## Observability Evidence

Observability Evidence: no new runtime stage, worker, queue, lease, or
batching change — the lift reuses the existing fetch path, and the
collapsed statement runs under the existing bounded-read telemetry (the
`cycle_steps_examined` and edge-flag span attributes plus the handler
telemetry asserted by the span test). One new span attribute,
`eshu.import_dependencies.cycle_java_edges_missing_package`, pinned by the
span test; one new coverage key,
`coverage.cycle_java_edges_missing_package`, asserted by unit and fixture
tests. No-Observability-Change beyond those two named signals.

## Contract classification

No fact kind, payload shape, `factschema`, collector, or registry change:
the parser's `package_name` is a file-payload top-level key that flows
opaquely (the `DecodeParsedFileDataImports` seam reads only the `imports`
bucket; the JavaScript `node_package_name` precedent shipped the same
way). The `source_package` edge property is additive graph metadata, not a
fact contract. Classification: patch (docs only from the contract
system's view). `factschema-diff`, `payload-usage-manifest`, and
`fact-kind-registry` gates stay green; golden-corpus cassettes and the B-12
snapshot are untouched (the new fixtures sit outside the gate corpus, as
in #7347).

## Merge notes (sibling lifts #7347/#7348, both merged into the base)

- `ImportRow.SourcePackage`, the writer `source_package` SET/param, the
  `cycle_language` CycleRows line, and the queryplan/testdata pins were
  intentionally byte-parallel with the sibling branches so the rebase was a
  union, not a redesign. Resolved as the three-way union on the #8086 merge.
- `Validate`'s language line, the `JavaPackageUnknown`/`GoPackageUnknown`/
  `TSPackageUnknown` fields, the MCP/OpenAPI language lines, the `code.md`
  cycle paragraphs, and the live-tests ledger count/digest conflicted
  textually and were unioned. `EffectiveCycleLanguage` lives only in
  `request.go` (the duplicate in the Java file was removed at rebase).
- The methodology rewrite declaration moved 7348 to 7349 with the exact
  5-file pinned-reader set this diff rewrites, per the declared procedure.
- Over-cap files from the union were split with no behavior change
  (`javaSourcePackage` to `import_extract_java.go`,
  `countJavaEdgesMissingPackage` to `code_import_dependencies_java_cycles.go`).
- Ledger re-pinned to count 574 for the rebased tree.

## Unresolved / NOT_CHECKED

- Remote `PROFILE` of the collapsed fetch on the largest repo (needs the
  Neo4j backend and the remote corpus): NOT_CHECKED, env-gated for CI.
- Live backend proofs (offlinetier Java test, methodology runner,
  NornicDB dialect legs): NOT_CHECKED locally, registered in the ledger
  for CI.
- The parent's "in batches of at most 10" has no determinable referent in
  the fetch, walk, writer, or ledger code; no batching semantic was found
  or introduced. Named unresolved rather than guessed.
