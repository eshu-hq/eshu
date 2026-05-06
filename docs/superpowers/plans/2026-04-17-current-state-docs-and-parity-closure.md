# Current-State Documentation And Parity Closure Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace migration-era public documentation with current-state Go platform documentation, then close and prove the remaining parity-for-parity gaps required for truthful cutover.

**Architecture:** Public docs under `docs/docs/` describe only the rewritten Go platform. Migration records, parity signoff artifacts, and stale bridge language are removed from the public doc set. Every public capability claim must map to current Go code plus current Go or integration proof in this branch. Remaining parity gaps are tracked as engineering work, not softened by doc wording.

**Tech Stack:** MkDocs, Go runtimes in `go/cmd/*`, Go domain packages in `go/internal/*`, Docker Compose, Helm, OpenAPI, OTEL telemetry, Neo4j, PostgreSQL

---

## Decision Summary

This plan replaces the old public migration narrative with a current-state documentation contract:

1. `docs/docs/` must describe only the live Go platform.
2. Public docs must not describe a migration, cutover, bridge, dual path, or historical Python ownership.
3. Public docs may claim support only when the claim is backed by current Go code and current proof in this branch.
4. If parity is not actually closed, the gap belongs in this plan and in implementation work, not in softened public wording.
5. Historical signoff pages, closure matrices, and public ADR records are deleted or folded into evergreen architecture and operator docs.

## Current Truth From The Audit

These areas are already real in Go and should be documented as current-state behavior:

- Go runtime and service ownership in `go/cmd/api`, `go/cmd/ingester`, `go/cmd/projector`, `go/cmd/reducer`, `go/cmd/bootstrap-index`, `go/cmd/mcp-server`, `go/cmd/admin-status`
- Shared runtime, status, and telemetry ownership in `go/internal/runtime`, `go/internal/status`, `go/internal/telemetry`
- Native parser ownership in `go/internal/parser`
- Collector ownership in `go/internal/collector`
- Relationship mapping ownership in `go/internal/relationships`
- Projector and reducer ownership in `go/internal/projector` and `go/internal/reducer`
- Query and admin/OpenAPI ownership in `go/internal/query`
- Neo4j and Postgres ownership in `go/internal/storage/neo4j` and `go/internal/storage/postgres`
- Terraform provider schema runtime ownership in `go/internal/terraformschema`

These doc issues are still present and must be corrected:

- public migration/signoff pages still exist
- Python parser labels still appear in some language and matrix pages even when the implementation and proof are Go-backed
- local testing docs are still too Python-first for a Go-owned platform
- some public pages still contain migration-era wording such as `compatibility bridge`
- public docs still include at least one broken or misleading cross-link risk
- public ADR content still exists under `docs/docs/adrs/`

## File Structure And Work Buckets

### Delete From Public Docs

- `docs/docs/reference/python-to-go-parity.md`
- `docs/docs/reference/parity-closure-matrix.md`
- `docs/docs/reference/merge-readiness-signoff.md`
- `docs/docs/adrs/2026-04-17-neo4j-deadlock-elimination-batch-isolation.md`

### Rewrite As Evergreen Current-State Docs

- `docs/docs/architecture.md`
- `docs/docs/reference/source-layout.md`
- `docs/docs/reference/local-testing.md`
- `docs/docs/reference/runtime-admin-api.md`
- `docs/docs/reference/service-workflows.md`
- `docs/docs/reference/configuration.md`
- `docs/docs/reference/logging.md`
- `docs/docs/reference/relationship-mapping.md`
- `docs/docs/reference/relationship-mapping-observability.md`
- `docs/docs/reference/telemetry/index.md`
- `docs/docs/reference/telemetry/logs.md`
- `docs/docs/reference/telemetry/traces.md`
- `docs/docs/reference/telemetry/metrics.md`
- `docs/docs/reference/telemetry/cross-service-correlation.md`
- `docs/docs/deployment/overview.md`
- `docs/docs/deployment/service-runtimes.md`
- `docs/docs/deployment/docker-compose.md`
- `docs/docs/deployment/helm.md`
- `docs/docs/deployment/argocd.md`
- `docs/docs/guides/relationship-graphs.md`
- `docs/docs/guides/shared-infra-trace.md`
- `docs/docs/guides/ci-cd.md`
- `docs/docs/guides/collector-authoring.md`
- `docs/docs/guides/terraform-providers/index.md`
- `docs/docs/languages/python.md`
- `docs/docs/languages/feature-matrix.md`
- `docs/docs/languages/support-maturity.md`

### Keep, But Re-verify During Execution

- `docs/docs/services/bootstrap-index.md`
- `docs/docs/services/ingester.md`
- `docs/docs/services/resolution-engine.md`
- `docs/docs/concepts/how-it-works.md`
- `docs/docs/concepts/graph-model.md`
- `docs/docs/concepts/modes.md`
- all remaining `docs/docs/languages/*.md` pages not listed above

## Chunk 1: Public Docs Purge And Information Architecture Cleanup

**Files:**
- Delete: `docs/docs/reference/python-to-go-parity.md`
- Delete: `docs/docs/reference/parity-closure-matrix.md`
- Delete: `docs/docs/reference/merge-readiness-signoff.md`
- Delete: `docs/docs/adrs/2026-04-17-neo4j-deadlock-elimination-batch-isolation.md`
- Modify: `docs/docs/index.md`
- Modify: `docs/docs/architecture.md`
- Modify: `mkdocs.yml`
- Modify: any nav/index pages that still link to deleted pages
- Test: `mkdocs build --strict`

- [x] **Step 1: Write the failing docs navigation check**

Run:

```bash
rg -n "python-to-go-parity|parity-closure-matrix|merge-readiness-signoff|docs/docs/adrs/" docs mkdocs.yml
```

Expected: references still exist and identify the exact pages/nav entries to remove.

- [x] **Step 2: Remove the public migration/signoff documents**

Delete the public migration-record pages and the public ADR page, then remove all navigation and cross-references to them.

- [x] **Step 3: Rewrite architecture/index pages to stop referencing migration artifacts**

Update `docs/docs/index.md` and `docs/docs/architecture.md` so they point to evergreen runtime, service, workflow, relationship, and operator docs only.

- [x] **Step 4: Run strict docs build**

Run:

```bash
mkdocs build --strict
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add docs/docs mkdocs.yml
git commit -m "docs: remove public migration records from docs set"
```

## Chunk 2: Runtime, Service, And Operator Docs Rewrite

**Files:**
- Modify: `docs/docs/deployment/overview.md`
- Modify: `docs/docs/deployment/service-runtimes.md`
- Modify: `docs/docs/deployment/docker-compose.md`
- Modify: `docs/docs/deployment/helm.md`
- Modify: `docs/docs/deployment/argocd.md`
- Modify: `docs/docs/reference/source-layout.md`
- Modify: `docs/docs/reference/runtime-admin-api.md`
- Modify: `docs/docs/reference/service-workflows.md`
- Modify: `docs/docs/reference/configuration.md`
- Modify: `docs/docs/reference/local-testing.md`
- Modify: `docs/docs/services/bootstrap-index.md`
- Modify: `docs/docs/services/ingester.md`
- Modify: `docs/docs/services/resolution-engine.md`
- Test: `mkdocs build --strict`
- Test: focused Go verification for touched runtime/query docs

- [ ] **Step 1: Write the failing doc-truth scan**

Run:

```bash
rg -n "migration|cutover|bridge|legacy Python|dual path|PYTHONPATH=src|uv run pytest" docs/docs/deployment docs/docs/reference docs/docs/services
```

Expected: hits identify stale runtime/operator wording that must be rewritten.

- [ ] **Step 2: Rewrite runtime and service docs to current-state Go ownership**

Document only the actual Go binaries, mounted admin/status surfaces, telemetry ownership, and deployment shapes that exist under `go/cmd/*` and `go/internal/*`.

- [ ] **Step 3: Rewrite the local testing runbook to be Go-first**

The runbook must:

- start with focused `go test` for touched packages
- include `go vet` and `golangci-lint`
- include Compose-backed proof where relevant
- explicitly document the Docker host-path caveat: use real absolute directories, not symlinks
- keep any remaining Python-only validation commands only when they are still truly required

- [ ] **Step 4: Verify runtime and docs truth**

Run:

```bash
go test ./cmd/api ./cmd/ingester ./cmd/projector ./cmd/reducer ./internal/runtime ./internal/query -count=1
go vet ./cmd/api ./cmd/ingester ./cmd/projector ./cmd/reducer ./internal/runtime ./internal/query
golangci-lint run ./cmd/api/... ./cmd/ingester/... ./cmd/projector/... ./cmd/reducer/... ./internal/runtime/... ./internal/query/...
mkdocs build --strict
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add docs/docs
git commit -m "docs: rewrite runtime and operator docs for go platform"
```

## Chunk 3: Language Docs, Matrix Cleanup, And Evidence-Backed Support Claims

**Files:**
- Modify: `docs/docs/languages/python.md`
- Modify: `docs/docs/languages/feature-matrix.md`
- Modify: `docs/docs/languages/support-maturity.md`
- Modify: selected `docs/docs/languages/*.md` pages where evidence references or labels drift from current Go code
- Modify: `docs/docs/contributing-language-support.md`
- Test: parser-, reducer-, query-, and relationship-focused Go tests for touched claims
- Test: `mkdocs build --strict`

- [ ] **Step 1: Write the failing label and evidence scan**

Run:

```bash
rg -n "DefaultEngine \\(python\\)|tests/unit/parsers/test_.*py|tests/integration/test_.*py|src/eshu" docs/docs/languages docs/docs/contributing-language-support.md
```

Expected: stale Python-era labels or evidence references are reported.

- [ ] **Step 2: Rewrite parser ownership labels to current Go truth**

Remove Python-engine labels where the implementation is now Go-backed. Replace them with exact Go parser entrypoints and Go proof references.

- [ ] **Step 3: Reconcile support matrices with real Go proof**

Every `supported`, `partial`, or `bounded` label in the matrix pages must match the actual Go implementation and actual proof in this branch.

- [ ] **Step 4: Add proof where claims are real but under-documented**

If the code is present but a public claim lacks current proof, add the minimal missing Go test before preserving the claim.

- [ ] **Step 5: Verify**

Run the smallest relevant scopes first, then broader checks. Use examples like:

```bash
go test ./internal/parser ./internal/collector ./internal/projector ./internal/reducer ./internal/storage/neo4j ./internal/query ./internal/relationships -count=1
golangci-lint run ./internal/parser/... ./internal/collector/... ./internal/projector/... ./internal/reducer/... ./internal/storage/neo4j/... ./internal/query/... ./internal/relationships/...
mkdocs build --strict
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add docs/docs
git commit -m "docs: align language support pages with go parser truth"
```

## Chunk 4: Relationship Mapping, Terraform/Terragrunt, And Workflow Surface Hardening

**Files:**
- Modify: `docs/docs/reference/relationship-mapping.md`
- Modify: `docs/docs/reference/relationship-mapping-observability.md`
- Modify: `docs/docs/guides/relationship-graphs.md`
- Modify: `docs/docs/guides/shared-infra-trace.md`
- Modify: `docs/docs/guides/ci-cd.md`
- Modify: `docs/docs/guides/terraform-providers/index.md`
- Modify: related language pages if the public relationship semantics need cross-links
- Test: Go relationship, query, reducer, and Terraform-schema verification

- [ ] **Step 1: Write the failing relationship-doc truth scan**

Run:

```bash
rg -n "compatibility bridge|temporary bridge|cutover|migration seam|legacy" docs/docs/reference/relationship-mapping* docs/docs/guides/relationship-graphs.md docs/docs/guides/shared-infra-trace.md docs/docs/guides/ci-cd.md docs/docs/guides/terraform-providers/index.md
```

Expected: stale wording and pages needing current-state rewrites are identified.

- [ ] **Step 2: Rewrite relationship docs around actual Go evidence families**

Make the docs explicitly describe the current Go-owned evidence and reduction path for:

- Terraform module sources
- Terragrunt includes, `read_terragrunt_config`, `find_in_parent_folders`, and dependency config paths
- Docker Compose build/image/`depends_on`
- GitHub Actions delivery and reusable workflow evidence
- Jenkins shared-library and repo evidence
- Ansible role, playbook, inventory, vars, and task entrypoint evidence
- ArgoCD, Kustomize, Helm, Crossplane, and CloudFormation evidence families

- [ ] **Step 3: Preserve and clarify Terraform provider schema purpose**

Keep `docs/docs/guides/terraform-providers/index.md` as an evergreen current-state page describing the live Go runtime use of packaged provider schemas.

- [ ] **Step 4: Verify**

Run:

```bash
go test ./internal/terraformschema ./internal/relationships ./internal/reducer ./internal/query ./internal/storage/postgres -count=1
golangci-lint run ./internal/terraformschema/... ./internal/relationships/... ./internal/reducer/... ./internal/query/... ./internal/storage/postgres/...
mkdocs build --strict
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add docs/docs
git commit -m "docs: rewrite relationship and terraform docs to current go behavior"
```

## Chunk 5: Final Parity Sweep And Cutover Readiness Gate

**Files:**
- Modify: any remaining `docs/docs` pages that still contradict current Go truth
- Modify: `docs/docs/index.md`
- Modify: `docs/docs/why-eshu.md` if needed
- Modify: `docs/docs/reference/troubleshooting.md` if operator guidance drifted
- Test: full docs build and repo truth scan

- [ ] **Step 1: Run the final stale-language scan**

Run:

```bash
rg -n "migration|cutover|bridge|legacy Python|DefaultEngine \\(python\\)|PYTHONPATH=src|uv run pytest|src/eshu" docs/docs
```

Expected: only intentional historical references in non-public or explicitly bounded pages remain. Public docs should not read as migration records.

- [ ] **Step 2: Reconcile any remaining contradictions**

Fix or remove any page that still overstates support or still speaks in migration language.

- [ ] **Step 3: Run final verification**

Run:

```bash
go test ./internal/parser ./internal/collector ./internal/projector ./internal/reducer ./internal/relationships ./internal/query ./internal/runtime ./internal/telemetry ./internal/storage/neo4j ./internal/storage/postgres ./cmd/... -count=1
go vet ./internal/parser ./internal/collector ./internal/projector ./internal/reducer ./internal/relationships ./internal/query ./internal/runtime ./internal/telemetry ./internal/storage/neo4j ./internal/storage/postgres ./cmd/...
golangci-lint run ./internal/parser/... ./internal/collector/... ./internal/projector/... ./internal/reducer/... ./internal/relationships/... ./internal/query/... ./internal/runtime/... ./internal/telemetry/... ./internal/storage/neo4j/... ./internal/storage/postgres/... ./cmd/...
git diff --check
mkdocs build --strict
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add docs/docs docs/superpowers/plans
git commit -m "docs: finalize current-state go platform documentation"
```

## Parallel Subagent Waves

After plan approval, execute with these independent waves:

### Wave A: Public Docs Purge

- ownership: delete public migration records and clean navigation
- write scope: `docs/docs/index.md`, `mkdocs.yml`, deleted reference pages

### Wave B: Runtime And Operator Docs

- ownership: runtime, deployment, services, local testing, configuration, telemetry
- write scope: `docs/docs/deployment/*`, `docs/docs/services/*`, `docs/docs/reference/local-testing.md`, `docs/docs/reference/configuration.md`, `docs/docs/reference/runtime-admin-api.md`, `docs/docs/reference/service-workflows.md`, `docs/docs/reference/telemetry/*`

### Wave C: Language Truth Pass

- ownership: `docs/docs/languages/*`, `docs/docs/contributing-language-support.md`
- focus: parser ownership labels, matrix truth, missing proof

### Wave D: Relationship And Terraform/Terragrunt Docs

- ownership: relationship mapping, shared-infra trace, CI/CD, provider-schema docs
- write scope: `docs/docs/reference/relationship-mapping*.md`, `docs/docs/guides/relationship-graphs.md`, `docs/docs/guides/shared-infra-trace.md`, `docs/docs/guides/ci-cd.md`, `docs/docs/guides/terraform-providers/index.md`

## Remaining Engineering Gaps To Track Separately

This docs plan does not assert that parity is finished everywhere. It enforces a rule:

- if a feature is not parity-complete, track it as engineering work
- do not hide it with public doc wording

Use this plan to keep the public docs honest while the remaining parity work is closed in code.

Plan complete and saved to `docs/superpowers/plans/2026-04-17-current-state-docs-and-parity-closure.md`. Ready to execute.
