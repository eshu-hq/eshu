# Eshu Console PR 1 Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first production-shaped read-only Eshu Console slice in `apps/console`.

**Architecture:** The console is a separate React/Vite/TypeScript package from the public homepage. It uses a contract-first Eshu API client that preserves response envelopes, a typed fixture fallback for demo mode, and an entity-centered workspace for repository and service/workload stories.

**Tech Stack:** React, Vite, TypeScript strict mode, Vitest, React Testing Library, React Router, future D3/uiGrid adapter boundaries.

---

## File Structure

- Create `apps/console/package.json` for independent console scripts.
- Create `apps/console/index.html`, `apps/console/vite.config.ts`,
  `apps/console/tsconfig.json`, `apps/console/tsconfig.app.json`, and
  `apps/console/tsconfig.node.json`.
- Create `apps/console/src/api/envelope.ts` for canonical envelope and error
  types.
- Create `apps/console/src/api/client.ts` for endpoint-aware fetch helpers.
- Create `apps/console/src/api/mockData.ts` for typed demo fixtures.
- Create `apps/console/src/api/repository.ts` for repository/story/status
  screen adapters.
- Create `apps/console/src/config/environment.ts` for endpoint persistence and
  demo mode.
- Create `apps/console/src/routes.tsx`, `apps/console/src/App.tsx`, and
  `apps/console/src/main.tsx` for shell routing.
- Create `apps/console/src/components/StatusStrip.tsx`,
  `apps/console/src/components/SearchCommand.tsx`, and
  `apps/console/src/components/WorkspaceTabs.tsx`.
- Create `apps/console/src/pages/HomePage.tsx`,
  `apps/console/src/pages/WorkspacePage.tsx`,
  `apps/console/src/pages/DashboardPage.tsx`,
  `apps/console/src/pages/CatalogPage.tsx`, and
  `apps/console/src/pages/FindingsPage.tsx`.
- Create `apps/console/src/visualization/PathMap.tsx` as the D3 boundary
  placeholder.
- Create `apps/console/src/grid/EvidenceGrid.tsx` as the uiGrid boundary
  placeholder.
- Create `apps/console/src/styles.css`.
- Create focused tests next to the relevant modules.
- Create `apps/console/README.md`.
- Modify root `package.json` to add console scripts.

## Task 1: Scaffold Console Package

**Files:**

- Create: `apps/console/package.json`
- Create: `apps/console/index.html`
- Create: `apps/console/vite.config.ts`
- Create: `apps/console/tsconfig.json`
- Create: `apps/console/tsconfig.app.json`
- Create: `apps/console/tsconfig.node.json`
- Create: `apps/console/src/test/setup.ts`
- Modify: `package.json`

- [ ] **Step 1: Write the failing package test**

Create a minimal test command target with a placeholder test that imports a
future app marker.

- [ ] **Step 2: Run the console test and verify it fails**

Run: `npm run console:test`

Expected: fails because `apps/console` does not exist yet.

- [ ] **Step 3: Add the package scaffold**

Add Vite, TypeScript, Vitest, and Testing Library config scoped to
`apps/console`.

- [ ] **Step 4: Run package checks**

Run:

```bash
npm run console:test
npm run console:typecheck
```

Expected: both pass.

## Task 2: Envelope Client And Demo Environment

**Files:**

- Create: `apps/console/src/api/envelope.ts`
- Create: `apps/console/src/api/client.ts`
- Create: `apps/console/src/config/environment.ts`
- Test: `apps/console/src/api/envelope.test.ts`
- Test: `apps/console/src/api/client.test.ts`
- Test: `apps/console/src/config/environment.test.ts`

- [ ] **Step 1: Write failing envelope tests**

Cover success envelopes, structured errors, truth labels, and freshness state.

- [ ] **Step 2: Run tests and verify failures**

Run: `npm run console:test -- envelope client environment`

Expected: fails because modules do not exist.

- [ ] **Step 3: Implement minimal types and client**

Implement:

- `EshuEnvelope<T>`
- `EshuTruth`
- `EshuError`
- `EshuApiClient`
- endpoint storage helpers
- demo-mode detection

- [ ] **Step 4: Run focused tests**

Run: `npm run console:test -- envelope client environment`

Expected: pass.

## Task 3: Shell, Routing, And Status Strip

**Files:**

- Create: `apps/console/src/routes.tsx`
- Create: `apps/console/src/App.tsx`
- Create: `apps/console/src/main.tsx`
- Create: `apps/console/src/components/StatusStrip.tsx`
- Create: `apps/console/src/pages/HomePage.tsx`
- Create: `apps/console/src/pages/DashboardPage.tsx`
- Create: `apps/console/src/pages/CatalogPage.tsx`
- Create: `apps/console/src/pages/FindingsPage.tsx`
- Test: `apps/console/src/App.test.tsx`
- Test: `apps/console/src/components/StatusStrip.test.tsx`

- [ ] **Step 1: Write failing routing and status tests**

Assert the shell renders role-neutral nav, demo/private mode, and workspace
links.

- [ ] **Step 2: Run tests and verify failures**

Run: `npm run console:test -- App StatusStrip`

Expected: fails because shell modules do not exist.

- [ ] **Step 3: Implement shell**

Add routes for `/`, `/dashboard`, `/catalog`, `/findings`, and workspace
routes.

- [ ] **Step 4: Run focused tests**

Run: `npm run console:test -- App StatusStrip`

Expected: pass.

## Task 4: Search And Workspace Story Flow

**Files:**

- Create: `apps/console/src/api/mockData.ts`
- Create: `apps/console/src/api/repository.ts`
- Create: `apps/console/src/components/SearchCommand.tsx`
- Create: `apps/console/src/components/WorkspaceTabs.tsx`
- Create: `apps/console/src/pages/WorkspacePage.tsx`
- Create: `apps/console/src/grid/EvidenceGrid.tsx`
- Create: `apps/console/src/visualization/PathMap.tsx`
- Test: `apps/console/src/components/SearchCommand.test.tsx`
- Test: `apps/console/src/pages/WorkspacePage.test.tsx`

- [ ] **Step 1: Write failing search/workspace tests**

Assert repository/service candidates render, selection routes to the workspace,
and the story page shows narrative, truth, freshness, evidence, deployment,
findings, and limitations.

- [ ] **Step 2: Run tests and verify failures**

Run: `npm run console:test -- SearchCommand WorkspacePage`

Expected: fails because modules do not exist.

- [ ] **Step 3: Implement typed fixtures and workspace page**

Use fixture fallback for repo and service/workload stories. Keep live API calls
behind `repository.ts` adapters.

- [ ] **Step 4: Run focused tests**

Run: `npm run console:test -- SearchCommand WorkspacePage`

Expected: pass.

## Task 5: Findings, Catalog, Dashboard, And Docs

**Files:**

- Modify: `apps/console/src/pages/DashboardPage.tsx`
- Modify: `apps/console/src/pages/CatalogPage.tsx`
- Modify: `apps/console/src/pages/FindingsPage.tsx`
- Create: `apps/console/README.md`
- Test: `apps/console/src/pages/DashboardPage.test.tsx`
- Test: `apps/console/src/pages/CatalogPage.test.tsx`
- Test: `apps/console/src/pages/FindingsPage.test.tsx`

- [ ] **Step 1: Write failing page tests**

Assert dashboard status, catalog rows, and dead-code finding rows render from
fixtures.

- [ ] **Step 2: Run tests and verify failures**

Run: `npm run console:test -- DashboardPage CatalogPage FindingsPage`

Expected: fails until pages are filled in.

- [ ] **Step 3: Implement pages and README**

Document local/private real-data mode, public fixture demo mode, and verification
commands.

- [ ] **Step 4: Run focused tests**

Run: `npm run console:test -- DashboardPage CatalogPage FindingsPage`

Expected: pass.

## Task 6: Latest MCP Contract Refactor

**Files:**

- Modify: `apps/console/src/api/repository.ts`
- Modify: `apps/console/src/api/serviceSpotlight.ts`
- Modify: `apps/console/src/api/changeSurface.ts`
- Modify: `apps/console/src/pages/CatalogPage.tsx`
- Modify: `apps/console/src/pages/DashboardPage.tsx`
- Modify: `apps/console/src/pages/ServiceSpotlightPanel.tsx`
- Modify: `apps/console/src/visualization/ServiceDeploymentLaneMap.tsx`
- Modify: `docs/superpowers/specs/2026-05-14-eshu-console-storytelling-contract-field-map-design.md`

- [ ] **Step 1: Write failing contract adapter tests**

Cover the newly hardened MCP/API contracts:

- service story uses the dossier fields before falling back to context rows
- node clicks without canonical IDs use `resolve_entity` before graph expansion
- catalog uses paged repository inventory and visible truncation state
- CloudFront or CDN-style edge evidence is represented as traffic evidence, not
  deployment ownership
- change-surface is only requested from a narrowed service, entity, file, or
  topic scope

- [ ] **Step 2: Run focused tests and verify failures**

Run:

```bash
npm run console:test -- repository serviceSpotlight changeSurface CatalogPage DashboardPage ServiceSpotlightPanel
```

Expected: fails until the adapters and screens consume the new contract shape.

- [ ] **Step 3: Implement resolver-first drilldowns**

Add a resolver boundary for graph clicks and search selections:

- already-canonical IDs open the story/context/evidence rail directly
- display names open a `resolve_entity` candidate picker first
- ambiguous or truncated results show the exact `limit` and `truncated` state
- selected candidates rerun the intended story or drilldown by canonical ID

- [ ] **Step 4: Refactor catalog into faceted inventory**

Move catalog from a repository-only table to bounded facets:

- repositories from `list_indexed_repositories`
- services and workloads from graph-backed catalog rows when available
- evidence families, freshness, language, and deployment-family filters
- visible paging state for `limit`, `offset`, and `truncated`

- [ ] **Step 5: Add edge-aware service traffic story**

Represent public traffic as a readable path:

```text
hostname -> CDN/edge -> origin/load balancer -> runtime target -> workload -> source repository
```

CloudFront distribution aliases, origins, cache behaviors, viewer certificates,
ACM links, and WAF links are edge evidence only unless a reducer produces
explicit workload ownership or deployment correlation.

- [ ] **Step 6: Re-scope change-surface UI**

Change-surface should appear as a review lens after the page has a narrowed
scope. Do not fire broad service-name impact probes as the primary page load.
Use service story, service investigation, entity resolution, and code topic
packets first, then expose change-surface for selected entities, paths, topics,
or dependency edges.

- [ ] **Step 7: Run focused contract checks**

Run:

```bash
npm run console:test -- repository serviceSpotlight changeSurface CatalogPage DashboardPage ServiceSpotlightPanel
npm run console:typecheck
go test ./internal/mcp ./internal/query -count=1
```

Expected: UI contract tests and backend MCP/query contract tests pass before
the next remote redeploy.

## Task 7: Verification

**Files:**

- All console files

- [ ] **Step 1: Run console gates**

Run:

```bash
npm run console:test
npm run console:typecheck
npm run console:build
```

- [ ] **Step 2: Run root frontend gates**

Run:

```bash
npm test
npm run typecheck
npm run build
```

- [ ] **Step 3: Run docs gates**

Run:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

- [ ] **Step 4: Browser verification**

Start the console dev server, open the app, and capture one desktop and one
mobile screenshot through the in-app browser or Playwright.

- [ ] **Step 5: Final review**

Confirm no real-data public mode is implied, no mutating controls exist, and all
truth/freshness/error states remain visible.
