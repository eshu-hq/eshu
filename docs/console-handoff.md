# Eshu Console — work handoff for agents

Goal: finish porting the redesigned console into `apps/console/src` (native TSX,
**live by default**), and create the missing backend API endpoints the UI needs.
The console currently hydrates from the live API and falls back to a
clearly-labeled demo fixture only when the API is unreachable; removing that demo
path in favor of explicit loading/empty/error states is tracked below ("console:
remove all demo/mock data; live-only with states"). This doc is split so agents
can create GitHub issues first, then work them.

The reference implementation is the working prototype at
`apps/console/prototype/eshu-console/Eshu Console.html` (+ `console/*.jsx`). It shows
the exact intended UX. The in-progress TSX port lives in `apps/console/src`
(adapter `api/eshuConsoleLive.ts`, `api/eshuGraph.ts`, `api/eshuService.ts`,
pages, components). Port the prototype's behavior onto **real endpoints**; where
an endpoint does not exist yet, see "Backend API" below.

---

## 0) Paste this first — have an agent create the GitHub issues

> You have repo write access and the `gh` CLI. Create GitHub issues in
> `eshu-hq/eshu` from the specs in `docs/console-handoff.md` (this file). Create
> one issue per "### ISSUE:" heading below. Use the heading text as the issue
> title, the body under it as the issue body, and apply the labels listed in each
> spec (create labels if missing: `area:console`, `area:api`, `frontend`,
> `backend`, `blocked-on-api`). Add every issue to a new milestone
> "Console v2 (native)". After creating them, post a summary comment listing the
> issue numbers grouped by Backend vs Frontend. Do not start coding yet.

---

## Backend API — endpoints the console needs (no mock data path)

These don't exist in `/api/v0/*` today (verified against `go/cmd/api` + the CLI
client). Each should return the standard `application/eshu.envelope+json`
envelope (`{ data, error, truth }`) and carry `truth.level` +
`truth.freshness.state` like the existing read endpoints.

### ISSUE: api: add repository file-tree endpoint
Labels: area:api, backend
**Why:** The console's repo browser needs to render a real directory tree (the
prototype's tree is fabricated and must be removed under the no-mock rule).
**Proposed contract:**
`GET /api/v0/repositories/{id}/tree?ref={branch}&path={subpath}`
→ `data: { ref, path, entries: [{ name, type: "dir"|"file", path, size?, child_count? }] }`
**Acceptance:** lists one directory level (or full subtree with `?recursive=true`);
404 envelope for unknown repo/path; truth reflects index freshness for that repo.

### ISSUE: api: add repository file-content endpoint
Labels: area:api, backend
**Why:** The repo code viewer must show real source; there is currently no
file-content/blob endpoint, so the viewer is stubbed behind a "requires content
API" state (see frontend issue).
**Proposed contract:**
`GET /api/v0/repositories/{id}/content?path={filepath}&ref={branch}`
→ `data: { path, ref, encoding: "utf-8"|"base64", content, size, language?, truncated? }`
**Acceptance:** returns file bytes for an indexed path; size cap + `truncated`
flag for large files; 404 envelope for missing path; never returns secrets that
the collectors redact.

### SHIPPED: api: repository branches/refs endpoint
Labels: area:api, backend
**Why:** The code viewer's branch selector uses source-backed refs when
ingestion captured them, and keeps the indexed-ref-only state when branch
metadata is unavailable.
**Contract:** `GET /api/v0/repositories/{id}/branches`
→ `data: { default_branch, branches: [{ name, kind, is_default, last_indexed_at, head_sha }] }`

### SHIPPED: api: historical time-series metrics endpoint
Labels: area:api, backend
**Why:** Dashboard/Operations sparklines + trend charts use real history for
ingestion throughput, queue depth, graph growth, and query latency p50/p95/p99.
**Contract:**
`GET /api/v0/metrics/timeseries?metric={name}&window={e.g.24h}&step={e.g.30m}`
where `metric ∈ { ingest_rate, queue_depth, dead_letters, graph_nodes, graph_edges, query_p50, query_p95, query_p99 }`
→ `data: { metric, unit, points: [{ t: ISO8601, v: number }] }`
**Note:** backed by the configured Prometheus/Mimir collector target when
available.
**Acceptance:** returns ordered points for the window; empty `points` (not an
error) when the source is not configured or a metric has no history yet.

### ISSUE: api: add single-vulnerability detail endpoint
Labels: area:api, backend
**Why:** The CVE detail page wants the full advisory (description, references,
CWE, CVSS vector, all affected components), not just the row from
`supply-chain/impact/findings`.
**Proposed contract:** `GET /api/v0/supply-chain/vulnerabilities/{advisory_id}`
→ `data: { advisory_id, title, description, severity, cvss, cvss_vector, epss, kev, cwe[], references[], fixed_version, affected: [{ repo_id, service_id, package, version }] }`
**Acceptance:** 404 envelope for unknown id; `affected[]` joins to graph entities
so the UI can link to services/repos.

### ISSUE: api: confirm or add blast-radius endpoint
Labels: area:api, backend
**Why:** The service spotlight's "blast radius" graph needs transitive dependents.
The CLI references `impact/entity-map`; confirm whether
`POST /api/v0/impact/blast-radius` exists. If not, add it.
**Proposed contract:** `POST /api/v0/impact/blast-radius` body
`{ name|selector, max_depth }` → `data: { target, impacted: [{ id, name, type, distance }], edges: [{ source, target, verb }] }`
**Acceptance:** bounded by `max_depth`; returns typed edges so the UI renders the
relationship layers.

---

## Frontend — native TSX port (live-only, no mock)

All under `apps/console/src`. Spec = the prototype in
`apps/console/prototype/eshu-console`. Match its visuals/interactions exactly
(dark graphite/bone/teal/ember theme, the same component classes already in
`src/styles.css`).

### ISSUE: console: remove all demo/mock data; live-only with states
Labels: area:console, frontend
- Delete `src/console/demoModel.ts` usage as a data source. The app must require a
  live connection (it already auto-connects from `eshu.console.environment`).
- Add explicit **loading**, **empty**, and **error** states per page/panel driven
  by the adapter's `provenance` map (`live`/`empty`/`unavailable`).
- No fabricated numbers anywhere. If a datum has no endpoint, render "—" or an
  "API not available" note — never invent it.
- Keep the data-source popover for entering base URL + API key.

### ISSUE: console: CVE detail view wired to the API
Labels: area:console, frontend
- Port `VulnDetail` from the prototype (`console/pages-data.jsx`): full-page CVE
  view with stats (CVSS, EPSS, KEV, package, versions, ecosystem, source) and a
  **centered affected-services graph** (`GraphCanvas`, vuln node `hero`).
- Data: `GET /api/v0/supply-chain/vulnerabilities/{id}` (new) for detail;
  `supply-chain/impact/findings` for the affected list/graph until the detail
  endpoint lands.
- CVE rows in the register + the drawer's expanded CVE link here
  (`#vulnerabilities?cve=...`; the hashchange handler must strip the `?` query —
  see prototype `app.jsx`).

### ISSUE: console: service spotlight drill-downs (drawer)
Labels: area:console, frontend
Port from prototype `ServiceDrawer`:
- **Blast-radius** stat → expands a radial `GraphCanvas` (`POST impact/blast-radius`).
- **Callers/Importers** stat → expands a radial graph (`POST code/relationships`,
  reverse edges).
- **Findings** count → toggles a list; **each CVE row expands** to full detail
  with a "View full vulnerability →" deep-link.
- Severity counts + the findings list must be derived from the **same** real
  source so the number always equals the listed rows (the prototype fixed this).
- **Deployment-path lane** items clickable → expand typed-edge evidence from
  `services/{id}/story`; the importers lane item opens the callers graph.

### ISSUE: console: repository browser on real graph data
Labels: area:console, frontend
- Repo **list** from `GET /api/v0/repositories` (+ `/by-language` for the filter).
- Repo **detail** from `repositories/{id}/stats` + `repositories/{id}/story` +
  `code/relationships` (symbol/relationship graph) + `code/search`.
- **No fabricated file tree or file contents.**

### SHIPPED: console: repo code viewer
Labels: area:console, frontend
- File-tree + code-viewer UI is wired to `repositories/{id}/tree`,
  `/content`, and `/branches`.
- Source-backed branch refs render a selector when multiple named refs are
  available, and selected refs are passed through tree/content reads.
- Indexed-ref-only repositories keep an explicit single-ref state instead of
  fabricated branch names.

### ISSUE: console: graph explorer on entity-map / relationships
Labels: area:console, frontend
- Explorer search → `POST entities/resolve`, then expand via
  `POST code/relationships` (or `POST impact/entity-map` for a broader neighborhood).
- Layer toggles map verbs → the existing layer colors. Node click opens the
  spotlight drawer for service/workload nodes, expands relationships otherwise.

### ISSUE: console: dashboard/operations real metrics (+ time-series when available)
Labels: area:console, frontend
- Stats from `index-status`, `ecosystem/overview`, `status/pipeline`,
  `status/ingesters`.
- Area charts and sparklines load from `GET /api/v0/metrics/timeseries` when
  the API has a configured metrics source with recent samples. Keep the current
  explicit empty states when history is unavailable; never render mock series.

---

## Notes for whoever implements

- Envelope contract + client already exist: `src/api/client.ts`,
  `src/api/envelope.ts`. Reuse `EshuApiClient.get/post`.
- Truth mapping: API `truth.level` is `exact|derived|fallback`; UI shows
  `fallback` as `inferred`. Freshness `building`→`lagging`, `unavailable`→`stale`.
  Keep this in one helper (`uiTruth`/`uiFresh` in `src/console/types.ts`).
- The adapter `src/api/eshuConsoleLive.ts` already maps index-status, catalog,
  repositories, by-language, status/ingesters, code/dead-code, supply-chain
  impact findings, ecosystem/overview — validated against the existing
  `src/api/liveData.ts`. Extend it; don't duplicate.
- Run `npm run console:typecheck` after each issue; the design environment can't
  typecheck, so keep changes compiling.
