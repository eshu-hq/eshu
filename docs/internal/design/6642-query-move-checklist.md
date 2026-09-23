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
| 4 | `querycontract/taxonomy` (planned as `language`) | 4 | — | open | 37 |
| 5 | `querycontract/entity` | 3 | [#7010](https://github.com/eshu-hq/eshu/pull/7010) | **merged** `91105376d` | 49 |
| 6 | `querycontract/evidence` | 3 | [#7013](https://github.com/eshu-hq/eshu/pull/7013) | **merged** `1d2bd268d` | 46 |
| 7 | `querycontract/visualization` | 2 | [#7021](https://github.com/eshu-hq/eshu/pull/7021) | **merged** `957772254` | 44 |
| 8 | `querycontract/answer` | 3 | — | open | 41 |
| | rename `querycontract` -> `contract` | — | — | blocked on `contract/` draining | |

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
