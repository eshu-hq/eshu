# Docs Change Tally

This file tracks the documentation cleanup PR at a human-reviewable level.
The full file-level index is split out so each Markdown file stays below the
repo's 500-line limit.

## Current Snapshot

- Total Markdown files left in the checkout after the current pass: 548
- Current branch doc status from
  `git diff --name-status origin/main -- '*.md'` after the current pass:
  104 added, 317 modified, 157 deleted, 77 renamed
- Copied image assets removed from this branch: 43 files under
  `docs/public/images/`. They were reference assets from another project and
  no longer appear in the source-doc reference scan.
- Stable public docs surface: `docs/public/`
- Maintainer-only docs surface: `docs/internal/`
- Deleted stable-doc history surfaces: `docs/plans/`, `docs/superpowers/`,
  and old ADR trees
- Remaining `AGENTS.md` files: 152 total; root plus scoped package/command
  files remain harness-loaded guidance.
  Current total scoped-agent content is 6,613 lines, with root `AGENTS.md`
  preserved as mandatory harness guidance.

## File Indexes

- Created documentation files: `docs/internal/docs-file-index-created.md`
- Modified documentation files: `docs/internal/docs-file-index-modified.md`
- Deleted documentation files: `docs/internal/docs-file-index-deleted.md`

The generated file indexes preserve the per-file tally requested for this
branch. Regenerate them from
`git diff --name-status origin/main -- '*.md'` after each cleanup pass.

## Pass Ledger

| Pass | Main action |
| --- | --- |
| Root Console Docs And Internal Working Notes | Moved durable console product/design contracts into the console README and removed stale root/internal notes. |
| Go Command Docker Compose Links | Repointed command and governance docs to the current Docker Compose run-local page. |
| Query And MCP Package Docs | Reduced query package docs and `doc.go` to current HTTP/MCP contracts. |
| Reducer Package Docs | Reduced reducer package README and `doc.go` to current reducer ownership. |
| Collector Package Docs | Reduced collector package README and `doc.go` to current source-observation contracts. |
| Terraform-State And Package-Registry Collector Docs | Rewrote collector docs around current state, registry, redaction, and package identity contracts. |
| AWS Cloud Shared Collector Docs | Updated AWS shared collector package docs and current scanner boundaries. |
| AWS Cloud Service README Evidence Cleanup | Collapsed AWS service/SDK leaf guidance into service READMEs. |
| OCI Registry Collector Docs | Rewrote OCI registry collector docs around provider boundaries and digest identity. |
| Parser And Terraform-State Backend Relationship Docs | Repaired parser and Terraform-state backend relationship docs. |
| Vulnerability Intelligence And CI/CD Run Collector Docs | Updated vulnerability and CI/CD collector docs against current code. |
| Confluence And Discovery Collector Docs | Updated Confluence and discovery docs against current code. |
| Parser Leaf AGENTS Cleanup | Deleted parser leaf `AGENTS.md` files after confirming README coverage. |
| AWS Service Leaf AGENTS Cleanup | Deleted AWS service and SDK leaf `AGENTS.md` files after README coverage. |
| OCI Provider Leaf AGENTS Cleanup | Deleted OCI provider/client leaf `AGENTS.md` files after README coverage. |
| Collector Leaf AGENTS Cleanup | Deleted collector leaf `AGENTS.md` files and updated parent collector/storage guidance. |
| Correlation Leaf AGENTS Cleanup | Deleted correlation child `AGENTS.md` files after README coverage. |
| Reducer, Relationships, Content, And Command AGENTS Cleanup | Deleted reducer child, relationships child, content child, and command-local `AGENTS.md` files. |
| Utility Package AGENTS Cleanup | Deleted utility package `AGENTS.md` files after README coverage. |
| Contract Package AGENTS Cleanup | Deleted contract package `AGENTS.md` files after README coverage. |
| Orchestration Package AGENTS Cleanup | Deleted app/coordinator/workflow/eshulocal `AGENTS.md` files after README coverage. |
| Subsystem AGENTS Consolidation | Moved relationship/status warnings into READMEs and deleted subsystem agent files. |
| Core Workflow AGENTS Consolidation | Moved correlation/projector/telemetry checklists into READMEs and deleted agent files. |
| Runtime And Cypher AGENTS Consolidation | Moved runtime and Cypher writer checklists into READMEs and deleted agent files. |
| Parser Collector Reducer AGENTS Consolidation | Moved parser/collector/reducer change checklists into READMEs and deleted agent files. |
| AWS Collector AGENTS Consolidation | Moved AWS collector service-change checklist into the README and deleted the agent file. |
| Postgres Storage Docs Split | Split Postgres storage docs into a short README plus `change-guide.md`; deleted the agent file. |
| Query And MCP AGENTS Consolidation | Moved query and MCP workflow rules into READMEs and deleted the last package-local agent files. |
| Tally File Split | Split the oversized tally into this summary plus generated created/modified/deleted file indexes. |
| HTTP API Reference Split | Split the oversized HTTP API reference into a short route map plus focused status, evidence, context, code, IaC/content/infra, and repository reference pages. |
| Local Testing Reference Split | Split the oversized local testing reference into a short verification map plus focused remote E2E, live-smoke, gate, discovery, and profiling pages. |
| Scoped AGENTS Restore | Restored scoped Go `AGENTS.md` files after verifying Codex treats nested `AGENTS.md` as harness-loaded scoped instructions. Root `AGENTS.md` / `CLAUDE.md` now state that package docs are dual-audience: README for humans, `doc.go` for godoc, and `AGENTS.md` for agents. |
| Main Rebase Refresh | Rebasing onto `origin/main` at `d80558e` kept Confluence multi-space, hosted E2E readiness, and index-status changes while preserving the docs information architecture cleanup. |
| Telemetry Reference Split | Split the oversized telemetry overview and metrics catalog into a route map plus focused runtime, collector, reducer/storage, shared-write, and streaming-memory pages grounded in `go/internal/telemetry`. |
| Relationship Mapping Reference Split | Split the oversized relationship-mapping page into a short route map plus evidence/resolution and runtime/story pages; corrected resolver behavior so docs no longer claim typed edges globally suppress generic `DEPENDS_ON`. |
| Service Runtimes Split | Split the oversized service-runtimes page into a short route map plus focused core-service, collector-service, and bootstrap-service pages; added Confluence and Terraform-state collector runtimes to the matrix and removed stale `chart/templates/...` references. |
| Architecture Page Rewrite | Reduced the architecture page to the current system shape, diagrams, runtime boundaries, package ownership, and links to canonical runtime/backend/profile references; repaired the fact-envelope architecture anchor. |
| NornicDB Tuning Split | Reduced the NornicDB tuning page to current knobs and decision rules; moved durable performance checkpoints into a focused evidence page. |
| Dead Code Reachability Split | Reduced the dead-code reachability spec from historical planning plus language inventory into the current runtime contract; moved per-language maturity into a focused reference page. |
| Environment Variables Split | Reduced the environment-variable reference to a route map plus focused runtime/storage, ingestion/queue, collector, and compose/test pages; updated the docs verifier so split environment reference pages seed `ESHU_*` truth. |
| Main Rebase Refresh For Draft PR | Rebasing onto `origin/main` at `a0d676f` kept collected documentation facts and hosted E2E graph-write hardening by porting durable updates into the new public docs surface. |
| Helm Values Split | Reduced the oversized Helm values page to a route map, split runtime/bootstrap, collector/webhook, and routing/storage values into focused pages, and trimmed the chart README so it points to the public operator docs instead of duplicating them. |
| Cypher Package README Rewrite | Reduced the Cypher storage README from a historical evidence dump into the current package guide; corrected the canonical phase list to include `package_registry` and aligned package comments with current package-registry writes. |
| AWS Cloud Collector Service Split | Reduced the AWS cloud collector public service doc into an overview/runbook plus focused security/config and scanner coverage pages grounded in command/runtime code. |
| Terraform-State Collector Service Split | Reduced the Terraform-state collector public service doc into an overview plus focused config/discovery and operations/troubleshooting pages grounded in command, parser, runtime, status, and telemetry code. |
| Telemetry Package README Rewrite | Reduced the telemetry package README from a duplicated metric/span/log catalog into the maintainer contract for package ownership, startup wiring, frozen registries, and change rules; updated scoped agent guidance to point at current public telemetry docs. |
| Capability Conformance Spec Rewrite | Reduced the capability conformance page from a stale copied capability list into the current contract guide for YAML source of truth, runtime profiles, truth ceilings, backend conformance, validators, and change policy. |
| Telemetry Logs And Correlation Rewrite | Replaced stale universal log-event guidance with the current Go structured log contract, corrected service names and cross-service correlation guidance, and removed old event families from operator recipes. |
| MCP Cookbook Rewrite | Reduced the MCP cookbook into copy-ready current recipes, removed invalid arguments from deployment and call-chain examples, and corrected the MCP package README to the then-current tool contract. |
| Documentation Updater Actuator Contract Rewrite | Reduced the updater actuator contract from stale future-planning prose into the current read-only documentation findings, facts, evidence-packet, freshness, permission, and error contract grounded in query/MCP code. |
| Bootstrap Index Docs And Copied Image Cleanup | Reduced the bootstrap-index package README into the current one-shot runtime contract, corrected scoped agent guidance and public service wording, and removed copied image assets that did not belong to Eshu docs. |
| Helm Collector And Webhook Values Rewrite | Reduced the collector/webhook Helm values page from example-heavy provider snippets into the current operator map for coordinator, direct collectors, claim-driven collectors, webhook routing, shared workload settings, and render guardrails grounded in chart values and templates. |
| Configuration Reference Rewrite | Reduced the configuration page from a duplicated environment catalog into a route map for `eshu config`, environment references, local binaries, graph backend install, project-local discovery, and workspace/recovery references grounded in CLI config code. |
| CLI Reference Rewrite | Reduced the CLI reference into a code-grounded command matrix; corrected root flags, scan flags, API-backed query/workspace behavior, remote flag handling, component flags, deprecated `start`, the unusable `w` shortcut, and service binary version probes. |
| Service Runtime Workflow Rewrite | Reduced the service workflow page into current ingestion, reducer, query, bootstrap/recovery, and collector-control flows; corrected source-local projector ownership, backend-neutral query wording, runtime matrix scope, ingester worker defaults, reducer shared-projection defaults, reducer retry attempts, and API key knobs. |
| Compose Helm And Local Binary Docs Rewrite | Corrected Compose service/metrics port tables, Neo4j stack scope, local binary/MCP ownership, AWS freshness webhook coverage, Helm render examples, and the Helm quickstart collector flow. |
| Fact Contract Rewrite | Replaced duplicated fact/plugin prose with a concise fact-envelope contract and a separate schema-versioning policy grounded in `go/internal/facts` and `go/internal/component`. |
| Collector Reducer Readiness Rewrite | Reduced the collector/reducer readiness page to current implemented lanes, claim-driven coordinator requirements, reducer truth boundaries, and proof gates. |
| Package README Compression | Reduced projector, runtime, ingester, workflow, query, and coordinator package READMEs to package ownership, invariants, telemetry, dependencies, and focused tests. |
| Public Reference Polish | Reduced telemetry trace/correlation, truth-label, MCP cookbook, documentation updater, local performance, and NornicDB pitfalls references while preserving current contracts. |
| Legacy Stub Deletion | Removed legacy getting-started, deployment, and Neo4j setup stubs after repointing backlinks to current run-local and Kubernetes docs. |
| Cypher Storage Guide Compression | Reduced the Cypher storage README from 487 to 254 lines, kept the hot-path evidence markers, and corrected scoped agent guidance for the current `package_registry` phase. |
| Postgres Scoped AGENTS Compression | Reduced the Postgres scoped agent guidance from 333 to 185 lines while preserving mandatory queue, lease, fencing, drift, status, and concurrency guardrails. |
| Docker Compose Run-Local Compression | Reduced the Compose run-local page from 308 to 280 lines by keeping the service/file map and linking remote E2E proof details to the focused local-testing references. |
| Parallel Remaining Docs Compression | Used subagents plus parent review to compress the remaining high-priority public, MCP, CLI, API, collector, reducer, why, local-data-root, and agent-guide docs while preserving code-verified contracts. |
| Terraform, Helm, Backend, And Package Docs Compression | Compressed Terraform provider guides, Helm values references, graph backend operations/install, Cypher performance discipline, bootstrap-index, observability, and core package READMEs while preserving code-verified contracts. |
| Local Testing, Public Reference, Service Runbook, And Scoped AGENTS Compression | Used subagents plus parent review to compress local-testing subpages, public reference contracts, service/command runbooks, and scoped agent guidance while preserving mandatory accuracy, performance, concurrency, telemetry, and proof rules. |
| Scoped AGENTS Link Repair And Fixture README Compression | Replaced deleted `docs/docs`, `docs/plans`, and `docs/superpowers` references in scoped agent guidance with current public/package docs; compressed Go, Rust, and TypeScript sample fixture READMEs to fixture intent and file maps; refreshed the docs inventory counts. |
| Main Rebase And Image-Claim Verifier Repair | Rebasing onto `origin/main` at `73e1930` kept the current 72-tool MCP contract and container-image identity docs, then tightened docs verification so colon-shaped identifiers are not misclassified as container image refs. |
| Verification Snapshot Compression | Reduced the internal verification snapshot from a historical command log into the current acceptance gates, proof coverage, and guidance for where durable evidence belongs. |
| Fixture README Compression | Reduced Terraform-state drift, Tier-2 drift, JavaScript sample, and dead-code fixture READMEs to assertion-focused fixture contracts while preserving verifier-backed scenario coverage. |
| Package README Long-Tail Compression | Reduced 15 command/internal package READMEs from 2,657 to 1,014 lines, keeping ownership, invariants, telemetry, focused tests, and links to public contracts while removing duplicated package maps and historical dumps. |
| Public Docs Long-Tail Compression | Reduced Compose, architecture, telemetry trace/log, and Helm values pages by removing repeated examples and symbol dumps while keeping operator contracts and links to focused references. |
| Sample Fixture README Compression | Rewrote C, C#, Java, PHP, and Swift sample-project READMEs as fixture contracts instead of runnable app guides, correcting stale paths and filenames. |
| Public Reference Follow-Up Compression | Reduced CLI indexing, NornicDB tuning evidence, and core service runtime pages while preserving distinct MCP cookbook, CLI reference, and tuning reader jobs. |
| Scoped AGENTS Current-Docs Repair | Replaced stale ADR phrasing in scoped agent guidance with current-doc and architecture-owner language while preserving mandatory accuracy, performance, concurrency, telemetry, and proof rules. |
| Package README Follow-Up Compression | Reduced 15 more command/internal package READMEs from 2,014 to 1,115 lines while preserving package ownership, invariants, telemetry, focused tests, and current public-doc links. |
| Internal Docs Update Guide Repair | Tightened the maintainer docs update guide around current public/internal/package surfaces, focused docs verification, package-doc gates, and strict MkDocs proof. |
| Scoped AGENTS ADR Cleanup Batch A | Replaced stale ADR gates in 15 scoped agent files with architecture-owner approval language while preserving mandatory local invariants and proof requirements. |
| Public Reference Duplicate Cleanup | Collapsed duplicated HTTP envelope/truth tables, removed repeated component-package YAML from the fact envelope reference, and corrected component package manager state wording. |
| Package README Mid-Size Compression | Reduced 15 more command/internal package READMEs while preserving package ownership, invariants, telemetry, focused tests, and links to canonical public docs. |
| Troubleshooting Reference Compression | Replaced the stale troubleshooting reference with a compact symptom map that points to current local binaries, Compose, backend, workflow, and telemetry docs. |
| Scoped AGENTS ADR Cleanup Batch B | Replaced stale ADR/current-tracker wording in 15 more scoped agent files with architecture-owner approval, migration, proof, telemetry, and package-doc requirements. |
| Public Reference Duplicate Cleanup Batch B | Removed duplicate raw-query and backend-default detail from the MCP cookbook and NornicDB tuning page while preserving larger reference pages with distinct reader jobs. |
| Package README Mid-Size Compression Batch B | Reduced 15 more package READMEs, keeping ownership, invariants, telemetry, focused tests, and current public-doc links while removing diagrams and duplicated catalogs. |
| Local Performance Envelope Compression | Reduced the local performance reference to profile targets, dogfood tiers, evidence rules, gate commands, and open evidence without lowering the performance bar. |
| Scoped AGENTS ADR Cleanup Batch C | Replaced deleted-ADR references in command, backend, collector, and OCI scoped agent files with current-doc, test, backend-conformance, and architecture-owner proof requirements. |
| Public Run And CLI Analysis Compression | Reduced Docker Compose and CLI analysis references by removing repeated proof commands, delegating runbook detail to focused pages, and correcting the call-chain API route. |
| Package README Mid-Size Compression Batch C | Reduced 15 more package READMEs, preserving ownership, invariants, telemetry, focused tests, and package-specific correctness notes. |
| Main Rebase Refresh At 48aae51 | Rebasing onto current `origin/main` preserved new package-registry query/schema truth while keeping the compressed public and package-doc structure. |
| MCP Diagnostic Cypher Contract Restore | Restored the diagnostics-only raw Cypher cookbook section with scoped input and tool-level `limit` so MCP contract tests keep raw queries out of normal prompt flows. |
| AWS SDK Adapter README Compression | Reduced eight AWS SDK adapter READMEs by removing repeated diagrams and shared dependency/telemetry prose while preserving each service's API allowlist, denylist, pagination, and redaction invariants. |
| AWS Service Scanner README Compression | Reduced eight AWS service scanner READMEs by removing repeated scanner/client diagrams and shared dependency/telemetry prose while preserving metadata-only, redaction, relationship-evidence, and no-inference invariants. |
| Public Runtime And Python Docs Compression | Corrected deployed runtime binary names, expanded the local installer output from the real install script, and reduced the Python parser page from a duplicated test inventory into a current parser/dead-code contract. |
| Parallel MCP Backend Telemetry Compression | Used subagents to compress MCP/CLI, graph-backend/NornicDB, and telemetry reference groups while preserving diagnostics-only raw Cypher, schema-first backend evidence, NornicDB tuning gates, exact service names, metric/span/log contracts, and bounded-label rules. |
| Parallel Deployment Collector Reference Compression | Used subagents plus parent review to compress Compose, Helm, service-runtime, collector, reducer, fact-envelope, component-package, language-query, tag-taxonomy, local testing, and internal agent-guide docs while correcting Helm/API/MCP command truth against templates and service binaries. |
| Templated IaC Fixture Contract Cleanup | Rewrote the templated IaC fixture README as a fixture contract and removed private/local source provenance from the README and manifest metadata. |
| CLI, Local Data, Ignore, And Correlation Fixture Repair | Corrected `eshu docs verify` CLI flag truth, local data-root reset behavior, `.eshuignore` matching/default-skip behavior, and the correlation DSL secondary-Dockerfile fixture contract. Restored the missing `Dockerfile.test` fixture required by the compose verifier. |
| Main Rebase Refresh At e6ac80a | Rebasing onto current `origin/main` kept the public docs information architecture, deleted stale `docs/docs` HTTP API history, and preserved the current 73-tool MCP contract plus service-catalog fact family truth in compressed package docs. |
| Eshuignore And Local Data Root Compression | Reduced `.eshuignore` and local data-root references to current operator contracts, removed long default-skip and recovery catalogs, and added focused code-backed verification commands. |
| Dead-Code Fixture Maturity Repair | Corrected dead-code fixture maturity docs against the query package maturity map so Haskell, Java, Kotlin, Rust, and Scala are active `derived` fixtures while Groovy remains `derived_candidate_only`. |
| Sample Fixture Contract Compression | Reduced sample-project READMEs to fixture contracts and removed tutorial/build-command prose that duplicated parser and query test ownership. |
| Architecture And Compose Duplicate Cleanup | Corrected CLI read-path diagrams, collapsed duplicated runtime-boundary prose, and reduced Compose service and endpoint inventories to concise operator contracts. |
| CLI And MCP Cookbook Duplicate Cleanup | Reduced the CLI reference to a command-family index and trimmed the MCP cookbook to copy-ready workflows while keeping diagnostics-only Cypher guidance. |
| Product Truth Fixture Contract Compression | Reduced product-truth, dead-IaC, and correlation DSL fixture READMEs to registry, verifier, expected-truth, and stable fixture-role contracts. |
| Main Rebase Refresh At 4777a92 | Rebasing onto current `origin/main` preserved service-catalog, container-image, package-registry, and compressed public/package docs while refreshing the verification counts after conflict resolution. |
| Public Reference Operator Compression | Reduced collector/reducer readiness, service workflows, local data-root, and Cypher performance pages by removing copied proof logs and package-local detail while preserving operator gates and current maintainer handoffs. |
| Fixture Test-Data Compression | Reduced TypeScript, dead-code, and Terraform-state fixture READMEs to fixture intent, stable file maps, and expected truth instead of tutorial or historical prose. |
| Scoped AGENTS ADR Language Cleanup | Replaced stale package-local ADR gate wording with architecture-owner approval language while preserving mandatory package-specific guardrails. |
| Generated Index And Inventory Refresh | Regenerated the modified-file index, marked deleted-plan reference repair complete, and refreshed branch-wide changed-doc counts. |
| Core Scoped AGENTS Compression | Reduced telemetry, runtime, Cypher storage, reducer, projector, MCP, and status agent guidance from 1,590 to 465 lines while preserving mandatory accuracy, performance, concurrency, telemetry, and proof guardrails. |
| Parser Scoped AGENTS Compression | Reduced parser scoped agent guidance from 586 to 365 lines while preserving deterministic output, runtime reuse, package-boundary, payload-shape, SCIP, and proof rules. |
| Collector Scoped AGENTS Compression | Reduced collector scoped agent guidance from 749 to 492 lines while preserving source-evidence, claim fencing, memory, discovery, telemetry, redaction, and performance proof rules. |
| Parallel Command Workflow Storage Docs Compression | Used five subagents plus parent review to compress command, workflow, correlation, graph, storage, IaC, public runbook, and package README docs. This pass changed 60 Markdown files with 2,090 insertions and 4,127 deletions while preserving root `AGENTS.md` and package-specific mandatory rules. |
| Parallel Front Door Parser Collector Docs Compression | Used five subagents plus parent review to compress root/developer/testing docs, repo/deploy READMEs, parser/collector/content scoped agent guidance, and public Terraform/logging/dead-code/E2E references. This pass changed 67 Markdown files with 1,364 insertions and 2,130 deletions while preserving root `AGENTS.md` and correcting Terraform provider schema totals to 21 providers and 6,236 resource types. |

## Verification Snapshot

Detailed historical verification moved to `docs/internal/docs-verification-snapshot.md` so this tally stays under the repo file-size limit.

Current pass proof:

- Rebase conflict resolution preserved the current 73-tool MCP contract,
  container-image identity read surface, and compressed public/package docs.
- Focused docs verification passed for `docs/public`, `go/internal`, `go/cmd`,
  and `tests/fixtures`.
- Focused fixture verification passed for `tests/fixtures` after the templated
  IaC fixture contract cleanup: 40 documents, 2 claims, 0 contradicted, and 0
  missing evidence claims.
- Focused CLI, local-data, ignore, MCP cookbook, and correlation fixture
  verification passed for the current pass with 0 contradicted and 0 missing
  evidence claims. Focused query and reducer tests passed for the correlation
  DSL fixture and secondary-Dockerfile rejection contract.
- Broad docs verification passed for `docs/public` and the full repository with
  0 contradicted and 0 missing evidence claims. Current public docs verifier
  result: 173 documents, 1167 claims, 11 unsupported shell-command claim
  types. Current full repository verifier result: 562 documents, 1303 claims,
  15 unsupported shell-command claim types.
- Current Go docs verifier result after parallel scoped-agent and README
  compression: 309 documents, 103 claims, 0 contradicted, 0 missing evidence,
  and 3 unsupported shell-command claim types.
- Current full repository docs verifier result after front-door/parser/collector
  compression: 562 documents, 1298 claims, 0 contradicted, 0 missing evidence,
  and 15 unsupported shell-command claim types.
- Current strict docs build passed:
  `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`.
- Focused public reference verification passed after the operator-reference
  compression: 74 documents, 907 claims, 0 contradicted, and 0 missing evidence
  claims.
- Focused fixture verification passed after the fixture test-data compression:
  40 documents, 2 claims, 0 contradicted, and 0 missing evidence claims.
- Focused Go docs verification passed after the scoped `AGENTS.md` ADR-language
  cleanup: 309 documents, 169 claims, 0 contradicted, and 0 missing evidence
  claims.
- Focused Go docs verification passed after scoped agent compression:
  `go/internal/parser` had 54 documents and 0 contradicted or missing evidence
  claims, `go/internal/collector` had 122 documents and 0 contradicted or
  missing evidence claims, `go/cmd/collector-aws-cloud` had 2 documents and 10
  valid claims, and the broad `go` docs verifier had 309 documents, 162 claims,
  0 contradicted, and 0 missing evidence claims.
- Focused service, collector, Terraform-state, reducer, fact, component,
  relationship, tag, and language-query tests passed for the current pass.
- Focused `.eshuignore` and local data-root verification passed for discovery,
  collector selection, `eshulocal`, `cmd/eshu`, and docs verifier gates.
- Focused dead-code fixture verification passed for the fixture README set and
  query maturity/root tests, with Groovy preserved as candidate-only.
- Focused sample-project fixture docs verification passed after compressing the
  README set to fixture contracts.
- Focused architecture and Docker Compose docs verification plus runtime Compose
  tests passed after correcting CLI read-path and service responsibility docs.
- Focused CLI command registration, MCP contract tests, and per-page docs
  verification passed after the CLI/MCP cookbook cleanup.
- Product-truth static registry verification, focused query/IaC reachability
  tests, and fixture docs verification passed after product-truth and
  correlation fixture compression.
- `scripts/verify-package-docs.sh`, `helm lint`, `helm template`, Markdown
  file-size scan, `git diff --check`, `cmp -s AGENTS.md CLAUDE.md`, and strict
  MkDocs build passed.

## What Is Left

- Continue reviewing docs by topic instead of by single file. Remaining
  high-value groups are now the long-tail scoped `AGENTS.md` files outside
  this compressed command/workflow/storage slice, the largest public reference
  pages that still read like inventories, and any remaining package README that
  duplicates public contracts.
- Next scoped-agent candidates by line count include content, content/shape,
  collector-git, buildinfo, contentrefs, repositoryidentity, parser SQL, and
  remaining collector/provider leaf packages.
  The latest pass completed those named candidates; the next candidates by
  line count are parent subsystem guidance and AWS service leaf guidance:
  reducer/dsl, storage/cypher, reducer, collector, reducer/tags, projector,
  vulnerability-intelligence collector, storage/postgres, runtime, AWS cloud,
  status, reducer/tfstate, parser, reducer/aws, coordinator, workflow,
  telemetry, and AWS service/SDK leaves.
- Keep deleting historical planning notes when current public or package-local
  docs already carry the useful invariant.
- Keep folding durable lessons into current architecture, workflow,
  performance, backend, MCP, collector, and package-local docs.
