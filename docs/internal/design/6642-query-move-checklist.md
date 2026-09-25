# Query move landing checklist (#6642)

Tracks the #6642 / #6597 moves as they land. It lives apart from
[the target tree](6642-query-target-tree.md) on purpose: it gains a row per PR,
and that doc sits 46 lines under the 500-line markdown cap, so a growing table
there would fail the cap on some later PR for a reason unrelated to its change.

## The moves

One destination directory per PR. A row ticks when its PR is merged, not when
it is opened. `querycontract` starts at 56 non-test files and retires its
`//nolint:dirgate` when what stays is under 40.

The `querycontract` leaves land **nested under the current name**
(`querycontract/<leaf>`), not at `contract/<leaf>`: today's `contract/` is a
different package and the name only frees itself once its 40 rows drain, which
is why the rename is sequenced late. The rename carries the leaves with it.
`querycontract/rowvalue/` is the existing precedent.

| # | destination | files | PR | state | `querycontract` after |
| --- | --- | ---: | --- | --- | ---: |
| 1 | `capability/` | 2 | [#6985](https://github.com/eshu-hq/eshu/pull/6985) | **merged** `2d68f1cbb` | — |
| 2 | `querycontract/kubernetes` | 2 | [#6990](https://github.com/eshu-hq/eshu/pull/6990) | **merged** `a8b9da00b` | 54 |
| 3 | `querycontract/code` | 2 | [#6998](https://github.com/eshu-hq/eshu/pull/6998) | **merged** `874012542` | 52 |
| 4 | `querycontract/taxonomy` (planned as `language`) | 4 | [#7031](https://github.com/eshu-hq/eshu/pull/7031) | **merged** `79752377d` | 37 |
| 5 | `querycontract/entity` | 3 | [#7010](https://github.com/eshu-hq/eshu/pull/7010) | **merged** `91105376d` | 49 |
| 6 | `querycontract/evidence` | 3 | [#7013](https://github.com/eshu-hq/eshu/pull/7013) | **merged** `1d2bd268d` | 46 |
| 7 | `querycontract/visualization` | 2 | [#7021](https://github.com/eshu-hq/eshu/pull/7021) | **merged** `957772254` | 44 |
| 8 | `querycontract/answer` | 3 | [#7025](https://github.com/eshu-hq/eshu/pull/7025) | **merged** `1d119f391` | 41 |
| | rename `querycontract` -> `contract` | — | — | blocked on `contract/` draining | |

Move-sequence row 4 is not a `querycontract` leaf, so it has its own table.
`querytestutil` starts at 42 non-test files.

| destination | files | PR | state | `querytestutil` after |
| --- | ---: | --- | --- | ---: |
| `querytestutil/content` and `querytestutil/graph` | 10 + 8 | [#7043](https://github.com/eshu-hq/eshu/pull/7043) | **merged** `950c16cc4` | 24 |

Move-sequence rows 7-30 move one family out of the root package per PR. Root
`internal/query` starts that block at 271 non-test files.

| destination | root files out | PR | state | root after |
| --- | ---: | --- | --- | ---: |
| `decode/factschema_shared.go` | 1 | [#7044](https://github.com/eshu-hq/eshu/pull/7044) | **merged** `42020445c` | 270 |
| `dependency/` (`handler.go`, `cypher.go`) | 2, +1 alias | [#7051](https://github.com/eshu-hq/eshu/pull/7051) | **merged** `31ce0cb56` | 269 |
| `observability/coverage/` (`handler.go`, `correlations.go`) | 2, +1 alias | [#7053](https://github.com/eshu-hq/eshu/pull/7053) | **merged** `8d5949f90` | 268 |
| `terraform/drift/` (`handler.go`, `config_state_evidence_access.go`) | 2, +1 alias | [#7055](https://github.com/eshu-hq/eshu/pull/7055) | **merged** `63613f158` | 267 |
| `kubernetes/` (`handler.go`, `correlations.go`, `runtime_workload_store.go`) | 3, +1 alias | [#7072](https://github.com/eshu-hq/eshu/pull/7072) | **merged** `052054294` | 265 |
| `metrics/` (`handler.go`, `prometheus.go`, `request.go`) | 3, +1 alias | [#7075](https://github.com/eshu-hq/eshu/pull/7075) | **merged** `152d58562` | 263 |
| `compare/` (`handler.go`, `evidence.go`, `story.go`) | 3, +1 alias | [#7079](https://github.com/eshu-hq/eshu/pull/7079) | **merged** `09fcb92b2` | 261 |
| `cicd/` (`handler.go`, `evidence_summary.go`, `run_correlations.go`, `run_correlation_aggregates.go`, `run_correlation_aggregates_handler.go`) | 5, +1 alias, −1 deleted selector forwarder | this PR | open | 256 |

Order is not free. `evidence` is a **base**, not a peer leaf: `answer` and
`visualization` both use `EvidenceCitationHandle` as a field, parameter and
map-key type, so it lands before either of them. Everything else is
independent. The `code` and `entity` counts differ by one between this doc and
[the move-cost doc](6642-query-move-cost.md); measured inbound inside
`querycontract` is zero for every candidate file in both groups, so it is a
labelling choice for those PRs rather than a coupling question.

### Alias retirements

The issue's no-retained-aliases rule is tracked by count, and the count is a
**family** count, not a `*_alias.go` glob: 21 non-test root files at
`2d68f1cbb`, which is 19 `*_alias.go`, plus the build-tagged
`entity_alias_live.go`, plus `envelope_aliases.go` (plural, and per the
move-cost doc "not an alias file" — 52 real type aliases with 1022 callers).
All 21 carry a disposition in
[the alias ledger](6642-query-move-cost.md#the-alias-ledger); audited at
`2d68f1cbb`, nothing uncovered.

Anyone updating this number must use the family rule. A `*_alias.go` glob
returns 19 and makes the docs' correct "Twenty-one" look like a miscount.

| retired by | file | remaining |
| --- | --- | ---: |
| — | (baseline at `2d68f1cbb`) | 21 |
| the `kubernetes` leaf | `k8s_match_alias.go` | 20 |

## Performance and observability evidence for the `kubernetes` leaf

The `perf-evidence` gate selects this PR because
`go/internal/query/impact/trace_deployment_resources.go` is on its hot-file
list, and that file is touched. It is worth saying exactly what the touch is
rather than producing a benchmark shaped like proof.

No-Regression Evidence: the change to every hot file in this PR is an
identifier repoint. `trace_deployment_resources.go` changes two identifiers on
one code line, plus the single import line that rename requires and nothing
else — `querycontract.NewK8sWorkloadMatchTarget` becomes
`kubernetes.NewWorkloadMatchTarget` and `querycontract.K8sSelectMatchInputFromEntity`
becomes `kubernetes.SelectMatchInputFromEntity`. `querycontract.IsK8sResourceKind`
on the line above is untouched — a K8s-named symbol that does not move, which the
rename correctly left alone. No call site, argument, allocation, loop bound,
batch size, transaction scope or query text changes.

Baseline and after are the same program. That is not an assertion from reading
the diff: normalising each moved file through the intended rename map, the
base-symbol qualification and the package/import lines, then diffing against the
moved file, yields **0 residual lines** for both files. The check is not blind —
seeding a single behavioural mutation (`strings.TrimSpace` to `strings.ToLower`)
into the pre-move file yields 2 residual lines. So the compiled behaviour is
identical by construction, and a before/after measurement would be measuring the
same instructions twice.

The diff adds **no** Cypher: `git diff <base>...HEAD -- '*.go'` matched zero added
lines containing `MATCH `, `MERGE `, `UNWIND `, `CREATE (`, `DETACH DELETE` or a
parameterised `SET`. No graph write, worker claim, lease, batching knob or
runtime Compose/Helm setting is touched, so there is no backend, input shape,
row count or terminal queue depth for this change to move. Backend version is
therefore not a variable here.

No-Observability-Change: no span, metric, log or status field is added,
removed or renamed. `LogSelectMixedVintageDrop` keeps its name, its
`DebugContext` level and its fields; it moved packages and lost its `K8s`
prefix, which changes the Go identifier and not the emitted record.

Why it is safe: `go vet ./...` exit 0; `go test ./internal/query/... -count=1`
exit 0 across 54 packages; `go test -list` still discovers all 44 `K8s`-named
tests in `internal/query` and `internal/query/impact`, so the suite that covers
this path still runs rather than merely still compiling.

## Performance and observability evidence for the `code` leaf

No-Regression Evidence: two files move from `querycontract/` to
`querycontract/code/`, 22 caller files repoint `querycontract.DeadCode*` and
`querycontract.CrossRepoDeadCode*` to `code.*`, and root's #6060 wrapper
`deadCodeCandidateEntityType` and aliases `crossRepoDeadCodeConsumerReads` /
`crossRepoDeadCodeHiddenConsumers` are deleted, their callers naming `code.*`
directly. Normalising every changed `.go` file through that rename map
(qualifier, alias and wrapper names, package clause, import lines; comments
excluded) and comparing line multisets against `aa7cc0d1d` over 31 files leaves
only the deleted declarations: the wrapper's three lines, the two `type` alias
lines, and blank lines. The check is not blind: seeding `strings.TrimSpace` to
`strings.ToLower` into the pre-move `DeadCodeRootKindsFromMetadata` adds 2. No
call site, argument, allocation, loop bound, batch size or query text changes,
so baseline and after are the same program and a timed comparison would measure
identical instructions.

One test changes on purpose. The OpenAPI dead-code contract test compared only
the `candidate_kind` enum's length with `DeadCodeCandidateLabels`; it now
compares the two as sets. Seeding `Trait` -> `Traits` into the label list, a
same-length drift the old check passed, fails it.

The diff adds no Cypher: added `.go` lines match none of `MATCH `, `MERGE `,
`UNWIND `, `CREATE (`, `DETACH DELETE` or a property `SET`. No graph write,
claim, lease, batching knob or runtime setting is touched.

No-Observability-Change: no span, metric, log or status field is added,
removed or renamed. The moved package holds types and three pure helpers and
emits nothing.

Why it is safe: `go build`, `go vet` and `go test -count=1` over
`./internal/query/...` exit 0. `go test -list` discovers 191 `DeadCode`- or
`CrossRepo`-named tests across `internal/query`, `impact`, `codequery` and
`codequery/deadcode`, the same 191 as at `aa7cc0d1d`, so the suite still runs
rather than only still compiling.

## Performance and observability evidence for the `entity` leaf

No-Regression Evidence: three files move from `querycontract/` to
`querycontract/entity/`, 20 files repoint `querycontract.X` to `entity.X`, and
root's #6060 entity-name alias block is deleted with its callers naming
`entity.*`. The perf-evidence gate selects hot files including `codequery/callers.go`,
`codequery/relationship_handlers.go`, `entity/handler.go`,
`querycontract/entity/repo_identity.go` and
`queryselector/entity_repo_identity.go` and `impact/exposure_path.go` because
they contain Cypher or worker tokens elsewhere; in each the diff changes only an import line and a package
qualifier. No SQL, Cypher, call site, argument, allocation or loop bound
changes. `buildEntityNameSearchQuery`'s body changes only type and constant
qualifiers, so its pinned queryplan source hash is refreshed while its SQL text
is byte-identical. Three hot-callsite `source_sha256` pins in
`go/internal/queryplan/testdata/query-source-coverage.yaml`
(`searchGraphEntitiesWithExact`, `ResolveEntity`,
`HydrateResolvedEntityRepoIdentity`) are refreshed for the same reason: each
body changes only an import and a qualifier.

No-Observability-Change: no span, metric, log or status field is added,
removed or renamed. The reader that owns the entity-name spans did not move.

Why it is safe: `go vet` over every `//go:build` tag in `internal/query`,
`go test ./internal/query/... -count=1` (54 ok),
`go test ./internal/queryplan/... -count=1` and `verify-dirgate.sh --all` all
exit 0.

## Performance and observability evidence for the `evidence` leaf

No-Regression Evidence: three files move from `querycontract/` to
`querycontract/evidence/`, 17 files repoint `querycontract.X` to `evidence.X`,
root's `evidence_boundaries.go` shim is deleted, and root's six unexported
evidence-citation aliases are removed with their callers naming `evidence.*`.
The exported `query.EvidenceCitationHandle` in `evidence_citation_public.go`
stays for `serviceintel` until the root drain (mapping row 294).
The perf-evidence gate selects hot files including `repository/handler.go`;
in each the diff changes only an import line and a package qualifier. No SQL,
Cypher, call site, argument, allocation or loop bound changes.
`getRepositoryStory`'s pin in
`go/internal/queryplan/testdata/query-source-coverage.yaml` is refreshed for
the same reason.

No-Observability-Change: no span, metric, log or status field is added,
removed or renamed. The moved package holds types and pure helpers.

Why it is safe: `go vet ./internal/query/...`, `go test ./internal/query/...
./internal/queryplan/... -count=1` (55 ok), `verify-dirgate.sh --all` (root
re-pinned 273 -> 272) and `verify-moved-file-refs.sh` all exit 0.

## Performance and observability evidence for the `visualization` leaf

No-Regression Evidence: two files move from `querycontract/` to
`querycontract/visualization/` (`visualization_packet.go` -> `packet.go`,
`visualization_packet_merge.go` -> `packet_merge.go`), and five files repoint
`querycontract.Visualization*` to `visualization.Visualization*`. Root has no
alias for these symbols to delete: root's `visualization_alias.go` aliases the
`query/visualization` handler package, which keeps its own forwarders. In every
touched Go file the diff changes only an import line, a package qualifier or a
comment. No SQL, Cypher, call site, argument, allocation or loop bound
changes, and the queryplan source-hash pins still match.

No-Observability-Change: no span, metric, log or status field is added,
removed or renamed. The moved package holds types and a pure in-memory
builder.

Why it is safe: `go vet ./internal/query/...`, `go test ./internal/query/...
./internal/queryplan/... -count=1`, `verify-dirgate.sh --all` and
`verify-moved-file-refs.sh` all exit 0.

## Performance and observability evidence for the `answer` leaf

No-Regression Evidence: three files move from `querycontract/` to
`querycontract/answer/`, and 17 non-test and 2 test files import the leaf. Root's
unexported `attachAnswerMetadata`, `serviceStoryAnswerData`,
`cloneTruthEnvelope` and `freshnessReason` forwarders and its exported
`BuildAnswerMetadata` and `AnswerMetadataFromData` wrappers are deleted,
because nothing outside the query root calls them; `answer_packet_routes.go`
held only one of them and is removed (root re-pinned 272 -> 271). The exported
aliases that packages outside `go/internal/query` name stay, pointed at
`answer.*`. In every touched Go file the diff changes an import line, a package
qualifier, a comment, or (in `buildPacketAnswer`) the name of a local that
shadowed the new import. No SQL, Cypher, call site, argument, allocation or
loop bound changes, and the queryplan source-hash pins still match.

No-Observability-Change: no span, metric, log or status field is added,
removed or renamed. The moved package holds types and pure builders.

Why it is safe: `go vet ./...`, `go test ./internal/query/...
./internal/queryplan/... -count=1`, `verify-dirgate.sh --all` and
`verify-moved-file-refs.sh` all exit 0.

## Performance and observability evidence for the `taxonomy` leaf

The leaf was planned as `querycontract/language`. It is named `taxonomy`
because three of its seventeen exported symbols are about languages,
`query/language` already exists and is its largest consumer, and a package of
that name would force a rename or import alias in six of its 28 importing files
(five declare a `language` local that shadows it, one collides on the package
name), measured by compiling that variant.

No-Regression Evidence: three files move from `querycontract/` to
`querycontract/taxonomy/` (`language_registry.go` -> `language.go`,
`language_query_entities.go` -> `entity_types.go` with `GraphResultMetadata`
split into `result_metadata.go`, `language_query_metadata.go` -> `search.go`).
`language_query_reasons.go`'s two wire constants fold into the parent's
`truth.go`. Root's `contentEntityTypeForResolve` and
`elixirSemanticEntityTypes` wrappers are deleted; no root file goes. In every
touched Go file the diff changes an import line, a package qualifier or a
comment. No SQL, Cypher, call site, argument, allocation or loop bound
changes. Four queryplan source-hash pins
(`listMostComplexFunctions`, `searchGraphEntitiesWithExact`,
`GetEntityContext`, `ResolveEntity`) refresh because their bodies now say
`taxonomy.X`.

No-Observability-Change: no span, metric, log or status field is added,
removed or renamed. The moved package holds maps and pure functions.

Why it is safe: `go vet ./...`, `go test ./internal/query/...
./internal/queryplan/... -count=1`, `verify-parser-relationship-kit.sh`,
`verify-dirgate.sh --all` and `verify-moved-file-refs.sh` all exit 0.

## Performance and observability evidence for the `querytestutil` leaves

No-Regression Evidence: 18 non-test files and their tests move from
`querytestutil/` to `querytestutil/content/` (the content-read doubles and the
fake `database/sql` driver) and `querytestutil/graph/` (the graph-read doubles,
the NornicDB Cypher-shape guards and the blast-radius cleanup probes). Every
moved file is test-only code: `internal/queryplan`'s inventory rejects a
production import of either leaf. Callers change an import line and a package
qualifier; three test files rename a local `graph` that would shadow the new
package. Two moved tests needed a path fix, both relative to their own
directory: the default-rows coverage test parses its sibling by file name, and
the X11 production-Cypher scan roots at `go/internal` and `go/cmd`, now one
level further up. `go test -list` finds the same 56 tests under
`querytestutil/...` on this branch as on `e6a8e11cb`.

Outside the moved tree, one production file changes a comment only:
`content_reader_k8s_select_candidates.go` now says the shared `ContentStore`
double lives in `querytestutil/content`. The one behavior change is
`internal/queryplan`'s production-import guard. It matched only an import path ending in
`querytestutil`, so a production import of `querytestutil/graph` passed. It now
matches the path element and exempts files inside the helper tree.
`TestDiscoverQueryCallsitesRejectsProductionImportOfNestedTestOnlyHelperLeaf`
failed before the fix and passes after. The guard still parses imports only,
once per non-test file.

No-Observability-Change: test doubles emit no telemetry, and no span, metric,
log or status field is added, removed or renamed.

## Performance and observability evidence for the `decode` leaf

No-Regression Evidence: root's `factschema_decode_shared.go` moves to
`decode/factschema_shared.go`. Its `queryDecodeError` alias and
`newQueryDecodeError` forwarder are deleted; callers name `decode.Error` and
`decode.New`. The schema-version literal and the `*string` deref are exported
as `decode.DefaultSchemaMajorVersion` and `decode.DerefString`, and the five
package-local copies of the literal and five of the deref in
`package/registry`, `workitem`, `incident/store` and
`supply/chain/{advisory,impact}` now call them. Every copy returned the same
`"1.0.0"` or the same zero-value deref, so no decoded value changes; the full
`internal/query` and `internal/queryplan` suites pass and no queryplan hash
moves.

No-Observability-Change: no span, metric, log or status field is added,
removed or renamed.

## Performance and observability evidence for the `dependency` leaf

No-Regression Evidence: `dependencies.go` and `dependencies_cypher.go` move to
`dependency/handler.go` and `dependency/cypher.go`. The Cypher text, parameters,
page cap, read timeout and row decoding are unchanged; the handler's root
forwarders (`QueryParam`, `WriteError`, `StringVal`, `BuildTruthEnvelope`, ...)
were one-line pass-throughs, so the handler now calls `querycontract` directly.
The queryplan source-coverage entry and the `QP-SC-DEPS` hot-cypher entry
re-key to the new file and symbol; their hashes moved for qualifier and rename
edits only. The `dependencies.list` capability row moves into
`dependency.Support()` with identical values. Root keeps `DependenciesHandler`
for `cmd/api` in `dependency_alias.go`.

No-Observability-Change: the span name, tracer, metrics and attributes are the
ones root emitted.

## Performance and observability evidence for the `observability/coverage` leaf

No-Regression Evidence: `observability_coverage.go` and
`observability_coverage_correlations.go` move to `observability/coverage/`.
Both SQL queries, the 200-row cap and the decode path are unchanged; the
handler's root forwarders were pass-throughs to `querycontract` and the test's
auth helpers pass-throughs to `queryauth`. The capability row moves into
`coverage.Support()` with identical values. Root's `openScopeQueryerTestDB`
becomes `querytestutil.OpenScopeQueryerTestDB` with copy-returning accessors.
The `internal/mcp` route-serves-data registry points at the new files and the
distinct `ObservabilityCorrelationStore` type name; its mutation tests pass.

No-Observability-Change: same span name and tracer; no metric or log change.

## Performance and observability evidence for the `terraform/drift` leaf

No-Regression Evidence: `terraform_config_state_drift.go` and
`terraform_config_state_drift_evidence_access.go` move to `terraform/drift/`.
Request validation, the 100/500 limits, the scoped-grant precheck and SQL-layer
grant binding, the store calls and the response shaping are unchanged. The
handler's root forwarders were pass-throughs to `querycontract` and `iac`; the
two root `iac` paging forwarders lost their last caller and are deleted. The
capability row moves into `drift.Support()` with identical values. Root keeps
`TerraformConfigStateDriftHandler` and
`NewPostgresTerraformConfigStateDriftFindingStore` for `cmd/api` and
`cmd/mcp-server` in `terraform_drift_alias.go`.

No-Observability-Change: same span name, tracer and instrumented store name; no
metric or log change.

## Performance and observability evidence for the `kubernetes` leaf

No-Regression Evidence: `kubernetes.go`, `kubernetes_correlations.go` and
`kubernetes_runtime_workload_store.go` move to `kubernetes/`. Both SQL
statements, their parameters, the 1-200 limit rejection, the anchor rule, the
empty-grant short-circuit, the keyset cursor and the decode path are unchanged.
The capability row moves into `kubernetes.Support()` with identical values.
The store interface is `WorkloadCorrelationStore` so the `internal/mcp`
route-serves-data registry's substring match stays unambiguous. The
fairness live test and the runtime-probe performance pair stay in root because
they drive root's `SupplyChainHandler`; `BuildRuntimeWorkloadQuery` is exported
so the root performance helper can EXPLAIN the store's SQL. Root keeps
`KubernetesHandler`, `PostgresKubernetesRuntimeWorkloadStore` and both store
constructors in `kubernetes_alias.go`.

No-Observability-Change: same span name and tracer; no metric or log change.

## Performance and observability evidence for the `metrics` leaf

No-Regression Evidence: `metrics.go`, `metrics_prometheus.go` and
`request_metrics.go` move to `metrics/`. The metric allow-list, range
validation, the Prometheus/Mimir client, the freshness mapping and the
request-metrics middleware (route-pattern labels, `Flush`/`Hijack` forwarding,
meter resolution inside `sync.Once`) are unchanged. The capability row moves
into `metrics.Support()` with identical values. The leaf's capability test is
renamed `TestTimeSeriesSupportIsDerived`: in the leaf it sees only
`main_test.go`'s registration, and root's `TestCapabilityMatrixMatchesYAMLContract`
proves the production registration (a `go test -overlay` that drops
`contract/metrics.go`'s registration fails it). The Ask SSE streaming
regression stays in root as `ask_sse_metrics_middleware_test.go` because it
drives root's `AskHandler`. Root keeps seven names in `metrics_alias.go` for
`cmd/api` and `cmd/mcp-server`.

No-Observability-Change: same metric names, labels and meter; no span or log
change.

## Performance and observability evidence for the `compare` leaf

No-Regression Evidence: `compare.go`, `compare_evidence.go` and
`compare_story.go` move to `compare/`; `context_story_limits.go` stays in root
because it only forwards to `querycontract` and no compare file calls it. Both
Cypher reads, their parameters, the list bounds, the grant handling and the
response shape are unchanged. The two reads were grandfathered `non_hot_reason`
entries; the receiver rename changes their source digest, so they convert to
typed `keyed_support` (`single_key`; `max_results` 201 and 1) and leave
`grandfatheredNonHotSourceDigests`, as `internal/queryplan`'s rules require.

No-Observability-Change: no span, metric or log change.

## Performance and observability evidence for the `cicd` leaf

No-Regression Evidence: `ci_cd.go`, `ci_cd_evidence_summary.go`,
`ci_cd_run_correlation_aggregates.go`,
`ci_cd_run_correlation_aggregates_handler.go` and `ci_cd_run_correlations.go`
move to `cicd/`; both SQL query families, their parameters, the list bounds,
the grant handling, the evidence summary assembly and the response shapes are
unchanged. `ci_cd_authz_test.go`, `ci_cd_story_parity_test.go` and
`ci_cd_story_readback_test.go` stay in root (auth middleware and other
families' surfaces); only the SQL predicate-order test moves, into
`cicd/queries_test.go`. The dead root selector forwarder
`resolveRepositorySelectorForRequestWithAccess` is deleted: the family calls
`selector.ResolveForRequestWithAccess` directly like every other leaf, and
keeping a callerless forwarder trips the graph-read capability sweep. These
are Postgres reads, not graph reads: nothing here registers in
`internal/queryplan`.

No-Observability-Change: no span, metric or log change.
