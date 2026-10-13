# 7348 — file_import_cycles lifts to TypeScript/JavaScript packages

Validation record for the single behavior change that lifts `file_import_cycles`
from Python-only to Python plus TS/JS package cycles (split from #6851 per the
arbiter verdict there; epic #6833 out of scope).

## Design

- **Parser identity already carried — no parser change.** The task required
  checking what #7432/#7453 already carry before inventing anything. They carry
  all of it: the JS parser flags `import type` and per-specifier `type`
  modifiers with `ImportFlagTypeOnly` (`go/internal/parser/javascript/syntax/imports.go`,
  pinned by `TestDefaultEngineParsePathTypeScriptImportFlags`), and stamps every
  file with its owning npm package (`node_package_name`, the nearest
  `package.json` name, pinned by
  `TestDefaultEngineParsePathStampsNodePackageName` /
  `TestDefaultEngineParsePathOmitsNodePackageNameWithoutManifest`). No new
  parser code was written.
- **Projector-owned mapping.** The projector derives the file-to-package
  mapping from `parsed_file_data` (`node_package_name`) for
  javascript/typescript/tsx files only (`jsSourcePackage`) and stamps it onto
  every `IMPORTS` edge the file emits as `SourcePackage`. The writer persists
  it with an unconditional explicit-string `SET`, so a re-projected edge
  overwrites a stale package.
- **Shared collapse rule applied as-is.** For TS/JS rows the projector-owned
  package becomes both the collapse node key and the declared module, so
  `buildImportCycleGraph` and the walk run byte-unchanged: exact module-name
  equality joins packages, same-package files land on one node, relative
  specifiers match no package and close nothing, and a same-package edge
  (`destination == source`) is excluded by the existing skip. No shared file
  changed behavior: `enumerateImportCycles`, `buildImportCycleGraph`, and the
  flag partition are byte-identical.
- **Validation lift.** `language=typescript`, `tsx`, and `javascript` are
  accepted; every other non-empty, non-python language is still rejected. An
  unfiltered request stays Python-only, exactly as before: changing unfiltered
  semantics would be a second behavior change.
- **Legacy graphs.** A TS/JS edge with no `source_package` predates the mapping
  and is unplaceable, so it closes nothing. The drop is counted
  (`CycleEnumeration.TSPackageUnknown`,
  `coverage.cycle_ts_edges_missing_package` on TS/JS requests, span attribute
  `eshu.import_dependencies.cycle_ts_edges_missing_package`), never silent.
- **Response shape kept.** `cycle_length`, `cycle_path`, `cycle_edges` are
  unchanged; for TS/JS requests the file and module slots carry npm package
  names. TS/JS requests report `coverage.query_shape`
  `ts_package_import_cycle`.
- **Contract classification.** No fact kind or payload shape changes: the
  parser emits no new key, and `SourcePackage` is projector-internal
  (an `ImportRow` field, not a fact payload). No major/minor/patch bump
  applies; no cassette or B-12 snapshot change. `source_package` is a new
  graph edge property, which the reader treats as unknown-when-absent.

## Failing first

| Test | Before |
| --- | --- |
| `TestBuildFileImportCycleRowsFindsTwoNodeTSPackageCycle` (plus 3-/5-node, collapse, `TestCycleRowsFetchesTSEdgesForTSRequests`, per-language fetch) | no cycles: the reader dropped every non-python edge; `cycle_language` hardwired to `python` |
| `TestImportDependencyRequestAcceptsTSFileImportCycles` | `file_import_cycles currently supports python module-name cycle detection` |
| `TestBuildFileImportCycleRowsTSEdgesWithoutPackageAreDropped` | `coverage.cycle_ts_edges_missing_package` absent |
| `TestTSImportEdgesCarryFileLevelPackageIdentity` (plus unplaced, non-JS) | `SourcePackage = ""` (proved sensitive: blanking `jsSourcePackage` fails it; restored) |
| `TestCanonicalNodeWriterCarriesImportSourcePackage` | statement missing the `SET` (proved sensitive: removing the `SET` fails it; restored) |
| `TestImportDependencyInvestigationSpanCarriesTheCycleWalkSignals` (new attr) | attribute missing from the span |

The diamond, type-only, same-package, and relative-specifier negatives pass
before and after: they guard against false positives the new path could
introduce. Each mutated file was restored; `git status` shows only the intended
files.

## Graph truth

- `TestTSPackageCyclesFixtureTruthAgreement` drives the tracked fixture
  (`tests/fixtures/ecosystems/ts_import_cycles`, five scenarios with disjoint
  `@cycles-<scenario>/*` package families: 2-package cycle with two files in
  one package, 3-package cycle, 5-package cycle, diamond, type-only negative)
  through the production parser, production `BuildMaterialization`, and
  production `BuildFileImportCycleRows`: 17 files parsed, full package-mapping
  density, the type-only edge present in the projection, exactly the [2 3 5]
  cycles, no diamond/type-only member, rotation-normalized starts,
  `query_shape` `ts_package_import_cycle`, zero missing-package.
- `TestFileImportCyclesTSTruthAgainstARealGraph`
  (`go/internal/replay/offlinetier/ts_package_cycles_live_test.go`,
  env-gated, registered in `specs/live-tests.d/live-tests-03.yaml`) seeds real
  TS edges plus one legacy edge and reads them through the production
  `imports.CycleRows` and response shaper: NOT_CHECKED locally (no live
  backend in this lane; the gate `scripts/verify-live-tests-ledger.sh` passes
  with the new row, count 572).
- Golden corpus: no cassette or snapshot change (the lift reads existing
  `IMPORTS` edges plus the new property; no new fact kind). The
  `golden-corpus-mirror` unit check (`go test
  ./cmd/golden-corpus-gate/`) is part of final verification below.

## Performance and scale

- **Corpus measurement (remote, NOT_CHECKED in this lane).** The largest TS
  corpus, portal-nextjs-platform (~9,874 `IMPORTS` edges reported), fits the
  25,000-row scan bound, so no scan-bound work is needed. The required remote
  proof — `FileImportCycleEdgeRowsCypher` under `PROFILE` per TS language
  (rows vs the 25,000 bound), enumeration `steps_examined` vs the 250,000-step
  budget with the 10x margin bar (≤25,000 steps), and wall time on the
  dedicated Neo4j host — still needs a remote run and is the documented
  follow-up on #7348.
- **In-process probe at corpus magnitude (measured, this lane).** A synthetic
  npm-package graph with realistic monorepo sparsity (mostly-DAG forward edges
  plus one 2-cycle and one 3-cycle) through the production
  `BuildFileImportCycleRows`: 1,000 packages / 9,610 edges → 2 cycles,
  **9 steps examined of the 250,000-step budget (27,000x margin, bar is 10x)**,
  `truncated:false`, wall 12.9 ms; 400 packages / 7,481 edges → 2 cycles,
  9 steps, 17.2 ms. The SCC prefilter leaves the acyclic bulk nothing to
  walk, so cost is flat in corpus size for realistic shapes.
- **Adversarial shape (measured, disclosed).** A fully strongly-connected
  400-package / 9,874-edge graph exhausts the 250,000-step budget in ~23 ms
  and reports `truncated:true` with stop reason `step_budget` — the designed
  shared-walk behavior (never a silent partial list), identical for Python;
  not a TS-specific regression. Real npm package graphs are sparse; the
  remote corpus run above settles the production shape.
- **Write-path change (no-regression decision).** The writer adds one
  same-row `SET` property to the existing `UNWIND`-batched idempotent edge
  statement (MERGE identity, anchor, batch shape, and indexes unchanged), and
  the read adds one projected column to the same bounded fetch. Per
  cypher-query-rigor this trades a full bench for a no-measurable-regression
  check: focused writer/projector/query suites (below) are green with no
  timing change, and the query-plan pins re-verified green.

No-Regression Evidence: `go test` green with no timing change for the
touched packages (`internal/query/codequery/imports`,
`internal/query/codemodel`, `internal/query/codequery` including the span
test, `internal/projector/canonical`, `internal/storage/cypher`,
`internal/parser/javascript` flag and package-name gates, `internal/query`
queryplan pins plus methodology, `internal/queryplan`); the hot-path Cypher
diff is one appended same-row `SET` property and one appended `RETURN`
column on statements whose anchor, MERGE identity, batching, and bounded
`LIMIT $scan_limit` are unchanged, so no plan-shape change is expected and
the query-plan regression pins re-verified green at HEAD.

Observability Evidence: the new span attribute
`eshu.import_dependencies.cycle_ts_edges_missing_package` (zero for
non-TS/JS reads) is pinned by
`TestImportDependencyInvestigationSpanCarriesTheCycleWalkSignals`; the
matching `coverage.cycle_ts_edges_missing_package` response field is pinned
by `TestBuildFileImportCycleRowsTSEdgesWithoutPackageAreDropped` and the
fixture agreement test; the route observability list in
`docs/public/reference/http-api/code.md` names the new attribute.

## Verification (final)

- `go build ./...`, `go vet ./...`: exit 0 (with `GOTOOLCHAIN=go1.26.9`).
- Focused suites green: `internal/query/codequery/imports`,
  `internal/query/codemodel`, `internal/query/codequery` (incl. span),
  `internal/projector/canonical`, `internal/storage/cypher`,
  `internal/parser/javascript` (flag + package-name gates),
  `internal/query` (queryplan pins + methodology), `internal/queryplan`.
- Query-plan pins re-measured at HEAD (variant family, safe family,
  handler/hot manifests, source coverage, cycle-edges contract): green.
- `bash scripts/verify-live-tests-ledger.sh`: ok (727 rows classified).
- `gofumpt` on changed Go files: clean. License headers on new `.go` files:
  present.
- NOT_CHECKED: `make pre-push` / `make pre-pr` (coordinator owns them), live
  Docker/NornicDB/Postgres lanes, remote Neo4j PROFILE corpus measurement,
  full CI `required-gates-complete`.
