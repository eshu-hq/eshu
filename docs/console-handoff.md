# Console readiness checklist

> **Doc status:** refreshed from the original Console v2 handoff (June 2025).
> All Console v2 backend and frontend issues from that handoff are closed or
> shipped. This file is now a **current proof/readiness checklist** for agents
> working on the console surface. Do not re-open the Console v2 backlog items
> listed in the archived section at the bottom.

---

## Current open work

These issues are the active console surface backlog as of the last audit:

| Issue | Title | Status |
|-------|-------|--------|
| [#3326](https://github.com/eshu-hq/eshu/issues/3326) | Add full console/dashboard E2E proof against local Compose | OPEN |
| [#3328](https://github.com/eshu-hq/eshu/issues/3328) | Add Node, marketing site, and console gates to CI | OPEN |
| [#3329](https://github.com/eshu-hq/eshu/issues/3329) | Upgrade vulnerable frontend toolchain dependencies | OPEN |
| [#3331](https://github.com/eshu-hq/eshu/issues/3331) | Add console bundle budget and split heavy graph/diagram chunks | OPEN |

Do not create new issues for items already on this list.

---

## Console proof/readiness checklist

Run this checklist before marking console work done or before requesting a
review that touches the console surface.

### 1. Private mode — correct default behavior

- [ ] Default mode is `"private"` (see `apps/console/src/config/environment.ts`
  `defaultEnvironment.mode`).
- [ ] With no saved environment the app shows the data-source popover, not a
  fabricated data set.
- [ ] Connecting to a live API at `/eshu-api/` (or a custom base URL) succeeds
  and all panels hydrate from real data.
- [ ] `source.mode` shows `"private"` in the connection strip after a successful
  live connection.

### 2. Demo mode — optional, explicitly labeled

- [ ] Demo mode activates only when `env.mode === "demo"` (set via
  `eshu.console.environment` in `localStorage`).
- [ ] The connection strip displays `"demo"` when demo mode is active.
- [ ] Demo data comes only from `apps/console/src/api/demoClient.ts` and
  `demoFixtures.ts`; live pages do not import these directly.
- [ ] No panel invents numbers outside of the explicit demo code path.

### 3. Dashboard expectations

- [ ] `DashboardPage` stat cards (repositories, services, findings) bind to
  real `index-status`, `ecosystem/overview`, `status/pipeline`,
  `status/ingesters` responses.
- [ ] Sparklines and area charts load from `GET /api/v0/metrics/timeseries`.
  When no configured metrics source has recent samples, the charts render
  an explicit empty/unavailable state — not mock series.
- [ ] Suggested questions panel renders from the live `ask` surface.
- [ ] `npm run console:typecheck` passes with no errors.
- [ ] `npm run console:test` passes (`DashboardPage.test.tsx` covers the key
  stat-card and empty-state paths).
- [ ] `npm run console:build` produces a clean build.

### 4. Repository browser

- [ ] Repository list comes from `GET /api/v0/repositories`.
- [ ] File tree uses `GET /api/v0/repositories/{id}/tree?path=&ref=`.
- [ ] File content uses `GET /api/v0/repositories/{id}/content?path=&ref=`.
- [ ] Branch selector uses `GET /api/v0/repositories/{id}/branches`.
- [ ] Indexed-ref-only repos show a single-ref state instead of fabricated
  branch names.
- [ ] No fabricated file tree or file contents anywhere under
  `apps/console/src/pages/RepoSourcePage.tsx`.

### 5. Vulnerability / supply-chain surface

- [ ] Vulnerability list comes from `GET /api/v0/supply-chain/impact/findings`.
- [ ] CVE detail page (`VulnDetailPage.tsx`) calls
  `GET /api/v0/supply-chain/vulnerabilities/{advisory_id}` for the full
  advisory.
- [ ] Affected-services section joins to graph entities (repo, service links).
- [ ] Severity counts in the panel equal the count of listed finding rows —
  no separate fabricated totals.

### 6. Service spotlight / workspace

- [ ] Blast-radius graph uses `POST /api/v0/impact/blast-radius`.
- [ ] Callers/Importers graph uses `POST /api/v0/code/relationships` (reverse
  edges).
- [ ] Deployment-path lane items expand typed-edge evidence from
  `services/{id}/story`.
- [ ] Findings count equals the listed CVE rows from the same API source.
- [ ] `ServiceSpotlightPanel.test.tsx` passes with current fixtures.

### 7. Graph explorer / code graph

- [ ] Entity search uses `POST /api/v0/entities/resolve`.
- [ ] Neighborhood expansion uses `POST /api/v0/code/relationships` or
  `POST /api/v0/impact/entity-map`.
- [ ] Node click opens the spotlight drawer for service/workload nodes.
- [ ] `CodeGraphPage.test.tsx` passes.

### 8. Required live-stack proof (before shipping console changes)

Console unit tests run against mocked adapters. Before marking a console
feature or fix done, an operator must confirm the following against a real
local Compose stack (see [#3326](https://github.com/eshu-hq/eshu/issues/3326)):

```bash
# Start the local stack
docker compose up -d

# Verify API health
curl -s http://localhost:8080/api/v0/status/pipeline | jq .

# Run the console in dev mode pointed at the local stack
cd apps/console
VITE_ESHU_API_BASE=http://localhost:8080/eshu-api/ npm run dev

# After loading in a browser:
# 1. Dashboard panel shows real repository and service counts.
# 2. Metrics charts render data or an explicit empty state.
# 3. At least one repository's file tree and content load without error.
# 4. A CVE detail page renders without a 404 or fallback stub.
```

This gate is not automated yet. Issue [#3326](https://github.com/eshu-hq/eshu/issues/3326)
tracks adding a browser-level E2E proof. Issue [#3328](https://github.com/eshu-hq/eshu/issues/3328)
tracks wiring the console npm gates into CI.

---

## Shipped: Console v2 backend endpoints

All endpoints that were listed as "ISSUE:" items in the original handoff are
now shipped and registered in `go/internal/query/`. Do not re-create issues for
these.

| Endpoint | Handler / file | Shipped |
|----------|---------------|---------|
| `GET /api/v0/repositories/{id}/tree` | `repository.go` | yes |
| `GET /api/v0/repositories/{id}/content` | `repository.go` | yes |
| `GET /api/v0/repositories/{id}/branches` | `repository.go` | yes |
| `GET /api/v0/metrics/timeseries` | `metrics.go` | yes |
| `GET /api/v0/supply-chain/vulnerabilities/{advisory_id}` | `supply_chain_vulnerability_detail_handler.go` | yes |
| `POST /api/v0/impact/blast-radius` | `impact.go` (`findBlastRadius`) | yes |

OpenAPI docs: `go/internal/query/openapi_paths_repositories.go`,
`go/internal/query/openapi_paths_repositories_branches.go`.

---

## Shipped: Console v2 frontend pages

All major frontend pages from the original handoff are ported under
`apps/console/src/pages/` and `apps/console/src/api/`:

| Feature | Page / adapter file |
|---------|---------------------|
| Dashboard with live stats and timeseries | `DashboardPage.tsx` |
| Repository browser + file tree/content/branches | `RepositoriesPage.tsx`, `RepoSourcePage.tsx`, `api/repoSource.ts` |
| Vulnerability list | `VulnerabilitiesPage.tsx`, `VulnerabilitiesCatalog.tsx` |
| CVE detail view | `VulnDetailPage.tsx`, `api/vulnerability.ts` |
| Service spotlight (blast radius, callers, findings, deployment lanes) | `WorkspacePage.tsx`, `ServiceSpotlightPanel.tsx`, `api/serviceSpotlight.ts` |
| Graph explorer / code relationships | `CodeGraphPage.tsx`, `api/eshuGraph.ts` |
| Collector readiness | `CollectorReadinessPage.tsx` |
| Evidence packet reader | see `api/investigationPacket.ts` |

Truth-label helpers live in `apps/console/src/console/types.ts` (`uiTruth`,
`uiFresh`). The live API adapter is `apps/console/src/api/liveData.ts`.

---

## Archive — original Console v2 handoff instructions

The section below is preserved for historical context only. The agent prompt
and issue-creation instructions are no longer actionable — the referenced
issues are closed or the work is shipped.

<details>
<summary>Original Console v2 handoff (archived — do not act on)</summary>

The original `docs/console-handoff.md` (before this refresh) contained:

- A prompt for an agent to create GitHub issues in the `eshu-hq/eshu` repo
  under the milestone "Console v2 (native)". Those issues were either created
  and closed, or the work shipped directly without separate issues.
- Specs for backend API endpoints (`file-tree`, `file-content`, `branches`,
  `timeseries`, `vulnerability detail`, `blast-radius`) — all now shipped (see
  table above).
- Specs for frontend pages (`demo/mock removal`, `CVE detail`, `service
  spotlight drill-downs`, `repository browser`, `repo code viewer`, `graph
  explorer`, `dashboard real metrics`) — all now shipped (see table above).

The milestone "Console v2 (native)" may or may not exist on GitHub; if it
does, its open items have been superseded by the issues in the
[Current open work](#current-open-work) section above.

</details>
