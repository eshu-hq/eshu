# #6694 kubernetes node-writer leaf: move evidence

## Moved (behavior-preserving, `git mv` + package/qualifier edits only)

The two remaining root Kubernetes node writers now live in the new
`storage/cypher/kubernetes` leaf (`package kubernetes`, external alias
`k8snodewriter`):

- `namespace.go` (+ `namespace_test.go`, `namespace_reconcile_test.go`) —
  from `kubernetes_namespace_node_writer.go` (+ root reconciliation test)
  and `edge/writer/kubernetes_namespace_node_test.go`
- `workload.go` (+ `workload_test.go`, `workload_bench_test.go`) — from
  `kubernetes_workload_node_writer.go` (+ tests)

Plus the leaf contract proof and harness:

- `families_test.go` (`TestKubernetesNodeWritersLiveInLeaf`: both
  constructors resolve in the leaf, all three sibling-read templates stay
  exported with their `UNWIND` batching shape) — red before the move
  (undefined symbols), green after
- `harness_test.go` (leaf-local `recordingExecutor`,
  `recordingGroupExecutor`, `sqlSequentialRecordingExecutor`,
  `noopGroupExecutor` duplicates with duplication-source comments)
- Doc trio: `doc.go`, `README.md`, `AGENTS.md`

Type renames (binding `docs/internal/naming.md` no-stutter rule, cf.
`correlation.Writer`): `KubernetesNamespaceNodeWriter` →
`kubernetes.NamespaceNodeWriter` (`NewNamespaceNodeWriter`),
`KubernetesWorkloadNodeWriter` → `kubernetes.WorkloadNodeWriter`
(`NewWorkloadNodeWriter`). Method names, template constant names, and test
function names are unchanged. Reducer-owned consumer interfaces keep their
names (structural satisfaction, untouched).

Shared-helper handling (issue's "split shared helpers before moving leaves"):

- `buildBatchedRetractStatements` is shared with a root stayer
  (`canonical_node_writer_retract.go`), so it stays in root and the leaf
  carries an unexported copy (flux-seam A1 precedent). The copy is named
  `buildBatchedNamespaceRetractStatements` because the ifa direct-family
  scan merges every scanned leaf into one key space keyed by bare function
  name — a same-named copy fatals that guard
  (`direct_family_scan_types_test.go`, extended to the leaf in this move).
- All other cross-file deps (`Executor`, `GroupExecutor`, `Statement`,
  operation/metadata keys, `DefaultBatchSize`, `BuildBatchedStatements`,
  `WrapRetryableNeo4jError`) are shared parent contracts resolving through
  the `sourcecypher` import. No root stayer references a moved symbol
  (verified by full affected-tree build + vet).

Repoints (direct caller migration, no root facade, per the #8640/#8675
precedent):

- `go/cmd/reducer/canonical_graph_writers.go`: 2 field types + 2
  constructors `sourcecypher.*` → `k8snodewriter.*`.
- `edge/materialized`: `single_type_families.go` (2 template consts +
  import; reason strings name templates, not paths).
- `internal/ifa/materializededges`: `direct_family_scan_types_test.go`
  (scan patterns + package switch + policy comment),
  `kubernetes_namespace_environment.go` (2 qualifier comments).
- `internal/ifa/familyodu`: 1 qualifier comment.
- `internal/replay/{offlinetier,costcounting}`: 3 constructor repoints +
  `k8snodewriter` imports; live tests skip without backends exactly as
  before; cost-budget JSON prose repointed.
- `internal/query/impact/deployment`: 3 path comments.
- `internal/graph`: `schema_tables.go`, `schema_tables_indexes.go`,
  `README.md` (2) path comments.
- `graphschemacompat/AGENTS.md`, `module-node-identity-6106` evidence doc:
  `:89` → `kubernetes/namespace.go:91` (verified line of the edge MERGE).
- `local-performance-envelope.md`: `workload.go:24` (line verified).
- `evidence-5434-namespace-environment-retract.md` (root): type + path.
- `reducer/telemetry.md`: benchmark package path.
- Root `cypher/README.md`: writer entries (names, constructors, leaf path)
  + workload section (benchmark/test commands → leaf package).
- `specs/ci-gates.d/gates-11/12/13.yaml`: trigger path.
- `.github/workflows/ifa-determinism-gate.yml`: trigger path.
- `scripts/lib` (5 files): fault/registry comments `:90` → `:92`
  (verified), live-gate selector pair.
- Ifa `testdata/.../ifa-kubernetes-namespace-environment-family-expected-edges.json`
  note: template path.
- `internal/queryplan/testdata/statement-builders.yaml`: 1 new file entry,
  2 builders transcribed from discovery output (byte-exact template, digest,
  count), sorted placement. Execution profile proven against the base B-7
  differential captures (run 39515780996, pair1+pair2 neo4j+nornicdb):
  the stale-edge retract (`old_env.name`) is recorded once per backend
  (executed-but-unattributed: discovery cannot resolve the caller-supplied
  template const inter-procedurally); the absent-node sweep
  (`KubernetesNamespace {cluster_id`) has zero recordings on all four legs
  (standard zero-execution exempt: sweep runs only after an explicit
  reconcile-complete snapshot).

## No-Regression Evidence

Baseline: pre-move files at the base (`git mv` records renames; sha256 over
the backtick Cypher literal blocks matches old/new per file —
`6feb6308…` namespace, `263d9efe…` workload; the only literal-adjacent
edits are package clauses, the `sourcecypher` import, qualifier prefixes,
the leaf-specific helper name, and type renames). After:

- `go build` on the affected trees (`storage/cypher/...`,
  `internal/reducer/...`, `internal/ifa/...`, `internal/replay/...`,
  `internal/graphowner/...`, `cmd/reducer/...`) — clean. (Three unrelated
  `cmd/` binaries fail to LINK in this environment with `disk quota
  exceeded`; compile is clean and the failure is at output-file mapping,
  unrelated to this change.)
- `go vet` on the same trees — clean.
- `go test -count=1 ./internal/storage/cypher/kubernetes/
  ./internal/storage/cypher/ ./internal/storage/cypher/edge/writer/
  ./internal/storage/cypher/edge/materialized/` — ok (leaf incl. all 17
  moved tests + the new location pin + bench).
- `go test -list '.*' ./internal/storage/cypher/kubernetes/` discovers
  every moved test (`TestKubernetesNamespaceNodeWriter*` ×10,
  `TestKubernetesWorkloadNodeWriter*` ×5, `TestKubernetesNodeWritersLiveInLeaf`,
  `BenchmarkKubernetesWorkloadNodeWriter`).
- `go test -count=1 ./internal/ifa/materializededges/
  ./internal/queryplan/ ./internal/graphowner/ ./internal/query/impact/...
  ./internal/reducer/ ./internal/graph/... ./internal/backendconformance/
  ./internal/replay/costcounting/ ./internal/replay/offlinetier/
  ./cmd/reducer/` — ok (incl. the re-wired blindness scan and the
  re-derived manifest test).
- `go test ./internal/storage/cypher/kubernetes/ -run '^$' -bench
  BenchmarkKubernetesWorkloadNodeWriter -benchtime=10x` — runs
  (`25069 allocs/op`, matching the README baseline exactly; no
  performance claim, move-only).
- Dirgate: root `internal/storage/cypher` 58 → 56 non-test files
  (shrink, no trailer); new leaf `kubernetes/` holds 2 non-test files
  (under the 40 cap); `namespace.go`/`workload.go` keep the
  `kubernetes_` prefix dropped per the no-stutter file rule and stay under
  500 lines.
- `gofmt -l` clean on the leaf; `git diff --check` clean.
- B-7 byte-identical replay stays a CI gate (no live backend locally);
  rc-158 + `node_count_KubernetesNamespace` pass on the base captures.

Input shape: unchanged reducer projection rows; batching, dispatch
(sequential retract vs grouped upsert), retry wrapping, and retract
predicates are the same compiled code at a new path. The only production
wiring change is the import path in `cmd/reducer` + `ifa/materializededges`
+ replay tests, covered by `go build` and the suites above.

## No-Observability-Change

No telemetry, metrics, spans, structured logs, status surfaces, or pprof
endpoints are added, removed, renamed, or re-labeled by this move: the
moved files contain no telemetry surfaces beyond the existing `Statement`
metadata keys (unchanged values), executor dispatch, retry wrapping, and
failure logging, all preserved. Telemetry coverage for the writer paths is
unchanged.

## Still in root (later #6694 slices)

Per-file census on this base: `CanonicalNodeWriter` core + phases/options,
Statement-seam adapters over nested leaves (canonical atlantis/codeowners/
deployable-unit/gitlab/helm-template/invokes-cloud-action/kustomize/
runs-in/submodule, crossplane, package-registry, terraform, phase-group),
shared contracts (writer/executor/retry/chunk/row helpers, retract
registries, generation reconciliation), shared helpers consumed by cloud
leaves (`posture_node_existence`, `edgeless_reap`), and
`CanonicalNodeWriter`-method families needing a parts seam (`oci_registry`,
`module_identity`, tfstate resolvers). The dirgate cap row is write-once
and cannot be re-pinned down; the delta rule guards every merge while the
root shrinks (58 → 56 here).
