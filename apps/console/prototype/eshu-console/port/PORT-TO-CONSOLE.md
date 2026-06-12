# Porting the redesigned console into `apps/console` + live API

This package contains the redesigned Eshu console (dark graph-first UI), a
standalone demo dataset, and a live-loader shim that maps the **real Eshu HTTP
API (`/api/v0/*`)** into the same prototype view-models. Two ways to run it
against your live stack.

---

## The one hard constraint: serving origin

A browser can only call the Eshu API without CORS when the page is **served from
an origin that proxies to it**. Your `apps/console` already does this — `vite.config.ts`
proxies `"/eshu-api" → http://127.0.0.1:8080`. So the page must be served by that
dev server (or any host that proxies `/eshu-api/`). Opening the HTML from `file://`
or a sandbox preview cannot reach `localhost:8080` (mixed-content + CORS + opaque origin).

The MCP endpoint (`:8081/sse`) is the **assistant** protocol (JSON-RPC/SSE) and is
not the surface a dashboard should consume — use the HTTP API on `:8080`.

---

## Path A — run the prototype behind the existing proxy (fastest, ~2 min)

1. Copy these into the console's static dir:
   ```
   cp "Eshu Console.html"  apps/console/public/index-console.html
   cp -r console            apps/console/public/console
   cp -r assets             apps/console/public/assets
   ```
2. Start the console dev server (it proxies `/eshu-api/`):
   ```
   npm run --prefix apps/console dev
   ```
3. Open `http://localhost:5174/index-console.html`.
4. Click the **Demo data** pill (top-right) → **Live Eshu API** → base `/eshu-api/`,
   paste your **API key**, **Connect**.
   - The key lives only in memory for the current session. The prototype stores
     the recent API base URL in `localStorage` (`eshu.console.environment`), but
     never persists the API key or writes it to source.
5. Live sections hydrate from the graph when the API endpoint is available;
   panels without live rows render explicit empty/unavailable states.

The client + mappers already live in `console/data.js` (`ESHU.EshuApiClient`,
`ESHU.loadLive`). Endpoints used:

| Console panel        | Endpoint |
| -------------------- | -------- |
| Dashboard / Ops stats| `GET /api/v0/ecosystem/overview`, `GET /api/v0/index-status`, `GET /api/v0/metrics/timeseries` |
| Repositories         | `GET /api/v0/repositories`, `GET /api/v0/repositories/{id}/stats`, `GET /api/v0/repositories/{id}/story` |
| Repository source    | `GET /api/v0/repositories/{id}/tree`, `GET /api/v0/repositories/{id}/content?path=`, `GET /api/v0/repositories/{id}/branches` |
| Catalog              | `GET /api/v0/catalog?limit=2000` |
| Language chart       | `GET /api/v0/repositories/language-inventory` |
| Collectors           | `GET /api/v0/status/ingesters` |
| Findings             | `POST /api/v0/code/dead-code` |
| Code Graph           | `POST /api/v0/code/dead-code`, `POST /api/v0/code/relationships` |
| Graph Explorer       | `POST /api/v0/entities/resolve`, `POST /api/v0/code/relationships`, `GET /api/v0/services/{name}/context`, `GET /api/v0/repositories/{id}/context`, `POST /api/v0/impact/entity-map` |
| Vulnerabilities      | `GET /api/v0/supply-chain/impact/findings`, `GET /api/v0/supply-chain/advisories`, `GET /api/v0/supply-chain/vulnerabilities/{id}` |
| Images               | `GET /api/v0/images` |
| IaC                  | `GET /api/v0/iac/resources` |
| SBOM                 | `GET /api/v0/supply-chain/sbom-attestations/attachments/count`, `GET /api/v0/supply-chain/sbom-attestations/attachments/inventory` |
| Dependencies         | `GET /api/v0/dependencies` |
| Observability        | `GET /api/v0/observability/coverage/correlations?provider=` |
| Topology             | `GET /api/v0/services/{name}/story`, `GET /api/v0/services/{name}/context` |
| Cloud resources      | `GET /api/v0/cloud/resources` |
| Cloud canonical inventory | `GET /api/v0/cloud/inventory` |

---

## Path B — keep the production console aligned

Use the production loaders in `apps/console/src/api/` as the current contract.
They are stricter and broader than the historical `port/eshuConsoleLive.ts`
seed: they include Images, IaC, SBOM, Dependencies, advisories, observability
coverage, metrics series, and repo/source drilldowns. When a prototype surface
is added, update both:

1. **Live console API layer.** Add or adjust typed loaders under
   `apps/console/src/api/`, with tests next to the loader and page. Do not copy
   `port/eshuConsoleLive.ts` into production as the full adapter.

2. **Prototype live loader.** Update `console/live-parity-loader.js` so the
   standalone prototype can hydrate the same section when pointed at `/eshu-api/`.
   Keep API keys session-only.

3. **Visual system.** `console/styles.css` is framework-agnostic CSS (tokens +
   component classes). Drop it in and replace the current console CSS, or port the
   `:root` token block first and migrate panel-by-panel.

4. **Components.** The pages in `console/*.jsx` are plain React (no app-specific
   deps). Keep their public route hashes aligned with `apps/console/src/App.tsx`
   via `console/routes.js` so design-tool flows match live console URLs.

5. **Truth mapping.** UI chips expect `exact | derived | inferred`; the API emits
   `fallback` where the prototype shows `inferred`. `chipTruth()` in `data.js`
   is the prototype mapping; the live TypeScript loaders preserve `TruthLevel`
   and section provenance.

---

## What stays representative until wired

- **Repository branch selection** — source browsing uses
  `GET /api/v0/repositories/{id}/tree`, `/content?path=`, and `/branches`.
  The current `/branches` route exposes the derived indexed commit ref; true
  multi-branch selection remains gated on ingested Git ref names.
- **Time-series gaps** — dashboard and Operations trend lines hydrate from
  `GET /api/v0/metrics/timeseries` when a metrics source is configured. The
  live/prototype loaders use the supported `ingest_rate`, `queue_depth`,
  `dead_letters`, `graph_nodes`, `graph_edges`, `query_p50`, `query_p95`, and
  `query_p99` series. Prototype sparklines without a live metric name, such as
  write-throughput, cache-hit, and vulnerability-feed intake decoration, stay
  demo-mode only; connected live Operations renders explicit contract-pending states
  until issue #2216 defines named live contracts.
- **Vuln CVSS/EPSS/KEV detail** depends on the vulnerability-intelligence collector
  being enabled and scheduled with bounded source targets. Keep the UI empty/limited
  instead of fabricating rows until issue #2217 proves the live runtime collector path
  through `supply-chain/advisories` and `supply-chain/vulnerabilities/{id}`.

---

## Security note

The API key is a bearer credential. It is entered at runtime into the data-source
panel and kept only in memory for the current session. The browser may persist
the recent API base URL in `localStorage` (`eshu.console.environment`), but the
API key is **not** persisted there and is not committed to any file in this
package. Rotate it if it has been shared in plaintext anywhere.
