# Implementation Plan: Local Code Intelligence Host, Authoritative Graph Mode, And Backend Conformance

**Date:** 2026-04-20
**Owner:** Allen Sanabria
**Tracks ADR:** `2026-04-20-embedded-local-backends-desktop-mode.md`
**Status:** In Progress (local host, local-authoritative path, and backend conformance shipped; install trust, OCI plugins, and support-path closure remain)

**Companion Specs:**

- `docs/docs/adrs/2026-05-04-neo4j-parity-optimization-plan.md`
- `docs/docs/reference/capability-conformance-spec.md`
- `docs/docs/reference/backend-conformance.md`
- `docs/docs/reference/truth-label-protocol.md`
- `docs/docs/reference/local-data-root-spec.md`
- `docs/docs/reference/dead-code-reachability-spec.md`
- `docs/docs/reference/fact-schema-versioning.md`
- `docs/docs/reference/plugin-trust-model.md`
- `docs/docs/reference/local-performance-envelope.md`
- `docs/docs/reference/local-host-lifecycle.md`

---

## Status Review (2026-05-04)

**Current disposition:** In progress; shipped runtime slices are separate from
promotion blockers.

The plan has shipped the local host foundation, embedded NornicDB as the
default local-authoritative graph runtime, explicit process-mode NornicDB
installer path, progress view, and the docs/runtime split between local
binaries, default NornicDB Compose, and Neo4j compatibility Compose. Eshu
currently tracks latest NornicDB `main`; normal local binaries embed NornicDB,
while process-mode testing uses explicit `--from` installs or
`ESHU_NORNICDB_BINARY`. Deployment binaries remain plain service binaries that
connect to external Bolt-compatible graph endpoints. The embedded release
manifest intentionally has no accepted process-mode assets.

**Remaining work:** keep latest-main NornicDB validation current, finish the
install trust policy and broader host envelope, complete OCI/plugin contract
work, and finish the remaining support-path closure items. Graph-analysis work
has shipped useful local-authoritative read paths, but broader hardening and
truth-class promotion remain tracked work.

## Wave 1 Closeout Split (2026-05-03)

The current branch has two different kinds of work in this plan. Keep them
separate when reviewing the ADR:

| Bucket | Chunks | Current state |
| --- | --- | --- |
| Shipped | 1, 2, 3 | Capability labels, storage capability ports, local host supervision, embedded Postgres lifecycle, owner records, progress/status surfaces, and local-host startup evidence are in place. |
| Shipped with open promotion gates | 3.5, 4 | `local_authoritative` NornicDB lifecycle, installer/upgrade/log/stop commands, backend-routed graph queries, query selector normalization, graph-analysis compose coverage, and full-corpus latest-main evidence are present. They are not a completed backend promotion until latest-main validation, install trust policy, broader host coverage, and the production comparison gate pass. |
| Shipped / in progress | 5, 5b | Backend conformance has a matrix, DB-free Go harness, live Compose-backed adapter check, and explicit NornicDB profile gates for `local_authoritative`, `local_full_stack`, and `production`. The latest-main production-profile proof drained the full corpus in `878s` and passed API/MCP evidence checks. The accepted Neo4j parity ADR now records the schema-first proof and shared-writer cleanup, so Neo4j research is no longer the gating item for this plan. |
| Blocked / not started | 6, 7 | OCI collector plugins and the remaining support-path closure items are not complete. Chunk 7 now depends on install trust, broader host envelope, and the plugin/support work, not on Neo4j parity research. |

The long table below remains the evidence ledger. This split is the reviewer
shortcut for what shipped versus what still blocks closeout.

## Chunk Status

This table reflects the current branch state, including active working-tree
slices in the current PR. It MUST be updated in the same PR that changes
chunk-visible code. Drift between this table, the working tree, and
verification evidence = reviewer rejects PR.

| Chunk | Title | Status | Evidence | Remaining |
| --- | --- | --- | --- | --- |
| 1 | Capability contract + truth labels | Shipped | `488ff808`, `35a3a091` | — |
| 2 | Capability ports (`GraphQuery`, `ContentStore`) | Shipped | `08795558`, `07619013`, `085c91a3` | — |
| 3 | Lightweight local host | Shipped | `a3e05ecf`, `c832a84c`, current branch local-host supervisor + embedded Postgres lifecycle, current branch `TestLocalAuthoritativeStartupEnvelope` with 2026-04-23 cold start `9.045253708s` and warm restart `490.996625ms` evidence attached in docs | — |
| 3.5 | Embedded NornicDB + `local_authoritative` profile | In progress | `0e4d8a5f`, current branch profile/backend and runtime-gating slices, `da35d729`, current branch authoritative sidecar lifecycle + shared Bolt-driver path + graph-aware reclaim, manual smoke with `/tmp/nornicdb-headless` showing healthy owner + clean Ctrl-C shutdown, current branch binary verification + random workspace credentials, `575ca864` opt-in syntax/workaround gates, `5f5a781e` schema-dialect router + `TestNornicDBSchemaAdapterVerification` pass, current branch explicit-source `eshu install nornicdb --from <source>` installer with managed `${ESHU_HOME}/bin/nornicdb-headless` discovery, local binaries, local archives, macOS packages, URLs, SHA-256 checks, and remote download cancellation/timeout handling for explicit process-mode testing; current branch intentionally leaves the embedded release manifest empty while Eshu tracks latest NornicDB `main`, so bare no-argument process installs fail with explicit latest-main `--from` guidance instead of using the old forked asset; current branch embeds NornicDB in the `eshu` owner process by default when built with `-tags nolocalllm`, requires embedded mode by default instead of silently falling back to process mode, keeps `ESHU_NORNICDB_RUNTIME=process` / `ESHU_NORNICDB_BINARY` as explicit maintainer overrides, builds only the local owner `eshu` with embedded tags in `scripts/install-local-binaries.sh` while keeping service/deployment binaries plain, and proves the embedded HTTP/Bolt lifecycle with `TestEmbeddedLocalNornicDBRuntimeStartsHTTPAndBolt`; current branch `eshu graph logs` workspace log reader, current branch owner-aware `eshu graph stop`, current branch foreground `eshu graph start` local-host shortcut, current branch stopped-owner `eshu graph upgrade --from <source>`, 2026-04-22 smoke proving install → start → status running → logs → stop → status stopped, current branch NornicDB grouped-write capability router with `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true` reserved for conformance, current branch `TestLocalAuthoritativeStartupEnvelope` with cold start `9.045253708s` and warm restart `490.996625ms`, current branch `eshu watch` / `eshu graph start` live local progress panel from the shared status store, 2026-05-03 full-corpus latest-main proof after upstream handoff for `orneryd/NornicDB#136`, current branch production-profile proof `eshu-full-pr138-a2c630af-b68b4ef-20260504T120630Z`, and current branch `TestServiceRunAcksSuccessfulProjectionAfterShutdownCancel` proving completed projector work uses a bounded shutdown ack context instead of ending foreground `eshu graph start` with the `ack projector work: begin: context canceled` tail | release packaging for embedded local binaries, broader host coverage, broader query/memory perf envelope |
| 4 | Authoritative graph analysis hardening | In progress | current branch backend-routed call-chain Cypher builder keeps Neo4j on the existing projected-node-map `shortestPath` query while routing NornicDB through anchored `shortestPath` matches plus raw `nodes(path)` normalization in Go; current branch transitive callers/callees route through `/api/v0/code/relationships` with backend-aware traversal builders, `analyze callers|calls --transitive --depth`, and MCP `find_all_callers` / `find_all_callees`; current branch dead-code now runs as a dedicated query module that keeps the graph-backed candidate scan but defaults to derived truth, excludes Go entrypoints, direct Go Cobra/stdlib-HTTP/controller-runtime signature roots, parser-backed Python FastAPI/Flask/Celery decorator roots, parser-backed JavaScript/TypeScript Next.js route exports and Express handler registrations, Go exported public-package symbols, test files, and obvious generated code, strips comments before query-time source heuristics run, prefers parser-emitted `dead_code_root_kinds` from entity metadata when that content metadata exists, now also emits parser-backed Go Cobra `Run` / `RunE` registrations, direct/proven `ServeMux` HTTP registrations, Python FastAPI/Flask/Celery decorator roots, and JavaScript/TypeScript Next.js/Express route roots as derived framework roots, preserves `dead_code_root_kinds` across the mixed native+SCIP supplement merge path, requires pointer-form `*http.Request` for stdlib HTTP handler roots, dedupes merged controller-runtime aliases across import paths, hardens the Express detector seam with `javaScriptExpressServerSymbols(...)` so the dead-code registration path explicitly requires a typed `server_symbols []string` contract, emits structured `analysis` metadata about the currently modeled root categories plus `modeled_framework_roots`, `roots_skipped_missing_source`, and parser-vs-fallback framework-root counters, and exposes a bounded `limit` plus `truncated` signal instead of silently truncating at 100 rows; current branch dead-code also routes NornicDB through the explicit `NOT EXISTS { MATCH ... }` candidate-query form and proves the live `local_authoritative` handler path with `TestLocalAuthoritativeDeadCodeSyntheticEnvelope` after switching the synthetic seed to a staged NornicDB-friendly containment write flow; current branch `eshu analyze dead-code` accepts `--repo` selectors that resolve repository name, slug, local path, or canonical ID through `/api/v0/repositories` before posting the canonical `repo_id`; current branch code-query HTTP handlers also resolve repository selectors in `repo_id` fields through the repository catalog so code search, relationships, dead-code, call-chain, and complexity requests can use canonical IDs, names, repo slugs, or indexed paths at the public boundary; current branch repository context, story, stats, and coverage routes now resolve the `{repo_id}` path segment through the same exact-match repository selector seam, so repository MCP/API users can address those routes by canonical ID, name, repo slug, or indexed path while the server still normalizes to canonical repo IDs before graph/content reads, and the escaped-path regression tests now prove `%2F` selectors survive real `http.ServeMux` route decoding; current branch content file/entity reads, content search, and `entities/resolve` now also normalize repository selectors in `repo_id` and `repo_ids`, so MCP/API callers can scope those requests by canonical ID, name, repo slug, or indexed path without changing the underlying canonical query semantics; current branch CLI `eshu stats [repo-or-path]` now preserves repository selectors such as repo names and slugs while still canonicalizing a real existing local path to an absolute path before calling `/api/v0/repositories/{repo_id}/stats`; current branch content-store repository selector resolution now uses exact `ingestion_scopes` matches instead of relisting the full repository catalog on every code-query request, which removes the hot-path full scan without adding cache staleness; current branch now annotates Go selector calls with receiver metadata and upgrades calls on the enclosing method receiver to exact class-context resolution, which restores same-file edges like `handleRelationships -> transitiveRelationshipsGraphRow` without loosening import-qualified calls such as `fmt.Println`; current branch `TestJavaScriptExpressServerSymbols`, `TestDefaultEngineParsePathGoEmitsDeadCodeRootKinds`, `TestDefaultEngineParsePathGoDoesNotMarkValueRequestAsHTTPHandlerRoot`, `TestDefaultEngineParsePathGoEmitsDeadCodeRegistrationRoots`, `TestDefaultEngineParsePathGoIgnoresUnknownHandleFuncReceivers`, `TestDefaultEngineParsePathGoAnnotatesReceiverSelectorCalls`, `TestDefaultEngineParsePathPythonEmitsDeadCodeRootKinds`, `TestDefaultEngineParsePathPythonDoesNotMarkUnknownDecoratorsAsDeadCodeRoots`, `TestDefaultEngineParsePathJavaScriptEmitsDeadCodeRootKinds`, `TestMergeSCIPSupplementPreservesDeadCodeRootKinds`, `TestHandleCallChainRewritesShortestPathAnchorsForNornicDB`, `TestHandleCallChainSupportsEntityIDAndRepoScopedLookupForNornicDB`, `TestHandleRelationshipsReturnsTransitiveCallers`, `TestHandleRelationshipsReturnsTransitiveCallersForNornicDB`, `TestHandleDeadCodeReturnsDerivedTruthAndAnalysisMetadata`, `TestHandleDeadCodeExcludesDefaultEntrypointsTestsAndGeneratedCode`, `TestHandleDeadCodeExcludesGoPublicAPIRootsOutsideInternalPackages`, `TestHandleDeadCodeExcludesGoFrameworkRootsBySignature`, `TestHandleDeadCodeExcludesPythonFrameworkRootsFromMetadata`, `TestHandleDeadCodeExcludesJavaScriptFrameworkRootsFromMetadata`, `TestHandleDeadCodeUsesParserRootMetadataWithoutSourceCache`, `TestHandleDeadCodeUsesParserRegistrationRootMetadataWithoutSourceCache`, `TestHandleDeadCodeDoesNotTreatGoCommentSubstringsAsFrameworkRoots`, `TestHandleDeadCodeReportsModeledGoFrameworkRootsInAnalysis`, `TestHandleDeadCodeReportsMissingSourceForGoFrameworkRootChecks`, `TestHandleDeadCodeRespectsLimitAndReportsTruncation`, `TestRunAnalyzeDeadCodeResolvesRepoSelectorAlias`, `TestRunAnalyzeDeadCodeFailsOnAmbiguousRepoSelector`, `TestHandleDeadCodeResolvesRepositorySelectorAlias`, `TestHandleCallChainResolvesRepositorySelectorAlias`, `TestContentReaderMatchRepositoriesReturnsExactMatches`, `TestContentReaderResolveRepositoryRejectsAmbiguousMatches`, `TestGetRepositoryContextAcceptsRepositorySlugSelector`, `TestGetRepositoryContextDecodesEscapedRepositorySlugPathValue`, `TestGetRepositoryStoryAcceptsRepositoryPathSelector`, `TestGetRepositoryStatsAcceptsRepositorySlugSelector`, `TestContentHandlerReadFileResolvesRepositorySelectorAlias`, `TestContentHandlerSearchFilesResolvesRepositorySelectorAliases`, `TestContentHandlerSearchEntitiesResolvesRepositorySelectorAlias`, `TestResolveEntityAcceptsRepositorySelectorAlias`, `TestRunStatsPreservesRepositorySelector`, `TestRunStatsCanonicalizesExistingPathSelector`, `TestExtractCodeCallRowsResolvesGoReceiverVariableCallsWithoutTreatingImportsAsLocal`, and 2026-04-23 `TestLocalAuthoritativeCallChainSyntheticEnvelope` / `TestLocalAuthoritativeTransitiveCallersSyntheticEnvelope` / `TestLocalAuthoritativeDeadCodeSyntheticEnvelope` with synthetic call-chain p95 `789.709µs`, synthetic transitive-caller p95 `1.917916ms`, and synthetic dead-code p95 `3.174125ms` through the real `local_authoritative` handlers; current branch also adds `./scripts/verify_graph_analysis_compose.sh`, a dedicated full-stack compose gate over `tests/fixtures/graph_analysis_compose` that proves direct callers, transitive callers, shortest call-chain path, dead-code results, and the expected canonical `CALLS` edges after a fresh bootstrap run, plus `./scripts/verify_graph_analysis_dogfood_compose.sh`, a self-repo dogfood lane that discovered the receiver-call gap and now validates it against `eshu-local-codeintel`; local Compose now bounds Neo4j to `512m` heap and `512m` page cache by default through `ESHU_NEO4J_HEAP_INITIAL_SIZE`, `ESHU_NEO4J_HEAP_MAX_SIZE`, and `ESHU_NEO4J_PAGECACHE_SIZE` so single-repo dogfood verification does not rely on unbounded JVM defaults; 2026-04-23 live self-repo NornicDB dogfood proved the current branch can already serve `eshu list`, `eshu find name handleRelationships`, and `eshu analyze dead-code --repo eshu-local-codeintel`, but graph-backed CLI queries remain blocked by repo-scale canonical projection timing out before the canonical nodes become queryable | broader framework/public-API/reflection root registry beyond current direct Go, Python decorator, and JavaScript/TypeScript route rules, remove legacy query-time string matching after a guaranteed reindex window, active-repo perf evidence including parser-overhead measurement, keep the new dogfood compose lane green under restart/degraded-stack conditions, repo-scale canonical-write tuning or redesign for NornicDB, consider lightweight caching only if exact-match selector queries still show measurable pressure |
| 5 | Backend conformance suite | Shipped | current branch adds `specs/backend-conformance.v1.yaml`, documents the backend matrix, and adds `go/internal/backendconformance` with matrix validation plus reusable `GraphQuery` and Cypher write corpora; current branch `TestNeo4jReaderSatisfiesBackendConformanceGraphQuery`, `TestReducerNeo4jExecutorRunsBackendConformanceWriteCorpus`, and `TestNornicDBGroupedConformanceExecutorRunsBackendConformanceWriteCorpus` prove the current read and reducer write adapter surfaces consume the harness without live databases; current branch adds `TestLiveBackendConformance`, `scripts/verify_backend_conformance_live.sh`, and the end-to-end matrix step that runs the shared corpus against real NornicDB and Neo4j Bolt endpoints before bootstrap data is loaded | — |
| 5b | NornicDB conformance across profiles | In progress | current branch adds `profile_matrix` gates to `specs/backend-conformance.v1.yaml`, validates those gates in `TestBackendConformanceMatrixDefinesNornicDBProfileGates`, records `local_authoritative` startup/query/full-corpus API/MCP evidence, marks `local_full_stack` against the NornicDB Compose E2E matrix with `ESHU_QUERY_PROFILE=local_full_stack`, and records production-profile proof `eshu-full-pr138-a2c630af-b68b4ef-20260504T120630Z`: latest NornicDB `main` `b68b4ef`, `895` corpus Git configs, `896` API repositories, `8458/8458` queue rows succeeded, `0` pending/in-flight/retrying/failed/dead-letter, terminal in `878s`, API/MCP health green, and relationship-evidence drilldown successful on both surfaces. The accepted Neo4j parity ADR now records the schema-first proof `eshu-neo4j-schema-profile-20260504T182712Z`, so this profile gate no longer waits on Neo4j research. | finish install trust, broader host envelope, and the remaining support-path closure items |
| 6 | OCI collector plugin contract | Not started | — | all |
| 7 | Neo4j support-path closure | Not started | — | final Neo4j support posture, migration guidance if needed, operator docs |

Chunk 4 query-truth addendum: the older table-row wording that graph-backed
CLI queries were blocked by canonical projection is superseded by the
2026-04-25 live NornicDB dogfood proof below. The current branch now proves
direct `CALLS`, transitive caller, entity-ID call-chain, and entity-resolution
repo identity API queries for `handleRelationships -> transitiveRelationshipsGraphRow`
through the live local-authoritative API.

Latest NornicDB dogfood evidence:
- 2026-05-04 production-profile proof
  `eshu-full-pr138-a2c630af-b68b4ef-20260504T120630Z` rebuilt Eshu
  `a2c630af` and NornicDB latest `main` `b68b4ef`, indexed the full corpus
  with `895` Git configs, returned `896` repositories from the API, drained
  `8458/8458` queue rows in `878s`, and ended with `0` pending, in-flight,
  retrying, failed, or dead-letter rows. API and MCP health passed, the API
  index-status queue summary matched Postgres queue truth, API relationship
  evidence returned `resolved_37127efb5f70cd95`, and MCP
  `get_relationship_evidence` returned a result with two content blocks.
  Cleanup stopped API, MCP, NornicDB, Postgres, and Eshu owner processes.
- 2026-05-03 full-corpus proof `eshu-full-pr136-01413d04-a9ccd0f-20260503T0121Z`
  rebuilt Eshu `01413d04` and a clean NornicDB `a9ccd0f` binary from the
  `UNWIND MATCH SET` hot-path branch, drained `896` repos / `8458` queue rows
  in about `873s`, and ended with `0` retrying, failed, or dead-lettered rows.
  API/MCP health passed against the completed stack, `/api/v0/repositories`
  returned `896`, and relationship-evidence drilldown returned durable
  Postgres detail through `resolved_id`. This is promotion-positive for the
  hot path. `orneryd/NornicDB#136` has since merged, but the lane still waits
  on a release asset or explicitly accepted pinned build before it is
  release-backed.
- 2026-05-02 focused evidence-pointer proof found and isolated a NornicDB
  relationship-property persistence bug in the embedded backend path. Eshu's
  graph writer was already setting `resolved_id`, `generation_id`,
  `evidence_type`, `evidence_kinds`, and counts on typed repository edges, but
  NornicDB `main` (`v1.0.43`) returned empty relationship properties. The
  minimal NornicDB HTTP probe `MERGE (a)-[rel:T]->(b) SET rel.resolved_id =
  $rid` reproduced the backend issue. After the NornicDB patch and a fresh
  focused Eshu rerun with rebuilt binaries, projector `6/6` and reducer `56/56`
  drained with no retrying/failed/dead-letter rows, and direct graph API proof
  showed repo edges plus evidence-hop edges carrying `resolved_id` and evidence
  metadata. The backend fix is now tracked upstream as
  `orneryd/NornicDB#135`; Copilot's standalone-`SET` review comment was
  resolved in NornicDB commit `2461a46`, with package verification
  `go test -tags 'noui nolocalllm' ./pkg/cypher -count=1` and `git diff --check`.
  A rebuilt PR-branch headless binary also passed a direct HTTP probe for
  `ON CREATE SET` plus standalone relationship `SET`, preserving `resolved_id`
  through `properties(rel)`. The remaining action is merge/release handoff
  before release-backed evidence-pointer validation.
- the 2026-04-27 isolated `php-large-repo-b` stage-ledger rerun on Eshu `e774d50c` and NornicDB `v1.0.43` drained healthy and made the remaining local-authoritative cost profile concrete: snapshot stream `40.545s`, streaming fact upsert `52.718s`, fact reload `12.907s`, projection build `1.420s`, content-store write `169.325s`, canonical graph write `117.801s`, reducer-intent enqueue `0.040s`, and all reducer domains completed without timeout. This moves the next Chunk 3.5 tuning slice from blind NornicDB row-cap changes to content writer sub-stage evidence (`prepare_files`, `upsert_files`, `prepare_entities`, `upsert_entities`) plus fact persistence review before any chunked-generation workflow redesign.
- the narrower `Function=10` lane lowered per-statement cost but over-fragmented the self-repo run, so the built-in row cap now moves to `Function=15`
- that `Function=15` rerun advanced through `Variable` with stable early chunks around `19.9s-21.4s`
- the next repo-scale blocker is now the `retract` phase, not `entities`, so the branch runs NornicDB retract statements sequentially instead of bundling all stale deletes into one grouped transaction
- projector dead-letter persistence now sanitizes backend error text before writing to Postgres so NUL bytes from NornicDB cannot break failure updates
- the earlier rerun proved `Variable rows=25` was too wide before file-scoped entity batching, but the 2026-04-27 focused ladder after that batching change showed the opposite: `php-large-repo-b` completed `131,977` `Variable` rows in `196.713s` at `10`, `130.082s` at `25`, `118.136s` at `50`, and `102.820s` at `100`, with zero singleton fallbacks, zero retries, zero dead letters, and max grouped execution `0.607s`; the built-in Variable row cap is now `100` while the grouped-statement cap remains `5`
- the current branch now supersedes the earlier `label + file_path` grouping with a cleaner split: canonical entity node upserts batch across files with the simple NornicDB-friendly `UNWIND ... MERGE (n:<Label> {uid: row.entity_id}) SET n += row.props` shape, while `phase=entity_containment` attaches those nodes back to files in a separately measured batch phase
- projector same-scope claim fencing is now proven too: a deliberate second-generation trigger during the first generation's `Function` phase held queue state at `pending=1, in_flight=1` instead of the old overlapping `in_flight=2` failure mode
- the follow-up `Variable rows=15` / `5`-statement experiment improved individual Variable chunks into roughly the `11.6s-17.4s` band, but it still took about `23m` to reach Variable and ran about `35m` total before manual stop, so it is not the next default candidate
- after re-reading NornicDB's performance and Neo4j migration docs, the branch identified a more fundamental local-only gap: `eshu graph start` applied Postgres schema but did not apply the NornicDB graph schema before starting reducer/ingester, which meant schema-backed `MERGE` hot paths could fall back to label scans even though Eshu's checked-in schema defines the right `uid` constraints
- current branch now applies the backend-routed graph schema immediately after local NornicDB readiness and before owner-record publication or child startup, preserving the same NornicDB schema dialect used by `bootstrap-data-plane`
- the branch now emits rolling and final `nornicdb entity label summary` logs with `phase`, per-label rows, statements, executions, grouped chunks, total duration, max execution duration, and row-width totals so the next tuning slice can optimize cumulative node-upsert and containment-edge cost instead of reacting to isolated chunk logs
- the first remote self-repo rerun after the entity/containment split failed fast on `Annotation` because the old NornicDB schema dialect translated composite `IS UNIQUE` to `IS NODE KEY`, which incorrectly required sparse semantic labels to carry `name`; the follow-up run also proved the current NornicDB binary still rejects Eshu composite `IS UNIQUE`, so current branch now skips unsupported composite uniqueness DDL for NornicDB and keeps `uid` uniqueness as the canonical merge identity
- the same remote run proved the split changed the real bottleneck: `phase=entities` completed in `25.523448885s` total, including `Function` at `3.10382615s` and `Variable` at `20.695746985s`; the remaining slow lane is now `phase=entity_containment`, where `Function` containment alone took `248.58715967s`, so the next tuning slice must target containment-edge shape rather than node upsert row width
- current branch now keeps the split node-upsert / containment-edge shape for backends that support node-only batched `MERGE`, while older NornicDB builds stay on the proven file-scoped combined shape: each statement matches the `File` anchor with `$file_path`, unwinds entity rows for that file, upserts nodes, and attaches `CONTAINS` in the same statement. The opt-in syntax gate caught that builds without the generalized row-safe hot path can collapse the standalone node-only batch shape, while the combined shape preserves row-bound entity identity. The NornicDB hot-path branch in `/Users/allen/os-repos/NornicDB-eshu-map-merge-hotpath` proved the desired faster shape needed row-safe `SET += row.props` support inside the generalized `UNWIND/MERGE` batch path plus unique-constraint-backed `MERGE` lookup for `File.path` and canonical `uid` constraints; Eshu therefore keeps `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true` as an explicit latest-main evaluation switch until broader conformance settles the default.
- entity resolution now treats NornicDB optional-projection placeholders such as `r.id` / `r.name` as missing repo identity and hydrates code entities from the content store plus repository catalog before graph-only workload backfill, preventing `eshu find name` from returning projection strings as canonical repo truth.
- remote self-repo dogfood on the patched NornicDB branch shows the root cause clearly: before unique-constraint lookup, `Function` took `40.039s` for 750 rows and `158.568s` for roughly 2,142 rows while chunk duration climbed toward `8s`; after unique-constraint lookup, the same lane took `12.589s` for 750 rows and `46.566s` for roughly 2,110 rows, and the `files` phase dropped from roughly `26s-28s` to `7.351s`. That confirms the major remaining cost was NornicDB label-scan fallback on schema-constrained `MERGE`, not `.git` parsing, Postgres, or machine starvation.
- the same `dogfood-mapmerge-unique-*` run later completed `Function` at `9021` rows in `444.997825518s` and `Struct` at `916` rows in `81.059759011s`; `Variable` remained active past `5000` rows in `563.749941325s` with chunk times drifting into the `6.5s-6.6s` range. The queue stayed single-owner (`in_flight=1`, no overdue claims), so the next optimization must profile NornicDB's remaining linear write path rather than guessing at more Eshu batch defaults.
- after the NornicDB unique-constraint validation patch (`023ec51`) rebuilt on the 16-vCPU remote test host, the same self-repo dogfood run proved the root cause was fixed for canonical writes: `Variable` completed all `40163` rows in `30.375581617s`, the full canonical `entities` phase completed in `42.085095138s`, source-local projection succeeded with `55295` facts in `49.603476029s`, and the collector log explicitly skipped `.git` (`dirs_skipped..git=1`). The next blocker moved out of canonical projection and into reducer `semantic_entity_materialization`, which still used the old scalar NornicDB compatibility writer and timed out after `15s`.
- upstream handoff is now split into two clean NornicDB PRs instead of one local fork patch stack: `https://github.com/orneryd/NornicDB/pull/115` carries the indexed UNIQUE validation fix that made canonical projection finish in roughly `49.6s`, and `https://github.com/orneryd/NornicDB/pull/116` carries the `UNWIND ... MATCH ... MERGE ... SET n += row.props ... MERGE relationship` hot-path/router fix needed by the prior row-property semantic writer and canonical relationship hot-path evaluation. Chunk 3.5 remains in patched-binary evaluation until those PRs are merged, released, and pinned in Eshu's installer manifest.
- the branch first tried routing NornicDB semantic entity materialization through the shared batched `UNWIND $rows AS row` writer used by Neo4j because the patched NornicDB binary supports the needed `UNWIND` / schema-backed `MERGE` / row property update shape. That adapter-routing cleanup proved the stale scalar compatibility writer was no longer required, but the next trace pass showed the shared row-property template was still not NornicDB's fastest semantic shape.
- the first rerun with that batched semantic writer still timed out after `18.140664853s`; the new diagnostic then narrowed the failing statement to only `Annotation rows=19`, proving the issue was semantic Cypher shape rather than row count alone. A focused NornicDB trace probe then proved the row-properties shape (`UNWIND ... MATCH File ... MERGE node ... SET n += row.properties`) was schema-indexed but still missed NornicDB's generalized `UNWIND/MERGE` batch hot path. Current branch now routes NornicDB semantic writes through merge-first explicit per-label row templates (`UNWIND ... MERGE node ... SET field assignments ... MATCH File ... MERGE CONTAINS`) and keeps `ESHU_NORNICDB_SEMANTIC_ENTITY_LABEL_BATCH_SIZES=Function=15,Variable=10,...` for the genuinely high-cardinality semantic labels. Neo4j keeps the existing semantic writer path so backend comparison remains meaningful.
- upstream handoff is now complete: PRs `#115`, `#116`, and `#118` are merged into NornicDB `main`, including unique-constraint validation, the `UNWIND ... MATCH ... MERGE ... SET n += row.props ... MERGE relationship` hot path, and unique-constraint-backed `MERGE` lookup. Copilot review comments were resolved before merge: row counts now reflect required `MATCH` filtering, unsupported `OPTIONAL MATCH` mutations no longer fall through to the read-only UNWIND path, and non-comparable unique lookup values become safe misses instead of panics.
- the 2026-04-25 remote self-repo dogfood run against the stacked `#115 + #116 + #118` binary completed: source-local projection succeeded with `55384` facts in `50.461321256s`, canonical `entities` finished in `42.723968989s`, `Function` stayed flat through `9045` rows with about `2.857s` total label time, `semantic_entity_materialization` succeeded in `142.392295866s`, `sql_relationship_materialization` succeeded in `222.19561979s` for `291` edges, and the queue reached empty about `4m57s` after owner start.
- the 2026-04-27 monitored representative 20-repo gate on the 16-vCPU remote VM completed healthy in about `17m15s` with projector `20/20`, reducer `163/163`, and no retry, dead-letter, or failed queue rows. Continuous resource sampling showed the end-of-run tail was not disk-bound or machine-saturated: the remaining work was a small number of same-scope reducer items, led by semantic and SQL relationship reducers (`102.700286273s`, `63.978445716s`, `45.364188352s`, `42.800414892s`). Chunk 4 should therefore prioritize conflict-domain routing and exact Cypher-shape profiling for semantic/relationship materialization before another full-corpus run, while keeping 8 reducer workers as the production-like default for proof runs.
- a follow-up 2026-04-25 remote self-repo dogfood run rebuilt the headless binary directly from upstream NornicDB `main` at merge commit `501a121d7882cadf2bb3ec657178a54b33d5967b` (`NornicDB v1.0.42-hotfix`) with `go build -buildvcs=false -tags 'noui nolocalllm'`. The run reached healthy state about `4m36s` after owner start: source-local emitted `55409` facts and projected `55384` in the same ~`50s` band, canonical `entities` stayed fixed (`Function` `9045` rows in `2.836596812s`; `Struct` `917` rows in `0.361973072s`), `semantic_entity_materialization` succeeded in `141.031360077s`, `sql_relationship_materialization` succeeded in `217.718543222s` for `291` edges, and `eshu graph stop` left `owner_present=false` / `graph_running=false`.
- a 2026-04-27 isolated `php-large-repo-b` rerun on Eshu `dcb5e466` with `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, and the `#119 + #120` NornicDB binary validates the patched-binary cross-file containment switch on the noisy PHP stress repo without promoting it to the default. The run discovered `74,475` files, persisted `176,201` facts, finished collection/emission in `161.706108907s`, completed canonical `Variable` with `131,977` rows / `13,198` statements / `2,640` grouped executions in `301.798956955s`, drained all reducer domains with no timeout/dead-letter/failure lines, and reached queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0` at `2026-04-27T15:41:40Z`. The remaining performance finding is graph-size slope in NornicDB file-anchor and relationship-existence paths, not another Eshu semantic/reducer cap.
- this changes the next Chunk 3.5 closeout target: canonical entities are no longer the wall on the remote host, while SQL relationship materialization is now disproportionately slow and CPU-bound inside NornicDB. The next slice should inspect the `UNWIND ... MATCH (source:SqlTable|SqlView|...) ... MERGE relationship` path and decide whether to patch NornicDB's multi-label indexed `MATCH` planner or route Eshu through a documented single-label backend dialect seam.
- that inspection found the SQL-edge statement was falling out of NornicDB's `UnwindMergeChainBatch` because the post-`UNWIND` mutation starts with lookup `MATCH` clauses, uses `|` label alternatives, binds the relationship variable (`[rel:REFERENCES_TABLE]`), and then sets relationship properties. The local NornicDB worktree `/Users/allen/os-repos/NornicDB-eshu-sql-edge-hotpath` now carries focused fix commit `05f4b3c` and regression tests for the exact Eshu SQL relationship shape; `go test -tags 'noui nolocalllm' ./pkg/cypher ./pkg/storage ./pkg/bolt -count=1` and a headless build both pass.
- remote patched-binary dogfood with `/home/ubuntu/os-repos/NornicDB/bin/nornicdb-headless-eshu-sql-edge-hotpath` (`NornicDB v1.0.43`) proved the fix moves the wall again: owner start `2026-04-25T04:40:25Z`, healthy `2026-04-25T04:43:31Z` (~`3m06s`), source-local projection `34.279920s`, `sql_relationship_materialization` `3.921462s` for the same repo, and `semantic_entity_materialization` remains the dominant domain at `131.247187s`; clean stop left `owner_present=false` and `graph_running=false`.
- `https://github.com/orneryd/NornicDB/pull/119` is now open for the SQL-edge hot path. The next ESHU-side evidence pass found the semantic domain is spending most of its remaining time re-upserting ordinary Go functions that canonical projection already wrote: the completed snapshot had `7,622` semantic `Function` nodes, but only about `2,475` carried extra callable semantic metadata. The current branch now narrows Go semantic extraction and semantic-intent creation to enriched Go callables, leaving plain Go functions on the canonical-only path while preserving semantic readiness for real semantic follow-ups.
- the first remote rerun with that Go write-set reduction correctly removed the `Function` wall but exposed the next semantic label directly: `Module rows=45` exceeded the bounded `15s` semantic write timeout. The branch now caps NornicDB semantic `Module` rows at `10` by default, matching the same evidence-driven per-label strategy used for `Function` and `Variable`.
- the `Module=10` rerun then progressed to the next measured semantic family and timed out on `ImplBlock rows=103` after `52.188006393s`. The branch now caps NornicDB semantic `ImplBlock` rows at `10` as well; this keeps the default tuned to observed wide-label families while preserving larger batches for labels that have not shown timeout behavior.
- the `ImplBlock=10` rerun completed cleanly against the same PR `#119` NornicDB binary: owner start `2026-04-25T05:21:23Z`, healthy at `2026-04-25T05:24:29Z` (~`3m06s`), source-local projection `34.065189964s` for `55,439` facts, canonical `entities` `26.240370051s`, `sql_relationship_materialization` `2.587845711s`, `semantic_entity_materialization` `127.982626601s`, and code-call projection completed after semantic readiness in `4.282710051s`. Clean stop left `owner_present=false` / `graph_running=false`.
- the first semantic-attribution rerun exposed a NornicDB concurrency bug rather than a Eshu timeout: the sidecar panicked with `fatal error: concurrent map read and map write` in `StorageExecutor.findMergeNodeInCache` because cloned executors shared `nodeLookupCache` but each clone copied its own value mutex. PR `#119` branch commit `4521bcb` now shares the cache lock across clones; the regression test failed before the fix, passes after it, `go test -race -tags 'noui nolocalllm' ./pkg/cypher -run TestCloneWithStorageSharesNodeLookupCacheLock -count=1` passes, and the remote branch rebuilt the headless binary successfully.
- the follow-up remote dogfood with that NornicDB lock fix and Eshu semantic statement attribution completed cleanly: owner start `2026-04-25T05:46:36Z`, healthy progress `2026-04-25T05:49:42Z` (~`3m06s`), source-local projection `34.099439413s` for `55,471` facts, canonical `entities` `26.301571598s`, `sql_relationship_materialization` `2.546008605s`, `semantic_entity_materialization` `128.151959988s`, and code-call projection completed in `4.456774766s`. The semantic attribution shows the remaining wall is concentrated in a few labels rather than broad graph-write slowness: `ImplBlock` `103` rows across `11` statements took `79.586s`, `Module` `45` rows across `5` statements took `34.734s`, `Protocol` `9` rows took `6.918s`, while `Function` `2,485` rows across `166` statements took only `1.625s`.
- inspection of Eshu's Go-owned graph schema then found the exact asymmetry behind that attribution: `Function` and `Variable` had single-property `uid` uniqueness constraints, but the slow module-like semantic labels did not. The current branch now adds `uid` uniqueness constraints for `Module`, `ImplBlock`, `Protocol`, and `ProtocolImplementation` while preserving the separate `Module.name` index used by canonical import-graph writes, and keeps the projector `entityTypeLabelMap` aligned with every schema label that has a `uid` uniqueness constraint. `TestSchemaStatementsContainsUIDConstraints` was tightened from substring matching to exact statement matching after it falsely passed `module_uid_unique` via `terraform_module_uid_unique`, and `TestEnsureSchemaWithBackendExecutesNornicDBStatements` now proves the NornicDB schema bootstrap executes those constraints.
- the clean remote rerun with those schema constraints proved the hypothesis and removed the semantic wall: owner start `2026-04-25T06:03:46Z`, healthy progress `2026-04-25T06:04:49Z` (~`63s`), source-local projection `34.216079101s` for `55,470` facts, `sql_relationship_materialization` `2.583036883s`, `semantic_entity_materialization` `4.827652236s`, and code-call projection `4.265383252s`. The previously slow labels collapsed onto the unique-lookup path: `Module` `45` rows took `0.034952s` total, `ImplBlock` `103` rows took `0.072267s`, `Protocol` `9` rows took `0.004776s`, and `ProtocolImplementation` `3` rows took `0.002808s`. `.git` remained skipped and clean stop left `owner_present=false` / `graph_running=false`.
- PR `#119` review follow-up commit `5121f55` now resolves Copilot's remaining SQL-edge comments before upstream merge: any-label schema misses fall back to label scans for unindexed alternatives, the redundant `AllNodes` fallback is gone, no-op relationship `MERGE` skips mutation notifications, and `CreateEdge` `ErrAlreadyExists` either refetches a concurrent relationship or retries with a new edge id. The NornicDB branch passes `go test -tags 'noui nolocalllm' ./pkg/storage ./pkg/cypher -count=1`.
- PR `#119` follow-up commit `26b901f` resolves the last Copilot thread by making the unindexed `SqlFunction.uid` fallback coverage explicit. Until PR `#119` merges and a release asset is pinned, Eshu dogfood and `local_authoritative` validation should use the `eshu-sql-edge-hotpath` branch binary explicitly through `ESHU_NORNICDB_BINARY` / installer `--from`.
- the remote self-repo rerun against that exact `#119` binary and Eshu commit `5db74ae5` kept the ~one-minute dogfood envelope: owner start `2026-04-25T10:37:11Z`, code-call projection cycle completed `2026-04-25T10:38:20Z` (~`69s`), source-local projection `34.120669469s`, canonical phase-group write `27.299008161s`, `sql_relationship_materialization` `2.629018298s`, `semantic_entity_materialization` `4.870551017s`, and code-call projection `4.302299486s`. The collector skipped `.git` (`dirs_skipped..git=1`), and clean stop left `owner_present=false` / `graph_running=false`.
- the follow-up warm API query proof found a Chunk 4 query-truth issue: `eshu list` and `eshu find name handleRelationships` succeeded against the same NornicDB-backed dogfood graph, but `eshu analyze dead-code --repo eshu --limit 5` returned IaC/provenance entities such as `ArgoCDApplication`, `KustomizeOverlay`, `HelmValues`, and `K8sResource`. Current branch now gates the graph-backed dead-code candidate query to code labels (`Function`, `Class`, `Struct`, `Interface`) before `LIMIT` and keeps a defensive output guard so stale/non-code backend rows cannot leak into the public result; rebuilding only `eshu-api` on the remote owner and rerunning `eshu analyze dead-code --repo eshu --limit 5` / `--limit 50` then returned no IaC/provenance results through the live NornicDB-backed API. The same path now fetches a bounded raw-candidate policy buffer (`max(501, 10x + 1)`, capped at `1000`) before applying entrypoint/test/generated/public-API filters so early excluded roots do not make the public result underfill when displayable candidates exist later in the ordered scan; a second remote API rebuild proved `--limit 5` now returns five code entities (`Function`, `Interface`, `Struct`) instead of the previous empty/truncated shape.
- the same live graph then exposed a NornicDB read-dialect issue in code relationships and call-chain: the canonical `CALLS` edge `handleRelationships -> transitiveRelationshipsGraphRow` existed, but the Neo4j-shaped relationship query used `collect(DISTINCT {map})` projections that could leak placeholder property strings, and the Neo4j-shaped call-chain query used parameterized `shortestPath` endpoint anchors that returned no path or hung. Current branch keeps this inside the query adapter seam: NornicDB direct relationship reads now use anchored row queries, transitive callers/callees use bounded one-hop BFS, placeholder relationship properties are stripped, and NornicDB call-chain uses the same bounded BFS rows while Neo4j keeps `shortestPath`. Remote API proof against the same self-repo owner now returns `eshu analyze calls handleRelationships --repo eshu` with `transitiveRelationshipsGraphRow`, `/api/v0/code/relationships` returns `handleRelationships` as a depth-1 incoming transitive caller, and `/api/v0/code/call-chain` returns the two-node entity-ID chain at depth 1.
- repo-scoped call-chain endpoint names now use graph reachability as a narrow ambiguity tie-breaker: if content finds multiple exact endpoint candidates but exactly one start/end pair is reachable within the requested depth, the API resolves to those entity IDs; otherwise ambiguity remains explicit and actionable.
- a fresh 2026-04-25 remote dogfood rerun on `ESHU_HOME=/tmp/eshu-dogfood-fresh.JcCtVs` confirmed the clean-home authoritative lane stays in the fast envelope after the retract/containment fixes: owner start `2026-04-25T17:49:35Z`, `Health: healthy` at `2026-04-25T17:50:41Z` (~`66s`), empty queue at the same checkpoint, `sql_relationship_materialization` `2.709911757s`, `semantic_entity_materialization` `5.067362874s`, and code-call projection cycle completion `4.396658558s` about `72s` after owner start.
- follow-up commit `297da4ca` closes the remaining NornicDB direct-relationship identity gap that dogfood exposed: direct code-relationship metadata and one-hop reads now bridge `id`/`uid` instead of matching `uid` only, and the response path hydrates repo identity before returning results. Live API proof on the same owner now returns canonical `repo_id=repository:r_7dcdc31d` / `repo_name=eshu` for `eshu find name handleRelationships` and no longer leaks placeholder `repo.id` / `repo.name` values through `eshu analyze calls`.
- follow-up commits `2fd6972c`, `7da2cf13`, and `87de590a` finish that direct-relationship read path: the NornicDB adapter now avoids the broad `id OR uid` predicate, uses ordered indexed `uid` then `id` metadata and one-hop lookups, and emits one-hop relationships as a single relationship-pattern `MATCH` so `type(rel)` survives response filtering. The remote API rebuilt on the same owner with `ESHU_GRAPH_BACKEND=nornicdb` now returns direct `CALLS` edges for `eshu analyze calls handleRelationships --repo eshu` instead of an empty `outgoing` array.
- the next remote owner restart reached canonical retraction and failed on the broad stale `File` delete (`DETACH DELETE f`) because it attempted to remove every previous-generation file and edge in one NornicDB request. Current branch now excludes current materialization file paths from file retraction, so stable files are updated in place during the file upsert phase and only removed or renamed paths are candidates for deletion. The follow-up retract redesign now preserves current directory paths and family-scoped entity IDs as well, expands canonical retract coverage for all projectable labels, and explicitly refreshes touched structural edges before stale entity pruning and upsert so the old wipe/rewrite behavior does not leave stale relationships behind when nodes are preserved. The structural-edge refresh now batches file paths and entity IDs after the next remote rerun proved a single file/entity edge refresh over the full repo path list still exceeded the bounded NornicDB request budget.
- fresh remote dogfood on commit `a083cf92` with `ESHU_HOME=/tmp/eshu-dogfood-a083b.IPluPl` proved the scoped retract/file-edge pruning path on a clean graph: owner start `2026-04-25T18:40:56Z`, healthy at `2026-04-25T18:42:01Z` (~65s), collector skipped `.git` (`dirs_skipped..git=1`), snapshot emitted `1,616` files / `56,067` facts in `13.938566986s`, canonical phase-group write completed `9,020` statements in `31.554647267s`, `code_call_materialization` succeeded in `2.015869566s`, `sql_relationship_materialization` in `2.684154892s`, and `semantic_entity_materialization` in `4.97578507s`. The queue drained to `pending=0 in_flight=0 failed=0 dead_letter=0`; live API checks against the fresh NornicDB owner passed `eshu list`, `eshu find name handleRelationships`, `eshu analyze calls handleRelationships --repo eshu`, `eshu analyze chain handleRelationships transitiveRelationshipsGraphRow --repo eshu --depth 3`, and `eshu analyze dead-code --repo eshu --limit 5`; `eshu graph stop` left `owner_present=false` / `graph_running=false`.
- the first remote multi-repo run against `/home/ubuntu/eshu-test-repos` intentionally moved beyond the single self-repo dogfood lane. It discovered the expected repo-directory corpus and skipped `.git` in indexed repos, but dead-lettered `helm-charts` after a `K8sResource` canonical entity chunk of `25` file-scoped one-row inline-containment statements hit the `15s` NornicDB timeout. The failure narrows the next slice: add `K8sResource=5` to the NornicDB entity label phase-group defaults and rerun the same corpus cleanly before judging broader multi-repo performance.
- the follow-up multi-repo rerun with `K8sResource=5` got past the `helm-charts` timeout and shifted the wall to first-generation `retract` work for the two PHP repos. Because those scopes had no prior generation, stale-generation cleanup was a no-op semantically but still expensive operationally. Current branch now propagates `ingestion_scopes.active_generation_id` plus a previous-generation-exists flag through projector claims, marks first-generation canonical materializations, skips stale retract phases only when no prior generation exists, and keeps scoped retraction unchanged for refresh projections or follow-up generations after a failed first attempt.
- the next clean multi-repo rerun proved that grouped-statement caps and row caps are distinct controls. The safe first-generation retract skip moved `php-large-repo-c` through canonical projection quickly (`entities` completed in `42.280045651s`, `Variable` wrote `19,995` rows in `39.958509486s`), but `helm-charts` still dead-lettered because one `redirects-httproute.yaml` statement carried `29` same-file `K8sResource` rows and hit the `15s` NornicDB timeout. Current branch now adds a NornicDB-only `K8sResource=5` entity row cap as well as the existing `K8sResource=5` grouped-statement cap, so file-scoped inline containment splits large manifests into multiple bounded statements.
- the follow-up multi-repo rerun with the `K8sResource` row cap moved the `helm-charts` scope through canonical projection instead of timing out: `K8sResource` completed `275` rows across `193` statements in `131.135765046s` with `max_statement_rows=5` and max grouped execution `11.914367608s`, under the `15s` deadline. The next blocker shifted to the huge PHP API repo (`216,610` facts), where the generic `files` phase grouped `15` 500-row file-upsert statements and hit the same `15s` deadline. Follow-up commit `1e127fb5` tagged file statements as `phase=files`, emitted file-row summaries, and added a NornicDB-only `ESHU_NORNICDB_FILE_PHASE_GROUP_STATEMENTS` cap (`5` default). The next clean rerun proved that grouped-statement cap but still dead-lettered the PHP repo on chunk `2/3` with `5` file statements whose first statement carried `rows=500`; the cap narrowed transaction width but not per-statement row width. Current branch therefore adds `ESHU_NORNICDB_FILE_BATCH_SIZE` (`100` default) and a file-only writer row cap so large file inventories can reduce rows inside each file-upsert statement without changing Neo4j defaults or lowering entity/repository/directory phases.
- remote multi-repo validation of commit `13a5b76b` on `/home/ubuntu/eshu-test-repos` proved the file row cap clears the remaining hard blocker: the large PHP API repo's `files` phase completed `71` file statements in `105.489761166s`, with chunks bounded to `rows=100` instead of the previous `rows=500` timeout shape. The same run completed the full local-authoritative queue with `Health: healthy`, projector `succeeded=23 dead_letter=0 failed=0`, reducer `succeeded=169 dead_letter=0 failed=0`, and queue `pending=0 in_flight=0`. The largest remaining cost is now throughput, not correctness: the PHP repo's `Variable` label wrote `174,411` rows across `20,894` statements in `125.913498259s`, and the full canonical write completed in `241.859647305s` without timeout.
- the corrected 2026-04-25 timing harness added live-owner API proof and separated self-repo, focused-regression, and full-corpus evidence. The self-repo run converged in `85s` and passed repository listing, symbol find, direct calls, call-chain, and dead-code CLI checks; the focused `crossplane-xrd-irsa-role` run converged in `20s` with `K8sResource=1`, proving the previous K8sResource timeout was grouped transaction width. The first full `/home/ubuntu/eshu-e2e-full` run advanced to `321/896` repositories in `722s` before dead-lettering on the reducer semantic retract shape, not parsing or `.git` traversal. A follow-up label-scoped retract run avoided the hard timeout but showed first-generation repos still paying no-op semantic cleanup per label. Current branch now promotes `K8sResource=1` to the default grouped cap, skips semantic retract on first-generation first attempts, and keeps NornicDB refresh/retry semantic retract label-scoped so the next full-corpus run measures the next real bottleneck.
- the next full-corpus run got past that first-generation semantic cleanup and found the next real bottleneck: same-scope reducer graph-write contention. `/home/ubuntu/eshu-e2e-full` failed at `722s` on `hapi-amqp` when `semantic_entity_materialization` timed out on `Function rows=4` while `inheritance_materialization` for the same `scope_id` was concurrently writing the graph for about `16s`. Current branch now applies the same ownership principle used for projector work to reducer claims: claim selection rejects scopes with another unexpired claimed/running reducer item, and batch claims choose only one pending/retrying item per scope so unrelated repos keep parallelism without overlapping graph writes for the same repo.
- the reducer-fenced rerun proved that fix and moved the full-corpus wall forward: `hapi-amqp` crossed cleanly with inheritance at `17.615437958s` followed by semantic materialization at `0.026024905s`, then `/home/ubuntu/eshu-e2e-full` failed later at `932s` after `365/896` repos on a source-local `K8sResource rows=5` inline-containment statement in `iac-eks-argocd/teams/devops-team/rbac.yaml`. Current branch now narrows the NornicDB `K8sResource` row cap to `1` and makes graph write timeout errors include the tuning env var (`ESHU_CANONICAL_WRITE_TIMEOUT`) so future production-style failures are diagnosable from the error string. Follow-up queue plumbing now persists typed graph timeouts as `failure_class=graph_write_timeout` with sanitized phase/label/row details; typed graph write deadlines are now bounded-retry candidates, while deterministic syntax/schema failures remain terminal because they do not implement the retry contract.
- a follow-up diagnostic run with `ESHU_CANONICAL_WRITE_TIMEOUT=30s` moved the full-corpus wall to `1694s` and proved the timeout knob is effective, but it exposed the next real write shape instead of finishing: `semantic_entity_materialization` timed out on `Annotation rows=500`. That slice added a NornicDB semantic `Annotation` row cap so huge annotation batches split before they monopolize the graph writer.
- the rerun with `Annotation=100` moved past that timeout and hit a different concurrency surface: NornicDB returned `conflict: edge ... changed after transaction start` during `inheritance_materialization`. Current branch now treats that optimistic write conflict as transient in the shared graph retry executor and marks exhausted retries queue-retryable, preserving cross-scope parallelism while letting conflict-heavy reducers retry instead of dead-lettering.
- the retry-enabled rerun proved the conflict classifier live, then failed later at `2170s` on a semantic `Function rows=15` statement reaching the `30s` write budget. Current branch narrows NornicDB semantic `Function` rows to `10` while leaving canonical Function tuning unchanged.
- the next full-corpus timing pass used the narrowed Function cap and failed later at `1859s` on `Annotation rows=100` reaching the same `30s` budget, so the branch narrowed Annotation to `50`. The follow-up crossed that wall and reached `504/896` projector scopes before `Annotation rows=50` failed at `1849s`; several successful 50-row Annotation writes were already within roughly five seconds of the deadline. A follow-up with `Annotation=25` crossed the previous `504`-scope wall but still failed on `Annotation rows=25`, with another 25-row statement completing at `25.49501893s`. Current branch narrows only the NornicDB semantic `Annotation` cap to `10` and keeps the timeout as an explicit operator knob rather than raising it again.
- the `Annotation=10` full-corpus timing pass advanced to `1975s` and `522/896` projector scopes before another `Annotation rows=10` timeout. The attribution profile showed the bigger active bottleneck was not another semantic cap: `fsbo-mobile/.yarn/releases/yarn-4.13.0.cjs` generated `45,381` facts from `59` files and repeatedly consumed `6s-8s` per ten `Variable` canonical rows. Current branch now prunes `.yarn` plus Yarn Berry Plug'n'Play loader files (`.pnp.cjs`, `.pnp.loader.mjs`) in default native discovery so generated package-manager bundles do not enter parsing, materialization, or graph projection.
- the `.yarn`-pruned full-corpus run progressed to roughly `620/896` repositories and failed after `47m12s` on `semantic_entity_materialization` with only `Function rows=2`; replaying that same statement against the stopped graph completed quickly. The current branch therefore treats this as contention/coordination evidence, not another batch-size problem: reducer writes now pass `ESHU_CANONICAL_WRITE_TIMEOUT` through Neo4j-driver `tx_timeout` metadata just like the ingester already did, so the NornicDB server sees the same deadline as Eshu's client context. NornicDB source review confirmed snapshot isolation and commit-time conflict detection are expected behavior, so the upstream follow-up is now `orneryd/NornicDB#120`, which maps MVCC conflict/deadlock errors to Neo4j transient transaction codes at the Bolt layer rather than serializing all Eshu graph writes.
- the next full-corpus pass exposed the current IaC-heavy hot path before failure: `K8sResource` canonical writes were already at `rows=1` and one grouped statement, but the one-row `UNWIND ... MATCH File ... MERGE K8sResource ... MERGE CONTAINS` shape repeatedly cost roughly `3.2s-4.1s` under concurrent projection. Because the row and group caps are already minimal, the current branch now routes those one-row file-scoped inline-containment writes through the existing singleton parameterized execute-only statement shape instead of another tuning knob.
- `orneryd/NornicDB#120` has expanded from conflict-code mapping into the current upstream compatibility branch for this full-corpus lane. It now also shares the `nodeLookupCache` mutex across transaction clones, fixing the `fatal error: concurrent map writes` crash seen in the first 2026-04-26 remote full-corpus pass, and teaches NornicDB's generalized `UNWIND/MATCH/MERGE` batch hot path to accept named relationship variables with `SET rel.*`. That last fix keeps Eshu's file-scoped entity-containment statement out of the generic fallback path that previously failed on source text containing `REMOVE`.
- the first `#120`-only full-corpus lane was stopped because it did not include PR `#119`'s SQL-edge hot path, so it was not the combined evidence Eshu needs before promotion. The fork `main` was also realigned with `orneryd/main` after confirming merged PRs `#107`, `#115`, `#116`, and `#118` already represent the old fork-main patch content upstream.
- the current patched-binary evidence lane uses a combined NornicDB branch (`linuxdynasty/NornicDB:eshu-119-120-combined`, commit `86e78f1`) that stacks `#119` and `#120` over `orneryd/main`. Local and remote NornicDB focused gates passed with `go test -tags 'noui nolocalllm' ./pkg/cypher ./pkg/bolt ./pkg/server ./pkg/neo4jcompat -count=1`, and the remote headless binary was rebuilt from that exact commit at `/home/ubuntu/os-repos/NornicDB/bin/nornicdb-headless-eshu-119-120-combined`.
- the combined binary completed the `/home/ubuntu/eshu-test-repos` 23-repo authoritative lane in `80s` after passing the self-repo lane in `20s` and the focused Crossplane lane in `5s` with final `Health: healthy`, empty queue, and no dead letters. The shutdown emitted expected `context canceled` projection logs after the harness stopped the already-healthy owner; those are not counted as run failures.
- the superseded full-corpus lane `/tmp/eshu-perf-20260426T124838Z-119-120-full` used Eshu `323b6b53` and NornicDB `86e78f1`. It passed the self-repo warmup in `20s`, passed focused Crossplane in `5s`, and at the early 896-repo checkpoint was progressing with projector `succeeded=63`, reducer `succeeded=455`, `dead_letter=0`, and no overdue claims. Follow-up drain-gate review showed the old harness could combine early healthy status with later collector completion, so this lane is valid throughput/activation evidence but not a complete projection-drain proof.
- live inspection of that full-corpus lane found a Eshu flow-shape issue independent of NornicDB Cypher compatibility: local-host ingester commits were still doing per-new-repo relationship backfill inside the repository commit transaction. On a first-generation corpus scan every repo is new, so that repeats a corpus-wide `latest relationship facts` scan hundreds of times. Current branch adds a collector batch-drain hook and wires ingester/local-host collection to skip per-commit backfill, then run one deferred `BackfillAllRelationshipEvidence` plus `ReopenDeploymentMappingWorkItems` after the changed-repo batch drains. Focused tests `TestServiceRunCallsAfterBatchDrainedOnceAfterCommittedBatch` and `TestBuildIngesterCollectorServiceDefersRelationshipBackfillToBatchDrain` lock this in.
- the corrected drain-gate lane with Eshu `56d8f9c8` and NornicDB `86e78f1` drained self-repo in `76s` and focused Crossplane in `10s`. The full `/home/ubuntu/eshu-e2e-full` lane stayed healthy-but-progressing with no dead letters and no overdue claims, but still had roughly `6k` queue items outstanding when stopped; the dominant visible backlog was reducer work, especially reopened `deployment_mapping`. This is consistent with the current first-generation queue shape: `896` repos times seven shared follow-up reducer domains can publish `6,272` reducer rows before accounting for completed or reopened work. That moves the next Chunk 3.5/4 task to reducer queue-shape and convergence throughput analysis under the NornicDB `workers=1` default before another full-corpus promotion claim.
- the first `ESHU_REDUCER_WORKERS=2` A/B against the corrected drain gate preserved self-repo and focused Crossplane correctness, but the full corpus reported stalled health from overdue reducer claims rather than NornicDB write conflicts. The root cause is Eshu's batch-claim window: the reducer could lease more intents than workers could start before the `60s` claim lease expired. Current branch therefore keeps Neo4j's broad batch-claim default but sets NornicDB's default `ESHU_REDUCER_BATCH_CLAIM_SIZE` to `1` so a claimed reducer item means a worker can start it immediately.
- the follow-up A/B with `ESHU_REDUCER_WORKERS=2` plus `ESHU_REDUCER_BATCH_CLAIM_SIZE=1` removed the lease-window stall (`overdue_claims=0`), then failed at `311s` on a single-row semantic `Annotation` write while source-local projector work was still running. That makes the next throughput/correctness boundary a NornicDB-specific phase-readiness problem: reducer graph-write domains should not contend with source-local canonical projection during first-generation local-authoritative scans until a measured design proves the overlap is safe. Current branch now adds that first seam at the durable queue boundary: when `ESHU_QUERY_PROFILE=local_authoritative` and `ESHU_GRAPH_BACKEND=nornicdb`, reducer `fact_work_items` claims wait until source-local projector work drains.
- the first full-corpus rerun with that drain gate proved the boundary did what it was supposed to do: reducer claims stayed at `0` while source-local projector work was active, self-repo drained in `75s`, focused Crossplane drained in `15s`, and all `896` repos finished collection in `647.703088168s`. The next hard failure was no longer reducer overlap; it was `search-api-legacy` timing out in canonical `phase=files` after checked-in PEAR/Phing PHP build-tool sources under `framework/library/pear/php/phing/...` inflated the source-local file set. The follow-up lane pruned that Phing subtree, reduced the repo to `104,589` facts, and then named the broader PEAR root at `framework/library/pear/php/PEAR/FixPHP5PEARWarnings.php`, so current branch records the whole family as `files_skipped.content.vendored-pear` instead of widening the graph write timeout or lowering global file batching.
- the focused tail-corpus rerun after PEAR pruning then named a different source-shape problem: the WordPress repo still emitted `204,911` facts and timed out in canonical `phase=files`, but the chunk mixed authored-looking theme files with third-party plugin code under `wp-content/plugins/wordpress-seo/...`. That is exactly the kind of ambiguity Eshu should not solve with a global heuristic. Current branch adds `.eshu/discovery.json` with explicit `ignored_path_globs` and `preserved_path_globs`, prunes matching subtrees before descent, and reports `dirs_skipped.user.<reason>` / `skip_reason=user:<reason>` so repo owners can mark exact noisy roots without hiding authored CMS plugin/theme code by default. The earlier `.eshu/vendor-roots.json` `vendor_roots` / `keep_roots` shape remains accepted for compatibility.
- the next focused tail-corpus run with WordPress vendor roots active moved the failure to `php-large-repo-b`: canonical `phase=files` chunk times rose monotonically from subsecond to `29.65s`, then timed out at chunk `21/24`. Because that shape follows graph size rather than a single row family, Eshu inspected NornicDB's `MERGE` lookup path and found that explicit property indexes, not uniqueness constraints alone, are the lookup precondition before NornicDB falls back to label scans. Commit `ae314624` adds NornicDB-only lookup indexes for the file-phase anchors `Repository.id`, `Directory.path`, and `File.path`.
- recent operator-evidence commits tighten the active Chunk 3.5 lane without changing production Neo4j behavior: `b309200c` classifies canonical graph write deadlines as typed `graph_write_timeout` failures, `f28aabec` surfaces the latest persisted queue failure in status/progress output, and `1679950f` adds versioned discovery advisory reports for noisy-repo tuning.
- the isolated `php-large-repo-b` reruns on the remote 16-vCPU test host then closed the next source-shape loop. With the NornicDB lookup indexes active but no repo-local map, the repo still emitted `11,641` files / `410,855` facts (`221.9s` snapshot, `69.2s` fact commit) and spent graph time on archive paths such as `_old/...`. A repo-local archive map (`_old/**`, `*_old/**`, `*-old/**`) skipped `17` directories and reduced the input to `7,644` files / `284,188` facts (`147.0s` snapshot, `50.7s` commit). Adding explicit static-library globs for proven third-party browser files (`fotorama.js`, `photorama.js`, `calendar.js`, `sharethis.js`, `masonry.pkgd.js`, `modernizr.js`) skipped only `199` more files but reduced the input to `206,890` facts (`135.8s` snapshot, `28.6s` commit) and completed local_authoritative healthy in `471s`; canonical files completed in `88.0s`, entities in `53.6s`, and the queue drained back to healthy. Treat this as the working model for noisy repo families: use built-in safe filters for obvious generated/vendor shapes, then require explicit repo/org maps for archive roots and checked-in browser libraries where authored-code risk is ambiguous.
- current branch also closes the missing patched-binary switch proof: `TestNornicDBBatchedEntityContainmentFullStackUsesCrossFileBatchedEntityRows` verifies the opt-in `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true` writer shape sends normal cross-file entities through one row-scoped batched statement and avoids file-scoped singleton execution. The queued full-corpus A/B run remains the performance gate before considering that switch for default promotion.
- the isolated `php-large-repo-b` rerun after the local-host child-exit fix reached healthy main queue in about `11m39s` with `284,188` facts and no hard graph-write failures, then exposed a reducer code-call guard: the sidecar kept failing on `code call acceptance intent scan reached cap (10000)`. Current branch makes that full-slice guard explicit as `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` (`250000` default) because code-call projection must load the complete accepted repo/run before retracting and rewriting repo-wide CALLS edges. The follow-up remote rerun on Eshu `33e540b6` and NornicDB `86e78f1` cleared the guard: source-local projection succeeded in `400.532644185s`, canonical `files` completed in `91.243188629s`, canonical `entities` completed in `123.190269023s`, code-call projection completed `24,583` rows in `21.615712301s`, final status was `Health: healthy` with queue `pending=0 in_flight=0`, and the exact error scan found no acceptance-cap, graph-timeout, dead-letter, panic, or fatal lines.
- current branch adds a consolidated [Environment Variables](../reference/environment-variables.md) operator reference so Chunk 3.5 / Chunk 4 tuning does not rely on scattered folklore. The page records each supported variable's owner runtime, default, purpose, and when to tune it, and explicitly separates correctness guards such as `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` from graph-write throughput knobs such as `ESHU_CANONICAL_WRITE_TIMEOUT` and the NornicDB phase/label caps.
- the latest full-corpus lane with Eshu `d1141690` and NornicDB `86e78f1` completed source-local/projector work for all `896` repos at about `41m12s`, then hit a reducer `graph_write_timeout` at about `1h05m` on `semantic_entity_materialization` (`Function rows=4`, `attempt_count=1`). Because a nearby same-size Function statement completed in `26.724928881s`, this is timeout-pressure evidence, not enough proof of a deterministic semantic-write bug. Current branch now treats typed graph write deadlines as bounded-retry candidates and documents the next validation ladder: isolate the failing repo with a larger correctness-validation write budget, then rerun a 15-20 repo corpus, then repeat the full corpus only after those smaller lanes drain cleanly.
- the focused replay of that exact failed repo used Eshu `a8ee127b`, NornicDB `86e78f1`, fresh `ESHU_HOME`, and `ESHU_CANONICAL_WRITE_TIMEOUT=120s` against `/home/ubuntu/eshu-e2e-full/api-node-ai-product-description-generation`. It drained healthy in roughly `12s`: discovery emitted `544` facts, source-local projection succeeded in `0.325947169s`, the prior `Function rows=4` semantic statement completed in `0.006154829s`, all `9` queue rows ended `succeeded`, and the log scan found no `graph_write_timeout`, dead letter, panic, fatal, or acceptance-cap lines. This validates the single-repo-first ladder and moves the next proof gate to a 15-20 repo medium corpus before another full-corpus attempt.
- the medium gate then used Eshu `1a978f69`, NornicDB `86e78f1`, fresh `ESHU_HOME`, and `ESHU_CANONICAL_WRITE_TIMEOUT=120s` against `/home/ubuntu/eshu-test-repos` (`23` repos). It drained healthy in about `5m09s`: projector `succeeded=23`, reducer `succeeded=184`, total `fact_work_items` `succeeded=207`, queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`, and `content_entities=182,305`. The log scan found no `graph_write_timeout`, panic, fatal, acceptance-cap, or true failure lines. This clears the medium correctness gate and moves the next proof back to the full corpus with the same correctness-validation timeout and discovery-report capture for later performance tuning.
- the next full-corpus correctness lane used Eshu `95f2c8aa`, NornicDB `86e78f1`, fresh `ESHU_HOME`, and `ESHU_CANONICAL_WRITE_TIMEOUT=120s` against all `896` repos. It was intentionally stopped after about `4h48m` because it had already proved the retry behavior and exposed the next bounded semantic row family: source-local/projector work completed for all repos, queue status still had `dead_letter=0 failed=0`, but the latest retrying failure was `semantic_entity_materialization` with `TypeAlias rows=42`. The same window showed `TypeAlias rows=5` completing in `21.326317754s`, `Annotation rows=6` consuming `108.062344975s`, and `TypeAnnotation rows=181` consuming `118.286012152s`. Current branch therefore narrows the default NornicDB semantic caps to `Annotation=5,TypeAlias=5,TypeAnnotation=50` while keeping the medium/focused proof ladder before another full-corpus drain attempt.
- the focused replay of that exact `TypeAlias rows=42` repo used Eshu `a5db4165`, NornicDB `86e78f1`, fresh `ESHU_HOME`, and `ESHU_CANONICAL_WRITE_TIMEOUT=120s` against `/home/ubuntu/eshu-e2e-full/api-node-ai-provider`. It drained healthy in about `22s`: projector `succeeded=1`, reducer `succeeded=8`, queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`, and the semantic reducer item completed in `0.456658928s`. The former `TypeAlias rows=42` write split into eight `rows=5` statements plus one `rows=2` statement, each completing in milliseconds, with no `graph_write_timeout`, semantic failure, dead letter, panic, or fatal lines.
- the DB-driven medium rerun used the same Eshu `a5db4165` and NornicDB `86e78f1` baseline against `/home/ubuntu/eshu-test-repos` (`23` repos) with a fresh `ESHU_HOME`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, and the corrected drain gate that polls Postgres instead of matching stale progress logs. It reached `healthy_db_drained` in about `5m`: projector `succeeded=23`, reducer `succeeded=184`, total `fact_work_items` `succeeded=207`, and queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. The log scan found no `graph_write_timeout`, semantic failure, true dead letter, panic, fatal, or acceptance-cap lines. This re-clears the medium gate with the new semantic caps and makes the next validation step a full `/home/ubuntu/eshu-e2e-full` DB-driven drain run, not another broad timeout increase.
- the next full-corpus DB-driven drain run used Eshu `95d6091e`, NornicDB `86e78f1`, fresh `ESHU_HOME`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, `ESHU_REDUCER_WORKERS=2`, and `ESHU_REDUCER_BATCH_CLAIM_SIZE=1` against all `896` repos. It completed source-local/projector work for all repos at about `43m38s` with no failure rows, then exposed reducer convergence as the active bottleneck: long `inheritance_materialization` items regularly took `100s+`, one `workload_materialization` item took `323.084087394s`, and at about `1h12m` the queue correctly retried a `semantic_entity_materialization` timeout (`TypeAlias rows=4`) instead of dead-lettering. Code review of that live lane found a Eshu recovery bug independent of the slow Cypher shapes: batch reducer claims only selected `pending` / `retrying` rows, unlike the single claimer, so a restart in batch mode could strand expired `claimed` / `running` reducer rows. Current branch aligns batch reclaim with the single-claim path while preserving same-scope reducer fencing.
- the focused replay of that exact failed repo (`/home/ubuntu/eshu-e2e-full/api-node-ai-summary`) used Eshu `a592b480`, the same NornicDB binary, fresh `ESHU_HOME`, and the same `120s` write budget. It drained healthy immediately: projector `succeeded=1`, reducer `succeeded=8`, queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`, and the formerly failing `semantic label=TypeAlias rows=4` statement completed in `0.00333166s`. This disproves row count as the root cause for the full-corpus timeout and points back to accumulated graph-size lookup behavior. Current branch therefore adds NornicDB-only explicit `uid` lookup indexes for every graph label in `uidConstraintLabels`, mirroring the earlier `Repository.id` / `Directory.path` / `File.path` merge-anchor fix while leaving Neo4j on constraint-backed lookup behavior.
- Eshu `53bb7803` was pulled onto the remote VM, rebuilt, and verified against the real NornicDB binary: the NornicDB compatibility syntax test accepted the new `CREATE INDEX ... ON (n.uid)` form, the focused `api-node-ai-summary` replay drained healthy again with `TypeAlias rows=4` completing in `0.00400289s`, and the medium `/home/ubuntu/eshu-test-repos` corpus drained healthy in `289s` (`projector succeeded=23`, `reducer succeeded=184`, `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`). The next proof step is a fresh full `/home/ubuntu/eshu-e2e-full` run on `53bb7803` to validate whether explicit semantic UID lookup indexes remove the accumulated-graph timeout.
- reducer write-path review found the same lookup-index asymmetry in workload materialization: the direct workload Cypher matches and merges through `Workload.id`, `WorkloadInstance.id`, and `Platform.id`, while NornicDB only had explicit merge-anchor indexes for `Repository.id`, `Directory.path`, `File.path`, and semantic `uid`. Because the prior full run already showed a `workload_materialization` item taking `323.084087394s`, current branch adds NornicDB-only explicit lookup indexes for those workload identity keys before restarting the full-corpus proof, rather than waiting for another full-run timeout to rediscover the same graph-size lookup class.
- the fresh full-corpus lane with the workload lookup indexes finished source-local/projector work for all `896` repos at about `42m` and then began reducer drain with no failure rows. It also exposed a Eshu queue-ownership invariant independent of NornicDB query shape: batch reducer mode could lease more work items than ready workers could start, leaving queued claimed rows without heartbeats until slow graph-write work completed. Current branch now makes the batch dispatcher worker-readiness-aware, so `ClaimBatch` only leases as many intents as can immediately enter the heartbeat-protected execution path; this preserves the existing batch claim API while removing the preclaimed-but-unowned lease window before the next full-corpus proof.
- the next live full-corpus lane validated that dispatcher change and moved the bottleneck from lease safety to reducer query shape. Source-local drained all `896` repos at about `42m` with `2,606,031` facts (`p50=0.25s`, `p90=5.46s`, `p95=139.79s`, max `746.01s`), and reducer opened with exactly `2` claimed rows under `ESHU_REDUCER_WORKERS=2` with no overdue claims. At `54m52s`, reducer had `215` successes and no failure rows, but every slow reducer success above `10s` was `inheritance_materialization` (`101s`, `165s`, `166s`, `267.879987639s`) while most other domains remained millisecond-scale. The root cause is a Eshu first-generation flow issue: inheritance materialization retracted reducer-owned inheritance edges even when `PriorGenerationCheck` would prove no prior generation exists. Current branch now skips that retract only for first attempt of a true first generation, while keeping retracts on retries and later generations for cleanup correctness. A focused remote replay of the formerly slow `IP2Location` inheritance row on commit `715bed68` drained healthy with queue `pending=0 in_flight=0`, logged `inheritance materialization skipped first-generation retract`, and completed the inheritance reducer item in `0.004070534s`.
- the follow-up focused inheritance subset on commit `4481a894` hardlinked ten formerly slow-in-full repos into one workspace (`TechSupportDatabaseUpdates`, `Tech-Support`, `ansible-install-ssm-agent`, `ansible-aws-nightlysnapshots`, `IP2Location`, `XMLVALIDATOR2`, `POCMatterport`, `api-node-ai-facet-formatter`, `api-aws-metadata`, `api-listings-360`). It drained in `13s` with `10` projector successes and `72` reducer successes, no failures, no overdue claims, and inheritance reducer durations between `0.001082855s` and `0.010728765s`. The medium `/home/ubuntu/eshu-test-repos` gate then drained `21` scopes / `174` work items in `20s` with queue `pending=0 in_flight=0`, no failures, and no overdue claims; the slowest inheritance item was `0.204147545s`, and the only reducer item above `1s` was `semantic_entity_materialization` at `1.704710746s`. This clears the focused and medium proof ladder for the first-generation inheritance retract fix before the next full-corpus measurement run.
- after the semantic Cypher probe showed the old `UNWIND ... MATCH File ... MERGE node ... SET n += row.properties` shape missed NornicDB's generalized batch hot path, Eshu `f72724d6` switched only the NornicDB reducer semantic writer to a merge-first explicit-row template (`UNWIND ... MERGE node ... SET field assignments ... MATCH File ... MERGE CONTAINS`) while leaving Neo4j on the existing writer for comparison. The remote self-repo lane with NornicDB `86e78f1` drained healthy: source-local projection succeeded with `51,811` facts in `31.868573152s`, semantic materialization completed in `2.999627444s`, and the queue ended empty with no retry/dead-letter/failure rows.
- the 2026-04-27 medium `/home/ubuntu/eshu-test-repos` lane on the same Eshu/NornicDB pair drained `23` repos in `316s` with final `Health: healthy` and queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. The largest source-local item was `php-large-repo-a` at `148,948` facts in `170.376262229s`; its semantic reducer item completed in `5.954617004s`, and the error scan found no `graph_write_timeout`, semantic failure, acceptance-cap, panic, fatal, or dead-letter lines. This re-clears the focused and medium proof ladder after the merge-first semantic writer. Next validation should be a DB-driven full-corpus drain with discovery-report capture, not another broad timeout increase or blind semantic cap change.
- a follow-up targeted five-repo lane on Eshu `c598000d` intentionally avoided the full corpus and combined the previous semantic timeout repos with the two noisy PHP repos: `api-node-ai-provider`, `api-node-ai-summary`, `api-node-ai-product-description-generation`, `php-large-repo-b`, and `php-large-repo-a`. It drained healthy in `854s` with queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. The two large source-local projections succeeded (`148,948` facts in `166.496305644s` and `176,201` facts in `521.49982913s`), and their semantic reducers completed in `6.33473887s` and `15.762956452s`. The scan found no graph write timeout, semantic failure, acceptance-cap, panic, fatal, retry, or dead-letter lines. This proves the problem-repo lane first; next validation should scale to a larger representative subset before a full 896-repo run.
- the larger representative subset then used Eshu `5c9b169a`, NornicDB `86e78f1`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, `ESHU_REDUCER_WORKERS=2`, `ESHU_REDUCER_BATCH_CLAIM_SIZE=1`, and `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT=250000` against `50` repos selected from `/home/ubuntu/eshu-e2e-full`. It drained healthy in `884s` with final queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`, and the failure scan found no `graph_write_timeout`, semantic failure, acceptance-cap, panic, fatal, retry, or dead-letter lines. The only long hold was source-local projection for `php-large-repo-b`, which already had all `176,201` facts persisted while it wrote `131,977` `Variable` and `28,926` `Function` entities into NornicDB. Its `Variable` phase crossed `102,654` rows and `13,200` statements before drain, showing the remaining optimization target is high-cardinality canonical entity writes plus repo-local generated/vendor input classification, not reducer semantic correctness. This clears the larger-subset proof ladder; the next full-corpus run should use the same DB-driven drain gate and discovery-report capture, while any next Eshu code change should first measure why `Variable` entity write cost grows with graph size.
- the next full-corpus lane with `ESHU_REDUCER_WORKERS=8` showed the remaining reducer semantic failures were graph-size relationship checks, not simple row-width mistakes: all `896` projector items completed, but `Function rows=1`, `Function rows=4`, `Annotation rows=5`, and `TypeAlias rows=5` still timed out at `ESHU_CANONICAL_WRITE_TIMEOUT=120s`. Live graph probes showed indexed `uid` node lookups usually returned quickly, while even targeted `File-[:CONTAINS]->entity` checks took roughly `2.3s-2.9s`. Current branch therefore makes NornicDB semantic materialization property-only for canonical-owned labels: source-local canonical projection owns entity node lifecycle and file containment, semantic materialization enriches those nodes by `uid`, retry/prior-generation cleanup uses `REMOVE` on semantic properties rather than node deletion, and semantic-owned `Module` keeps the existing file-containment write path because canonical modules are keyed by `name`.
- the follow-up 2026-04-27 representative 20-repo lane used Eshu `9ff4252f`, NornicDB `v1.0.43`, `ESHU_REDUCER_WORKERS=8`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT=250000`, and the property-only NornicDB semantic materialization path against a hardlinked subset from `/home/ubuntu/eshu-e2e-full`. It drained healthy in about `17m32s`: projector `succeeded=20`, reducer `succeeded=163`, and queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. The long pole was `php-large-repo-b`: Eshu had already skipped `_old` and `vendor` paths in the content store (`vendor_files=0`, `_old=0`), but the indexed repo still contained `7,636` files, about `1.14M` lines, `131,977` `Variable` entities, and `28,926` `Function` entities. Canonical `Variable` projection completed all `131,977` rows as `13,198` statements / `2,640` grouped executions in `204.509776279s` with `max_execution_duration_s=0.178891191`; semantic reducer writes drained without graph timeouts. This clears the representative subset gate and confirms the next performance work should measure high-cardinality canonical entity volume and repo/org site-template classification before another full-corpus burn.
- the next 2026-04-27 representative 100-repo lane used Eshu `00913d60`, NornicDB `v1.0.43`, `ESHU_REDUCER_WORKERS=8`, `ESHU_REDUCER_BATCH_CLAIM_SIZE=8`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT=250000`, and the same property-only NornicDB semantic path against a deterministic subset from `/home/ubuntu/eshu-e2e-full`. It drained healthy in about `24m27s`: projector `succeeded=100`, reducer `succeeded=772`, and queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`; there were no persisted failure rows and one retried NornicDB transient transaction conflict. The final reducer tail showed low CPU and idle disk while only a few graph writes remained in flight, and individual semantic statements occasionally took tens of seconds. That evidence points at backend write serialization / transaction-contention boundaries, not VM resource saturation or another discovery/parser bottleneck. This clears the larger-subset proof ladder; the next validation gate is a full `/home/ubuntu/eshu-e2e-full` drain, while the next performance slice should measure conflict-domain routing before changing row caps again.
- the latest single-repo performance slice is measuring before redesigning the repo workflow. The isolated `php-large-repo-b` run after worker-parallel pre-scan dropped snapshot stream time from `148.403917778s` to `40.991875025s`, leaving fact commit at about `51.344587838s` and source-local projection at `303.054837237s`. Current branch now adds operator-visible stage logs for those remaining seams: `ingestion commit stage completed` splits the durable Postgres transaction into catalog, fact-upsert, relationship-backfill, queue-enqueue, and commit timings, while `projector work stage completed` / `projector runtime stage completed` split fact load, build, canonical write, content write, and intent enqueue. The next focused rerun should decide whether Chunk 4 needs fact-store batching, content-store batching, graph-query shape work, or a larger chunked-generation architecture.
- the content-writer sub-stage rerun on Eshu `318c83e4` and NornicDB `v1.0.43` drained `php-large-repo-b` healthy after remote disk cleanup. The measured source-local ledger is now: discovery `0.299s`, pre-scan `16.511s`, parse `16.499s`, materialize `6.951s`, fact upsert `51.223s`, fact load `12.816s`, projection build `1.491s`, content file upsert `11.418s`, content entity upsert `158.293s`, canonical graph write `119.290s`, and reducers fully drained. Because `prepare_entities` was only `0.117s`, the next slice is a focused Postgres content-entity persistence experiment: expose and test the entity batch-size seam first, then A/B one large repo before touching trigram indexes, transaction semantics, or chunked-generation architecture.
- that focused persistence experiment found the batch-size seam is useful for diagnosis but not the fix for the noisy repo. On the same isolated `php-large-repo-b` run, `ESHU_CONTENT_ENTITY_BATCH_SIZE=600` reduced `content_entities` statements from `537` to `269`, yet `upsert_entities` stayed essentially unchanged (`158.293s` to `158.814s`). A direct Postgres microbench showed the root cost is `content_entities_source_trgm_idx` maintenance over oversized entity snippets: unindexed copy `1.661s`, btree-indexed copy `2.827s`, full trigram-indexed copy `132.174s`. `Variable` entities contributed about `1.108 GB` of `source_cache`; bounding oversized `Variable` snippets to `4 KiB` while preserving full-file content search reduced the microbench indexed text to `168 MB` and full-index insert to `30.982s`. The next Chunk 3.5 proof is a fresh single-repo rerun with that source-cache shaping rule before returning to medium or full-corpus validation.
- the fresh single-repo proof on Eshu `f8322c41` validated that shaping rule end to end. The run used NornicDB `v1.0.43`, `ESHU_REDUCER_WORKERS=8`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, and the same isolated `php-large-repo-b` repo; it drained healthy with projector `1/1`, reducer `8/8`, and queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. Fact upsert fell from about `52.4s` to `19.752s`, content entity upsert fell from `158.293s` to `31.956s`, total content write fell from about `170s` to `43.762s`, and total source-local projection fell from about `309s` to `165.604s`; canonical graph write stayed comparable at `120.248s`, confirming the fix hit the intended Postgres/fact-payload bottleneck. The persisted table still had `160,909` entities, with `37,288` truncated `Variable` rows, `164 MB` total `source_cache`, and no error-only scan hits for `ERROR`, `graph_write_timeout`, panic, fatal, or acceptance-cap lines.
- the follow-up medium-corpus proof on Eshu `a7078ddf` used NornicDB `v1.0.43`, `ESHU_REDUCER_WORKERS=8`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT=250000`, and `/home/ubuntu/eshu-test-repos`. It drained `23` repos healthy in about `3m11s`, compared with the prior same-corpus healthy lane at about `5m16s`: projector `23/23`, reducer `184` succeeded work items, and queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. The largest PHP projector item dropped from about `170.4s` to `101.3s`, while the run-level long pole became canonical graph write (`78.490s` for `php-large-repo-a`). This promotes the source-cache shaping rule to medium-corpus evidence and moves the next Chunk 3.5 optimization target back to canonical graph Cypher shape / NornicDB lookup behavior rather than further content-store tuning.

Current tuning plan:
- the latest-main NornicDB full-corpus lane is promotion-positive for Chunk 5b:
  Eshu `a2c630af` with NornicDB `main` `b68b4ef` drained `8458/8458` work
  rows in `878s`, left pending, in-flight, retrying, failed, and dead-letter
  rows at `0`, and passed API/MCP relationship-evidence drilldowns.
- keep the current reducer conflict-key fencing as an ownership invariant, not
  a NornicDB-only timeout workaround. Future tuning should measure conflict
  domains, graph write shape, and content/fact persistence before changing
  worker or batch defaults again.
- keep the safer semantic label caps and NornicDB write budgets as the current
  baseline. Change them only after a focused attribution profile names the
  label, row count, graph size, and query shape.
- complete the Neo4j parity optimization research before closing Chunk 5b. If
  Neo4j remains a production-promoted alternative for companies that already pay
  for and operate Neo4j, it needs a focused adapter optimization pass and a
  terminal baseline. If the evidence later points to a narrower support posture,
  document that explicitly so users understand why NornicDB carries the fast
  path.
- keep Bolt/Cypher portability bounded to documented adapter seams. Do not let
  support for Neo4j, NornicDB, or any future Cypher-speaking database turn into
  handler-level branches or one-off workaround sprawl.
- treat the old `ESHU_REDUCER_WORKERS=1` and single-item claim window as historical stability baselines, not the final performance answer. The active design now uses bounded reducer workers plus durable conflict keys; the next evidence should decide whether shared projection needs the same partition-lease seam.
- keep the default NornicDB graph write budget at `30s`: the full-corpus timing run proved `15s` is too tight under production-scale local_authoritative concurrency, while `30s` still exposed the real `Annotation rows=500` statement-shape issue instead of hiding it. Timeout errors must continue to name `ESHU_CANONICAL_WRITE_TIMEOUT` and include the statement summary, and both ingester/projector and reducer graph writers must send the same budget as Bolt `tx_timeout` metadata.
- keep rebuilding dogfood NornicDB headless binaries directly from upstream `main` for merged fixes such as PRs `#115`, `#116`, and `#118`; use explicit `ESHU_NORNICDB_BINARY` / installer `--from` binaries for active compatibility lanes
- keep using explicit latest-main `ESHU_NORNICDB_BINARY` / installer `--from` binaries until a future release or accepted-build policy replaces that flow
- add a user-editable vendored-source map/config after the evidence-backed PEAR slice: built-in hard skips should stay limited to proven high-confidence families, while repo/workspace config should let users declare custom vendored roots and keep-roots without requiring a Eshu release for every legacy layout. The first pass should report suspected vendored roots before skipping unproven families by default.
- keep full-corpus runs as the final validation gate only. The active ladder is focused repo replay, representative 15-20 repo subset, larger representative subset when needed, and only then `/home/ubuntu/eshu-e2e-full`; otherwise every timeout investigation burns hours before proving the small-scope hypothesis.
- use `eshu index --discovery-report <file>` for focused noisy-repo analysis
  before adding more global discovery filters. The advisory JSON captures
  top noisy directories/files, entity cardinality, and skip breakdowns so
  `.eshu/discovery.json` changes are evidence-backed rather than timeout-driven.
- the latest full-corpus correctness lane `/tmp/eshu-full-validation-20260427T024256Z-95f2c8aa`
  was stopped at the next semantic attribution point rather than left to burn
  another full-run cycle. It proved typed timeouts retry instead of
  dead-lettering, but it is not promotion-positive because the queue had not
  drained.
- keep dead-code truth labeled `derived`; the bounded policy buffer makes small-limit CLI output useful again, but broader root modeling is still required before promoting dead-code from derived to exact
- keep NornicDB call-chain and transitive relationship reads on bounded BFS until upstream proves parameterized `shortestPath` endpoint anchors and projected map collection behave like Neo4j on the Eshu query corpus
- bundle future small heuristic changes with their safety gate when they are one logical fix, so rollback/bisect history tracks the behavioral unit rather than the order in which the concern was discovered
- treat the growing `ESHU_NORNICDB_*` surface as an operator contract: keep the consolidated [NornicDB Tuning](../reference/nornicdb-tuning.md) page current, and add new phase-specific knobs only after logs prove the existing phase/label controls do not describe the bottleneck
- watch future heavy phases such as call edges, infra edges, and other shared reducer domains, but do not pre-create tuning knobs for them until repo-scale evidence names the phase, row count, grouped statement count, and timeout shape

---

## Executive Summary

This plan implements a local-first developer workflow without creating a second
authoritative graph product.

The work is organized around one principle:

- lightweight local mode should be excellent for code lookup and code
  comprehension
- authoritative graph truth should remain owned by the full local stack and
  production runtime path

The implementation is therefore split into six chunks:

1. define the capability contract and truth labels
2. make query surfaces depend on capability ports instead of backend brands
3. ship the lightweight local code-intelligence host
4. harden authoritative graph-backed code analysis in full local stack and prod
5. add backend conformance testing before any new graph backend is declared
   supported
6. add OCI-packaged collector plugin seams at the fact-emission boundary

This keeps production risk bounded while still giving developers the local
stdio MCP workflow they want.

## Success Criteria

### Developer experience

- a developer can install one `eshu` binary and use local stdio MCP without
  Docker for code-intelligence queries
- local mode supports single-repo and monofolder indexing
- local mode is explicit when an answer is `exact`, `derived`, or `fallback`

### Production safety

- the deployed runtime contract remains split by ownership
- no production query or reducer feature regresses because of local-mode work
- service-profile runtime tests continue to pass unchanged

### Architecture

- API, MCP, projector, reducer, and query layers depend on capability ports
  instead of concrete backend brands where practical
- backend conformance tests exist before any alternate graph backend is called
  supported

### Extensibility

- new collectors can be packaged as OCI artifacts and emit facts through a
  stable plugin contract

---

## Query Capability Matrix

This matrix is the contract the implementation should target.

| Capability | Lightweight local | Full local stack | Production |
| --- | --- | --- | --- |
| Exact symbol lookup | Yes | Yes | Yes |
| Fuzzy symbol search | Yes | Yes | Yes |
| Variable lookup | Yes | Yes | Yes |
| Code / comment / docstring search | Yes | Yes | Yes |
| Decorator / annotation search | Yes | Yes | Yes |
| Argument-name search | Yes | Yes | Yes |
| Methods on class | Yes | Yes | Yes |
| Import / reference discovery | Yes | Yes | Yes |
| Inheritance / implementation discovery | Yes, when semantic facts suffice | Yes | Yes |
| Complexity and hotspot queries | Yes | Yes | Yes |
| Direct callers / callees | Derived or exact only if proven | Yes | Yes |
| Transitive callers / callees | No promise unless authoritative graph exists | Yes | Yes |
| Call-chain path tracing | No promise unless authoritative graph exists | Yes | Yes |
| Dead code | No promise unless authoritative graph exists | Yes | Yes |
| Code + infra blast radius | Limited | Yes | Yes |

---

## Chunk 1: Capability Contract And Truth Labels

### Goal

Define the product contract before changing more runtime behavior.

### Work

- introduce common truth labels across CLI, MCP, and HTTP:
  - `exact`
  - `derived`
  - `fallback`
- add one shared truth-level type and response field rather than scattered
  prose-only fallback strings
- define a structured unsupported-capability error for high-authority queries
  that lightweight local mode cannot answer correctly
- document which queries belong to:
  - `CodeSearch`
  - `SymbolGraph`
  - `CallGraph`
  - `CodeQuality`
  - `PlatformImpact`
- update API/MCP/CLI docs to reflect capability semantics rather than backend
  assumptions

### Likely touch points

- `docs/docs/why-eshu.md`
- `docs/docs/reference/http-api.md`
- `docs/docs/guides/mcp-guide.md`
- `docs/docs/reference/truth-label-protocol.md`
- `docs/docs/reference/capability-conformance-spec.md`
- `specs/capability-matrix.v1.yaml`
- `go/internal/query/openapi*.go`
- MCP tool descriptors and response payload structs

### Verification

- focused query/API tests for new truth labels
- strict docs build

---

## Chunk 2: Capability Ports Instead Of Backend Brands

### Goal

Reduce direct dependency on `Neo4jReader` and backend-specific wiring in the
read path.

These ports do not exist today as named interfaces. This chunk is net-new
interface extraction, not a move to an already-portable design.

### Work

- extract or tighten the read-side storage-seam interfaces first:
  - `GraphQuery`
  - `ContentStore`
- keep higher-order capability groupings such as `CodeSearch`,
  `SymbolGraph`, and `CallGraph` as follow-on interfaces only if adapter
  tests show that the storage-seam ports are too coarse
- move API and MCP construction toward these ports rather than concrete backend
  readers
- keep the existing service runtime behavior the same while making backend
  swaps a wiring concern
- keep wire compatibility by supporting parallel old/new wiring during the
  extraction until contract tests prove equivalence

### Shared-state and concurrency considerations

- do not widen transaction scope accidentally while extracting interfaces
- keep graph writes and relational writes owned by their existing runtimes
- preserve reducer/projector ordering and current queue contracts

### Likely touch points

- `go/cmd/api/wiring.go`
- `go/cmd/mcp-server/wiring.go`
- `go/internal/query/*.go`
- `go/internal/projector/*.go`
- `go/internal/reducer/*.go`
- `go/internal/storage/*`

### Verification

- `cd go && go test ./internal/query ./cmd/api ./cmd/mcp-server -count=1`
- `cd go && go vet ./internal/query ./cmd/api ./cmd/mcp-server`
- contract tests proving old and new wiring return the same response shape

---

## Chunk 3: Lightweight Local Code Intelligence Host

### Goal

Ship a single-binary local host that gives developers a strong stdio MCP and
CLI story without requiring Docker.

### Work

- manage embedded local Postgres lifecycle inside `eshu`
- add a local host mode used by `eshu watch`, `eshu mcp stdio`, and local query
  commands
- persist local index state under a stable per-workspace data root
- define the local data-root spec: layout, version file, ownership lock,
  migration rules, and reset behavior
- support:
  - single repo
  - monofolder / multi-repo workspace
- expose the local code-intelligence tier:
  - definitions
  - search
  - methods
  - imports
  - inheritance where semantic facts suffice
  - complexity

### Workflow shape

1. file change or initial index enters local host
2. collector parses and emits facts
3. projector writes content/entity/search-support tables
4. query surfaces read those tables directly
5. stdio MCP serves the same query contract as CLI/API

### Shared-state inventory

- local Postgres data directory
- workspace ownership record
- local status/report state
- content/entity/query-support relational tables

### Concurrency plan

- one local host process owns the workspace data root
- second invocations use a lock protocol with stale-lock recovery and fail-fast
  behavior when safe attachment is not possible
- fsnotify events are debounced and coalesced to avoid parse storms
- collector and projector work stay bounded with explicit backpressure
- child runtime shutdown must be coordinated and observable
- no orphaned embedded Postgres process on `SIGINT` or host shutdown
- embedded Postgres crash recovery and stale data-root ownership must be
  exercised explicitly

### Telemetry

- local host lifecycle spans
- local Postgres startup/shutdown logs
- query truth-level counters
- local index freshness and queue/status metrics where applicable

### Likely touch points

- `go/cmd/eshu/*.go`
- `go/internal/runtime/*.go`
- `go/internal/storage/postgres/*.go`
- `docs/docs/reference/local-data-root-spec.md`
- `docs/docs/reference/local-host-lifecycle.md`
- `docs/docs/reference/local-performance-envelope.md`
- local MCP startup and discovery code

### Verification

- focused CLI/runtime tests
- local lifecycle tests for clean shutdown
- perf-envelope smoke tests against the documented local targets
- manual smoke test for `eshu watch .` + `eshu mcp stdio`

---

## Chunk 3.5: NornicDB Laptop Sidecar And `local_authoritative`

### Goal

Add an explicit authoritative-local runtime contract without silently turning
it into lightweight mode.

### Work

- add `local_authoritative` runtime selection to the local host
- default laptop graph discovery and future install flow to
  `nornicdb-headless`, while allowing explicit opt-in to the larger full
  `nornicdb` binary
- persist profile and graph-backend metadata in `owner.json`
- reserve graph-sidecar paths inside the local workspace data root
- fail loudly when `local_authoritative` is requested before the graph sidecar
  lifecycle is wired
- add the graph-sidecar startup, health, and shutdown lifecycle behind the
  local host once the NornicDB adapter is ready
- add the first installer slice:
  `eshu install nornicdb --from <source> [--sha256 <hex>] [--force]`, which
  verifies a local binary, local tar archive, or URL-backed archive, copies
  the extracted binary to `${ESHU_HOME}/bin/nornicdb-headless`, and records a
  managed install manifest without yet inventing a no-arg release selector

### First implementation slice

- add `graph/` to the local workspace layout
- add `profile`, `graph_backend`, and graph-sidecar metadata fields to
  `owner.json`
- make the local host resolve `ESHU_QUERY_PROFILE` and `ESHU_GRAPH_BACKEND`
  explicitly instead of hardcoding lightweight mode
- reject unsupported `local_authoritative` startup before workspace ownership
  or embedded Postgres boot so the failure is immediate and unambiguous

### Likely touch points

- `go/cmd/eshu/local_host.go`
- `go/internal/eshulocal/layout.go`
- `go/internal/eshulocal/owner.go`
- `go/cmd/api/wiring.go`
- `go/cmd/mcp-server/wiring.go`
- `go/cmd/ingester/local_lightweight.go`
- `docs/docs/reference/local-data-root-spec.md`
- `docs/docs/reference/local-host-lifecycle.md`
- `docs/docs/adrs/2026-04-22-nornicdb-graph-backend-candidate.md`

### Verification

- focused local-host tests for profile/backend resolution
- owner-record round-trip tests including graph metadata
- layout tests proving a stable `graph/` path per workspace
- manual proof that `local_authoritative` fails loudly until sidecar wiring is
  implemented
- opt-in syntax gate against a real NornicDB binary:
  `ESHU_NORNICDB_BINARY=/tmp/nornicdb-headless go test ./cmd/eshu -run TestNornicDBSyntaxVerification -count=1 -v`.
  The evaluated NornicDB binary accepts the multi-label fulltext
  procedure form and `COLLECT(DISTINCT {map literal})`, but still rejects Eshu's
  composite `IS UNIQUE` schema syntax.
- opt-in workaround gate against the same binary:
  `ESHU_NORNICDB_BINARY=/tmp/nornicdb-headless go test ./cmd/eshu -run TestNornicDBCompatibilityWorkarounds -count=1 -v`.
  The 2026-04-22 run also passed composite `IS NODE KEY` and the multi-label
  fulltext procedure form. `IS NODE KEY` remains a syntax-compatible option,
  but Eshu does not use it for sparse semantic labels because it makes every
  participating property required.
- graph schema dialect gate:
  `ESHU_NORNICDB_BINARY=/tmp/nornicdb-headless go test ./cmd/eshu -run TestNornicDBSchemaAdapterVerification -count=1 -v`.
  The current branch passes after routing schema bootstrap through the
  backend-specific renderer: Neo4j keeps composite `IS UNIQUE`, while
  NornicDB skips unsupported composite uniqueness DDL, keeps single-property
  `uid` uniqueness constraints for canonical merge identity, and still skips
  Neo4j's multi-label fulltext fallback.
- installer gate:
  `go test ./cmd/eshu -run TestInstallNornicDB -count=1`.
  The current branch run passes local-file copy, checksum mismatch rejection,
  JSON output, and managed-binary discovery preference.
- graph log CLI gate:
  `go test ./cmd/eshu -run TestRunGraphLogs -count=1`.
  The current branch run passes workspace-root resolution, log streaming, and
  missing-log guidance without taking ownership of the workspace.
- owner-aware graph stop gate:
  `go test ./cmd/eshu -run TestGraphStop -count=1`.
  The current branch run passes live-owner signaling, stale graph direct stop,
  and lightweight-owner rejection without introducing a second graph owner.
- graph start CLI gate:
  `go test ./cmd/eshu -run TestRunGraphStart -count=1`.
  The current branch run passes foreground exec into `local-host watch` with
  `ESHU_QUERY_PROFILE=local_authoritative` and `ESHU_GRAPH_BACKEND=nornicdb`.
- graph upgrade CLI gate:
  `go test ./cmd/eshu -run TestRunGraphUpgrade -count=1`.
  The current branch run passes stopped-owner local-file replacement and
  rejects upgrade while a workspace owner or graph backend is still healthy.
- local-authoritative startup perf gate:
  `ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless ESHU_LOCAL_AUTHORITATIVE_PERF=true go test ./cmd/eshu -run TestLocalAuthoritativeStartupEnvelope -count=1 -v`.
  The 2026-04-23 run measured cold start `9.045253708s` and warm restart
  `490.996625ms` at the owner-record plus ingester handoff, both within the
  documented `local_authoritative` startup envelope.
- manual local-authoritative lifecycle smoke:
  `ESHU_HOME=/tmp/eshu-local-authoritative-smoke ./go/bin/eshu install nornicdb --from /tmp/nornicdb-headless`;
  `./go/bin/eshu graph start --workspace-root <repo>`;
  `./go/bin/eshu graph status --workspace-root <repo>` showed
  `profile=local_authoritative`, `graph_backend=nornicdb`, and
  `graph_running=true`; `./go/bin/eshu graph logs --workspace-root <repo>`
  printed the NornicDB process-mode log; `./go/bin/eshu graph stop --workspace-root <repo>`
  cleanly stopped the owner; final status showed `owner_present=false` and
  `graph_running=false`.
- manual local-authoritative MCP smoke:
  `ESHU_HOME=/tmp/eshu-local-authoritative-e2e2 ESHU_CANONICAL_WRITE_TIMEOUT=2s ./go/bin/eshu graph start --workspace-root <repo>`;
  `./go/bin/eshu mcp start --workspace-root <repo>`;
  MCP `search_file_content` for `startManagedLocalGraph` returned two Go
  files from `postgres_content_store`; MCP `find_code` returned the
  `startManagedLocalGraph` function with `truth.profile=local_authoritative`
  and `truth.basis=content_index`; `get_index_status` reported degraded graph
  projection after a bounded NornicDB canonical write timeout; `eshu graph stop`
  cleanly stopped the owner and final status showed `owner_present=false`.

---

## Chunk 4: Authoritative Graph Analysis Hardening

### Goal

Ensure the high-value graph-backed code-intelligence surface is solid in full
local stack and production.

### Work

- harden direct caller/callee queries
- harden transitive caller/callee queries
- harden call-chain path queries
- define and implement dead-code policy based on explicit reachability roots
- publish a dead-code reachability spec that covers framework callbacks,
  background workers, SQL entrypoints, and language/framework-specific roots
- ensure reducers materialize the graph truth these queries require

### Required modeling decisions

- what counts as a root for dead-code analysis:
  - `main`
  - HTTP handlers
  - CLI commands
  - framework callbacks
  - tests excluded or not
- how dynamic dispatch and reflection are represented
- how cross-file and cross-repo code calls are admitted

### Conflict-domain reasoning

- reducer acceptance and graph-write phases remain the authoritative bottleneck
- code-call and semantic-entity materialization must not rely on timing or
  watch-loop luck
- completion must be provable from durable state, not inferred from log order

### Likely touch points

- `go/internal/reducer/code_call_*`
- `go/internal/query/code_*`
- `go/internal/storage/cypher/*` for backend-neutral Cypher write contracts
  and writers
- `go/internal/storage/neo4j/*` only for Neo4j-specific graph adapter seams
- `docs/docs/reference/dead-code-reachability-spec.md`
- compose verification scripts for graph-backed queries

### Verification

- positive, negative, and ambiguous truth tests
- full-stack compose validation for callers/callees/path/dead-code
- direct query/API verification against a fresh run
- compose-backed end-to-end validation is mandatory for this chunk

---

## Chunk 5: Backend Conformance Suite

### Goal

Make future graph backend evaluation safe and evidence-based.

### Work

- define backend capability matrix:
  - canonical writes
  - direct graph reads
  - path traversal
  - full-text support
  - dead-code readiness
  - performance envelope
- add a conformance harness that runs the same query corpus against any
  backend adapter under test
- classify backends as:
  - unsupported
  - experimental
  - local-only
  - production-capable
- include deterministic read-shape checks plus write-semantics tests for
  ordering, MERGE/upsert behavior, and transaction visibility

### Important rule

No backend should be described as supported because it "speaks Cypher" alone.

### Likely touch points

- backend test harnesses
- adapter packages
- docs describing backend support status

### Verification

- deterministic conformance runs in CI for supported adapter surfaces
- explicit failure reports for missing capability classes

---

## Chunk 6: OCI Collector Plugin Contract

### Goal

Let developers add new collectors without patching the core runtime by hand.

### Work

- define a collector plugin contract at the fact-emission seam
- publish fact-schema versioning and compatibility rules before plugin loading
  work starts
- specify plugin metadata:
  - supported source kinds
  - emitted fact kinds and versions
  - compatibility range
  - packaging metadata
- support OCI artifact distribution for collector plugins
- keep reducers and graph writers unchanged by plugin packaging

### Design rule

Collectors emit versioned facts. They do not write canonical graph truth
directly.

Plugin loading requires an explicit trust model: signing or allowlisting,
provenance checks, and hard failure on incompatible fact-schema versions.

Chunk 6 must not begin until the fact-schema-versioning and plugin-trust-model
specs are frozen.

### Likely touch points

- collector runtime loading
- fact envelope/version negotiation
- `docs/docs/reference/fact-schema-versioning.md`
- `docs/docs/reference/plugin-trust-model.md`
- plugin documentation and packaging tooling

### Verification

- plugin load tests
- fact compatibility tests
- OCI packaging and fetch smoke tests

---

## Observability And Reliability Requirements

Every chunk above must preserve the repo's operating priorities:

1. accuracy
2. performance/concurrency
3. reliability

Required observability themes:

- query truth level must be visible
- local-host process ownership and shutdown must be inspectable
- reducer convergence must be diagnosable from durable state and telemetry
- backend selection must be explicit in logs and spans

Suggested telemetry dimensions:

- `storage_profile`
- `graph_backend`
- `truth_level`
- `workspace_id`
- `repo_scope`

---

## Non-Goals

- replacing Neo4j in production during this implementation
- replacing Postgres in the service profile
- pretending lightweight local mode has full authoritative graph parity
- introducing a new query language
- moving graph or reducer writes into collector plugins
- cross-workspace shared local data roots

---

## Verification Matrix

Minimum verification by work area:

- docs or product-contract changes:
  - `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
- query contract changes:
  - `cd go && go test ./internal/query ./cmd/api ./cmd/mcp-server -count=1`
  - `cd go && go vet ./internal/query ./cmd/api ./cmd/mcp-server`
- runtime/local-host changes:
  - `cd go && go test ./cmd/eshu ./internal/runtime ./internal/status -count=1`
- facts/reducer/query truth changes:
  - `cd go && go test ./internal/projector ./internal/reducer ./internal/storage/postgres -count=1`
  - compose-backed query verification when graph truth is affected
- Chunk 4 compose end-to-end gate:
  - callers/callees/call-chain/dead-code must pass against a fresh full-stack run
- repo hygiene:
  - `git diff --check`

---

## Implementation Order

The recommended order is:

1. capability contract and truth labels
2. capability-port extraction
3. lightweight local host
4. authoritative graph query hardening
5. backend conformance harness
6. OCI collector plugin seam

This order is deliberate:

- it avoids building local mode on top of unstable query semantics
- it avoids backend experiments before the capability boundaries exist
- it protects production by keeping the authoritative path intact while local
  workflows improve
