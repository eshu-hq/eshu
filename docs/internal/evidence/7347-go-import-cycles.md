# 7347 — file_import_cycles lifts to Go packages

Validation record for the single behavior change that lifts `file_import_cycles`
from Python-only to Python plus Go package cycles (split from #6851 per the
arbiter verdict there; epic #6833 out of scope).

## Design

- **Projector-owned mapping.** The Go parser reports each file's own package
  import path (`package_import_path`, from the collector's per-directory
  prescan), absent without package identity. The projector derives the
  file-to-package mapping from `parsed_file_data` (file-level key first, then
  the unanimous functions-bucket value so re-projection needs no
  re-collection) and stamps it onto every `IMPORTS` edge as `source_package`.
  The writer persists it with an unconditional explicit-string `SET`, so a
  re-projected edge overwrites a stale package.
- **Shared collapse rule applied as-is.** For Go rows the projector-owned
  package becomes both the collapse node key and the declared module, so
  `buildImportCycleGraph` and the walk run byte-unchanged: exact module-name
  equality joins packages, same-package files land on one node, and a
  same-package edge (`destination == source`) is excluded by the existing
  skip. No shared file changed behavior.
- **Validation lift.** `language=go` is accepted; every other non-empty,
  non-python language is still rejected. An unfiltered request stays
  Python-only, exactly as before: changing unfiltered semantics would be a
  second behavior change.
- **Legacy graphs.** A Go edge with no `source_package` predates the mapping
  and is unplaceable, so it closes nothing. The drop is counted
  (`CycleEnumeration.GoPackageUnknown`, `coverage.cycle_go_edges_missing_package`
  on Go requests, span attribute
  `eshu.import_dependencies.cycle_go_edges_missing_package`), never silent.
- **Response shape kept.** `cycle_length`, `cycle_path`, `cycle_edges` are
  unchanged; for Go requests the file and module slots carry package import
  paths. Go requests report `coverage.query_shape` `go_package_import_cycle`.

## Failing first

| Test | Before |
| --- | --- |
| `TestBuildFileImportCycleRowsFindsTwoNodeGoPackageCycle` (plus 3-/5-node, collapse, `TestCycleRowsFetchesGoEdgesForGoRequests`) | no cycles: the reader dropped every non-python edge |
| `TestImportDependencyRequestAcceptsGoFileImportCycles` | `file_import_cycles currently supports python module-name cycle detection` |
| `TestBuildFileImportCycleRowsGoEdgesWithoutPackageAreDropped` | `coverage.cycle_go_edges_missing_package` absent |
| `TestGoImportEdgesCarryFileLevelPackageIdentity` (plus fallback, precedence) | `SourcePackage = ""` |
| `TestDefaultEngineParsePathGoEmitsFilePackageIdentity` | `package_import_path = <nil>` |
| `TestCanonicalNodeWriterCarriesImportSourcePackage` | statement missing the `SET` |
| `TestImportDependencyInvestigationSpanCarriesTheCycleWalkSignals` (new attr) | attribute missing from the span |

The diamond, type-only, and same-package negatives pass before and after:
they guard against false positives the new path could introduce. The fixture
integration test fails on a production mutation (mapping blanked) and passes
restored. Each mutated file was restored; `git status` shows only the intended
files.

## Graph truth

- `TestGoPackageCyclesFixtureTruthAgreement` drives the tracked fixture
  (`tests/fixtures/ecosystems/go_import_cycles`, module `example.com/cycles`:
  2-package cycle with two files in one package, 3-package cycle, 5-package
  cycle, diamond) through the production parser,
  production `BuildMaterialization`, and production `BuildFileImportCycleRows`:
  exactly cycles of length [2 3 5], rotation-normalized, no diamond member in
  any path, full mapping density, `GoPackageUnknown == 0`. The type-only
  negative has no fixture (the Go parser never emits one) and is covered with
  a synthetic row.
- `TestFileImportCyclesGoTruthAgainstARealGraph` (offlinetier, env-gated)
  seeds real Go edges with `source_package` plus one legacy edge and reads
  them through production `CycleRows`: SKIPPED here (no Docker daemon on this
  host), registered in `specs/live-tests.d/live-tests-03.yaml` as scheduled
  with the exemption inventory re-pinned to count 572. For CI.
- B-7/B-12: no cassette or snapshot change. The change adds an edge property,
  not an edge or node: `IMPORTS`/`Module`/`File` counts are untouched, and the
  golden-corpus unit gate plus the static gate script pass (full live run not
  done here: no Docker, and a peer holds the live-gate lock).

## Claim table (measured at HEAD `22ba1ed13`, worktree `7347-go-import-cycles`)

| Claim | Command | Measured | Match |
| --- | --- | --- | --- |
| Real repo `go/` package graph enumerates within budgets | scratch probe (deleted after): `go list -e -json ./...` over `go/` shaped to edge rows, production `BuildFileImportCycleRows`, `language=go` | packages=1238, edge_rows=12545, cycles=0, steps=0/250000, wall 13.53 ms, truncated=false | yes |
| 233-edge cyclic proxy keeps a 10x step margin | same probe, synthetic clustered 233-edge package graph with a planted 5-cycle | rows=233, page=201 (walk hit the 1000-cycle cap), steps=11507/250000 (21.7x margin), wall 2.23 ms | yes |
| Edge-fetch rows fit the 25,000-row scan bound | static: reported Go max 233; measured repo 12545 | 233/25001 (107x), 12545/25001 | yes |
| Wall time fits the 250 ms ceiling | same runs | 13.53 ms, 2.23 ms | yes |
| Shared collapse rule unchanged | `git diff` on `code_import_dependencies_cycles.go`, `code_import_dependencies_cycle_rows.go`, `code_import_dependencies_flags.go`, `code_import_dependencies_walk_test.go` | empty | yes |
| Python behavior unchanged | full package runs for codemodel, codequery/..., golang parser, canonical projector, cypher writer, query, queryplan, mcp, openapi, replay/offlinetier | all ok | yes |

The `go list` probe labels its provenance: it measures the production
collapse plus enumeration over a real 1,238-package graph, not the parser or
the remote `PROFILE` the arbiter ruling asks for. The remote Neo4j `PROFILE`
(edge rows vs the scan bound, steps vs budget, wall time on the largest Go
corpus) is NOT_CHECKED here: no remote host in this lane, no Docker on this
host. The methodology live contract (`TestImportDependencyMethodologyLive`)
and its payload calibration (one more projected column per row) need a live
re-run at PR time; the four refreshed `query_sha256` values are recomputed
production texts, verified by the unit registry test, not re-measured budgets.

## Performance Evidence:

Query shape: `FileImportCycleEdgeRowsCypher`, the same anchored bounded read
`(Repository {id})-[:REPO_CONTAINS]->(File)-[rel:IMPORTS]->(Module)` with
`LIMIT $scan_limit` (25,001), now projecting one more raw relationship
property (`rel.source_package`). Anchor, predicate, ordering, and scan bound
are unchanged, so the plan shape the `QP-CODE-IMPORT-CYCLES` entry pins is
unaffected apart from its hashes (refreshed with the recomputed production
values; reachable-request count 488 and variant count 280 unchanged). No new
scan, expand, or eager operator is possible from a trailing `RETURN` column;
the live `PROFILE` re-proof is deferred to the owner's backend (see above).

## No-Regression Evidence:

The in-process runs above: 12,545 real package-import rows enumerate in
13.53 ms untruncated, and a 233-edge adversarial cyclic proxy closes its cap
in 2.23 ms with 11,507 of 250,000 steps. Per-edge work added is one string
copy plus one map lookup in the projector and one string compare per row in
the reader, linear in the row count.

## Observability Evidence:

The `query.import_dependency_investigation` span gains
`eshu.import_dependencies.cycle_go_edges_missing_package` (zero for non-Go
reads), asserted by the span test; the response carries the same count as
`coverage.cycle_go_edges_missing_package` on Go requests. OpenAPI, the MCP
tool description, the HTTP reference, the Go language doc, and the codemodel
README are updated to the same contract.

## Contract classification

No fact-kind or fact-payload change: `parsed_file_data` stays an untyped
object (file schema allows additional properties), the new parser key is
absent without package identity, `ImportRow.SourcePackage` is
projector-internal (minor, additive, in the spirit of #7345), and the
`IMPORTS.source_package` property is projected graph, not a fact payload.
`verify-factschema-diff.sh` exits 0 (no breaking changes) and
`verify-payload-usage-manifest.sh` exits 0. The MCP/OpenAPI change is
description text only: no new tool, no changed argument or response shape.
`
...[truncated 473 chars]
