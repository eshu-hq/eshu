# ADR: Pre-Merge Validation — Local MCP Self-Indexing + IaC Chart Parity For Go Runtime

**Date:** 2026-04-20
**Status:** Superseded
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- `2026-04-20-workflow-coordinator-and-multi-collector-runtime-contract.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md`
- `docs/docs/deployment/service-runtimes.md`
- `docs/docs/reference/local-testing.md`
- `~/repos/mobius/iac-eks-eshu/chart/` (private internal)

---

## Status Review (2026-05-03)

**Current disposition:** Superseded.

This ADR was a pre-merge gate for an older Go-runtime branch and mixed public
repo requirements with private IaC chart validation. Current public deployment
docs, Helm templates, Docker Compose docs, local-testing docs, and CI gates have
replaced the actionable parts.

**Remaining work:** create a new deployment/parity tracker if private ops-qa or
internal IaC validation is still needed. Do not treat this ADR as the active
public chart contract.

## Context

The `codex/go-data-plane-architecture` branch replaces the Python runtime with a
Go-only data plane:

- `eshu-api` (HTTP API Deployment)
- `eshu-mcp-server` (MCP transport, separate binary)
- `eshu-ingester` (StatefulSet, workspace PVC)
- `eshu-reducer` (resolution-engine Deployment)
- `eshu-workflow-coordinator` (new control-plane Deployment, dark by default)
- `eshu-bootstrap-data-plane` (bootstrap binary + compose/runtime helper; no
  Kubernetes rendering path in either chart today, see D2.7)
- `eshu-bootstrap-index` (one-shot helper)

Before merging this long-running branch into `main`, two claims must be
validated:

1. **The local stack indexes this repo and serves MCP to Claude Code well
   enough to reduce code-finding token cost.** Eshu inherits Eshu
   (Eshu) positioning; self-indexing is the minimum demo.
2. **The private-internal IaC chart at `~/repos/mobius/iac-eks-eshu/` plus the
   `argocd/eshu-hqgraph/overlays/ops-qa/` overlay deploys the new Go
   runtime correctly**, including the new workflow-coordinator workload and
   renamed env-var contract.

Both validations are research-only today. Execution (docker compose up, helm
template, kubectl apply) happens after this ADR is accepted.

---

## Decision

### D1 — Local MCP Self-Indexing Is A Pre-Merge Gate

Before `main` merge:

1. Run `docker compose up --build` with `ESHU_FILESYSTEM_HOST_ROOT` pointing at a
   real directory that contains this repository (not a symlink; worktrees have
   a `.git` file pointer that the discovery path may skip — use the primary
   clone at `~/personal-repos/eshu`, or a fresh copy under
   `/Users/allen/eshu-local-index/`).
2. Run `./scripts/sync_local_compose_mcp.sh` so `.mcp.json` picks up the live
   mcp-server port and bearer token.
3. Confirm the pipeline reached steady state:
   - `curl -s http://localhost:8080/admin/status | jq .` — no failed work items
   - `curl -s http://localhost:8080/api/v0/repositories | jq '. | length'` — ≥1
4. Point Claude Code at the `eshu-local-compose` MCP entry and run a baseline
   set of queries against this repo:
   - `resolve_entity` on a known symbol (e.g. `DeployableUnitCorrelationHandler`)
   - `analyze_code_relationships` with `query_type: find_callers` on a
     reducer phase publisher (note: `find_callers` is not a standalone MCP
     tool — it is the enum value used inside `analyze_code_relationships`.
     See `go/internal/mcp/tools_codebase.go:52` for the full accepted enum)
   - repository-scoped content fetch
5. Record token-in / token-out for a matched MCP-assisted session vs. a
   Grep/Glob-only session on the same task.

Merge is not blocked on a specific token-savings threshold, but an honest
qualitative result ("MCP surfaced the handler directly" vs. "MCP returned
nothing useful") is required and should be captured in the merge PR body.

Known gotcha (carried from prior runs): if `.eshu-fixture-manifest` survives in
the `eshu_data` volume from a previous run, bootstrap skips discovery. Run
`docker compose down -v` between shape changes, or delete that manifest.

### D2 — IaC Chart Must Reach Parity With The Go Runtime Before ops-qa Deploy

The private-internal chart at `~/repos/mobius/iac-eks-eshu/chart/` (v0.1.40,
appVersion `v0.0.58`) trails the upstream chart at
`deploy/helm/eshu/`. The following parity gaps MUST close
before this branch is merged and tagged for an ops-qa rollout.

#### D2.1 Missing templates

| Template | Upstream status | IaC status | Required for |
| --- | --- | --- | --- |
| `deployment-workflow-coordinator.yaml` | present | **missing** | new control plane (dark mode gate) |
| `service-workflow-coordinator.yaml` | present | **missing** | admin/metrics surface for coordinator |
| `networkpolicy.yaml` | present | **missing** | egress/ingress lockdown default |

Without the workflow-coordinator templates, the dark-by-default rollout has no
target to deploy to; the operator has no way to validate the coordinator
before turning on claim ownership.

#### D2.2 Missing values keys

IaC `chart/values.yaml` is missing:

- `workflowCoordinator:` block — required keys: `enabled` (default `false`),
  `deploymentMode: dark`, `claimsEnabled: false`, `collectorInstances: []`,
  `replicas`, `revisionHistoryLimit`, `connectionTuning`, `resources`.
- `connectionTuning:` blocks on `api`, `resolutionEngine`, and `ingester` for
  Postgres and Neo4j pool tuning — upstream supports all three.
- `networkPolicy.enabled` — default `true` per upstream.

Already present in IaC `chart/values.yaml` (do NOT re-add):

- `observability:` base block (lines 138–166) with `otel` + `prometheus`
  sub-blocks.
- `initContainerSecurityContext` (lines 29–37) with `runAsUser: 0` for DDL
  paths and `readOnlyRootFilesystem: true`.
- `podSecurityContext` / `containerSecurityContext` (lines 14–27).

The above correction supersedes any earlier draft wording that listed
`observability` or `initContainerSecurityContext` as missing — they are
present. The real gaps are the workflow-coordinator, connection-tuning, and
network-policy blocks.

#### D2.3 Helper functions

`chart/templates/_helpers.tpl` must define (or inherit) the helpers the new
upstream templates use:

- `eshu.renderConnectionTuningEnv`
- `eshu.renderOtelEnv`
- `eshu.renderPrometheusEnv`
- `eshu.workflowCoordinatorFullname`
- `eshu.workflowCoordinatorSelectorLabels`

Codex finding: IaC `deployment.yaml` already calls `eshu.renderOtelEnv` and
`eshu.renderPrometheusEnv` (line 65–66), so those helpers are present. The
coordinator-specific helpers and `eshu.renderConnectionTuningEnv` must be
added or back-ported.

#### D2.4 Environment variable drift — python-era keys still present

`chart/values.yaml env:` and the ops-qa overlay workload-specific env maps
(`app-values.yaml resolutionEngine.env:` and `app-values.yaml ingester.env:`)
contain keys the Go runtime does not read:

- `ESHU_COMMIT_WORKERS`
- `ESHU_ADAPTIVE_GRAPH_BATCHING_ENABLED`
- `ESHU_ASYNC_COMMIT_ENABLED`
- `ESHU_FUNCTION_CALL_GLOBAL_FALLBACK`
- `ESHU_CALL_RESOLUTION_SCOPE`
- `ESHU_VARIABLE_SCOPE`
- `ESHU_INDEX_QUEUE_DEPTH`
- `ESHU_REPO_FILE_PARSE_MULTIPROCESS`
- `ESHU_MULTIPROCESS_START_METHOD`

These are silently ignored by the Go binaries. Wiring must be split between
env vars whose consuming workload is actually rendered today vs. env vars
that belong to workloads not yet in the chart (bootstrap-index,
workflow-coordinator).

**Wire NOW — consumers render today:**

- Ingester (StatefulSet):
  - `ESHU_PARSE_WORKERS`
  - `ESHU_SNAPSHOT_WORKERS`
  - `ESHU_LARGE_REPO_FILE_THRESHOLD`
  - `ESHU_LARGE_REPO_MAX_CONCURRENT`
- Reducer / resolution-engine (Deployment):
  - `ESHU_REDUCER_WORKERS`
  - `ESHU_SHARED_PROJECTION_WORKERS`
  - `ESHU_SHARED_PROJECTION_PARTITION_COUNT`
  - `ESHU_SHARED_PROJECTION_BATCH_LIMIT`
  - `ESHU_SHARED_PROJECTION_POLL_INTERVAL`
  - `ESHU_SHARED_PROJECTION_LEASE_TTL`
  - `ESHU_REDUCER_BATCH_CLAIM_SIZE`
- All Go workloads (optional, node-dependent):
  - `GOMEMLIMIT`

**Reserve — do NOT wire into chart until consumer lands:**

- `ESHU_PROJECTION_WORKERS` — bootstrap-index. Per D2.7, bootstrap-index has
  no Kubernetes rendering path today. Adding this env var to the chart
  now would only attach to a workload that does not exist; defer until a
  bootstrap-index Job or init container is decided.
- Workflow-coordinator tuning (`ESHU_WORKFLOW_COORDINATOR_*`) — covered by
  D2.1/D2.2 when the coordinator Deployment/values block lands.

#### D2.5 MCP-server deployment gap (pre-existing in both charts)

Neither upstream nor IaC chart ships a separate Deployment for
`eshu-mcp-server`. The ops-qa gateway exposes `mcp-eshu.qa.ops.bgrp.io`, but
the backing service routes to `eshu-api` which runs only `eshu-api` (HTTP API,
`/api/*`). The API binary does not mount the MCP SSE or `/mcp/message`
transport — that is the MCP-server binary’s job per
`docs/docs/deployment/service-runtimes.md` §MCP Server.

Two acceptable remediations (pick one before merge):

- **Option A (preferred):** add `deployment-mcp-server.yaml` and
  `service-mcp-server.yaml` to upstream `deploy/helm/eshu/`
  and mirror them into the IaC chart. Update the ops-qa HTTPRoute/Gateway
  parent ref to point at the new MCP service.
- **Option B:** retire or rename the `mcp-eshu.qa.ops.bgrp.io` hostname
  entirely (e.g., to `api-eshu.qa.ops.bgrp.io`). Do NOT keep an MCP-branded
  hostname routing to the API service while documenting that MCP is absent —
  that is a contract lie that will mislead operators and IDE clients. If
  MCP transport is deferred, the overlay must not advertise an MCP
  hostname at all until the MCP Deployment lands.

Either way, the current config is misleading and must be resolved before
merge to avoid landing a public endpoint whose DNS advertises a protocol
it does not serve.

#### D2.6 Image tag

- IaC `chart/Chart.yaml appVersion: "v0.0.58"` and `chart/values.yaml
  image.tag: "v0.0.58"` predate this branch HEAD. The CI image pipeline for
  `private-registry.example.com/platform/eshu-hqgraph` must build and push
  a new tag from this branch before the ops-qa argocd sync will find the
  image.
- Tag proposal: `v0.0.1-go` or a date-stamped tag like `v0.0.1-20260420`,
  reflecting the Python→Go cutover.

#### D2.7 ArgoCD sync ordering + schema bootstrap gap

**Schema bootstrap is currently absent from both charts.** Correction to
earlier draft wording: neither the IaC chart
(`~/repos/mobius/iac-eks-eshu/chart/templates/statefulset.yaml:50`) nor the
upstream chart (`deploy/helm/eshu/templates/`) ships a
`eshu-bootstrap-data-plane` init container. The only init container in
either chart is the ingester `workspace-setup` step
(`chart/templates/statefulset.yaml:51`). That step only prepares the PVC
directory; it does NOT run DDL against Postgres or Neo4j.

Consequences:

- On a cold environment, the API/ingester/reducer pods will start against
  empty databases. DDL must be applied by some other mechanism (manual
  `psql`, an out-of-band migration Job, or the runtime's lazy schema apply
  on first write). Merging without clarifying this will produce a broken
  first-sync in ops-qa.
- The `docs/docs/deployment/service-runtimes.md` and CLAUDE.md references
  to `eshu-bootstrap-data-plane` describe a runtime that exists as a
  binary/compose service but has no Kubernetes rendering path in either
  chart today.

Required before merge:

1. Decide whether schema bootstrap is (a) deliberately absent because the
   Go runtimes apply DDL on startup, (b) implicit via `eshu-bootstrap-index`
   as a one-shot Job, or (c) missing and must be added as a
   `eshu-bootstrap-data-plane` Kubernetes `Job` or init container.
2. Document the chosen answer in `service-runtimes.md` and carry it into
   the chart.

ArgoCD wave guidance (independent of the schema question):

- `argocd.syncWave: "1"` at both base and overlay is acceptable for the
  application chart once the schema question above is resolved.
- Verify neo4j + postgresql Helm releases sit in an earlier wave (or use
  `dependsOn`) so application pods can reach the databases on first sync.
  Base kustomization already includes `externalsecret-*` resources;
  confirm they finish reconciling before application manifests apply.

### D3 — Acceptance Criteria For Merge

This ADR is accepted and the branch may merge to `main` only after:

- [ ] D1 local MCP self-indexing validation run; result captured in merge PR body.
- [ ] D2.1 IaC chart PR adds `deployment-workflow-coordinator.yaml`,
      `service-workflow-coordinator.yaml`, and `networkpolicy.yaml`.
- [ ] D2.2 IaC `values.yaml` adds `workflowCoordinator:`, per-service
      `connectionTuning:`, and `networkPolicy:` blocks with safe defaults
      (dark, disabled, empty). Do NOT re-add `observability:` or
      `initContainerSecurityContext` — both already present.
- [ ] D2.3 IaC `_helpers.tpl` carries the required helper functions.
- [ ] D2.4 IaC `values.yaml` and ops-qa overlay python-era env-var keys
      removed; Go-runtime tuning env vars wired where needed.
- [ ] D2.4 (blast radius) IaC chart tests updated:
      `chart/tests/runtime-api-ingester-split.sh:105` asserts
      `ESHU_COMMIT_WORKERS` renders exactly once and will fail after the
      env-var cleanup. Replace with an assertion on the Go-runtime
      equivalent for a workload that actually renders today (e.g.
      `ESHU_PARSE_WORKERS`, `ESHU_SNAPSHOT_WORKERS`, or a reducer/shared
      projection tuning key). Do NOT switch this assertion to
      `ESHU_PROJECTION_WORKERS`, because D2.4/D2.7 reserve that knob until a
      bootstrap-index Kubernetes workload exists.
      Audit other chart test files for additional python-era env-var
      assertions.
- [ ] D2.4 (doc drift) IaC `README.md:11` ("MCP/API surface"),
      `AGENTS.md:23–30` ("HTTP API + MCP", "eshu serve start" runtime
      command), and upstream `deploy/helm/eshu/README.md:6`
      ("HTTP API + MCP") rewritten to match the split API + MCP Deployment
      contract from `docs/docs/deployment/service-runtimes.md`. Runtime
      command strings updated to `eshu api start` (or the chosen canonical
      command) where the docs mention the API boot path.
- [ ] D2.5 MCP-server deployment gap resolved under Option A (add MCP
      Deployment) or Option B (retire/rename the `mcp-eshu.*` hostname). Do
      NOT pick a hybrid that keeps an MCP-named hostname pointed at the
      API service.
- [ ] D2.6 A new image tag built from this branch HEAD is pushed to JFrog.
- [ ] D2.7 Schema bootstrap question answered (runtime applies DDL / Job /
      init container) and chart updated accordingly; `service-runtimes.md`
      reflects reality.
- [ ] `helm lint chart/` and `helm template chart/ -f argocd/.../ops-qa/app-values.yaml`
      both pass in the IaC repo.

---

## Consequences

### Positive

- Merging without D1 risks shipping an MCP-first product with no working MCP
  demonstration against its own codebase. The self-indexing loop is the
  fastest way to prove Eshu/Eshu lineage and the cheapest token-savings pitch.
- Merging without D2 risks deploying the ops-qa overlay with silently-broken
  env vars and no coordinator workload, forcing an emergency rollback and
  manual cluster cleanup.
- Documenting the python-era env-var drift here prevents operators from
  "tuning" symbols that have no effect in the Go runtime.

### Negative

- D2 adds scope to the branch beyond the Go data-plane slice. Accept the
  scope here because the chart parity is load-bearing for the first ops-qa
  deploy; deferring it means the branch merges but cannot be deployed.
- D2.5 may force a chart-template decision on MCP hosting that hasn’t
  surfaced before. Option B is still a real scope increase because it
  requires DNS/overlay cleanup and operator-doc rewrites even if the MCP
  Deployment itself is deferred.

### Risks

- If docker compose cannot index this repo (memory pressure, very large
  facts set), D1 becomes a task of its own. Start with `ESHU_PARSE_WORKERS=2`
  and `ESHU_LARGE_REPO_MAX_CONCURRENT=1` on macOS Docker Desktop.
- If the catalog image pipeline lives outside this repo, D2.6 requires
  coordination with whoever owns the pipeline; surface that dependency early.

---

## Out Of Scope

- AWS cloud collector deployment shape (covered by the separate AWS collector
  ADR and its plan).
- Terraform-state collector deployment shape (covered by the tfstate ADR).
- Replacing `argocd.syncWave` with a richer dependency graph — current wave
  is acceptable.
- Neo4j or Postgres sizing changes for ops-qa.

---

## Open Questions

- Should the MCP deployment carry its own `ServiceMonitor`, or reuse the
  API’s? Upstream `servicemonitor.yaml` covers API + ingester + reducer +
  coordinator; MCP was never added.
- Should `eshu-bootstrap-index` continue to be Docker-Compose-only, or does
  the ops-qa environment want a one-shot `Job` manifest for empty-environment
  recovery? D2.7 keeps this open on purpose: if bootstrap schema/state cannot
  be guaranteed by the hosted runtimes, a Kubernetes Job may be the cleanest
  answer. Resolve this together with the schema-bootstrap decision rather than
  assuming compose-only up front.
