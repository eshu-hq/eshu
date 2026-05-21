# Docs Change Tally

This file tracks the documentation cleanup PR at a human-reviewable level.
The full file-level index is split out so each Markdown file stays below the
repo's 500-line limit.

## Current Snapshot

- Total Markdown files left in the checkout after the current pass: 555
- Current branch doc status from
  `git diff --cached --name-status origin/main -- '*.md'` after the current
  pass: 188 created, 272 modified, 117 deleted
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
`git diff --cached --name-status origin/main -- '*.md'` after staging each
cleanup pass.

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

## Verification Snapshot

- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 569 documents, 1,751 claims, 0 contradicted, and 0 missing
  evidence claims after the telemetry package README rewrite.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 181 documents, 1,302 claims, 0 contradicted, and 0 missing
  evidence claims after the telemetry package README rewrite.
- `go run ./cmd/eshu docs verify ../go/internal/telemetry --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 2 documents, 0 claims, 0 contradicted, and 0 missing evidence
  claims after the telemetry package README rewrite.
- `go test ./internal/telemetry -count=1`, `go test ./cmd/eshu -count=1`,
  `git diff --check`, and `cmp -s AGENTS.md CLAUDE.md` passed after the
  telemetry package README rewrite.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after the telemetry package README rewrite.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 569 documents, 1,750 claims, 0 contradicted, and 0 missing
  evidence claims after the Terraform-state collector service split.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 181 documents, 1,302 claims, 0 contradicted, and 0 missing
  evidence claims after the Terraform-state collector service split.
- `go run ./cmd/eshu docs verify ../docs/public/services --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 9 documents, 69 claims, 0 contradicted, and 0 missing evidence
  claims after the Terraform-state collector service split.
- `go run ./cmd/eshu docs verify ../docs/public/reference/environment-collectors.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 69 claims, 0 contradicted, and 0 missing evidence
  claims after adding `ESHU_TERRAFORM_SCHEMA_DIR`.
- `go test ./cmd/collector-terraform-state ./internal/collector/terraformstate ./internal/collector/tfstateruntime -count=1`,
  `go test ./cmd/eshu -count=1`, `git diff --check`, and
  `cmp -s AGENTS.md CLAUDE.md` passed after the Terraform-state collector
  service split. `scripts/verify-package-docs.sh` reported no changed Go
  package source files.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after adding the Terraform-state collector split pages to navigation.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 567 documents, 1,746 claims, 0 contradicted, and 0 missing
  evidence claims after the AWS cloud collector service split.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 179 documents, 1,298 claims, 0 contradicted, and 0 missing
  evidence claims after the AWS cloud collector service split.
- `go run ./cmd/eshu docs verify ../docs/public/services --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 7 documents, 66 claims, 0 contradicted, and 0 missing evidence
  claims after the AWS cloud collector service split.
- `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/... -count=1`,
  `go test ./cmd/eshu -count=1`, `scripts/verify-package-docs.sh`,
  `git diff --check`, and `cmp -s AGENTS.md CLAUDE.md` passed after the AWS
  cloud collector service split.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after adding the AWS cloud collector split pages to navigation.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 565 documents, 1,751 claims, 0 contradicted, and 0 missing
  evidence claims after the Cypher package README rewrite.
- `go run ./cmd/eshu docs verify ../go/internal/storage/cypher --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 2 documents, 1 claim, 0 contradicted, and 0 missing evidence
  claims after the Cypher package README rewrite.
- `go test ./internal/storage/cypher -count=1`, `go test ./cmd/eshu -count=1`,
  `git diff --check`, and `cmp -s AGENTS.md CLAUDE.md` passed after the Cypher
  package README rewrite.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 565 documents, 1,748 claims, 0 contradicted, and 0 missing
  evidence claims after the Helm values split.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 177 documents, 1,303 claims, 0 contradicted, and 0 missing
  evidence claims after the Helm values split.
- `go run ./cmd/eshu docs verify ../docs/public/deploy/kubernetes --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 12 documents, 27 claims, 0 contradicted, and 0 missing evidence
  claims after the Helm values split.
- `helm template eshu ./deploy/helm/eshu` and
  `helm lint ./deploy/helm/eshu` passed after the Helm values split. Helm lint
  reported only the chart-icon recommendation.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after adding the split Helm values pages to navigation.
- `go test ./cmd/eshu -count=1`, `git diff --check`, and
  `cmp -s AGENTS.md CLAUDE.md` passed after the Helm values split.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 562 documents, 1,748 claims, 0 contradicted, and 0 missing
  evidence claims after rebasing onto `a0d676f`.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 174 documents, 1,299 claims, 0 contradicted, and 0 missing
  evidence claims after rebasing onto `a0d676f`.
- `go run ./cmd/eshu docs verify ../docs/public/reference --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 74 documents, 1,049 claims, 0 contradicted, and 0 missing
  evidence claims after rebasing onto `a0d676f`.
- `go test ./cmd/eshu -count=1`
  passed after teaching the docs verifier to read split `environment-*.md`
  reference pages and after rebasing onto `a0d676f`.
- `go run ./cmd/eshu docs verify ../docs/public/reference/telemetry --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 10 documents, 2 claims, 0 contradicted, and 0 missing evidence
  claims after correcting the status CLI reference to `eshu-admin-status`.
- `go run ./cmd/eshu docs verify ../docs/public/deployment --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 8 documents, 33 claims, 0 contradicted, and 0 missing evidence
  claims after the service runtime split.
- `go run ./cmd/eshu docs verify ../docs/public/architecture.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 1 claim, 0 contradicted, and 0 missing evidence
  claims after the architecture rewrite.
- Focused verifier tests passed for package docs, collector authoring, and
  repository documentation ownership after the scoped `AGENTS.md` restore.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after adding the environment-variable split pages to navigation and
  repairing the Docker Compose route-map link, then passed again after
  rebasing onto `a0d676f`.
- `git diff --check` passed after rebasing onto `a0d676f`.
- `cmp -s AGENTS.md CLAUDE.md` passed after the environment-variable split.
  It passed again after rebasing onto `a0d676f`.

## What Is Left

- Continue reviewing oversized public and package docs. The current largest
  real documentation files are
  `docs/public/reference/capability-conformance-spec.md`,
  `docs/public/reference/telemetry/logs.md`, and
  `docs/public/reference/mcp-cookbook.md`. The larger
  `tests/fixtures/sample_projects/sample_project_typescript/README.md` fixture
  remains test data, not a public documentation target.
- Keep deleting historical planning notes when current public or package-local
  docs already carry the useful invariant.
- Keep folding durable lessons into current architecture, workflow,
  performance, backend, MCP, collector, and package-local docs.
