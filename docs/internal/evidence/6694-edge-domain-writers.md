# #6694 edge/writer leaf 2: domain evidence writer move evidence

## Moved (behavior-preserving, `git mv` + package/qualifier edits only)

The five remaining root domain edge/evidence writers now live in the existing
`edge/writer` leaf (`package writer`, external alias `edgewriter`):

- `code_interproc_evidence.go` (+ `_test.go`)
- `code_taint_evidence.go` (+ `_test.go`)
- `incident_routing_evidence.go` (+ `_test.go`)
- `kubernetes_correlation.go` (+ `_test.go`, `_bench_test.go`)
- `observability_coverage.go` (+ `_test.go`, `_bench_test.go`)

Plus their contract tests, which reference the moved symbols and stay
in-package per the leaf `AGENTS.md` (`package writer`, never the parent-test
fakes):

- `retract_registry_contract_test.go` (sole-creator/attacher census; walk root
  `".."` → `"../.."`, still covering `go/internal/storage/`; basename
  comparison survives the move by design)
- `code_evidence_retract_dispatch_test.go` (unchanged behavior)
- `code_evidence_bolt_retract_test.go` (harness block deleted: it was
  byte-identical to the leaf `bolt_harness_test.go` copy, which now names the
  restored root `bolt_harness_test.go` as its duplication source)

Shared-helper handling (issue's "split shared helpers before moving leaves"):

- `ChunkStrings` relocated root-internally
  (`code_interproc_evidence.go` → `statement_chunk.go`), same package,
  same signature. Zero caller changes: the four `cloud/*` leaf consumers and
  the moved writers keep resolving `cypher.ChunkStrings`.
- The moved files' only other cross-file unexported deps
  (`validateStaleEvidenceRetractInputs`, `positiveEvidenceCleanupLimit`) move
  with them in `code_taint_evidence_writer.go`. No root stayer references a
  moved symbol (verified by full-module build).

Root keeps its own bolt harness (`bolt_harness_test.go`, restored verbatim
from the pre-move file modulo the trailing blank line, which gofumpt drops
at EOF): the remaining root live tests
(`canonical_index_key_live_test.go`, `canonical_moved_block_live_test.go`,
`harness_test.go`, `tfstate_state_match_edge_live_test.go`) still resolve
`boltRetractTestRunner` in-package. No new duplication: the leaf copy serves
the moved evidence tests, the root copy serves the canonical/tfstate/index
tests, and both copies are byte-identical.

Repoints (direct caller migration, no root facade, per the #6694 lane
precedent):

- `go/cmd/reducer/canonical_graph_writers.go`: 5 field types + 5 constructors
  `sourcecypher.*` → `edgewriter.*` (`edgewriter` already imported).
- `edge/materialized`: `direct_families.go` (5 template vars + import swap),
  `single_type_families.go` (14 template vars; `edgewriter` already
  imported), `retract_contract_test.go` (4 vocabulary calls + import swap),
  `held_variants_test.go` (4 walked paths `../..` → `../writer`),
  reason strings cite `edge/writer/<file>.go` (citation regex admits
  subpaths; guard resolves).
- `internal/ifa/materializededges`: `assert.go`, `incident_routing_evidence.go`
  (vocabulary calls; unused root import dropped),
  `direct_family_coverage_test.go` (import swap + stale path comment).
- `internal/replay/{offlinetier,costcounting}`: constructor/type repoints +
  `edgewriter` imports; live tests skip without backends exactly as before.
- `cloud/resource` tests (3 files): constructor repoints + import swaps.
- `unwind_row_keys_live_test.go` (`package cypher_test`): constructor repoint.
- Prose path repoints: `specs/ifa-materialized-edge-coverage-direct.v1.yaml`
  (10 reasons), ifa `testdata/*/ifa-*-expected-edges.json` notes (5),
  `collector-extraction-policy.md`, `391-observability-coverage-correlation.md`
  (path + `edgewriter.` qualifier), `infra_relationship_filter_test.go`
  comment, `scripts/lib/ifa_family_registry{,_pins}/kubernetes_correlation*`
  (`:51` → `:56` at the new path).
- `internal/queryplan/testdata/statement-builders.yaml`: 5 new file entries,
  14 builders re-derived from discovery output (byte-identical templates,
  new `source_sha256` over qualified source). No exempt added: execution
  proof stays with the CI B-7/B-12 replay; any `never-executed` finding there
  takes the standard zero-executions exempt with the capture as evidence.
- Package docs: leaf `doc.go`/`README.md` name the five writers; root
  `AGENTS.md` evidence commands point at the leaf package.

## No-Regression Evidence (move-only, statement-identical):

Baseline: pre-move files at the rebase base (`git mv` records renames;
a literal-diff of the 5 impl + 10 moved test files shows zero Cypher/string
literal changes — only package clauses, the `sourcecypher` import, qualifier
prefixes, gofumpt alignment, the registry walk root, and the deleted
byte-identical harness duplicate). After:

- `go build ./...` — clean (full module, incl. all repointed callers).
- `go vet` on the cypher tree, `internal/ifa/...`,
  `internal/replay/{costcounting,offlinetier}`, `cmd/reducer`,
  `internal/queryplan` — clean.
- `go test -count=1 ./internal/storage/cypher/
  ./internal/storage/cypher/edge/writer/
  ./internal/storage/cypher/edge/materialized/
  ./internal/storage/cypher/cloud/resource/` — ok (root, leaf incl. all
  10 moved test files + the new location pin, materialized incl. the two
  path-pinned guards, cloud resource).
- `go test -list` on the leaf discovers every moved test
  (`TestCodeInterprocEvidence*`, `TestCodeTaintEvidence*`,
  `TestIncidentRoutingEvidence*`, `TestKubernetesCorrelationEdgeWriter*`,
  `TestObservabilityCoverageEdge*`, `TestEvidenceNodeSoleCreatorsCensus*`,
  `TestBoltRetract*`).
- `go test -count=1 ./internal/ifa/materializededges/
  ./internal/queryplan/ ./internal/backendconformance/
  ./internal/reducer/{kubernetescorrelation,incident,obscoverage}/
  ./cmd/reducer/ ./cmd/ci-gates/ -run
  TestEveryDirectWriterFileSelectsTheCoverageGate` — ok.
- Cost-budget tests for both repointed families pass; offlinetier live tests
  skip without backends exactly as before (DSN-gated, unchanged).
- `go test ./internal/storage/cypher/edge/writer -run
  TestDomainFamiliesLiveInEdgeWriterLeaf` — red before the move (8 undefined
  symbols), green after.
- Dirgate: root `internal/storage/cypher` 61 → 56 non-test files (shrink, no
  trailer); leaf `edge/writer` 21 → 26 (under the 40 cap); no naming
  collisions added (leaf has no subpackages).
- Queryplan manifest test green after re-derivation; B-7/B-12 byte-identical
  replay stays a CI gate (no live backend locally).
- `gofumpt`-clean (via `gofmt -l`; import grouping + map alignment only);
  `git diff --check` clean; every touched file < 500 lines.

Input shape: unchanged reducer projection rows; batching, dispatch
(sequential vs grouped), retry wrapping, and retract predicates are the same
compiled code at a new path. The only production wiring change is the import
path in `cmd/reducer` + `ifa/materializededges`, covered by `go build` and
the suites above.

## No-Observability-Change:

No telemetry, metrics, spans, structured logs, status surfaces, or pprof
endpoints are added, removed, renamed, or re-labeled by this move: the moved
files contain no telemetry surfaces beyond the existing `Statement` metadata
keys (unchanged values), executor dispatch, retry wrapping, and failure
logging, all preserved. Telemetry coverage for the writer paths is unchanged.

## Still in root (later #6694 slices)

Per-file census on this base: the remaining root writers are `CanonicalNodeWriter`
methods (tfstate, OCI registry), Statement-seam adapters over nested leaves
(canonical `atlantis`/`codeowners`/`deployable-unit`/`gitlab`/`helm-template`/
`invokes-cloud-action`/`kustomize`/`runs-in`/`submodule`, crossplane,
package-registry, phase-group metadata is shared phase constants), shared
contracts (writer/executor/retry/chunk/row helpers, retract registries), and
domain node writers with no target leaf yet (`kubernetes_*_node_writer`,
`module_identity`, `posture_node_existence`, code-evidence node writers are
already here). The dirgate cap row (107) is write-once and cannot be
re-pinned down; the delta rule guards every merge while the root shrinks.
