# Eshu Console

`apps/console` is the private read-only product console for Eshu. It is
separate from the root Cloudflare Pages homepage.

The console is role-neutral at the front door: search for a repository, service,
or workload, then open an entity workspace with story, evidence, deployment,
code, findings, and freshness context.

## Product Contract

Eshu Console turns Eshu's code-to-cloud graph into a readable operating
surface. It serves engineers, platform teams, SREs, support, directors,
executives, and finance-adjacent stakeholders who need the same graph truth at
different depths.

The console should help users answer:

- what a repository, service, or workload does
- what deploys it, where it runs, and what depends on it
- what evidence supports each relationship
- whether indexing is healthy, stale, partial, or blocked
- which findings need action
- which replatforming/import-plan candidates are ready, refused, or missing
  ownership evidence
- what changed between retained repository or service generations
- who can reach secret metadata through reducer-owned Secrets/IAM posture facts,
  and which trust-chain gaps keep access provenance-only
- what is known, missing, inferred, or stale
- which cloud resources are unmanaged, drifting, or blocked from safe import

Important claims need a plain-language summary first and a clear path to the
underlying evidence.

## Design Contract

The console is a working product surface, not a homepage or generic dashboard.
Keep it bright, readable, and dense enough for repeated operational use. Use
color for relationship type, status, freshness, risk, and selection; never use
color as decoration or the only state indicator.

Design rules:

- show the story and the proof together
- keep graph labels and table rows readable before adding visual polish
- preserve truth, freshness, missing-evidence, and limitation states in text
- avoid dark-card sameness, decorative gradients, glass effects, nested cards,
  modal-first drilldowns, and mock-data language on real-data surfaces
- keep non-graph summaries available when graph views are not the fastest way
  to understand the evidence

## Modes

### Demo mode

Demo mode uses typed fixtures only. It is explicit, not the local default. Use
it for public demos, screenshots, and development when no Eshu API is running.

Demo mode must not imply that public users can browse real Eshu data.

### Private real-data mode

Private mode points the console at an Eshu API base URL, such as a local Compose
stack or an internal deployment.

The local development default is `/eshu-api/`, which the console Vite server
proxies to `http://127.0.0.1:8080`. Start a local Eshu API before opening the
console if you want real data. The proxy target is overridable with the
`ESHU_DEV_PROXY_TARGET` environment variable (for example, to point at a stack
bound to a non-default host port without editing `vite.config.ts`).

Until Eshu has real auth and authorization, real-data console deployments should
stay local or inside a trusted private network. The console is read-only, but
the data can still expose repositories, services, infrastructure, docs, runtime
state, findings, and security posture.

No mutating controls belong in this app until auth, audit logging, confirmation,
and role policy exist.

## Local Development

From the repository root:

```bash
npm run console:test
npm run console:typecheck
npm run console:build
npm run console:bundle-report
```

`console:build` runs the Vite build and then enforces the documented bundle
budget against the emitted chunks.
`console:bundle-report` prints the post-build bundle composition table used in
CI logs; `first-load?` marks the main entry and eager vendor chunks as `yes` and
route/diagram async chunks as `no`.

Run the console dev server:

```bash
npm run --prefix apps/console dev
```

The root helper scripts run against the console Vite config while sharing the
repository lockfile and dependency install.

## Live E2E Gate

`npm run console:e2e` is a browser-level gate that drives the PRIVATE/LIVE
console against the real local Docker Compose stack and proves every major route
renders real data or an explicit empty/unavailable state with **no demo
fallback, no unhandled browser console errors, and no unexpected failed network
requests**. It is the integration counterpart to the unit tests: the pass/fail
decision logic is the unit-tested pure module `src/e2e/routeAssertions.ts`, and
the Playwright runner `e2e/runConsoleLiveE2E.ts` only captures browser signals
and feeds them to that evaluator.

The TypeScript runner is loaded through Vite's SSR transformer by the
`scripts/console-live-e2e-runtime.mjs` bootstrap, so the gate runs on the repo's
supported Node range without relying on native Node TypeScript stripping
(default only on Node >= 23.6) or an extra dependency such as `tsx`. The runner
itself is type-checked Docker-free as part of `npm run console:typecheck` (which
chains `console:e2e:typecheck`).

The gate seeds `localStorage` with `{ mode: "private", apiBaseUrl: "/eshu-api/" }`
before the app boots, starts the console Vite dev server (which owns the
`/eshu-api` proxy) with `VITE_ESHU_API_KEY` so the console authenticates, walks
the routes enumerated from the router, and writes durable proof to the
gitignored `e2e-artifacts/` directory (per-route screenshots, a Playwright
trace, and a JSON report). It never falls back to mocks: a refused proxy, a
demo banner, a console error, or any unexpected non-2xx fails the run loudly.

### Exact local command sequence

```bash
# 1. From the repo root, bring up (or reuse) the local stack. The gitignored
#    env file pins a local API key and shifts host ports so the gate can coexist
#    with another stack on the default ports. Create it once (see the example in
#    e2e-artifacts/.env.console-e2e committed to your local machine only):
#      ESHU_API_KEY=<local-only-key>
#      ESHU_HTTP_PORT=9080
#      ESHU_MCP_PORT=9081
#      ESHU_POSTGRES_PORT=15433
#      NEO4J_HTTP_PORT=7475
#      NEO4J_BOLT_PORT=7688
#      ESHU_API_METRICS_PORT=19474   # plus the other *_METRICS_PORT shifts
#      ESHU_FILESYSTEM_HOST_ROOT=./tests/fixtures/ecosystems
docker compose -p eshu-3326-e2e --env-file e2e-artifacts/.env.console-e2e \
  -f docker-compose.yaml up --build -d

# 2. Wait for the stack to be ready (poll health/readiness; do not sleep blindly).
#    The npm script also probes /healthz and /readyz before launching the browser.
for ep in /healthz /readyz; do
  until [ "$(curl -sS -m3 -o /dev/null -w '%{http_code}' http://127.0.0.1:9080$ep)" = "200" ]; do
    sleep 2
  done
done

# 3. Install deps and the browser, then run the standard gates plus the live gate.
npm ci
npx playwright install chromium
npm run console:typecheck
npm run console:test          # includes the route-assertion evaluator unit tests
npm run console:build
npm run console:e2e           # the live browser gate; exits non-zero on any failure

# 4. Tear the stack down when finished.
docker compose -p eshu-3326-e2e --env-file e2e-artifacts/.env.console-e2e \
  -f docker-compose.yaml down -v
```

If your stack runs on the default ports (`8080`/`8081`) with a known API key,
omit the port overrides and set `ESHU_E2E_API_KEY` (or `ESHU_API_KEY`) and
`ESHU_E2E_API_BASE` for the gate instead. The gate does not manage Docker; the
stack lifecycle stays explicit so a long-lived stack can be reused across runs.

The runner reads `ESHU_E2E_API_KEY`/`ESHU_API_KEY` and `ESHU_E2E_API_BASE` from
the environment (the wrapper sources `e2e-artifacts/.env.console-e2e`); the key
is never hard-coded. Artifacts in `e2e-artifacts/` are local proof only and are
gitignored — never commit screenshots, traces, or the local key.

## API Contract

The console treats `application/eshu.envelope+json` as the canonical response
contract. Screen code must preserve:

- `truth.level`
- `truth.profile`
- `truth.freshness.state`
- structured `error.code`
- limits, truncation, and unsupported-capability states when an API returns
  them
- Secrets/IAM posture routes as reducer read models even while graph projection
  remains gated/default-off

Do not flatten truth and freshness into generic loading or error states.

Cloud drift surfaces use bounded POST readbacks:

- `POST /api/v0/cloud/runtime-drift/findings`
- `POST /api/v0/aws/runtime-drift/findings`
- `POST /api/v0/iac/unmanaged-resources`
- `POST /api/v0/iac/management-status/explain`
- `POST /api/v0/iac/terraform-import-plan/candidates`

The console must render safety gates, missing evidence, pagination, and refused
candidate state as read-only context. It must not emit Terraform HCL, run
Terraform, import resources, or mutate cloud state.

The Changed Since console surface is backed by:

- `GET /api/v0/freshness/changed-since`
- `GET /api/v0/freshness/services/changed-since`
- `GET /api/v0/freshness/generations`

It must keep retained-window limits visible. When the API returns
`unavailable=true` or `unavailable_reason=retention_expired`, the page must show
that unavailable state instead of treating the result as an empty diff.

## Service Evidence Graph

The Service Story page (`/service-story`, `/service-story/:serviceName`) renders
the bounded `service_story` visualization packet as an interactive code-to-cloud
graph. It is purely source-backed:

- It fetches `GET /api/v0/services/{service_name}/story`, then derives the packet
  with `POST /api/v0/visualizations/derive` (`view: "service_story"`). The derive
  route is a side-effect-free transformation, so the console performs no
  client-side graph synthesis.
- Node types, categories, relationships, and truth labels come only from the
  packet. The legend reflects the node types actually present; missing collector
  lanes are never backfilled with invented nodes.
- `limits` and `truncation` stay visible: a truncated subgraph shows the dropped
  node/edge counts so a bounded subset is never read as the full picture.
- Empty, unsupported, partial, and error states are first-class UI. The page
  never renders a stale graph when the story or derive route fails.
- Selecting a node or an evidence-lane relationship pill opens the shared inline
  **evidence panel** (`EvidencePanel`, see below) that shows the truth label,
  packet truth basis/level/freshness, joined facts, the source evidence handle
  (with a link into repository source when present), and limitations. Missing
  optional fields render an explicit "not provided" state and unknown truth
  labels render literally, so the panel never hides uncertainty or collapses on
  partial data. It is keyboard-closable (Escape) and focuses its close control on
  open.

See `docs/public/reference/visualization-packets.md` for the packet contract.

## Evidence Panel Primitive

`EvidencePanel` (`src/components/EvidencePanel.tsx`) is the reusable, inline
evidence-panel primitive behind the console-wide "everything clickable reveals
its evidence" pattern. Any clickable element — a graph node or edge, a service
story evidence-lane pill, a stat tile, or a table row — maps its facts into the
packet-agnostic `EvidencePanelData` contract and renders the panel in-flow next
to the element, with no modal scrim, so the operator keeps page context.

The contract is intentionally decoupled from any single API shape so the same
primitive backs many surfaces:

- `title` / `kindLabel` — the element identity and what was selected.
- `truthLabel` — the per-element truth signal. Known labels (`exact`, `derived`,
  `fallback`) render as a colored Truth chip; unknown labels render literally so
  uncertainty is never normalized away. `fallback` maps to the console
  `inferred` vocabulary.
- `truth` — the envelope-level basis/level/freshness, or `null` when the source
  returned none (rendered as an explicit unavailable state).
- `facts` / `sections` — joined label/value rows; empty values are dropped rather
  than rendered as blank rows.
- `evidence` / `limitations` — supporting evidence and bounded-subset caveats.
- `sourceHref` / `sourceLabel` — an optional deep link into indexed source.

Per-surface mappers keep pages thin and testable:
`visualizationEvidencePanelData` maps a `VisualizationPacket` node/edge selection
(Service Story), and `graphNodeEvidencePanelData` / `graphEdgeEvidencePanelData`
map `GraphModel` nodes/edges (Graph Explorer). New adopters add a small mapper to
this contract rather than coupling the panel to their data shape. Each evidence
fetch is bounded and source-backed, so opening a panel stays within the
few-seconds interaction budget.

## Service Intelligence Report

The Service Report page (`/service-report`, `/service-report/:serviceName`)
renders the service investigation packet from
`GET /api/v0/investigations/services/{service_name}` in report mode: coverage
state (complete/partial/unknown), evidence families, findings, repository scope,
and suggested investigations.

- Complete, partial, unsupported, empty, stale, and API-failure states are all
  preserved as visible UI; the page never shows stale report content on error.
- Suggested investigations are clickable **only** when the backing tool maps to a
  console destination with valid inputs (e.g. `get_service_story` → the evidence
  graph for the same service). Tools with no console destination render as plain
  text so the operator still sees the recommendation without a dead link.
- The page links into the evidence graph view for the same service.

## Ask Eshu

The Ask Eshu page (`/ask`) is the natural-language Q&A surface over the
code-to-cloud graph. It fronts `POST /api/v0/ask`: the user asks in plain
language, the backend runs the bounded agent loop, and the page renders an
evidence-backed answer — prose, a Mermaid diagram, or an exported artifact
(JSON/YAML/CSV/Markdown).

- The client (`src/api/askEshu.ts`) defaults to the SSE variant
  (`Accept: text/event-stream`) so `trace` steps stream into a live reasoning
  timeline; it falls back to the synchronous JSON path and supports cancel via an
  `AbortController`. Normalization lives in `src/api/askEshuNormalize.ts`.
- Components live in `src/components/ask/`: `AskInput`, `ReasoningTrace`,
  `AnswerView`, `ArtifactCard`, `TruthBadge`, `EvidenceList`, and the
  empty/error states. Mermaid is **lazy-loaded** (dynamic `import`) and rendered
  with `securityLevel: "strict"`, falling back to the diagram source on failure.
- Every answer **leads with the truth-class label and evidence**, not just
  prose. When narration is off the answer is evidence-only (`answer_prose` is
  empty); the page presents the trace, artifacts, and limitations and never shows
  partial results as complete.
- States are first-class: streaming, success, partial, evidence-only, disabled
  (503 / narration disabled), bad request (400), network abort, and demo mode
  (no live engine). The capability probe is
  `GET /api/v0/status/answer-narration`.
- Ask accepts both the **shared/admin** token and **scoped tokens**. A scoped
  caller's answer is bounded to its grant: the engine's in-process runner
  re-dispatches every inner tool call through the same scoped-route gate, so the
  model can only reach routes that are themselves scope-safe. A tool mapped to a
  non-allowlisted route returns 403 to the runner and surfaces as an unsupported
  tool in the answer — never as cross-scope data. Unauthenticated requests
  (no valid token) receive 401. No customer or workspace identity is baked into
  the example prompts.

## Related Docs

- `docs/public/reference/http-api.md`
- `docs/public/reference/truth-label-protocol.md`
- `docs/public/reference/visualization-packets.md`
- `docs/public/guides/visualization.md`
