# Docs Change Tally

This file tracks the documentation cleanup PR at a human-reviewable level.
The full file-level index is split out so each Markdown file stays below the
repo's 500-line limit.

## Current Snapshot

- Total Markdown files left in the checkout after the current pass: 548
- Current branch doc status from
  `git diff --name-status origin/main -- '*.md'` after the current pass:
  95 added, 162 modified, 148 deleted, 86 renamed
- Copied image assets removed from this branch: 43 files under
  `docs/public/images/`. They were reference assets from another project and
  no longer appear in the source-doc reference scan.
- Stable public docs surface: `docs/public/`
- Maintainer-only docs surface: `docs/internal/`
- Deleted stable-doc history surfaces: `docs/plans/`, `docs/superpowers/`,
  and old ADR trees
- Remaining `AGENTS.md` files: 152 total; root plus scoped package/command
  files remain harness-loaded guidance.

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
| MCP Cookbook Rewrite | Reduced the MCP cookbook into copy-ready current recipes, removed invalid arguments from deployment and call-chain examples, and corrected the MCP package README to the current 71-tool contract. |
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

## Verification Snapshot

Detailed historical verification moved to `docs/internal/docs-verification-snapshot.md` so this tally stays under the repo file-size limit.

Current pass proof:

- Focused docs verification passed for local-testing subpages, public reference pages, service runbooks, command READMEs, and scoped agent guidance with 0 contradicted and 0 missing evidence claims.
- Focused Go tests passed for query, MCP, API, CLI, backend conformance, runtime, status, truth, collector commands, Postgres storage, parser, and collector packages.
- Local script checks passed for remote E2E runtime-state validation and relevant local-testing shell syntax.
- Broad docs verification passed for `docs/public` and the full repository with 0 contradicted and 0 missing evidence claims.
- `scripts/verify-package-docs.sh`, `git diff --check`, `cmp -s AGENTS.md CLAUDE.md`, and strict MkDocs build passed.

## What Is Left

- Continue reviewing docs by topic instead of by single file. Remaining
  high-value groups are now the long-tail package READMEs, fixture docs that
  are true test data, generated/reference indexes, and any public pages still
  duplicating package-local contracts. The larger
  `tests/fixtures/sample_projects/sample_project_typescript/README.md` fixture
  remains test data, not a public documentation target.
- Keep deleting historical planning notes when current public or package-local
  docs already carry the useful invariant.
- Keep folding durable lessons into current architecture, workflow,
  performance, backend, MCP, collector, and package-local docs.
