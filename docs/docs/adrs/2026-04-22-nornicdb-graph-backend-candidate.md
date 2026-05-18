# ADR: NornicDB As Candidate Graph Backend

**Date:** 2026-04-22
**Status:** Accepted with conditions (2026-04-23)
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

**Acceptance Conditions (must remain true for acceptance to hold):**

- Eshu tracks the latest NornicDB `main` branch for now. Local-authoritative
  installs must use `eshu install nornicdb --from <source>` or
  `ESHU_NORNICDB_BINARY` with an explicitly built or selected NornicDB `main`
  binary; the embedded release manifest intentionally carries no accepted
  assets until a later release or accepted-build policy is chosen.
- Chunk 5 backend conformance suite passes against NornicDB for `GraphQuery`
  and the current Cypher write executor family (`Executor`, `GroupExecutor`,
  and `PhaseGroupExecutor`). Older notes that say `GraphWrite` refer to this
  write surface unless a later ADR introduces a narrower interface.
- Chunk 5b matrix runs pass against `local_authoritative`,
  `local_full_stack`, and `production` profiles with recorded perf evidence
- NornicDB is now the default `ESHU_GRAPH_BACKEND`; Neo4j remains the explicit
  official alternative while parity research decides its final production
  posture. The default switch is not the same as closing this ADR, and the
  latest-main/conformance/profile-matrix gates continue to be tracked here.

**Related:**

- `docs/docs/adrs/2026-04-20-embedded-local-backends-desktop-mode.md`
- `docs/docs/adrs/2026-04-20-embedded-local-backends-implementation-plan.md`
- `docs/docs/adrs/2026-05-04-neo4j-parity-optimization-plan.md`
- `docs/docs/reference/capability-conformance-spec.md`
- `docs/docs/reference/backend-conformance.md`
- `docs/docs/reference/truth-label-protocol.md`
- `docs/docs/reference/local-data-root-spec.md`
- `docs/docs/reference/local-host-lifecycle.md`
- `docs/docs/reference/local-performance-envelope.md`
- `docs/docs/reference/graph-backend-installation.md`
- `docs/docs/reference/graph-backend-operations.md`

---

## Status Review (2026-05-04)

**Current disposition:** Accepted with conditions; default backend selected,
promotion incomplete.

NornicDB is now the default graph backend, and Neo4j remains the official
alternative backend while parity research is active. The latest full-corpus
evidence is now a direct
latest-main proof against NornicDB `b68b4ef`: the full corpus drained in
`878s`, API/MCP health passed, and relationship evidence read back through both
surfaces. Eshu now makes the current dependency policy honest: for now, it
consumes latest NornicDB `main` through explicit `--from` installs or
`ESHU_NORNICDB_BINARY`, and the embedded release manifest has no accepted
assets.

**Remaining work:** keep latest-main validation current, complete the Neo4j
parity optimization research plan, finish broader host coverage, and define the
install trust policy before treating this ADR as complete. A release-backed or
signed accepted-build policy can still replace latest-main evaluation later,
but it is not the current default install contract.

## Promotion Guardrail (2026-05-04)

NornicDB remains **accepted with conditions** until all three promotion gates
are true at the same time:

| Gate | Current state | Blocks |
| --- | --- | --- |
| Latest-main dependency policy | The checked-in manifest intentionally has no accepted assets. Users install an explicit latest-main NornicDB binary with `--from` or point `ESHU_NORNICDB_BINARY` at one. | Treating no-argument installs as supported or silently falling back to an old forked asset. |
| Conformance | Chunk 5 now has the backend matrix, DB-free Go harness, and live Compose-backed adapter check; Chunk 5b has latest-main evidence for `local_authoritative`, `local_full_stack`, and `production`. | Calling NornicDB fully promoted until the Neo4j parity research, terminal comparison posture, and remaining trust gates are recorded. |
| Install trust | SHA-256 checks exist for explicit install sources, but binary signature policy remains future work. | Treating installed NornicDB binaries as a closed supply-chain contract. |

Default backend selection can stay in place while these gates close. Removing
the conditions requires a later ADR update with the accepted NornicDB build
policy, the Neo4j support posture, and any final host/support evidence.

## Evaluation Status

| Phase | Status | Evidence | Remaining |
| --- | --- | --- | --- |
| Profile/backend admission | In progress | `0e4d8a5f`, current branch local-host profile/backend gating, current branch loopback-TCP sidecar lifecycle and shared Bolt-driver path, manual smoke with `/tmp/nornicdb-headless` showing healthy owner + clean Ctrl-C shutdown; `575ca864` added `TestNornicDBSyntaxVerification` and `TestNornicDBCompatibilityWorkarounds`; `5f5a781e` added schema-dialect routing and `TestNornicDBSchemaAdapterVerification`; current branch managed-install discovery prefers `${ESHU_HOME}/bin/nornicdb-headless` after explicit env override; 2026-04-22 temporary-home smoke proved local_authoritative start/status/logs/stop with NornicDB; 2026-04-23 MCP smoke proved content-index-backed `search_file_content` and `find_code` continue to work while canonical graph projection degrades on a bounded NornicDB write timeout; current branch lets `eshu install nornicdb --from <source>` consume local binaries, local tar archives, macOS packages, and URLs; current branch remote installs honor `cmd.Context()` cancellation and use `ESHU_NORNICDB_INSTALL_TIMEOUT` (`30s` default) when slower links need a larger budget; current branch intentionally leaves the embedded release manifest empty while Eshu tracks latest NornicDB `main`, so no-argument installs fail with explicit latest-main `--from` guidance instead of using the old forked asset; current branch `TestLocalAuthoritativeStartupEnvelope` measured startup readiness at the owner-record plus ingester handoff with an explicitly installed binary: cold start `9.045253708s`, warm restart `490.996625ms` | install trust policy, broader host coverage, broader query/memory perf |
| Operator CLI surface | In progress | `da35d729`, current branch `eshu graph status`; current branch `eshu install nornicdb --from <source> [--sha256 <hex>] [--force]` installs from a local binary/archive/package/URL, honors `Ctrl-C` on remote downloads, accepts `ESHU_NORNICDB_INSTALL_TIMEOUT=<duration>` for slower links, and keeps headless as the managed laptop binary name; bare no-argument install is reserved for a future accepted manifest policy; current branch `eshu graph logs`; current branch owner-aware `eshu graph stop`; current branch foreground `eshu graph start`; current branch stopped-owner `eshu graph upgrade --from <source>`; current branch `eshu watch` / `eshu graph start` now render a live local progress panel from the shared status store (owner/profile/backend header, collector/projector/reducer lanes, and queue pressure) instead of a fake percentage bar; 2026-04-22 smoke proved install → start → status running → logs → stop → status stopped | signature verification, broader host coverage |
| Adapter conformance | In progress | current branch routes NornicDB canonical writes through bounded phase-group transactions by default, applies Bolt `tx_timeout` metadata plus client context deadlines, preserves production Neo4j grouped writes, and adds the explicit `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true` conformance switch for proving NornicDB grouped writes; current branch exposes NornicDB phase, row, and label tuning knobs; current branch routes call-chain, transitive relationships, and dead-code through backend-aware query builders; current branch preserves the latest-main evaluation switch `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true` for binaries that include the required row-safe hot path; the 2026-05-03 full-corpus proof against the NornicDB `#136` latest-main handoff drained cleanly and passed API/MCP drilldowns; current branch `TestServiceRunAcksSuccessfulProjectionAfterShutdownCancel` proves completed projector work can ack through foreground owner shutdown without the `ack projector work: begin: context canceled` tail; current branch adds `specs/backend-conformance.v1.yaml` plus the `go/internal/backendconformance` harness and proves the current `GraphQuery` and reducer Cypher write adapter surfaces can consume it; current branch adds the opt-in `TestLiveBackendConformance` and `scripts/verify_backend_conformance_live.sh` so the end-to-end matrix runs that same corpus against real NornicDB and Neo4j Bolt endpoints; current branch adds `profile_matrix` gates for `local_authoritative`, `local_full_stack`, and `production`, with local-full-stack CI running under `ESHU_QUERY_PROFILE=local_full_stack`; 2026-05-04 latest-main production-profile proof `eshu-full-pr138-a2c630af-b68b4ef-20260504T120630Z` drained cleanly and passed API/MCP relationship-evidence drilldowns; current branch accepts ADR `2026-05-04-neo4j-parity-optimization-plan.md` after proving Neo4j can follow the shared optimized adapter path when graph schema is applied before indexing; current branch also clarifies the containment split: Neo4j may use the row-scoped batched containment writer as a parity experiment, but NornicDB remains on file-scoped inline containment by default because the production-profile latest-main proof already drained the full corpus in `878s`; corrected schema-first Neo4j proof `eshu-neo4j-schema-profile-20260504T182712Z` applied graph schema before indexing, drained `8458/8458` queue rows in `577s`, and passed API/MCP evidence checks | broader query/memory perf envelope |
| Performance + promotion gates | In progress | current branch `TestLocalAuthoritativeStartupEnvelope`; 2026-04-23 measured local-authoritative cold start `9.045253708s` and warm restart `490.996625ms`; synthetic call-chain, transitive-caller, and dead-code envelopes passed through the real local-authoritative handlers; current branch graph-analysis Compose proof covers direct callers, transitive callers, shortest call-chain path, dead-code results, and canonical graph `CALLS` edges; 2026-05-03 full-corpus proof against latest-main NornicDB `#136` drained in under 15 minutes with no retrying, failed, or dead-lettered rows and passed API/MCP health plus evidence drilldowns; 2026-05-04 latest-main production-profile proof `eshu-full-pr138-a2c630af-b68b4ef-20260504T120630Z` rebuilt Eshu `a2c630af` and NornicDB `main` `b68b4ef`, drained `8458/8458` queue rows in `878s`, kept pending/in-flight/retrying/failed/dead-letter at `0`, returned `896` API repositories, and passed API/MCP evidence checks; corrected schema-first Neo4j proof `eshu-neo4j-schema-profile-20260504T182712Z` drained the same queue in `577s` and passed API/MCP evidence checks; current branch removes the foreground `graph start` ack-cancel tail by giving only the post-success projector ack a bounded shutdown context | install trust, broader host coverage |

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
- 2026-05-04 Neo4j comparison snapshot and correction
  `eshu-neo4j-baseline-pr138-a2c630af-20260504T123656Z` used the same Eshu
  branch to start a Neo4j comparison run. It was stopped by request before
  terminal convergence, so it is not a completed Neo4j baseline. The useful
  signal is still clear: at `1946s`, Neo4j had `553/896` source-local
  projector items succeeded, `5186` total work rows, `4841` succeeded rows,
  `337` pending rows, and `0` retrying, failed, or dead-letter rows. NornicDB
  had already finished the full corpus at `878s`. Later inspection found that
  this manual Neo4j runner skipped `eshu-bootstrap-data-plane`, so it had no
  graph schema bootstrap. Corrected schema-first run
  `eshu-neo4j-schema-profile-20260504T182712Z` applied graph schema before
  indexing, drained `8458/8458` queue rows in `577s`, returned `896` API
  repositories, and passed API/MCP evidence checks. The remaining rule is
  operational: every production-profile proof must apply graph schema before
  timing graph writes.
- Neo4j remains strategically useful for companies that already license and
  operate it. The target is not a forked query layer for every Bolt/Cypher
  database; it is a small set of documented adapter seams where a backend can
  prove the same Eshu truth contract without handler-level workarounds. If Eshu
  supports both NornicDB and Neo4j as production-promoted backends, Neo4j needs
  enough tuning that the full-corpus performance gap is not night-and-day.
- 2026-05-02 graph evidence-pointer validation found a backend correctness gap:
  direct repository relationships written by Eshu with `SET rel.resolved_id = ...`
  read back with empty relationship properties on NornicDB `main` (`v1.0.43`).
  A direct HTTP probe reproduced the issue with a minimal
  `MERGE (a)-[rel:T]->(b) SET rel.resolved_id = $rid` statement. The NornicDB
  patch adds `TestMergeRelationshipStandaloneSetPersistsProperties`, routes
  relationship `MERGE` segments through the context-aware path, and applies
  standalone `SET` to bound relationships before `RETURN`. Local
  `go test -tags 'noui nolocalllm' ./pkg/cypher -count=1` passed, the patched
  remote headless binary preserved relationship properties through HTTP, and a
  fresh focused Eshu rerun drained with projector `6/6`, reducer `56/56`, no
  retrying/failed/dead-letter rows, and direct graph API proof that repo edges
  and evidence hop edges carry `resolved_id` pointers. The backend fix is now
  tracked upstream as `orneryd/NornicDB#135`; Copilot's standalone-`SET`
  review comment was resolved in NornicDB commit `2461a46`, which adds
  `ON CREATE SET` plus standalone relationship `SET` regression coverage and
  passed `go test -tags 'noui nolocalllm' ./pkg/cypher -count=1` plus
  `git diff --check`. A rebuilt PR-branch headless binary also passed a direct
  HTTP probe for `ON CREATE SET` plus standalone relationship `SET`, preserving
  `resolved_id` through `properties(rel)`.
- `Function=15` is the better built-in compromise: it avoids the over-fragmented `Function=10` lane and still reaches `Variable` with stable early chunks around `19.9s-21.4s`
- the next repo-scale blocker is now `retract`, not `entities`; bundling all 9 stale-delete statements into one grouped transaction overflowed NornicDB's request budget
- the branch now executes NornicDB retract statements sequentially and sanitizes backend error text before projector dead-letter persistence so NUL bytes cannot break Postgres updates
- the current branch now goes one step further than the earlier `label + file_path` grouping: canonical entity node upserts are split from file containment edges, node upserts batch across files with the simple NornicDB-friendly `UNWIND ... MERGE (n:<Label> {uid: row.entity_id}) SET n += row.props` shape, and `phase=entity_containment` attaches those nodes back to files in a separately measured batch phase
- projector same-scope claim fencing is now proven too: a deliberate second-generation trigger during the first generation's `Function` phase held queue state at `pending=1, in_flight=1` instead of the old overlapping `in_flight=2` failure mode
- the earlier clean rerun exposed `Variable rows=25` as the next bottleneck, but the 2026-04-27 focused ladder after file-scoped entity batching reversed that conclusion: `php-large-repo-b` completed the same `131,977` `Variable` rows in `196.713s` at `10`, `130.082s` at `25`, `118.136s` at `50`, and `102.820s` at `100`, with zero singleton fallbacks, zero retries, zero dead letters, and max grouped execution `0.607s`; the built-in Variable row cap is now `100` while the grouped-statement cap remains `5`
- the follow-up `Variable rows=15` / `5`-statement experiment improved individual Variable chunks into roughly the `11.6s-17.4s` band, but it still took about `23m` to reach Variable and ran about `35m` total before manual stop, so it is not the next default candidate
- after re-reading NornicDB's performance and migration docs, the branch identified a local-host startup gap: `eshu graph start` applied Postgres schema but skipped graph schema bootstrap, leaving NornicDB without the schema-backed `MERGE` lookup preconditions its hot-path cookbook documents
- current branch now applies the backend-routed graph schema immediately after NornicDB sidecar readiness and before owner-record publication or reducer/ingester startup, using the same NornicDB schema dialect as `bootstrap-data-plane`
- the branch now emits rolling and final `nornicdb entity label summary` logs with `phase`, per-label rows, statements, executions, grouped chunks, total duration, max execution duration, and row-width totals so the next tuning slice can compare node-upsert cost against containment-edge cost before changing more defaults
- the first remote self-repo rerun after the entity/containment split exposed a schema-dialect correctness issue rather than a timeout: translating composite `IS UNIQUE` to `IS NODE KEY` made sparse `Annotation` rows fail on required `name`; the follow-up run also proved the current NornicDB binary still rejects Eshu composite `IS UNIQUE`, so current branch skips unsupported composite uniqueness DDL for NornicDB and relies on separate `uid` uniqueness constraints for canonical merge identity
- the same remote run proved canonical entity node upsert is no longer the main bottleneck: `phase=entities` completed in `25.523448885s` total, including `Function` at `3.10382615s` and `Variable` at `20.695746985s`; `phase=entity_containment` is now dominant, with `Function` containment alone taking `248.58715967s`
- current branch now keeps the split node-upsert / containment-edge shape only for backends that support node-only batched `MERGE`, while older NornicDB builds stay on the proven file-scoped combined shape: match the `File` anchor with `$file_path`, unwind entity rows for that file, upsert nodes, and attach `CONTAINS` in one statement. The opt-in syntax gate records why this is necessary: builds without the generalized row-safe hot path can collapse the standalone node-only batch shape, while the combined shape preserves row-bound entity identity. The NornicDB branch in `/Users/allen/os-repos/NornicDB-eshu-map-merge-hotpath` proved the faster MERGE-first row-file shape needed `SET += row.props` support inside the generalized `UNWIND/MERGE` batch hot path and unique-constraint-backed `MERGE` lookups for `File.path` and canonical `uid`; Eshu exposes `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true` only as a latest-main evaluation switch until broader conformance settles the default.
- remote self-repo dogfood confirms the tuning target: map-merge alone reduced statement fragmentation but still let `MERGE` chunk time grow with graph size; adding unique-constraint lookup cut `Function` from `40.039s` to `12.589s` at 750 rows and from `158.568s` to `46.566s` at roughly 2.1k rows, while the `files` phase dropped from roughly `26s-28s` to `7.351s`.
- the follow-up evidence is intentionally not a new Eshu tuning claim yet: `Function` completed at `9021` rows in `444.997825518s`, `Struct` completed at `916` rows in `81.059759011s`, and `Variable` was still progressing past `5000` rows in `563.749941325s`. That leaves a linear write-path cost that needs NornicDB CPU/heap profiling before we decide whether the next fix belongs in Badger/index maintenance, Cypher hot-path execution, Bolt transaction handling, or Eshu statement shape.
- after the NornicDB unique-constraint validation patch (`023ec51`) rebuilt on the 16-vCPU remote test host, canonical source-local projection stopped being the bottleneck: `Variable` completed all `40163` rows in `30.375581617s`, the full canonical `entities` phase completed in `42.085095138s`, and source-local projection succeeded with `55295` facts in `49.603476029s`. The same run proves `.git` was not being parsed (`dirs_skipped..git=1`). The next failure moved to reducer `semantic_entity_materialization`, where Eshu was still forcing NornicDB through the older scalar compatibility writer and hit the `15s` bounded write timeout.
- current branch now routes NornicDB semantic entity materialization back through the batched `UNWIND $rows AS row` writer because the patched NornicDB binary now supports the required row-based write shape. This keeps the compatibility decision at the adapter seam: when NornicDB lacks a generally useful primitive, patch NornicDB; when the primitive exists, Eshu should use the shared batched path instead of preserving stale defensive routing.
- the first rerun with the batched semantic writer still timed out after `18.140664853s`; the new timeout summary narrowed the failing statement to only `Annotation rows=19`, so the remaining reducer issue is the semantic Cypher shape rather than row width alone. A focused NornicDB trace probe then proved the `UNWIND ... MATCH (f:File {path: row.file_path}) ... MERGE (n:<Label> {uid: row.entity_id}) ... SET n += row.properties` shape was schema-indexed but still missed NornicDB's generalized `UNWIND/MERGE` batch hot path (`UnwindMergeChainBatch=false`). Current branch now routes NornicDB semantic writes through merge-first explicit per-label row templates (`UNWIND ... MERGE node ... SET field assignments ... MATCH File ... MERGE CONTAINS`), keeps `ESHU_NORNICDB_SEMANTIC_ENTITY_LABEL_BATCH_SIZES=Function=15,Variable=10,...` for high-cardinality labels, and includes semantic label/row count in timeout errors so future failures are diagnosable without another blind rerun. Neo4j keeps the existing semantic writer path so backend comparison remains meaningful.
- the 2026-04-27 remote self-repo lane with Eshu `f72724d6`, NornicDB `86e78f1`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, and the merge-first semantic writer drained to healthy. Source-local projection succeeded with `51,811` facts in `31.868573152s`, semantic materialization completed in `2.999627444s`, SQL relationship materialization completed in `2.274879377s`, workload materialization completed in `1.499372599s`, and the final queue was `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`.
- the follow-up 2026-04-27 medium lane against `/home/ubuntu/eshu-test-repos` (`23` repos) used the same Eshu/NornicDB pair and drained healthy in `316s`. The largest observed source-local projection was `php-large-repo-a` at `148,948` facts in `170.376262229s`; the large semantic reducers completed in `1.217969005s` and `5.954617004s`; final status was `Health: healthy` with queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`; and the log scan found no `graph_write_timeout`, semantic failure, acceptance-cap, panic, fatal, or dead-letter lines. This clears the focused and medium correctness gates for merge-first semantic writes; the next proof remains a full-corpus DB-driven drain, not another semantic cap tweak.
- before attempting another full corpus, the branch also ran a 2026-04-27 targeted five-repo lane on Eshu `c598000d` covering the prior small semantic regressions plus the two noisy PHP stress repos (`api-node-ai-provider`, `api-node-ai-summary`, `api-node-ai-product-description-generation`, `php-large-repo-b`, `php-large-repo-a`). It drained healthy in `854s`; `php-large-repo-a` projected `148,948` facts in `166.496305644s`, `php-large-repo-b` projected `176,201` facts in `521.49982913s`, their semantic reducers completed in `6.33473887s` and `15.762956452s`, and final queue state was `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. The error scan found no graph write timeout, semantic failure, acceptance-cap, panic, fatal, retry, or dead-letter lines. This validates the larger targeted-problem lane; the next run should be a larger representative subset before burning a full 896-repo corpus cycle.
- the larger representative subset ran next on Eshu `5c9b169a` with NornicDB `86e78f1`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, `ESHU_REDUCER_WORKERS=2`, `ESHU_REDUCER_BATCH_CLAIM_SIZE=1`, and `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT=250000` against `50` repos selected from `/home/ubuntu/eshu-e2e-full`. It drained healthy in `884s`, final queue state was `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`, and the failure scan found no graph write timeout, semantic failure, acceptance-cap, panic, fatal, retry, or dead-letter lines. The dominant cost was source-local canonical projection for `php-large-repo-b`: the repo had already persisted `176,201` facts while it wrote `131,977` `Variable` entities and `28,926` `Function` entities; `Variable` summaries reached `102,654` rows, `13,200` statements, and `130.161796981s` total label time before the run drained. This clears the representative-subset correctness gate after the merge-first semantic writer. The next full-corpus DB-driven run should keep discovery-report capture, and the next optimization should profile high-cardinality canonical entity writes and noisy repo input shape before changing another timeout or reducer semantic cap.
- the next full-corpus lane with eight reducer workers proved the semantic bottleneck was not row count alone: after all `896` source-local projector items succeeded, tiny reducer semantic statements such as `Function rows=1`, `Function rows=4`, `Annotation rows=5`, and `TypeAlias rows=5` still hit the `120s` correctness-validation write budget once the graph was populated. Targeted probes against the live NornicDB graph found `uid` node lookups were usually fast, but `MATCH (f:File {path})-[:CONTAINS]->(n {uid})` checks took roughly `2.3s-2.9s` even for sampled files with small out-degree. Source inspection confirmed NornicDB relationship `MERGE` existence checks still scan outgoing edges from the start node. Current branch therefore changes only the NornicDB semantic writer seam: canonical-owned semantic labels now enrich source-local canonical nodes by `uid` without re-merging `File-[:CONTAINS]->entity`, preserve `projector/canonical` evidence ownership, and clear semantic properties with `REMOVE` instead of `DETACH DELETE` on retries/prior generations; `Module` remains semantic-owned because canonical modules use `name` rather than `uid` and do not have the same file-containment invariant.
- upstream status as of 2026-04-25: the unique-constraint validation fix (`https://github.com/orneryd/NornicDB/pull/115`), the `UNWIND ... MATCH ... MERGE ... SET n += row.props ... MERGE relationship` hot-path/router fix (`https://github.com/orneryd/NornicDB/pull/116`), and unique-constraint-backed `MERGE` lookup (`https://github.com/orneryd/NornicDB/pull/118`) are merged into NornicDB `main`. Eshu should rebuild the next dogfood binary from upstream `main`, keep using explicit `--from`/`ESHU_NORNICDB_BINARY` until an upstream release asset is published and pinned, and then rerun the self-repo dogfood lane against the release-backed headless asset before removing temporary fork-binary language from the promotion gate.
- 2026-04-25 follow-up upstream handoff completed with `#118`, which teaches NornicDB `MERGE` lookup to use single-property unique-constraint metadata instead of falling back to label scans. Copilot review comments on PRs `#116` and `#118` were addressed before merge: `#116` counts only rows that survive required `MATCH` lookups and falls back safely for unsupported `OPTIONAL MATCH` mutation shapes; `#118` treats non-comparable unique lookup values as lookup misses instead of panicking.
- the 16-vCPU remote self-repo dogfood run against the stacked `#115 + #116 + #118` headless binary completed cleanly: owner start `2026-04-25T02:32:17Z`, empty queue at `2026-04-25T02:37:14Z`, source-local projection succeeded with `55384` facts in `50.461321256s`, canonical `entities` completed in `42.723968989s`, `Function` stayed flat through `9045` rows with about `2.857s` total label time, `semantic_entity_materialization` succeeded in `142.392295866s`, and `.git` remained skipped by the collector.
- the follow-up run rebuilt directly from upstream `main` at merge commit `501a121d7882cadf2bb3ec657178a54b33d5967b` (`NornicDB v1.0.42-hotfix`) and reproduced the same result without the temporary integration branch: owner start `2026-04-25T04:13:44Z`, healthy at `2026-04-25T04:18:20Z`, collector emitted `55409` facts while skipping `.git`, source-local projected `55384` facts, canonical `Function` wrote `9045` rows in `2.836596812s`, `semantic_entity_materialization` succeeded in `141.031360077s`, `sql_relationship_materialization` succeeded in `217.718543222s` for `291` edges, and a clean `eshu graph stop` left no owner or graph sidecar running.
- that same completed run moves the next bottleneck to SQL relationship writes rather than canonical entities: `sql_relationship_materialization` wrote only `291` edges but took `222.19561979s` while the NornicDB process consumed multiple CPU cores. The next compatibility slice should profile and narrow the `UNWIND ... MATCH (source:SqlTable|SqlView|...) ... MERGE relationship` shape before changing more Eshu batch defaults; likely candidates are multi-label indexed `MATCH` planning or a backend-dialect split into single-label relationship statements.
- source inspection of NornicDB `main` narrowed the SQL relationship bottleneck to a missing database hot path rather than a Eshu batch-size problem: `UNWIND ... MATCH ... MATCH ... MERGE (source)-[rel:TYPE]->(target) SET rel.*` fell back to per-row generic execution because the `UnwindMergeChainBatch` parser did not accept lookup-first mutation chains, `|` label alternatives, relationship variables, or relationship property `SET`. The local branch `/Users/allen/os-repos/NornicDB-eshu-sql-edge-hotpath` adds that support in commit `05f4b3c` plus `TestUnwindMatchRelationshipSetBatch_EshuSQLRelationshipShape`; focused tests and headless build pass.
- remote self-repo dogfood against that patched `v1.0.43` headless binary validated the performance claim: total owner start to healthy dropped to about `3m06s`, `sql_relationship_materialization` dropped from `217.718543222s` to `3.921462s`, source-local projection completed in `34.279920s`, and the remaining bottleneck is now `semantic_entity_materialization` at `131.247187s`. The owner stopped cleanly afterward with no graph sidecar left running.
- `https://github.com/orneryd/NornicDB/pull/119` now carries the SQL relationship hot-path fix upstream. With SQL edges no longer dominant, the next evidence pass inspected the completed graph snapshot and found semantic materialization rewrote `7,622` `Function` nodes even though only about `2,475` carried additional callable semantic metadata such as `docstring`, `class_context`, `impl_context`, decorators, type parameters, or async/annotation context. The current Eshu branch therefore tightens Go semantic extraction and projector triggering so plain Go functions remain canonical-only while enriched Go callables still publish semantic readiness. This is a correctness-preserving write-set reduction, not another NornicDB batch-size workaround.
- the first remote rerun with that write-set reduction exposed the next semantic label boundary: `Module rows=45` exceeded the bounded `15s` semantic write timeout. The branch now caps NornicDB semantic `Module` rows at `10` by default, keeping the fix scoped to the newly measured label family instead of lowering the whole semantic writer.
- the `Module=10` rerun proved that cap moved the boundary forward and then exposed `ImplBlock rows=103` as the next timeout family after `52.188006393s`. The branch now also caps NornicDB semantic `ImplBlock` rows at `10`, preserving the evidence-driven per-label approach instead of globally shrinking the semantic writer.
- the `ImplBlock=10` rerun completed successfully on the same remote host and PR `#119` binary: source-local projected `55,439` facts in `34.065189964s`, SQL relationships stayed fixed at `2.587845711s`, semantic materialization succeeded in `127.982626601s`, code-call projection completed after semantic readiness in `4.282710051s`, and total owner start-to-healthy stayed about `3m06s`. This proves the cap removes the timeout but not the semantic wall; the next adapter evidence must profile or attribute semantic label cost before another ESHU-side tuning change.
- the branch now adds NornicDB-only semantic statement attribution in the reducer execute-only path so the next run records `graph_backend`, semantic label, row count, duration, and statement summary for every semantic statement. This is an observability slice only: it does not change semantic write ordering, timeout behavior, or grouped rollback conformance.
- the first semantic-attribution rerun then exposed a database concurrency bug instead of another Eshu timeout: NornicDB panicked with `fatal error: concurrent map read and map write` in `StorageExecutor.findMergeNodeInCache` because `cloneWithStorage` shared `nodeLookupCache` while copying a distinct value mutex into each clone. PR `#119` branch commit `4521bcb` now shares the cache lock with the cache map; its regression test fails before the fix and passes after, including a focused race run with `go test -race -tags 'noui nolocalllm' ./pkg/cypher -run TestCloneWithStorageSharesNodeLookupCacheLock -count=1`.
- the follow-up remote dogfood with the rebuilt `v1.0.43` headless binary completed cleanly and reached healthy state in the same ~`3m06s` envelope: source-local projection `34.099439413s` for `55,471` facts, canonical `entities` `26.301571598s`, `sql_relationship_materialization` `2.546008605s`, `semantic_entity_materialization` `128.151959988s`, and code-call projection `4.456774766s`. The new attribution makes the remaining perf target specific: `ImplBlock` accounts for `79.586s` across `103` rows, `Module` for `34.734s` across `45` rows, and `Protocol` for `6.918s` across `9` rows, while `Function` now writes `2,485` rows in only `1.625s`. The next adapter evidence slice should inspect why those module-like semantic labels hit the slow NornicDB path before changing any more Eshu default caps.
- the first inspection slice found a Eshu schema precondition gap rather than another NornicDB hot-path bug: `Function` and `Variable` already had single-property `uid` uniqueness constraints, while `Module`, `ImplBlock`, `Protocol`, and `ProtocolImplementation` did not. The current branch now adds those constraints to the Go-owned graph schema, keeps the projector entity-type label map in lockstep with those `uid`-constrained labels, and tightens schema tests so `module_uid_unique` cannot be accidentally satisfied by `terraform_module_uid_unique`. This should let NornicDB use the same unique-lookup `MERGE` path for the module-like semantic labels that it already uses for the now-fast `Function` lane.
- the follow-up remote self-repo dogfood proved the schema precondition fix: owner start `2026-04-25T06:03:46Z`, healthy progress `2026-04-25T06:04:49Z` (~`63s`), source-local projection `34.216079101s` for `55,470` facts, `sql_relationship_materialization` `2.583036883s`, `semantic_entity_materialization` `4.827652236s`, and code-call projection `4.265383252s`. The slow module-like semantic labels are no longer the wall: `Module` `45` rows took `0.034952s`, `ImplBlock` `103` rows took `0.072267s`, `Protocol` `9` rows took `0.004776s`, and `ProtocolImplementation` `3` rows took `0.002808s`. `.git` remained skipped and `eshu graph stop` left no owner or graph sidecar running.
- PR `#119` review follow-up commit `5121f55` now covers the remaining SQL-edge hot-path edge cases before upstream merge: any-label lookups fall back to label scans when schema lookup misses an unindexed alternative label, the redundant `AllNodes` fallback was removed, no-op relationship `MERGE` no longer emits node mutation notifications, and `CreateEdge` `ErrAlreadyExists` now either refetches the concurrent relationship or retries with a new edge id. Focused `pkg/cypher` regressions cover all four paths, and `go test -tags 'noui nolocalllm' ./pkg/storage ./pkg/cypher -count=1` passes locally.
- PR `#119` follow-up commit `26b901f` resolves the last Copilot thread by making the unindexed `SqlFunction.uid` fallback coverage explicit. Until PR `#119` merges and a release-backed asset is pinned, Eshu should use the `eshu-sql-edge-hotpath` branch binary explicitly for dogfood and local-authoritative validation rather than reverting to upstream `main`.
- the remote self-repo dogfood rerun against that exact `#119` headless binary and Eshu commit `5db74ae5` stayed in the fast envelope: owner start `2026-04-25T10:37:11Z`, code-call projection cycle completed `2026-04-25T10:38:20Z` (~`69s`), collector snapshot `13.909s`, collector commit `4.253s`, source-local projection `34.120669469s`, canonical phase-group write `27.299008161s`, `sql_relationship_materialization` `2.629018298s`, `semantic_entity_materialization` `4.870551017s`, and code-call projection `4.302299486s`. The collector again skipped `.git` (`dirs_skipped..git=1`), and `eshu graph stop` left `owner_present=false` / `graph_running=false`.
- the follow-up warm API query proof found a query-truth bug rather than a backend write issue: `eshu list` and `eshu find name handleRelationships` succeeded against the NornicDB-backed dogfood graph, but `eshu analyze dead-code --repo eshu --limit 5` returned IaC/provenance entities (`ArgoCDApplication`, `KustomizeOverlay`, `HelmValues`, `K8sResource`) as dead-code candidates. Current branch now restricts dead-code candidate scans to code entity labels (`Function`, `Class`, `Struct`, `Interface`) before `LIMIT` and keeps a defensive output filter for stale/non-code backend rows; rebuilding only `eshu-api` on the remote owner and rerunning `eshu analyze dead-code --repo eshu --limit 5` / `--limit 50` then returned no IaC/provenance results through the live NornicDB-backed API. The same query path now fetches a bounded raw-candidate policy buffer (`max(501, 10x + 1)`, capped at `1000`) before applying entrypoint/test/generated/public-API filters so early excluded roots do not make the public result underfill when displayable candidates exist later in the ordered scan; a second remote API rebuild proved `--limit 5` now returns five code entities (`Function`, `Interface`, `Struct`) instead of the previous empty/truncated shape.
- the first multi-repo `/home/ubuntu/eshu-test-repos` lane exposed a NornicDB tuning distinction hidden by the single self-repo dogfood: `ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS=K8sResource=5` limits grouped statements but does not limit rows inside one file-scoped inline-containment statement. The clean rerun with first-generation retract skipping got `php-large-repo-c` through canonical projection (`entities` `42.280045651s`; `Variable` `19,995` rows in `39.958509486s`) but still dead-lettered `helm-charts` on one `K8sResource rows=29` statement. Current branch now adds `K8sResource=5` to the NornicDB entity label row-cap defaults so large Helm/Kustomize manifests split by rows and by grouped statement count.
- the next multi-repo rerun proved the `K8sResource` row cap and exposed the next phase-specific cost. `helm-charts` completed canonical projection: `K8sResource` wrote `275` rows across `193` statements in `131.135765046s`, `max_statement_rows=5`, max grouped execution `11.914367608s`, and no dead letter. The remaining failure moved to the large PHP API repo, where the canonical `files` phase grouped `15` file-upsert statements and hit the `15s` timeout. Follow-up commit `1e127fb5` tagged file-upsert statements with `phase=files` metadata and added `ESHU_NORNICDB_FILE_PHASE_GROUP_STATEMENTS` (`5` default). The clean rerun with that cap confirmed file-phase grouping was active, but the same PHP repo still dead-lettered on a grouped chunk of `5` statements because each file statement still carried `rows=500`. Current branch adds `ESHU_NORNICDB_FILE_BATCH_SIZE` (`100` default) so the adapter can bound rows inside each file-upsert statement independently from grouped transaction width.
- commit `13a5b76b` completed the first healthy `/home/ubuntu/eshu-test-repos` multi-repo authoritative run on the patched NornicDB binary. Evidence: `php-large-repo-a` `files` phase completed `71` file statements in `105.489761166s` after the row cap reduced chunk payloads to `phase=files rows=100`; `helm-charts` completed `K8sResource` with max grouped execution `11.853526358s`; final status reported `Health: healthy`, projector `succeeded=23 dead_letter=0 failed=0`, reducer `succeeded=169 dead_letter=0 failed=0`, and empty queue. The remaining slowest path is expected first-generation throughput on a very large PHP/static corpus: `Variable` wrote `174,411` rows in `125.913498259s`, and the repo's canonical write completed in `241.859647305s` without timeout.
- the same live graph then exposed a NornicDB read-dialect issue in code relationships and call-chain: the canonical `CALLS` edge `handleRelationships -> transitiveRelationshipsGraphRow` existed, but the Neo4j-shaped relationship query used `collect(DISTINCT {map})` projections that could leak placeholder property strings, and the Neo4j-shaped call-chain query used parameterized `shortestPath` endpoint anchors that returned no path or hung. Current branch keeps this inside the query adapter seam: NornicDB direct relationship reads now use anchored row queries, transitive callers/callees use bounded one-hop BFS, placeholder relationship properties are stripped, and NornicDB call-chain uses the same bounded BFS rows while Neo4j keeps `shortestPath`. Remote API proof against the same self-repo owner now returns `eshu analyze calls handleRelationships --repo eshu` with `transitiveRelationshipsGraphRow`, `/api/v0/code/relationships` returns `handleRelationships` as a depth-1 incoming transitive caller, and `/api/v0/code/call-chain` returns the two-node entity-ID chain at depth 1.
- entity resolution had the same optional-projection edge: NornicDB could return placeholder repo values such as `r.id` / `r.name` even when the content store knew the canonical repository. Current branch now treats those placeholder projections as missing repo identity and hydrates code entities from the content store plus repository catalog before falling back to graph-only workload repo backfill, so `eshu find name handleRelationships` cannot leak projection strings as repo truth.
- a fresh 2026-04-25 remote dogfood run on `ESHU_HOME=/tmp/eshu-dogfood-fresh.JcCtVs` with the patched `v1.0.43` sidecar proved the clean-home write path remains healthy after the retract fixes: owner start `2026-04-25T17:49:35Z`, local progress reported `Health: healthy` at `2026-04-25T17:50:41Z` (~`66s`), reducer queue drained to `pending=0 in_flight=0`, `sql_relationship_materialization` completed in `2.709911757s`, `semantic_entity_materialization` completed in `5.067362874s`, and the code-call projection cycle completed in `4.396658558s` about `72s` after owner start.
- follow-up live API proof on commit `297da4ca` fixed the remaining direct-relationship identity lie: `eshu find name handleRelationships` now returns canonical `repo_id=repository:r_7dcdc31d` / `repo_name=eshu`, and `eshu analyze calls handleRelationships --repo eshu` no longer leaks placeholder `repo.id` / `repo.name` values because the NornicDB direct relationship path now bridges `id`/`uid` and hydrates repo identity before returning the response.
- follow-up commits `2fd6972c`, `7da2cf13`, and `87de590a` close the remaining direct-relationship under-read: live NornicDB proved the broad `id OR uid` predicate can hang, metadata reads must use indexed `{uid: ...}` then `{id: ...}` node patterns, and direct one-hop reads must use a single relationship-pattern `MATCH` so `type(rel)` resolves to `CALLS` instead of `null`. After rebuilding the remote API with `ESHU_GRAPH_BACKEND=nornicdb`, `eshu analyze calls handleRelationships --repo eshu` now returns the expected direct callees including `filterNullRelationships`, `transitiveRelationshipsGraphRow`, and `resolveExactGraphEntityCandidate` with canonical repo identity intact.
- repo-scoped call-chain names now have a human-friendly ambiguity rule: content still owns the candidate set, but if one endpoint name has multiple production entities the query layer asks the authoritative call graph for reachable candidate pairs and selects the pair only when exactly one path exists within the requested depth. True multi-path ambiguity still fails loudly with candidate-pair details instead of guessing. The NornicDB path intentionally trades Neo4j's single `shortestPath` query for bounded one-hop BFS queries because live dogfood showed parameterized `shortestPath` endpoint anchors could return no path or hang; that makes query count a first-class budget, so ambiguous endpoint products above `100` candidate pairs are rejected before probing and all-probed/no-route cases return an explicit no-route ambiguity.
- the next remote owner restart exposed a canonical retract write-amplification issue rather than another query bug: the first broad `File` stale-delete tried to detach every previous-generation file node and its edges in one NornicDB request. Current branch now preserves current-generation file paths during file retraction and lets the file upsert phase update stable `File` nodes in place, matching NornicDB's documented preference for avoiding broad cleanup deletes on hot paths. The follow-up retract slice applies the same identity-preserving rule to current directories and family-scoped entity IDs, expands retract coverage to every canonical projectable label, and runs an explicit structural-edge refresh subphase before stale entity pruning so preserved nodes do not retain stale `IMPORTS`, file/entity `CONTAINS`, class containment, or nested-function containment edges and stale entity deletes have fewer edges to detach. The structural-edge refresh is now path/id-batched instead of one repo-wide `IN` list after the next remote rerun moved the timeout to the current file/entity edge refresh statement, and the next dogfood pass proved even five current files could overflow NornicDB's delete buffer. Current branch now treats file/entity refresh as a stale-edge prune: each current file deletes only `CONTAINS` edges whose target UID is absent from that file's current entity set, so stable file/entity edges survive instead of being deleted and recreated.
- fresh remote dogfood on commit `a083cf92` proved that scoped retract/file-edge pruning preserves the fast local-authoritative envelope on a clean graph: owner start `2026-04-25T18:40:56Z`, healthy at `2026-04-25T18:42:01Z` (~65s), `.git` skipped, `56,067` facts emitted, canonical phase-group write `31.554647267s`, `sql_relationship_materialization` `2.684154892s`, `semantic_entity_materialization` `4.97578507s`, and code-call materialization `2.015869566s`. The fresh NornicDB-backed API returned canonical repository truth for `eshu list` / `eshu find`, direct `CALLS` edges for `eshu analyze calls`, a depth-1 `handleRelationships -> transitiveRelationshipsGraphRow` chain, and five code-only dead-code candidates; the run stopped cleanly with no owner or graph sidecar left running.
- the temporary dogfood-only local NornicDB integration branch is no longer needed for the merged `#115`, `#116`, and `#118` fixes. PR `#119` remains the current open upstream handoff for the SQL-edge hot path; until it merges and a release asset is pinned, Eshu should keep using explicit `ESHU_NORNICDB_BINARY` / installer `--from` inputs that point at the `eshu-sql-edge-hotpath` branch binary for this patched-binary evidence lane.
- the first 2026-04-25 multi-repo remote dogfood against `/home/ubuntu/eshu-test-repos` used a fresh `ESHU_HOME=/tmp/eshu-test-repos.hZhc4y`, discovered 23 repository directories, and confirmed `.git` was skipped inside the indexed repos. Most repos progressed quickly, but the run found a new canonical-write tuning gap in `helm-charts`: `K8sResource` file-scoped inline-containment statements used the generic `25`-statement entity phase-group cap, and one chunk of `25` one-row statements hit the `15s` NornicDB Bolt timeout. This is a grouped-transaction-size issue, not a row-batch issue, so the next rerun uses the built-in `K8sResource=5` entity label phase-group cap rather than lowering global entity batching.
- the follow-up multi-repo rerun with `K8sResource=5` proved the timeout attribution: `helm-charts` completed canonical projection instead of dead-lettering, but the run then exposed a larger clean-home cost in the `retract` phase for the two PHP repos. Those scopes had no prior generation, so stale-generation cleanup was deleting nothing while still scanning/refreshing graph state for minutes before entity upsert could begin. Current branch now carries `active_generation_id` and a previous-generation-exists flag through projector work claims, marks first-generation canonical materializations explicitly, skips stale retraction only when no prior generation exists, and logs `canonical retract skipped for first generation`; refresh projections and follow-up generations after a failed first attempt still run the scoped retract path.
- the corrected 2026-04-25 timing harness now records clean wall-clock checkpoints and query proof for the dogfood lanes. The self-repo run on `/home/ubuntu/personal-repos/eshu` converged in `85s`, emitted `56,186` facts, completed source-local projection in `39.07596337s`, completed semantic materialization in `5.098503006s`, and passed `eshu list`, `eshu find name handleRelationships`, direct calls, call-chain, and dead-code CLI checks against the live NornicDB-backed API. A focused `/home/ubuntu/eshu-e2e-full/crossplane-xrd-irsa-role` rerun with `K8sResource=1` converged in `20s`, proving the prior `K8sResource` blocker was grouped transaction width. The first full `/home/ubuntu/eshu-e2e-full` corpus run advanced past that blocker but dead-lettered at `722s` after `321/896` repositories when a broad semantic retract statement tried to delete `Annotation|Typedef|TypeAlias|TypeAnnotation|Component|Module|ImplBlock|Protocol|ProtocolImplementation|Variable|Function` in one request and hit the `15s` timeout. Follow-up timing after label-scoped retract avoided that hard timeout but proved the deeper issue: fresh first-generation repos were still doing no-op semantic cleanup for every label. Current branch therefore makes `K8sResource=1` the built-in grouped cap, skips semantic retract on first-generation first attempts, and routes refresh/retry NornicDB semantic retract through one label-scoped statement per semantic label while preserving Neo4j's broad multi-label retract.
- the next full-corpus timing run moved past first-generation semantic cleanup and exposed a reducer ownership issue instead of a parser or graph-dialect issue: `/home/ubuntu/eshu-e2e-full` failed at `722s` on `hapi-amqp` when `semantic_entity_materialization` timed out on a tiny `Function rows=4` write while `inheritance_materialization` for the same scope was concurrently holding a graph write for about `16s`. This is same-scope reducer graph-write contention. The current branch now fences reducer claims by `scope_id`: unrelated repos can still run in parallel, but one repo scope cannot have another unexpired reducer work item claimed/running, and batch claims select at most one pending/retrying reducer item per scope.
- the rerun with reducer same-scope fencing crossed the old `hapi-amqp` wall: that repo's inheritance write took `17.615437958s`, then semantic materialization succeeded in `0.026024905s` instead of timing out. The full corpus advanced to `365/896` repositories and failed later at `932s` on a different source-local shape: one `K8sResource` inline-containment statement for `iac-eks-argocd/teams/devops-team/rbac.yaml` carried `rows=5` and hit the `15s` canonical write timeout. Current branch now narrows the built-in `K8sResource` entity row cap to `1` for NornicDB and adds an operator-facing timeout hint to graph write timeout errors so failures name `ESHU_CANONICAL_WRITE_TIMEOUT` directly. Follow-up queue plumbing now persists typed graph timeouts as `failure_class=graph_write_timeout` with sanitized phase/label/row details; typed graph write deadlines are now bounded-retry candidates, while deterministic syntax/schema failures remain terminal because they do not implement the retry contract.
- the next timing pass answered the timeout-policy question directly: `ESHU_CANONICAL_WRITE_TIMEOUT=30s` moved the full-corpus wall from `1163s` to `1694s`, proving the knob works and the new error hint is actionable, but the run still failed on a much wider semantic statement (`Annotation rows=500`). That slice promoted `30s` as the NornicDB default and introduced a semantic `Annotation` row cap instead of treating a larger timeout as the only fix.
- the next rerun crossed the previous Annotation timeout but exposed a NornicDB optimistic write-conflict surface during `inheritance_materialization`: `conflict: edge ... changed after transaction start`. Current branch classifies that NornicDB conflict text as transient in the shared graph retry executor and returns a queue-retryable error when local retries are exhausted, matching the existing Neo4j deadlock behavior without branching reducer domains by backend.
- the rerun with conflict retrying proved the classifier in live traffic, then moved to a later semantic wall: `Function rows=15` hit the `30s` graph write budget at `2170s`. Current branch narrows only the NornicDB semantic `Function` cap to `10`; canonical Function writes keep their separate tuning.
- the follow-up with semantic `Function=10` exposed one more high-width semantic family: `Annotation rows=100` still reached the `30s` write budget at `1859s`, so the branch narrowed Annotation to `50`. The next full-corpus pass crossed the prior wall and reached `504/896` projector scopes before `Annotation rows=50` failed at `1849s`; several successful `rows=50` statements were already in the `23s-25s` band. A follow-up with `Annotation=25` crossed the previous `504`-scope wall but still failed on `Annotation rows=25`, with another 25-row statement completing at `25.49501893s`. Current branch therefore narrows NornicDB semantic `Annotation` to `10`, keeps `ESHU_CANONICAL_WRITE_TIMEOUT` configurable, and updates the remote harness to test the same semantic cap baseline that ships in code.
- the `Annotation=10` full-corpus timing run failed later at `1975s` after `522/896` projector scopes. The immediate dead letter was still `semantic_entity_materialization` on `Annotation rows=10`, but the top canonical-write profile exposed the real upstream pressure: `fsbo-mobile/.yarn/releases/yarn-4.13.0.cjs` produced `45,381` facts from only `59` files and repeatedly spent `6s-8s` per ten `Variable` rows. That Yarn release bundle is package-manager generated code, not application source, so the current branch prunes `.yarn` plus Yarn Berry Plug'n'Play loader files (`.pnp.cjs`, `.pnp.loader.mjs`) during native discovery instead of lowering semantic row caps again.
- the follow-up full-corpus run with `.yarn` pruned reached roughly `620/896` repositories and failed after `47m12s` on a tiny `semantic_entity_materialization` write (`Function rows=2`) while source-local canonical writes for other scopes were active. Isolated replay of the same statement completed quickly, so the evidence points at graph-write contention and timeout propagation rather than row width. Source review confirmed NornicDB intentionally uses snapshot isolation with commit-time write-conflict detection; the ESHU-side fix is to keep same-scope reducer fencing, treat NornicDB MVCC conflicts as transient whole-work retries, and now propagate the same `ESHU_CANONICAL_WRITE_TIMEOUT` budget to reducer Bolt `tx_timeout` metadata so client and server deadlines agree. The corresponding upstream NornicDB fix is now proposed in `orneryd/NornicDB#120`: map MVCC conflict/deadlock errors to Neo4j transient transaction codes at the Bolt layer so driver-level managed transactions can retry them consistently.
- the next full-corpus timing pass moved past the prior row-width symptoms but exposed a different hot shape before failure: `K8sResource` canonical writes were already capped to one row and one grouped statement, yet each `UNWIND $rows AS row ... MATCH File ... MERGE K8sResource ... MERGE CONTAINS` statement repeatedly cost roughly `3.2s-4.1s` under concurrent IaC-heavy projection. That temporary evidence led Eshu to try one-row execute-only statements, but the combined `#119`/`#120` NornicDB binary changed the trade-off: the 2026-04-26 full-corpus lane showed per-file segmentation manufacturing thousands of execute-only singleton statements for ordinary one-row files. Current branch now keeps only true `shortestPath` / `allShortestPaths` hazard rows on the singleton fallback and lets normal one-row file-scoped inline-containment batches stay on the documented `UNWIND $rows AS row MERGE ... SET n += row.props ... MERGE CONTAINS` hot path.
- the follow-up full-corpus lane using Eshu `d8ced899` plus NornicDB `86e78f1` proved the one-row grouping fix was active (`singleton_statements=0` for normal labels), but it still failed at `331s` on `semantic_entity_materialization` (`Function rows=8`) while several concurrent one-row `K8sResource` grouped chunks were spending `10s-15s` each. Read-only source inspection found the sharper schema precondition: Eshu writes `MERGE (n:K8sResource {uid: row.entity_id})`, but `K8sResource` was missing from the Go-owned `uidConstraintLabels` set. Neo4j still had the composite `k8s_resource_unique` constraint, while NornicDB skips composite uniqueness and therefore could not use a schema-backed `uid` merge lookup for this hot path. Current branch adds `k8s_resource_uid_unique` so the documented grouped shape can stay concurrent without forcing NornicDB into a label/all-node scan.
- the next lane with Eshu `4fb13419` plus NornicDB `86e78f1` proved the `K8sResource.uid` schema precondition was the right root cause: focused Crossplane `K8sResource` chunks dropped to millisecond-scale writes and normal labels still reported `singleton_statements=0`. The full corpus then failed later at `437s` on three concurrent tiny reducer semantic writes (`TypeAlias rows=1/3/7`) all timing out at the same `30s` budget. Because row width and the prior `K8sResource` lookup were no longer the bottleneck, the current branch now defaults `ESHU_REDUCER_WORKERS` to `1` only when `ESHU_GRAPH_BACKEND=nornicdb`; Neo4j keeps the existing parallel default, and NornicDB operators can still override `ESHU_REDUCER_WORKERS` explicitly for contention experiments.
- `orneryd/NornicDB#120` now carries the current compatibility handoff: Bolt MVCC conflict/deadlock mapping to Neo4j transient transaction codes, a shared `nodeLookupCache` lock across transaction clones to fix the `fatal error: concurrent map writes` crash seen under Eshu load, and generalized `UNWIND/MATCH/MERGE` batch hot-path support for named relationship variables plus `SET rel.*`. The failed remote run at NornicDB commit `7d4de63` crossed the prior contention wall but hit `UNWIND MATCH failed: REMOVE requires a MATCH clause first` after Eshu's file-scoped entity-containment statement fell out of NornicDB's batch hot path and the fallback parser saw source text containing `REMOVE`. The follow-up branch head `3239983` keeps that statement on the intended hot path instead of asking Eshu to globally serialize graph writes.
- the first `#120`-only full-corpus lane was intentionally stopped because it did not include PR `#119`'s SQL-edge hot path and therefore could not prove the combined compatibility stack. The current evaluation branch is `linuxdynasty/NornicDB:eshu-119-120-combined` at commit `86e78f1`, stacking `#119` plus `#120` over `orneryd/main`.
- the combined branch passed both local and remote NornicDB focused gates with `go test -tags 'noui nolocalllm' ./pkg/cypher ./pkg/bolt ./pkg/server ./pkg/neo4jcompat -count=1`; the remote headless binary was rebuilt from that exact commit as `/home/ubuntu/os-repos/NornicDB/bin/nornicdb-headless-eshu-119-120-combined`.
- the combined binary completed the `/home/ubuntu/eshu-test-repos` 23-repo authoritative lane in `80s` after self-repo `20s` and focused Crossplane `5s` warmups. Final status was healthy with the queue drained and no dead letters; the only projection-failure log lines were shutdown `context canceled` messages emitted after the harness stopped the already-healthy owner.
- the manual `ESHU_REDUCER_WORKERS=1` diagnostic lane stayed healthy past the earlier concurrent semantic timeout wall, but the live source-local profile then named a new generated-source pressure source: Laravel/webpack `public/js/app.js`-style files that are not `.min.js` or `.bundle.js` still carried webpack bootstrap wrappers and emitted tens of thousands of generated `Variable` rows. Current branch now prunes large Webpack, Rollup, esbuild, and Parcel bundles by content signature and records them as `files_skipped.content.generated-*`, preserving authored JavaScript files while avoiding another row-cap workaround for generated artifacts.
- the rebuilt-reducer full-corpus lane confirmed `eshu-reducer` now starts with `workers=1` under NornicDB and showed the webpack skip counters firing, then failed later on `legacy-service-a` at the canonical `files` phase after `286,180` facts were emitted. The sampled paths included bundled Zend Framework (`src/marinus/library/Zend/...`) and checked-in browser/PDF libraries (`jquery.js`, `galleria`, `shadowbox`, `fpdf`) outside conventional `vendor/` directories. Current branch therefore extends the same discovery discipline to these known legacy vendored-library families and records the reason as `files_skipped.content.vendored-*` instead of lowering the global NornicDB file batch first.
- the now-superseded remote full-corpus lane used Eshu `b6087e2c` with the combined PR `#119` / `#120` NornicDB headless binary at `86e78f1`. It passed self-repo in `20s`, passed focused Crossplane in `5s`, and reported all `896` repos in `/home/ubuntu/eshu-e2e-full` in `642s` with `final_status=healthy`. Later drain-gate review showed this lane is valid collector-throughput and filter-activation evidence only, not promotion-complete projection evidence. It still proves the baked NornicDB reducer default (`workers=1`), the generated-bundle filters (`files_skipped.content.generated-webpack`, `files_skipped.content.generated-rollup`, `files_skipped.content.generated-esbuild`, `files_skipped.content.generated-parcel`), and the legacy vendored-library filters (`files_skipped.content.vendored-zend-framework`, `files_skipped.content.vendored-browser-library`, `files_skipped.content.vendored-fpdf`) were active under the production-scale corpus. That run also exposed harmless shutdown `context canceled` projector noise after the harness stopped the owner; current branch now treats owner-context cancellation during fact load/projection as shutdown instead of failing the durable projector work item, and logs that path at info level with `status=shutdown_canceled`.
- the follow-up lane with Eshu `dacc44db` and the same NornicDB binary finished collection for all `896` repos in `657.795874051s`, but log review showed the local-host supervisor stopped the owner immediately after the clean ingester exit while projector/reducer backlog remained (`Queue: pending=5955 in_flight=9`, `Reducer: pending=5914`). That makes earlier `final_status=healthy` timing useful for collector throughput only, not a complete projection-drain proof. Current branch fixes the Eshu lifecycle bug: in `local_authoritative` watch mode, a clean `eshu-ingester` exit now means "collection stream drained" and the owner keeps Postgres, NornicDB, and `eshu-reducer` alive until the user stops the workspace. The next promotion lane must poll status until `Health: healthy` with an empty queue before stopping the owner.
- the corrected drain-gate lane on Eshu `56d8f9c8` with the same NornicDB binary proved the new owner lifecycle on small lanes: self-repo drained healthy in `76s`, and focused Crossplane drained healthy in `10s`. The full `/home/ubuntu/eshu-e2e-full` lane was stopped before drain because the status panel still showed `Health: progressing`, no dead letters, no overdue claims, and roughly `6k` queue items outstanding (`Projector: pending=35 claimed=3 running=5 succeeded=853`, `Reducer: pending=5995 claimed=1 succeeded=306`, top backlog `deployment_mapping`). That queue shape is expected from the current shared-follow-up model: each repo can publish seven reducer domains (`workload_identity`, `deployable_unit_correlation`, `workload_materialization`, `code_call_materialization`, `deployment_mapping`, `sql_relationship_materialization`, `inheritance_materialization`), so `896` repos can enqueue `6,272` reducer items before reopened deployment mapping and already-drained work are considered. The next blocker is reducer convergence throughput and queue-shape analysis under NornicDB's current `workers=1` default, not graph-write correctness. Do not mark the full corpus promotion-positive until the corrected gate reaches `Health: healthy` with `Queue: pending=0 in_flight=0`.
- the follow-up `ESHU_REDUCER_WORKERS=2` corrected-drain A/B did not fail from NornicDB write conflicts, but it did expose a Eshu reducer lease-window bug: the batch claimer leased multiple intents into the in-memory worker channel, then slow graph-write items sat unstarted until their `60s` queue lease expired. Status reported `Health: stalled` with overdue reducer claims while the database still showed claimed-but-not-running reducer rows. Current branch therefore narrows the NornicDB reducer claim window to `ESHU_REDUCER_BATCH_CLAIM_SIZE=1` by default while leaving Neo4j's `workers * 4` batch claim unchanged.
- the rerun with `ESHU_REDUCER_WORKERS=2` plus the single-item NornicDB claim window proved that fix: the full corpus reached the same corrected drain gate with `overdue_claims=0` for roughly five minutes. It then failed at `311s` on `semantic_entity_materialization` with a single-row `Annotation` write timing out at the `30s` graph budget while source-local projection was still active (`Projector: running=6`, `Reducer: claimed=5`, no retry/dead-letter backlog before the timeout). Because one-row semantic writes cannot be made narrower, this names the next real bottleneck: global NornicDB graph-write contention between source-local canonical projection and reducer semantic writes during first-generation local-authoritative scans. Current branch now adds a NornicDB-local-authoritative reducer claim gate that waits for outstanding source-local projector work to drain before claiming reducer `fact_work_items`, preserving Neo4j's production concurrency path while removing the unsafe first-generation overlap.
- the follow-up full-corpus lane with Eshu `d1141690` and NornicDB `86e78f1` proved the source-local drain gate but also showed why full-corpus reruns are the wrong first debugging loop. Source-local/projector work completed all `896` repos at about `41m12s`, then reducer evaluation with effective `ESHU_REDUCER_WORKERS=2` hit `graph_write_timeout` after about `1h05m` on `semantic_entity_materialization` (`Function rows=4`, `attempt_count=1`) while another nearby four-row Function write had completed in `26.724928881s`. Current branch marks typed `GraphWriteTimeoutError` values as bounded-retry candidates so this pressure first re-enters the queue instead of dead-lettering on one attempt. The next validation lane must isolate that repo with a larger correctness-validation timeout before running the 15-20 repo corpus and then the full corpus again.
- the focused replay for that failed scope used Eshu `a8ee127b`, NornicDB `86e78f1`, fresh `ESHU_HOME`, and `ESHU_CANONICAL_WRITE_TIMEOUT=120s` against `/home/ubuntu/eshu-e2e-full/api-node-ai-product-description-generation`. It drained healthy in roughly `12s`: discovery emitted `544` facts, source-local projection succeeded in `0.325947169s`, the exact prior `semantic_entity_materialization` statement (`Function rows=4`) completed in `0.006154829s`, all `9` queue rows finished `succeeded`, and the error scan found no `graph_write_timeout`, dead letter, panic, fatal, or acceptance-cap lines. This proves the failed full-corpus statement was not deterministically invalid and validates the single-repo-first ladder before the medium-corpus rerun.
- the medium-corpus correctness lane then used Eshu `1a978f69`, NornicDB `86e78f1`, fresh `ESHU_HOME`, and `ESHU_CANONICAL_WRITE_TIMEOUT=120s` against `/home/ubuntu/eshu-test-repos` (`23` repos). It reached the healthy drain gate in about `5m09s`: projector `succeeded=23`, reducer `succeeded=184`, total `fact_work_items` `succeeded=207`, queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`, and `content_entities=182,305`. The error scan found no `graph_write_timeout`, panic, fatal, acceptance-cap, or true failure lines. The slowest visible shapes were source-local PHP file/entity phases from checked-in legacy libraries, so the next full-corpus run should keep the correctness timeout while preserving discovery-report evidence for later repo-local filtering/performance tuning.
- the follow-up full-corpus correctness lane with Eshu `95f2c8aa` and NornicDB `86e78f1` intentionally stopped after about `4h48m` once it had proved the new timeout retry path and named the next semantic write family. Source-local/projector work had completed all `896` repos and there were still no dead letters or failed rows, but the latest retrying failure was `semantic_entity_materialization` on `TypeAlias rows=42` at the `120s` correctness-validation budget. Nearby evidence showed `TypeAlias rows=5` completed in `21.326317754s`, `Annotation rows=6` consumed `108.062344975s`, and `TypeAnnotation rows=181` consumed `118.286012152s`. Current branch narrows the built-in NornicDB semantic caps to `Annotation=5,TypeAlias=5,TypeAnnotation=50` so the next full-corpus attempt starts from the observed statement families instead of simply raising the timeout again.
- the focused replay of that exact `TypeAlias rows=42` repo used Eshu `a5db4165`, NornicDB `86e78f1`, fresh `ESHU_HOME`, and `ESHU_CANONICAL_WRITE_TIMEOUT=120s` against `/home/ubuntu/eshu-e2e-full/api-node-ai-provider`. It drained healthy in about `22s`: projector `succeeded=1`, reducer `succeeded=8`, queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`, and semantic materialization completed in `0.456658928s`. The former wide write split into eight `TypeAlias rows=5` statements plus one `rows=2` statement, each millisecond-scale, with no graph timeout, semantic failure, dead letter, panic, or fatal lines.
- the DB-driven medium rerun used the same Eshu `a5db4165` and NornicDB `86e78f1` baseline against `/home/ubuntu/eshu-test-repos` (`23` repos) with a fresh `ESHU_HOME`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, and a Postgres queue-drain gate instead of stale-log matching. It reached `healthy_db_drained` in about `5m`: projector `succeeded=23`, reducer `succeeded=184`, total `fact_work_items` `succeeded=207`, and queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. The error scan found no `graph_write_timeout`, semantic failure, true dead letter, panic, fatal, or acceptance-cap lines. This re-clears the medium gate with the semantic caps that split the prior `TypeAlias rows=42` statement, so the next correctness proof is a full `/home/ubuntu/eshu-e2e-full` DB-driven drain run.
- the next full-corpus DB-driven drain run used Eshu `95d6091e`, NornicDB `86e78f1`, fresh `ESHU_HOME`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, `ESHU_REDUCER_WORKERS=2`, and `ESHU_REDUCER_BATCH_CLAIM_SIZE=1` against all `896` repos. It completed source-local/projector work for all repos at about `43m38s` with no failure rows, then exposed reducer convergence as the active bottleneck: long `inheritance_materialization` items regularly took `100s+`, one `workload_materialization` item took `323.084087394s`, and at about `1h12m` the queue correctly retried a `semantic_entity_materialization` timeout (`TypeAlias rows=4`) instead of dead-lettering. Code review of that live lane found a Eshu recovery bug independent of the slow Cypher shapes: batch reducer claims only selected `pending` / `retrying` rows, unlike the single claimer, so a restart in batch mode could strand expired `claimed` / `running` reducer rows. Current branch aligns batch reclaim with the single-claim path while preserving same-scope reducer fencing.
- the focused replay of that exact failed repo (`/home/ubuntu/eshu-e2e-full/api-node-ai-summary`) used Eshu `a592b480`, the same NornicDB binary, fresh `ESHU_HOME`, and the same `120s` write budget. It drained healthy immediately: projector `succeeded=1`, reducer `succeeded=8`, queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`, and the formerly failing `semantic label=TypeAlias rows=4` statement completed in `0.00333166s`. This disproves row count as the root cause for the full-corpus timeout and points back to accumulated graph-size lookup behavior. Current branch therefore adds NornicDB-only explicit `uid` lookup indexes for every graph label in `uidConstraintLabels`, mirroring the earlier `Repository.id` / `Directory.path` / `File.path` merge-anchor fix while leaving Neo4j on constraint-backed lookup behavior.
- Eshu `53bb7803` was pulled onto the remote VM, rebuilt, and verified against the real NornicDB binary: the NornicDB compatibility syntax test accepted the new `CREATE INDEX ... ON (n.uid)` form, the focused `api-node-ai-summary` replay drained healthy again with `TypeAlias rows=4` completing in `0.00400289s`, and the medium `/home/ubuntu/eshu-test-repos` corpus drained healthy in `289s` (`projector succeeded=23`, `reducer succeeded=184`, `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`). The next proof step is a fresh full `/home/ubuntu/eshu-e2e-full` run on `53bb7803` to validate whether explicit semantic UID lookup indexes remove the accumulated-graph timeout.
- reducer write-path review found the same lookup-index asymmetry in workload materialization: the direct workload Cypher matches and merges through `Workload.id`, `WorkloadInstance.id`, and `Platform.id`, while NornicDB only had explicit merge-anchor indexes for `Repository.id`, `Directory.path`, `File.path`, and semantic `uid`. Because the prior full run already showed a `workload_materialization` item taking `323.084087394s`, current branch adds NornicDB-only explicit lookup indexes for those workload identity keys before restarting the full-corpus proof, rather than waiting for another full-run timeout to rediscover the same graph-size lookup class.

- the 2026-05-13 full-corpus regression pass first proved a no-op repository cleanup issue, then the clean rerun moved the wall to shared `code_calls` projection: source-local projection was still progressing, but `code_calls` held more than `200k` open intents and individual edge-write groups were already taking tens of seconds to minutes. Query-shape review against NornicDB's hot-path docs showed Eshu matches file endpoints as `File {uid: ...}` while the NornicDB schema only had explicit file lookup support for `File.path`. Current branch adds `File` to the Go-owned `uid` constraint list, which also emits `nornicdb_file_uid_lookup`, and adds bounded code-call route summaries to shared-edge logs so future runs identify whether the slow path is `CALLS`, `REFERENCES`, `USES_METACLASS`, fallback multi-label routing, or a specific source/target label pair.
- Performance Evidence: PR #268 / issue #266 recovered the full-corpus NornicDB
  lane below the `20m` stop-and-fix mark. The clean 2026-05-14 run drained
  `896/896` repositories in `14m13.6s`; collector stream finished in
  `10m33.9s`, bootstrap collection in `11m43.7s`, terminal fact queue was
  `8347/8347` succeeded with `0` open, shared `code_calls` was
  `344148/0` open, shared `repo_dependency` was `1188/0` open, and
  failures/dead letters were `0/0`.
- Observability Evidence: the regression was isolated through admin status,
  Postgres queue/domain counts, shared projection backlog, bounded shared-edge
  route logs, source-local cleanup timing, reducer extraction timing, and
  focused replay of preserved `code_calls` payloads. The new CI gate
  `scripts/verify-performance-evidence.sh` now requires future hot Cypher,
  queue, worker, lease, batching, or collector fanout changes to carry tracked
  performance and observability markers before merge.
- No-Regression Evidence: current branch keeps graph schema DDL idempotent and
  keeps non-deadline statement failures on the warning-and-continue path, while
  returning context deadline/cancellation immediately so Kubernetes bootstrap
  jobs do not burn their whole active deadline with no operator-visible
  progress.
- Observability Evidence: current branch adds per graph schema statement
  `applying`, `applied`, and `failed` logs with backend, phase, ordinal, total,
  duration, failure class, and bounded statement summary; the
  `ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT` bootstrap wrapper defaults each graph
  DDL statement to a `2m` client deadline.
- the fresh full-corpus lane with those lookup indexes finished source-local/projector work for all `896` repos at about `42m` and entered reducer drain with no persisted failure rows. While observing that run, Eshu found another queue-side invariant: batch reducer claiming could lease more intents than ready workers could immediately execute, so slow reducer graph writes left later claimed rows sitting in memory without heartbeat renewal until their `60s` lease expired. Current branch fixes the dispatcher rather than hiding the signal with a larger lease: workers now advertise readiness before the claimer leases work, and `ClaimBatch` is bounded by currently ready worker slots so every claimed reducer item can start the heartbeat-protected execution path immediately.
- the corrected full-corpus lane then proved the dispatcher fix and named the next reducer bottleneck without waiting hours for another timeout. Source-local drained all `896` repos at about `42m` with `2,606,031` facts (`p50=0.25s`, `p90=5.46s`, `p95=139.79s`, max `746.01s`), reducer opened with exactly `2` claimed rows under `ESHU_REDUCER_WORKERS=2`, and there were no overdue claims or failure rows. By `54m52s`, reducer had advanced to `215` succeeded items, but every slow reducer success above `10s` was `inheritance_materialization` (`101s`, `165s`, `166s`, `267.879987639s`) while most other reducer domains completed in milliseconds. Code inspection showed those first-generation inheritance items always ran a graph retract scan even when no prior generation could have reducer-owned inheritance edges. Current branch therefore wires `PriorGenerationCheck` into `InheritanceMaterializationHandler`: first attempt of a true first generation skips the retract, while retries and prior generations still retract to clean partial writes or stale edges. A focused remote replay of the formerly slow `IP2Location` inheritance row on commit `715bed68` drained healthy with queue `pending=0 in_flight=0`, logged `inheritance materialization skipped first-generation retract`, and completed the inheritance reducer item in `0.004070534s`.
- the follow-up focused inheritance subset on commit `4481a894` hardlinked ten formerly slow-in-full repos into one workspace (`TechSupportDatabaseUpdates`, `Tech-Support`, `ansible-install-ssm-agent`, `ansible-aws-nightlysnapshots`, `IP2Location`, `XMLVALIDATOR2`, `POCMatterport`, `api-node-ai-facet-formatter`, `api-aws-metadata`, `api-listings-360`). It drained in `13s` with `10` projector successes and `72` reducer successes, no failures, no overdue claims, and inheritance reducer durations between `0.001082855s` and `0.010728765s`. The medium `/home/ubuntu/eshu-test-repos` gate then drained `21` scopes / `174` work items in `20s` with queue `pending=0 in_flight=0`, no failures, and no overdue claims; the slowest inheritance item was `0.204147545s`, and the only reducer item above `1s` was `semantic_entity_materialization` at `1.704710746s`. This clears the focused and medium proof ladder for the first-generation inheritance retract fix before the next full-corpus measurement run.
- the first corrected-drain rerun with that reducer gate proved the gate itself: self-repo drained healthy in `75s`, focused Crossplane drained healthy in `15s`, and the full `/home/ubuntu/eshu-e2e-full` lane finished collection for all `896` repos in `647.703088168s` while reducer claims stayed at `0` until projector work drained. The next failure moved back to source-local file projection, not reducer contention: `search-api-legacy` emitted `111,602` facts and timed out in `phase=files` on checked-in PEAR/Phing PHP build-tool sources under `framework/library/pear/php/phing/...`. The follow-up lane pruned that Phing subtree and reduced the same repo to `104,589` facts, then failed on the broader PEAR library root at `framework/library/pear/php/PEAR/FixPHP5PEARWarnings.php`. Current branch therefore adds a content-aware `files_skipped.content.vendored-pear` discovery filter rather than widening file write timeouts or lowering global file phase batching.
- the next focused tail-corpus rerun after PEAR pruning moved the blocker to a WordPress-shaped repo: `wordpress` emitted `204,911` facts and timed out in canonical `phase=files`, with the active chunk crossing authored-looking theme paths and third-party plugin paths such as `wp-content/plugins/wordpress-seo/...`. That evidence is intentionally not broad enough for a baked `wp-content/plugins/**` exclusion because many WordPress repos keep authored business logic there. Current branch therefore adds a user-editable `.eshu/discovery.json` map with `ignored_path_globs` and `preserved_path_globs`, pruning exact third-party subtrees before descent and reporting `dirs_skipped.user.<reason>` / `skip_reason=user:<reason>` so operators can tune noisy repos without Eshu silently hiding authored code. The older `.eshu/vendor-roots.json` shape remains accepted as a compatibility alias.
- the focused tail-corpus rerun with `.eshu/vendor-roots.json` on the WordPress repo proved the new user map is active (`dirs_skipped.user.wordpress-core=2`, `dirs_skipped.user.wordpress-seo=1`, `dirs_skipped.user.wordpress-default-theme=14`) and moved past WordPress. The next failure was isolated to `php-large-repo-b`: canonical `phase=files` chunk durations climbed steadily from `0.57s` to `29.65s` before chunk `21/24` timed out at the `30s` write budget. Source inspection of NornicDB's `findMergeNode` path showed uniqueness constraints alone do not appear to populate the property-index lookup used by `MERGE`; without an explicit property index, `MERGE (f:File {path: row.path})`, `MATCH (r:Repository {id: row.repo_id})`, and `MATCH (d:Directory {path: row.dir_path})` can fall back to label scans as the graph grows. Commit `ae314624` therefore adds NornicDB-only merge lookup indexes for `Repository.id`, `Directory.path`, and `File.path` instead of treating the symptom with smaller file batches or a wider timeout; Neo4j keeps the existing schema because its uniqueness constraints already create backing indexes.
- the follow-up `php-large-repo-b` isolated reruns on the remote 16-vCPU test host proved the bottleneck was a combination of NornicDB file-anchor lookup shape and repo-local vendored/archive content, not a representative authored-repo baseline. With the merge lookup indexes active but no repo-local map, discovery still produced `11,641` files / `410,855` facts (`221.9s` snapshot, `69.2s` fact commit) and the canonical files phase crossed archive paths such as `_old/...` while the run proceeded into high-volume entity writes. Adding only repo-local archive roots (`_old/**`, `*_old/**`, `*-old/**`) pruned `17` directories, dropping to `7,644` files / `284,188` facts (`147.0s` snapshot, `50.7s` fact commit). Adding explicit static-library file globs for proven third-party copies (`fotorama.js`, `photorama.js`, `calendar.js`, `sharethis.js`, `masonry.pkgd.js`, `modernizr.js`) skipped only `199` more files but cut facts to `206,890` (`135.8s` snapshot, `28.6s` fact commit), then completed the full local_authoritative run healthy in `471s`; canonical `phase=files` finished in `88.0s`, `phase=entities` in `53.6s`, and queue health returned to `pending=0 in_flight=0`. This keeps the policy line intact: Eshu should not globally hide ambiguous CMS/site code, but the repo-local map is effective for org-specific archive roots and checked-in third-party browser libraries.
- current branch adds `eshu index --discovery-report <file>` and the
  `ESHU_DISCOVERY_REPORT` bootstrap hook so noisy-repo tuning can be based on
  local JSON evidence: discovered/parsed/skipped/materialized counts, top noisy
  directories/files, entity counts, and skip breakdowns. The advisory JSON
  includes `schema_version=discovery_advisory.v1` so scripts can detect future
  shape changes; it remains a diagnostic artifact rather than high-cardinality
  telemetry or a stable public API. This supports the current policy of using
  `.eshu/discovery.json` for ambiguous repo-local archive/vendor decisions
  instead of baking broad global skips.
- while that full-corpus baseline continued, live Postgres and graph-write telemetry exposed a ESHU-side first-generation flow bug: the ingester path was still running relationship evidence backfill inside each new repository commit, which makes a clean local-host scan behave like O(repositories x active relationship facts). Current branch now matches the bootstrap-index ownership model more closely for ingester/local-host runs: per-repo commits set `SkipRelationshipBackfill=true`, and `collector.Service.AfterBatchDrained` runs one deferred `BackfillAllRelationshipEvidence` plus `ReopenDeploymentMappingWorkItems` after the changed-repo batch drains. This preserves the evidence-readiness contract while removing the repeated corpus-wide scan from the hot transaction path. The follow-up full-corpus lane using Eshu `ffac4c20` plus NornicDB `86e78f1` reported `final_status=healthy` in `726s` after collecting all `896` repos; shutdown emitted expected `context canceled` noise after the harness stopped the healthy owner. That makes the deferred-backfill slice promotion-positive, while the one-row grouped-batch fix remains a default-path performance cleanup for the large ordinary-singleton population seen in the same logs.
- the patched-binary batched-containment A/B lane using the same Eshu/NornicDB baseline failed in `230s`: canonical entity chunks were much faster (`Variable rows=10` chunks generally subsecond), but reducer pressure surfaced as a `semantic_entity_materialization` timeout on `TypeAlias rows=3` at the `30s` write budget while queue in-flight counts were high. Do not promote `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true` yet; it needs a separate reducer/semantic contention investigation rather than being treated as a finished default.
- current branch also adds a focused full-stack wiring proof for the patched-binary batched-containment switch: `TestNornicDBBatchedEntityContainmentFullStackUsesCrossFileBatchedEntityRows` verifies normal cross-file entity rows use one row-scoped `MERGE (n:<Label> {uid: row.entity_id}) SET n += row.props MATCH (f:File {path: row.file_path})` grouped statement and do not fall back to file-scoped singleton execution. That test is separate from the remote A/B run, which still decides whether `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true` should graduate from patched-binary evaluation to the default once PRs `#119` and `#120` are merged/released.
- the isolated `php-large-repo-b` rerun after the supervisor-exit fix reached a healthy main queue in about `11m39s` with `284,188` facts, source-local projection success in `401.07181508s`, canonical `files` chunk times below the `30s` write budget, canonical `entities` completed in `122.917410955s`, and no hard graph-write failures. That run then exposed a Eshu reducer guard rather than a NornicDB write bottleneck: the code-call projection sidecar repeatedly failed with `code call acceptance intent scan reached cap (10000)`. Because code-call projection retracts repo-wide CALLS edges and must rewrite the full accepted repo/run slice before marking intents complete, current branch replaces the hidden `10,000` cap with `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` (`250000` default). This is a full-slice correctness guard, not a graph-write batch-size knob; operators should use discovery advisory evidence before raising it further. The follow-up remote rerun on Eshu `33e540b6` and NornicDB `86e78f1` proved the guard change: source-local projection succeeded in `400.532644185s`, `phase=files` completed in `91.243188629s`, `phase=entities` completed in `123.190269023s`, code-call projection completed `24,583` rows in `21.615712301s`, final status returned to `Health: healthy` with queue `pending=0 in_flight=0`, and an exact error scan found no acceptance-cap, graph-timeout, dead-letter, panic, or fatal lines.
- the 2026-04-27 isolated patched-binary batched-containment rerun on Eshu `dcb5e466` proved the cross-file containment path can complete the formerly noisy `php-large-repo-b` stress repo when reducer concurrency is bounded and the completion gate waits for queue drain rather than watcher process exit. The repo discovered `74,475` files and persisted `176,201` facts; collection/emission completed in `161.706108907s`, source-local projection reached full content shape (`Variable=131,977`, `Function=28,926`, `Class=6`), and the main queue drained at `15:41:40Z` with `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. Canonical `Variable` used the intended batched shape (`batch_across_files=true`) and completed `131,977` rows as `13,198` statements / `2,640` grouped executions in `301.798956955s` with no singleton fallbacks; reducer domains then all succeeded (`semantic_entity_materialization` `15.987494447s`, `sql_relationship_materialization` `13.81636032s`, `workload_materialization` `14.294148328s`). This validates correctness for the patched-binary switch but does not yet promote it to the default: file-phase chunks still showed graph-size slope before entity writes, and Variable execution average grew from roughly `0.075s` to `0.114s`, so the next performance slice should prove whether NornicDB is still scanning on file/relationship existence checks before changing more Eshu batch caps.
- read-only source inspection after that run identified the strongest file-phase root-cause hypothesis: Eshu's `File` upsert shape should be eligible for NornicDB's `UnwindMergeChainBatch` path, but NornicDB `CreateNode` still calls unique-constraint validation that scans the existing label index for `File.path` before writing each new file node, and `MERGE (r)-[:REPO_CONTAINS]->(f)` checks relationship existence by scanning outgoing edges from the repository node. That matches the observed fixed-size file chunks getting slower as the repo graph fills. The next NornicDB-side proof should microbench the exact Eshu file Cypher with variants for node-only, full repo/dir relationships, and disabled `File.path` uniqueness; the likely fix is direct unique-value lookup for constraint validation, followed by a direct `(startID,type,endID)` relationship-existence path if repo out-degree remains visible.
- the follow-up NornicDB storage patch proof narrowed that hypothesis to the relationship-existence path and validated the backend-side fix. The patched binary from `linuxdynasty/NornicDB:fix/edge-between-index` adds a direct edge-between index and keeps it in sync across create, update, delete, bulk, transaction, and startup backfill paths. Re-running the focused `php-large-repo-a` repo with Eshu `98e56394`, NornicDB `v1.0.43`, `ESHU_REDUCER_WORKERS=8`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT=250000`, and `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true` drained healthy with queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. The canonical file phase completed `52` statements in `1.495799315s`, `Function` completed `19,496` rows in `6.052502677s`, `Variable` completed `118,768` rows as `1,188` statements / `238` grouped executions in `62.339678485s` with max execution `0.43696576s`, and total canonical write completed in `71.101742343s`. This is promotion-positive for the NornicDB PR because the exact Eshu `MERGE` relationship path now stays bounded instead of depending on start-node out-degree; remaining single-repo cost is high-cardinality entity volume, not another relationship-existence timeout.
- the follow-up medium-corpus proof used the same patched NornicDB binary against `/home/ubuntu/eshu-test-repos` and drained `23` repos by the `2026-04-28T01:07:15Z` poll, roughly `3m20s` after local-host start. Final durable state was projector `23/23`, reducer `184` succeeded items, and queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`; the error scan found no `ERROR`, `graph_write_timeout`, panic, fatal, acceptance-cap, or actual dead-letter lines. The large `php-large-repo-a` repo remained the canonical-write tail, but with bounded relationship work: discovery `0.364736817s`, pre-scan `7.726994876s`, parse `7.584278219s`, materialize `1.507529348s`, fact commit `19.203133155s`, content write `20.975333099s`, file phase `1.6830725s`, `Function` `19,496` rows in `7.312182761s`, `Variable` `118,768` rows in `69.750108696s` with max execution `0.437996133s`, canonical entities `77.19844595s`, and total canonical write `80.173499907s`. This clears the representative medium gate for the edge-between index patch while keeping the remaining tuning target honest: high-cardinality `Variable` entity volume still dominates, but file/relationship existence no longer shows the old graph-size slope.
- current branch adds a consolidated [Environment Variables](../reference/environment-variables.md) operator reference that makes the NornicDB tuning contract explicit across the whole Eshu runtime. It documents owner runtime, default, purpose, and "tune when" guidance for graph-write, discovery, reducer, database, telemetry, Compose, and compatibility variables so future NornicDB performance work starts from evidence instead of repeatedly rediscovering which knob owns which failure mode.
- follow-up policy after the first healthy multi-repo run: keep the consolidated [NornicDB Tuning](../reference/nornicdb-tuning.md) page as the operator source of truth for `ESHU_NORNICDB_*` variables, and add future phase-specific controls only after evidence names a new heavy phase such as call edges, infra edges, or another shared reducer domain.
- the follow-up 2026-04-27 representative 20-repo lane used Eshu `9ff4252f`, NornicDB `v1.0.43`, `ESHU_REDUCER_WORKERS=8`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT=250000`, and the property-only NornicDB semantic materialization path against a hardlinked subset from `/home/ubuntu/eshu-e2e-full`. It drained healthy in about `17m32s`: projector `succeeded=20`, reducer `succeeded=163`, and queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. The large tail was `php-large-repo-b`, not a failed semantic reducer: Eshu had already skipped `_old` and `vendor` paths in the content store (`vendor_files=0`, `_old=0`), but the indexed repo still contained `7,636` files, about `1.14M` lines, `131,977` `Variable` entities, and `28,926` `Function` entities. Canonical `Variable` projection completed all `131,977` rows as `13,198` statements / `2,640` grouped executions in `204.509776279s` with `max_execution_duration_s=0.178891191`; semantic reducer writes then drained without graph timeouts. This re-clears the subset gate and narrows the next tuning question to high-cardinality canonical entity volume plus org-specific site/template classification, not another blind semantic row-cap change.
- the follow-up 2026-04-27 representative 100-repo lane used Eshu `00913d60`, NornicDB `v1.0.43`, `ESHU_REDUCER_WORKERS=8`, `ESHU_REDUCER_BATCH_CLAIM_SIZE=8`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT=250000`, and the same property-only NornicDB semantic path against a deterministic subset from `/home/ubuntu/eshu-e2e-full`. It drained healthy in about `24m27s`: projector `succeeded=100`, reducer `succeeded=772`, and queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`; there were no persisted failure rows and one retried NornicDB transient transaction conflict. The final tail happened while disk and CPU were both low, with only a handful of graph writes in flight and individual semantic statements occasionally taking tens of seconds, which points to backend write serialization / transaction-contention boundaries rather than host resource saturation. This clears the larger representative subset gate and makes the next validation step a full-corpus drain as the final gate, while the next performance design should measure and route conflict domains instead of lowering row caps blindly.
- the monitored 2026-04-27 representative 20-repo rerun used Eshu runtime commit `f607bcbc` plus NornicDB `v1.0.43` with continuous CPU, disk, process, and queue sampling. It drained healthy in about `17m15s`: projector `succeeded=20`, reducer `succeeded=163`, and queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. The final tail confirmed this is not a host-resource problem: disk was often idle and aggregate CPU was low on the 16-vCPU VM while one NornicDB process used roughly one core and a handful of reducer items remained. The slowest reducer items were `semantic_entity_materialization` for `portal-java-ycm` at `102.700286273s`, `sql_relationship_materialization` for `php-large-repo-a` at `63.978445716s`, `semantic_entity_materialization` for `php-large-repo-b` at `45.364188352s`, and `sql_relationship_materialization` for `portal-java-ycm` at `42.800414892s`; the last two pending items waited behind the same-scope ordering guard until the earlier `portal-java-ycm` reducer completed. This clears the monitored subset gate and narrows the next optimization to conflict-domain routing plus exact semantic/relationship Cypher shape analysis, not lower worker counts or another blind timeout/row-cap change.
- read-only reducer flow review after the 100-repo lane identified the safest ESHU-side concurrency seam if the monitored 20-repo run confirms graph-write contention: generalize the existing Postgres claim-time same-`scope_id` exclusion into explicit conflict keys such as graph backend, reducer domain, repo/scope, and graph-write family. That preserves useful cross-repo concurrency, avoids leasing work before a worker can heartbeat it, and keeps coordination durable across reducer pods. For shared projection, mirror the same idea at the partition-lease boundary rather than hiding the router inside the final Cypher executor. This is intentionally different from lowering `ESHU_REDUCER_WORKERS`; lower worker counts remain a diagnostic or temporary safety valve, not the target architecture.
- current branch implements that first conflict-domain seam in the reducer queue itself. `fact_work_items` now stores `conflict_domain` and `conflict_key`, reducer claim SQL fences against unexpired claimed/running rows with the same conflict key, and legacy rows fall back to same-scope serialization via `COALESCE(conflict_key, scope_id)`. The initial policy is deliberately conservative: code-graph reducer domains (`semantic_entity_materialization`, `sql_relationship_materialization`, `code_call_materialization`, `inheritance_materialization`) serialize per repo, platform graph domains (`workload_identity`, `deployable_unit_correlation`, `cloud_asset_resolution`, `deployment_mapping`, `workload_materialization`) serialize per repo, and those two families can overlap after the source-local projector drain gate. With ready-worker batch claiming already in place, NornicDB reducer defaults move back to host CPU concurrency (`NumCPU`) and a claim window equal to worker count instead of using one worker as the safety mechanism; the local-authoritative owner also injects snapshot, parse, projector, and reducer worker counts from the developer host CPU count.
- NornicDB source review keeps the strongest backend hypothesis narrow: Eshu's high-volume file/entity paths use indexed-looking anchors, but relationship `MERGE` existence checks can still scan the start node's outgoing edges, and unique/index validation can still fall back to label scans when the exact hot path is not used. The next proof should compare exact Eshu file/entity Cypher variants (`node-only`, `node + repo edge`, `node + directory edge`, and full shape) against out-degree, unique-lookup, transaction-wait, and statement-duration counters before we decide whether the fix belongs in Eshu query shape, conflict routing, or another NornicDB PR.
- the 2026-04-27 isolated `php-large-repo-b` rerun with Eshu `1db42b58`, NornicDB `v1.0.43`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT=250000`, `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true`, and the built-in `Variable=100` default drained healthy in about `9m36s`: queue returned to `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`, collection/emission took `164.601696015s` plus a `52.854384033s` fact commit, source-local projection succeeded in `302.815071947s`, canonical `phase=entities` completed in `114.131774031s`, and `Variable` completed `131,977` rows as `1,320` statements / `264` grouped executions in `101.762514112s` with max execution `0.702016678s`. This proves the `Variable=100` default is safe for the stress repo, but also shows the next optimization is architectural: the single-repo wall clock is now dominated by front-half collection/fact commit plus one large source-local projection. Current branch therefore adds `collector snapshot stage completed` timing records for `discovery`, `pre_scan`, `parse`, and `materialize` so the next chunked-repo workflow design starts from stage evidence instead of static root-directory assumptions.
- those new timing records immediately disproved a static root-directory worker fix as the first move: on the same `php-large-repo-b` stress repo, `discovery` took `0.284619867s`, serial `pre_scan` took `124.388751739s`, worker-parallel `parse` took `16.451100303s` with `8` workers, and `materialize` took `6.912404476s`. Current branch now routes collector repository pre-scan through a worker-aware parser API while preserving the legacy sequential `PreScanPaths` and `PreScanRepositoryPaths` contracts for direct callers. The next focused rerun should prove whether `pre_scan` falls near parse-time before considering deeper chunked-generation workflow changes.
- the follow-up isolated rerun on Eshu `bb38549b` proved that worker-aware pre-scan fix directly: `pre_scan` dropped from `124.388751739s` to `16.66526715s` with `pre_scan_workers=8`, while `parse` remained comparable at `16.690813898s` and `materialize` stayed `6.985221748s`. The total snapshot stream dropped from `148.403917778s` to `40.991875025s`; fact commit stayed about `51.344587838s`, source-local projection stayed comparable at `303.054837237s`, reducer domains all succeeded, and the queue drained healthy at about `7m38s` end-to-end for the same large repo. Durable fact persistence and one-large-source-local-projection now remain the next non-graph bottlenecks to measure before any chunked-generation rewrite.
- current branch adds the missing stage ledger for those remaining bottlenecks. Ingestion commits now log `ingestion commit stage completed` for transaction begin, scope/generation upserts, repository-catalog load, streaming fact upsert with `fact_count` / `batch_count`, relationship backfill, projector enqueue, and transaction commit. Projector service and runtime now log `projector work stage completed` / `projector runtime stage completed` for fact loading, projection, build, canonical graph write, content-store write, and reducer-intent enqueue. The next isolated `php-large-repo-b` rerun should use these logs to decide whether the fix belongs in Postgres fact persistence, fact reload, content-store write batching, canonical graph Cypher, or a larger chunked-generation workflow.
- the follow-up isolated `php-large-repo-b` stage-ledger rerun on Eshu `e774d50c` and NornicDB `v1.0.43` drained healthy with queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. The new ledger shifted the next bottleneck from guessed graph tuning to measured storage work: snapshot stream took `40.545s`, streaming fact upsert took `52.718s` for `176,399` persisted rows / `569` batches, fact reload took `12.907s`, projection build took `1.420s`, content-store write took `169.325s`, canonical graph write took `117.801s`, reducer-intent enqueue took `0.040s`, and all reducer domains completed without timeout. Canonical `Variable` remained a graph cost center at `101.454s`, but Postgres content write was the largest single source-local stage, so the next slice instruments the content writer itself (`prepare_files`, `upsert_files`, `prepare_entities`, `upsert_entities`) before changing batch sizes, indexes, or workflow shape.
- the content-writer sub-stage rerun on Eshu `318c83e4` and NornicDB `v1.0.43` drained the same isolated `php-large-repo-b` repo healthy after clearing a remote disk-full condition that was unrelated to Eshu/NornicDB behavior. The new content ledger made the bottleneck precise: discovery `0.299s`, worker pre-scan `16.511s`, parse `16.499s`, materialize `6.951s`, fact upsert `51.223s` for `176,399` rows / `569` batches, fact load `12.816s`, projection build `1.491s`, content `prepare_files` `0.011s`, content `upsert_files` `11.418s`, content `prepare_entities` `0.117s`, content `upsert_entities` `158.293s` for `160,909` rows / `537` batches, canonical graph write `119.290s`, and all reducers succeeded with queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. This shifts the next local-authoritative performance slice to Postgres content-entity persistence mechanics: test transaction boundaries, entity batch size, and `source_cache` trigram/index write cost before any full-corpus rerun or chunked-generation redesign.
- the entity-batch A/B on the same isolated repo proved `ESHU_CONTENT_ENTITY_BATCH_SIZE` is not the root fix: raising the batch from `300` to `600` cut statements from `537` to `269`, but `upsert_entities` stayed flat (`158.293s` to `158.814s`). A direct Postgres microbench then isolated the actual cost: copying `160,909` `content_entities` rows into an unindexed table took `1.661s`, into the btree-indexed shape took `2.827s`, and into the full shape with `content_entities_source_trgm_idx` took `132.174s`. The source distribution explains why: `Variable` rows alone carried about `1.108 GB` of `source_cache`, including generated/vendor-style assignments up to `675 KB`. Current branch therefore bounds oversized `Variable` entity snippets at `4 KiB` while leaving `content_files` as the exact full-source search surface and recording truncation metadata. The same full-index microbench with the cap dropped indexed entity text to `168 MB` and insert time to `30.982s`, so the next runtime proof should rerun the single noisy repo before any medium or full-corpus lane.
- the runtime proof for that shaping rule used Eshu `f8322c41`, NornicDB `v1.0.43`, `ESHU_REDUCER_WORKERS=8`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, and the same isolated `php-large-repo-b` repo. The run drained healthy with projector `1/1`, reducer `8/8`, queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`, and no actual error-only scan hits for `ERROR`, `graph_write_timeout`, panic, fatal, or acceptance-cap lines. Source-local stages moved as predicted: fact upsert fell from about `52.4s` to `19.752s`, content entity upsert fell from `158.293s` to `31.956s`, total content write fell from about `170s` to `43.762s`, and total source-local projection fell from about `309s` to `165.604s` while canonical graph write stayed comparable (`120.248s`). Persisted content stayed at `160,909` entities; `37,288` `Variable` rows carried truncation metadata, total `source_cache` dropped to `164 MB`, and `Function` / `Class` source caches were untouched.
- the follow-up medium-corpus proof on Eshu `a7078ddf` used NornicDB `v1.0.43`, `ESHU_REDUCER_WORKERS=8`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT=250000`, and `/home/ubuntu/eshu-test-repos`. It drained `23` repos healthy in about `3m11s` with projector `23/23`, reducer `184` succeeded work items, and queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. The large `php-large-repo-a` repo wrote `138,712` content entities in `21.196s` but then spent `78.490s` in canonical graph write; across the corpus Eshu persisted `182,305` content entities, with only `1,463` truncated `Variable` rows and `24 MB` total `Variable` source cache. This confirms the source-cache cap generalizes beyond the isolated stress repo and narrows the next performance slice to canonical graph Cypher shape / NornicDB lookup behavior rather than further Postgres content-entity tuning.
- the focused `php-large-repo-a` Variable grouping A/B after that medium proof clarified an operator-facing naming trap: `ESHU_NORNICDB_ENTITY_LABEL_BATCH_SIZES=Variable=100` and `ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS=Variable=5` tune different layers. The first controls rows per Variable statement; the second controls how many Variable statements run in one grouped execution, so the effective grouped pressure is roughly `row_batch * statement_cap`. Keeping `Variable` rows at `100` but raising grouped statements from `5` to `10` completed healthy with only a marginal source-local improvement (`101.320s` to `98.670s` for the large repo), while `25` was clearly slower early in the Variable phase. Keep `Variable=5` as the best proven grouped-statement default and treat higher values as focused experiments, not evidence that the `Variable=100` row-batch default is wrong.
- 2026-05-02 backend handoff: NornicDB commit `a9ccd0f` adds a generic indexed batch path for `UNWIND $rows AS row MATCH (n:<Label> {uid: row.entity_id}) SET ...` and preserves MATCH semantics by skipping missing nodes instead of creating them. This met the maintainer patch bar because upstream-main NornicDB `ac41214` timed out or spent `70-120s` on tiny semantic batches in Eshu, while the patched binary passed focused Cypher/storage tests and drained Eshu proofs cleanly. The endpoint-heavy proof moved semantic handler sum from `48.722s` to `0.509s`; the mixed 25-repo proof moved wall time from `307s` to `201s`; and the full 878-repo proof `eshu-full-unwind-match-set-bbeb5b9d-a9ccd0f-20260502T142756Z` drained `8293/8293` work rows in `1216s` with `0` retrying, failed, or dead-lettered rows. A later Eshu `d641b7b8` rerun against upstream-main `ac41214` reproduced the missing-hot-path behavior: `30s` and `120s` full-corpus attempts retried tiny semantic batches, and a serialized semantic proof avoided retries but moved too slowly for the benchmark. Upstream handoff is now `orneryd/NornicDB#136`; local verification passed `go test -tags 'noui nolocalllm' ./pkg/cypher -run 'TestUnwindMatchSetBatch_UsesIndexedLookupWithoutCreatingMissingNodes|TestUnwindMatchMergeRelationshipSet_UsesChainBatchHotPath' -count=1` and `go test -tags 'noui nolocalllm' ./pkg/cypher ./pkg/storage -count=1`. The follow-up full 896-repo proof `eshu-full-pr136-01413d04-a9ccd0f-20260503T0121Z` drained `8458/8458` work rows in about `873s` with `0` retrying, failed, or dead-lettered rows and passed API/MCP health plus relationship-evidence drilldown checks. Treat this as a real NornicDB compatibility/performance win. NornicDB PR `#136` merged into upstream `main` on 2026-05-03 as `b68b4ef68d4ef827ee9f90aff5e04310c0e1e4c6`; Eshu still needs either an upstream release asset or an explicitly pinned accepted build before depending on it as a released default.

## Context

Eshu's authoritative graph backend is Neo4j. That choice is load-bearing for
production correctness and for the full-stack Compose profile, but it carries
three material costs:

- JVM footprint and ops surface that is heavy relative to the rest of the Eshu
  runtime.
- License model (Neo4j Community GPLv3, commercial Enterprise) that constrains
  downstream packaging.
- Runtime shape that is Docker- or Kubernetes-first, which is a friction point
  for developers who want a laptop-native authoritative graph experience
  without running Compose.

Eshu lightweight local mode was the first response: ship embedded Postgres with
relational code-intelligence tables and refuse high-authority graph queries
via structured `unsupported_capability`. That is correct for the
"single-binary, no extra services" promise, but it leaves a gap:

- Developers cannot run transitive caller analysis, call-chain path queries,
  or dead-code detection locally without Compose.
- That same gap means we cannot dogfood graph-backed code intelligence
  against the Eshu repository itself on a plain laptop.

The capability-port decomposition (ADR 2026-04-20 §5) already made swapping
graph backends a wiring concern rather than a code-rewrite. That opens the
door to evaluating an alternative graph backend without reopening handler-level
contracts.

## Candidate: NornicDB

NornicDB is a pure-Go graph database (module `github.com/orneryd/nornicdb`,
MIT licensed) that speaks Neo4j Bolt + Cypher. Storage is Badger v4 (pure-Go
LSM KV). It ships as a standalone binary / Docker image, not as an in-process
Go library.

Feature evidence (audited 2026-04-22 against the Eshu Cypher query surface):

- Partial coverage of the Cypher features Eshu uses today, including:
  - `MATCH` / `OPTIONAL MATCH`
  - `MERGE` with implicit and explicit `ON CREATE SET` / `ON MATCH SET`
  - Variable-length paths `*1..N`, `*0..N`, unbounded `*`
  - `shortestPath()` with relationship type filters
  - `UNWIND $rows AS row` batched writes
  - `WITH` chaining, `COLLECT(DISTINCT ...)` with map literals
  - `labels()`, `type()`, `nodes()`, `relationships()`, `startNode()`,
    `endNode()`, `length()`
  - `WHERE EXISTS { MATCH ... }` pattern predicates
  - `any(...)`, `all(...)` predicates
  - `CASE WHEN ... THEN ... ELSE`, list comprehensions, `coalesce()`
  - Single-property `CREATE CONSTRAINT ... IS UNIQUE`
  - Composite `CREATE CONSTRAINT ... IS NODE KEY`
  - `CREATE INDEX ... IF NOT EXISTS`
  - Fulltext procedure creation via
    `CALL db.index.fulltext.createNodeIndex(...)`
- Failed ESHU-hot-path syntax probes against `/tmp/nornicdb-headless`
  on 2026-04-22:
  - Eshu's Neo4j-compatible composite
    `CREATE CONSTRAINT ... REQUIRE (...) IS UNIQUE` form returned
    `invalid CREATE CONSTRAINT syntax`.
  - Eshu's Neo4j fulltext fallback uses multi-label
    `CREATE FULLTEXT INDEX ... FOR (n:A|B|C) ...`; NornicDB returned
    `invalid CREATE FULLTEXT INDEX syntax`.
  - The same run passed the procedure fallback
    `db.index.fulltext.createNodeIndex(...)` and
    `COLLECT(DISTINCT {map literal})`.
- Workaround probes against the same binary passed:
  - Composite node identity can also be expressed as
    `REQUIRE (f.name, f.path, f.line_number) IS NODE KEY`, but Eshu does not
    use that form for sparse semantic labels because node keys make every
    participating property mandatory.
  - Multi-label fulltext can be expressed with the procedure form
    `db.index.fulltext.createNodeIndex(...)`.
  - This is an adapter-compatibility option, not a production schema flip:
    Neo4j's key constraints are an Enterprise-only class, while Eshu's
    current composite `IS UNIQUE` constraints are the shared production
    schema contract.
- Eshu therefore routes graph schema bootstrap through a backend schema
  dialect:
  - `neo4j` receives the shared schema unchanged.
  - `nornicdb` skips unsupported composite `IS UNIQUE` DDL. Eshu does not
    translate those constraints to `IS NODE KEY` because node keys require
    every participating property while sparse semantic labels such as
    `Annotation` may not have all properties present.
  - `nornicdb` keeps the single-property `uid` uniqueness constraints that
    canonical writes use as merge identity and preserves the procedure-based
    fulltext form.
  - `nornicdb` skips the Neo4j multi-label `CREATE FULLTEXT INDEX` fallback
    because the verified multi-label path is
    `db.index.fulltext.createNodeIndex(...)`.
  - This routing is intentionally restricted to schema DDL; graph writes,
    query handlers, and MCP tools remain behind shared ports and conformance
    gates.
- Bolt 4.x fully implemented, Bolt 5.x backward compatible with negotiation.
- Eshu uses `github.com/neo4j/neo4j-go-driver/v5`; wire compatibility expected.
- NornicDB exposes explicit Bolt transaction hooks in the runtime
  `nornicdb serve` path, so Eshu can test Neo4j-style grouped canonical writes
  against it. Eshu keeps those grouped writes behind
  `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true` until conformance proves
  rollback, timeout, and no-partial-write behavior on the Eshu workload.
- The 2026-04-23 grouped-write safety probe against a rebuilt
  linuxdynasty-fork headless binary at `/tmp/nornicdb-headless-eshu-rollback`
  (`v1.0.42-hotfix`) passed the Eshu Neo4j-driver path: the Eshu canonical
  repository/file/function grouped commit succeeded, grouped rollback marker
  count was `0`, clean explicit rollback marker count was `0`,
  failed-statement explicit rollback marker count was `0`, and the timeout
  probe left no partial write. That makes grouped writes a valid conformance
  surface for the fixed binary; promotion still requires a release-backed
  fixed NornicDB binary plus the broader matrix and perf gates. The promotion
  gate is
  `ESHU_NORNICDB_REQUIRE_GROUPED_ROLLBACK=true go test ./cmd/eshu -run TestNornicDBGroupedWriteRollbackConformance`.

Non-standard extras NornicDB provides (not required by Eshu today, but
potentially useful later): vector search, hybrid retrieval, tritemporal
facts, as-of reads, graph-ledger modeling, MCP server, GPU acceleration for
semantic workloads.

Performance claims (NornicDB README): 12x-52x LDBC speedups over Neo4j on
published workloads, hybrid retrieval at low single-digit ms locally.
**These numbers are not measured against the Eshu workload.** Eshu's workload
is heavy on variable-length path traversal, `UNWIND` batched writes from the
reducer, and per-repo scope filtering. LDBC speedups do not translate
automatically; perf claims must be re-measured against Eshu queries before
adoption.

## Problem Statement

Eshu needs a graph backend that:

- Preserves authoritative graph truth across `local_authoritative`,
  `local_full_stack`, and `production` profiles.
- Is lighter to operate than Neo4j while remaining correct under the
  same query surface.
- Allows laptop-scale authoritative graph queries without requiring
  Compose.
- Does not force us to maintain two divergent graph codepaths
  indefinitely.

## Decision

This ADR is a **provisional** adoption decision. Adoption lands in stages
gated by evidence.

### 1. Adopt NornicDB as candidate backend for `local_authoritative` profile

Eshu introduces a new runtime profile, `local_authoritative`, that runs the
lightweight local host plus a user-level NornicDB sidecar. This profile
unlocks the high-authority graph queries that `local_lightweight` refuses.

NornicDB runs as a separate process. Laptop installs default to the
headless `nornicdb-headless` artifact; the full `nornicdb` binary remains
an explicit opt-in for users who accept the larger UI / local-LLM payload.
The current installer slice accepts explicit source artefacts with
`eshu install nornicdb --from <source>` and copies the verified binary to
`${ESHU_HOME}/bin/nornicdb-headless`. Supported explicit sources are local
binaries, local tar archives, macOS packages, and matching URLs. Bare
`eshu install nornicdb` is intentionally unavailable while Eshu tracks latest
NornicDB `main` and the embedded release manifest has no accepted assets.
Signature verification and broader coverage remain promotion prerequisites.
The sidecar is inspectable by
`eshu graph status`, `eshu graph logs`, owner-aware `eshu graph stop`, foreground
`eshu graph start`, and stopped-owner `eshu graph upgrade --from <source>` today.
Its runtime lifecycle is tracked in the workspace
data root (`owner.json` records the graph PID, loopback ports, and
per-workspace credentials copied from the graph credential file with `0600`
file permissions).

It does not run embedded in the `eshu` binary. The "lightweight" goal is
preserved by:

- one-command explicit-source install today, no-arg release install only after
  an accepted manifest policy exists
- loopback-only ports owned by the workspace lock
- process ownership tied to the workspace lock
- clean install / uninstall / upgrade

### 2. Evaluate release-backed production posture

The profile-matrix gate is no longer theoretical. `local_authoritative`,
`local_full_stack`, and the NornicDB production profile all have latest-main
evidence, including the 2026-05-04 full-corpus API/MCP proof. NornicDB is the
supported default backend. The remaining work is no longer whether Eshu can use
NornicDB by default; it is whether that default becomes release-backed instead
of tracking the latest accepted NornicDB `main` build. The remaining checks are:

- Neo4j parity research, followed by a terminal comparison and support-posture
  decision
- install-trust policy for accepted NornicDB binaries
- broader host coverage and documented backup, recovery, upgrade, and
  migration operations

### 3. Dual-backend operation during evaluation

Eshu supports both NornicDB and Neo4j adapters simultaneously. Operators select
the graph backend via the `ESHU_GRAPH_BACKEND` environment variable:

- `ESHU_GRAPH_BACKEND=nornicdb` — default today
- `ESHU_GRAPH_BACKEND=neo4j` — explicit Neo4j path

This dimension is also surfaced in responses (optional `truth.backend`
field) and in telemetry span / metric labels.

### 4. Decide Neo4j support path after parity research

After the default flip, Eshu will not automatically deprecate Neo4j. The next
step is the parity optimization plan:

- Map where Neo4j and NornicDB differ inside the backend-neutral writer and
  query seams.
- Add enough Neo4j phase, transaction, retry, queue, and query-plan evidence to
  identify the real bottleneck.
- Ship the smallest proven adapter-level parity slice.
- Rerun a terminal full-corpus comparison before deciding whether Neo4j remains
  production-promoted or moves to a narrower support posture.

### 5. Reject outright embedding

NornicDB does not ship as an in-process Go library. This ADR does not
attempt to embed it. The "lightweight" outcome is delivered by:

- one-command laptop install of a verified headless artefact through
  `eshu install nornicdb --from <source>` today, with pinned release
  download and signature verification required before promotion
- sidecar process lifecycle owned by the local host
- loopback-only health and Bolt endpoints recorded in `owner.json`
- deterministic shutdown sequencing documented in
  `local-host-lifecycle.md`

The earlier rejection in ADR 2026-04-20 of "embedded graph as co-equal
local truth path" stands. A sidecar with a strict install + lifecycle
contract is materially different from an in-process embed; this ADR is
explicit about that distinction.

## Rejection Criteria

NornicDB adoption is abandoned if any of the following is observed during
conformance evaluation:

- Critical Cypher feature gap on a Eshu hot path that cannot be worked around
  without rewriting multiple handlers (for example, composite unique
  constraints, fulltext index creation, or `COLLECT(DISTINCT {map literal})`
  failing to execute as Eshu writes it).
- Reducer bulk-write throughput on Eshu workload shapes falls meaningfully
  below the Neo4j baseline with no clear path to close the gap.
- Bolt handshake or driver incompatibility with
  `github.com/neo4j/neo4j-go-driver/v5` that cannot be resolved by driver or
  adapter configuration.
- MVCC / snapshot-isolation overhead measurably harms single-snapshot-per-tx
  projection writes against Eshu's reducer acceptance model.
- Multi-label MATCH or the fulltext index syntax Eshu uses today does not
  execute cleanly.

If any rejection criterion triggers, the capability-port decomposition still
stands. Any future candidate graph backend is evaluated through the same
matrix.

## Migration Path Summary

1. Land the `local_authoritative` profile: sidecar installer, adapter behind
   `GraphQuery` and the Cypher write executor family, data-root + lifecycle
   updates, conformance suite run at laptop scale.
2. If the laptop gate passes, run the conformance suite against Compose
   (`local_full_stack`) with NornicDB in place of Neo4j.
3. If the Compose gate passes, run conformance + perf against the remote
   896-repo E2E instance (`production`).
4. On full pass: keep NornicDB as the default, close the Neo4j parity decision,
   and ship migration guidance only where the final support posture needs it.

## Consequences

### Positive

- Single authoritative graph backend across laptop, Compose, and production
  if the gates pass.
- Lighter operational surface than Neo4j without reintroducing local graph
  drift.
- Pure Go supply chain.
- Capability-port pattern proven a second time.

### Negative

- Non-trivial evaluation work: adapter implementation, conformance runs at
  three scales, perf comparison.
- Dual-backend operation period adds wiring complexity until the Neo4j support
  posture closes.
- Version pinning and supply chain for a third-party graph binary becomes a
  first-class concern.

### Operational guardrails

- NornicDB is the default graph backend while promotion gates remain explicit.
- `ESHU_GRAPH_BACKEND` is validated at startup; no silent default drift.
- Response `truth.backend` field is optional but consistent across CLI /
  HTTP / MCP when surfaced.
- Operator-visible health probe covers both backends when present.

## Validation Requirements

Before the sidecar is called "supported" on `local_authoritative`:

1. `GraphQuery` plus the Cypher write executor family pass Eshu's existing
   handler and adapter tests.
2. Schema dialect verification passes on a real NornicDB instance:
   `TestNornicDBSchemaAdapterVerification` must execute the complete rendered
   NornicDB schema. The exact-Neo4j syntax probe remains useful evidence for
   upstream parser parity, but local support is gated on the rendered adapter
   schema.
3. Compose smoke test indexes a repo end-to-end with NornicDB in place of
   Neo4j.
4. Performance envelope at laptop scale meets the `local_authoritative`
   targets documented in `local-performance-envelope.md`.

Before promotion to `local_full_stack`:

5. Conformance matrix passes for every capability that Neo4j passes today.
6. Reducer bulk-write throughput parity or better.

Before promotion to `production`:

7. 896-repo remote instance parity on query and write paths.
8. Backup / recovery / upgrade story documented.

### Current Syntax Gate Result

`go test ./cmd/eshu -run TestNornicDBSyntaxVerification -count=1 -v`
skips by default unless `ESHU_NORNICDB_BINARY` is set. The explicit run below
is intentionally part of the promotion gate, not the default unit-test suite:

```bash
ESHU_NORNICDB_BINARY=/tmp/nornicdb-headless \
  go test ./cmd/eshu -run TestNornicDBSyntaxVerification -count=1 -v
```

Result on 2026-04-22: **failed**. Composite node `IS UNIQUE` and
multi-label `CREATE FULLTEXT INDEX` did not parse. The
`db.index.fulltext.createNodeIndex(...)` fallback and
`COLLECT(DISTINCT {map literal})` probes passed. At that point, NornicDB
remained an evaluation candidate only; `local_authoritative` could not be
documented as supported until those syntax gaps were resolved or the Eshu schema
layer had a reviewed backend-specific compatibility plan. Later chunks in this
ADR record the schema adapter, full-corpus proof, and default-backend decision.

`TestNornicDBCompatibilityWorkarounds` passed against the same binary with
composite `IS NODE KEY` and the multi-label fulltext procedure form. That
workaround is viable only behind a graph-backend schema adapter or an upstream
NornicDB parser fix; it must not replace the explicit Neo4j schema globally.

`TestNornicDBSchemaAdapterVerification` passed against the same binary after
the schema-dialect router rendered NornicDB-compatible DDL. This validates the
adapter approach for bootstrap schema only; broader graph-read and graph-write
conformance still must pass before promotion.

Manual MCP smoke on 2026-04-23 used
`ESHU_HOME=/tmp/eshu-local-authoritative-e2e2` and
`ESHU_CANONICAL_WRITE_TIMEOUT=2s`. The local host indexed the Eshu repo, MCP
`search_file_content` returned matching Go files from `postgres_content_store`,
and MCP `find_code` returned the `startManagedLocalGraph` function with
`truth.profile=local_authoritative` and `truth.basis=content_index`.
The same run intentionally showed canonical graph projection degrading with
`neo4j execute timed out after 2s: context deadline exceeded`, which proves the
local content-search path is isolated from NornicDB graph-write stalls. This did
not promote NornicDB at the time; it only proved the laptop coding workflow
remained usable while the backend was still under review.

Startup perf smoke on 2026-04-23 used an explicitly installed NornicDB binary at
`/tmp/eshu-bare-install-smoke/bin/nornicdb-headless`:

```bash
ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless \
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test ./cmd/eshu -run TestLocalAuthoritativeStartupEnvelope -count=1 -v
```

That run measured readiness at the owner-record plus ingester handoff with:

- cold start: `9.045253708s`
- warm restart: `490.996625ms`

This proves the current `local_authoritative` startup path meets the documented
startup envelope. It does not yet prove the broader laptop perf contract:
query latency, dead-code scan time, reducer throughput, and memory budgets
remain promotion gates.

Source audit on 2026-04-23 confirmed that NornicDB's actual `nornicdb serve`
Bolt path wires `DBQueryExecutor` with `BeginTransaction`,
`CommitTransaction`, and `RollbackTransaction`; targeted headless/no-local-LLM
NornicDB transaction tests passed with
`go test -tags 'noui nolocalllm' ./pkg/bolt ./pkg/txsession ...`. Eshu therefore
added a backend capability router instead of permanently hiding grouped writes.
The stricter Eshu sidecar probe uses the exact Neo4j-driver path Eshu uses. The
rebuilt linuxdynasty-fork binary at `/tmp/nornicdb-headless-eshu-rollback`
passed that probe after fixing transaction-wrapper reuse for recursive
`UNWIND ... MATCH ... MERGE` execution and database-scoped Bolt rollback.
Normal NornicDB runs stay sequential until the latest-main binary under
evaluation and the broader adapter matrix pass, while
`ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true` exposes grouped writes for adapter
conformance only.

## NornicDB Compatibility Workflow (MANDATORY)

When Eshu hits a NornicDB incompatibility (Cypher parse rejection, rollback
misbehavior, driver shape mismatch, missing procedure, etc.), the Eshu contributor
MUST follow this workflow before writing a ESHU-side workaround:

1. **Search the NornicDB source first.** The upstream NornicDB repository is
   checked out locally at:

   - `/Users/allen/os-repos/NornicDB/` — upstream reference
   - `/Users/allen/os-repos/NornicDB-eshu-bolt-rollback/` — ESHU-maintained fork
     for the grouped-write rollback conformance work

   Use `rg` or the Grep tool against those paths to confirm what the runtime
   actually supports, parses, or rejects. Read the source of truth before
   inferring behavior from test failures.

2. **Decide the patch surface.** Every NornicDB incompatibility resolves to
   exactly one of:

   - **NornicDB supports it already** (parser difference, missing workaround
     in Eshu). Fix: adjust Eshu's Cypher, driver call, or adapter to match
     what NornicDB's source-of-truth code accepts.
   - **NornicDB has a documented workaround** (procedure form, alternate
     syntax, different rollback API). Fix: route Eshu through the supported
     form behind the backend-dialect seam (`schemaDialect`,
     `canonicalExecutorForGraphBackend`, `buildCallChainCypher`, etc.). Do
     not branch query handlers on backend brand.
   - **NornicDB must be patched.** The Eshu path is correct and NornicDB is
     wrong. Open a branch in `NornicDB-eshu-bolt-rollback`, reproduce with a
     minimal test that mirrors the Eshu workload, land the fix, and pin the
     rebuilt binary through an explicit `--from` path or future accepted
     manifest policy until upstream absorbs the change.

3. **Record the decision.** Add the incompatibility, workaround route, and
   upstream patch status to:

   - this ADR's Feature evidence section (for adapter conformance claims)
   - the active Chunk 3.5 or Chunk 4 evidence row in
     `docs/docs/adrs/2026-04-20-embedded-local-backends-implementation-plan.md`
   - the 2026-04-23 grouped-write rollback fork precedent is the template:
     minimal repro in Eshu → minimal fix in NornicDB fork → rebuilt binary
     passes `TestNornicDBGroupedWriteRollbackConformance`.

4. **Prefer narrow seams.** Backend-dialect translation belongs in already
   narrow seams (schema DDL, canonical-write executor, call-chain/transitive
   Cypher builders). Do not widen handler, reducer, or MCP tool code with
   `if backend == nornicdb` branches. If a new seam is required, document
   it here before merging.

This workflow protects the capability-port boundary while giving contributors
a clear path to either ship a dialect rendering or patch upstream.

## Status Summary

This ADR is **Accepted with conditions** (2026-04-23). Eshu now treats NornicDB
as the default graph backend across all profiles while Neo4j remains the
explicit official alternative pending parity research. Acceptance is revocable:
if any condition regresses (rollback gate fails, signature policy slips, Chunk
5/5b matrix fails, or Neo4j support posture cannot be closed honestly),
acceptance reverts to Proposed and Eshu reopens the backend default decision.
