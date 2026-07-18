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
npm run console:i18n:check
npm run console:typecheck
npm run console:build
npm run console:bundle-report
```

`console:build` runs the Vite build and then enforces the documented bundle
budget against the emitted chunks.
`console:i18n:check` validates that shell message references resolve against the
default English catalog.
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
before the app boots and starts the console Vite dev server, which owns the
`/eshu-api` proxy. By default it retrieves the one-time initial credential,
claims a fresh identity surface through the real setup wizard, and runs every
catalogued route and action with the resulting HttpOnly browser-session cookie.
The retained facts and graph stay unchanged; operators attach an isolated,
empty auth schema/database to the retained corpus API so the wizard cannot
overwrite a long-lived user's identity. Authoritative comparator reads use the
same browser session, not a hidden bearer key. The runner writes durable proof
under the gitignored
`e2e-artifacts/console-live-e2e/<ESHU_E2E_PROOF_ID>/` directory (per-route
screenshots and a JSON report). Each proof owns and cleans only that directory,
so concurrent runs with distinct proof IDs cannot overwrite one another. It
never falls back to mocks: a refused proxy, a demo banner, a console error, or
any unexpected non-2xx fails the run loudly.

The JSON report includes a cold-bootstrap and per-navigation performance packet:
route-owned first-useful-content and API-quiet readiness time, request count,
exact duplicate groups, encoded transfer bytes, concurrency/ordering, slowest
request, TTFB, download time, and the post-response interval before useful
content. Request URLs are reduced to endpoint shapes and query-key names;
dynamic repository, service, incident, and vulnerability identifiers are
redacted. Headers, query values, bodies, cookies, and credentials are never
written. These diagnostics attribute latency but do not change the route verdict.

Code Graph uses the authenticated session repository catalog plus a bounded,
authorized `POST /api/v0/code/structure/inventory` entity page. Dead-code
findings remain a derived overlay and never define or replace repository scope.
Its canonical deep link is
`/code-graph?repo_id=<canonical-id>&entity_id=<optional-anchor>`; valid legacy
candidate links are canonicalized, while invalid explicit values remain visibly
unavailable. Refresh and browser history restore the URL-owned selection, and a
repository switch clears prior graph state through loading, error, retry, empty,
and stale-response states.

The relationship story supplies six typed edge families, related-node source
metadata, and provenance. Exact reads include canonical `repo_id`, inventory is
capped at 100 entities with explicit truncation, and the independent import-cycle
read remains repository scoped. The page issues no second untyped relationship
request for the same selection.

Semantic Search uses the authenticated repository catalog. Its searchable selector stores the canonical ID in the URL and request while rendering the human label.
Legacy name or slug links canonicalize only for one authorized match; ambiguous or unavailable values remain explicit and never run an unscoped or false-empty search.

Playwright trace capture is default-off because authenticated traces can retain bearer request headers. For a short-lived, locally protected debugging artifact, opt in with
`ESHU_CONSOLE_E2E_TRACE=1 npm run console:e2e`; delete `e2e-artifacts/console-live-e2e/<ESHU_E2E_PROOF_ID>/trace.zip` immediately after use and never attach it to an issue or commit it.

### Exact local command sequence

```bash
# 1. Keep the retained Compose project running, then install browser dependencies.
npm ci
npx playwright install chromium

# 2. Run the retained-corpus lifecycle helper. It builds an API image from the
#    exact current Dockerfile/Go/SDK manifest, creates a unique empty auth schema,
#    starts one API sidecar on the retained Postgres/NornicDB stores, drives the
#    wizard and all routes, verifies public identity rows were untouched, then
#    removes only its sidecar and proof schema.
ESHU_E2E_RETAINED_PROJECT=eshu \
ESHU_E2E_COMPOSE_ENV_FILE=e2e-artifacts/.env.console-e2e \
ESHU_E2E_RETAINED_API_PORT=18086 \
ESHU_E2E_CONSOLE_PORT=5182 \
ESHU_E2E_WIZARD_NEW_PASSWORD="$LOCAL_PROOF_PASSWORD" \
ESHU_E2E_CORPUS_ATTESTATION="$CORPUS_ATTESTATION" \
ESHU_E2E_CORPUS_REPOSITORY_COUNT="$CORPUS_REPOSITORY_COUNT" \
ESHU_E2E_INCIDENT_ID="$INCIDENT_ID" \
ESHU_E2E_SERVICE_NAME="$SERVICE_NAME" \
ESHU_E2E_SECRETS_SCOPE_ID="$SECRETS_SCOPE_ID" \
ESHU_E2E_CLOUD_SCOPE_ID="$CLOUD_SCOPE_ID" \
ESHU_E2E_AWS_SCOPE_ID="$AWS_SCOPE_ID" \
ESHU_E2E_SEMANTIC_REPOSITORY_ID="$SEMANTIC_REPOSITORY_ID" \
ESHU_E2E_SEMANTIC_QUERY="$SEMANTIC_QUERY" \
scripts/run-console-retained-e2e.sh

# Set ESHU_KEEP_RETAINED_PROOF=true only when an operator needs the isolated
# sidecar/schema left running for hands-on evidence. The retained stack itself
# is never stopped by this helper.
```

Use a distinct `ESHU_E2E_RETAINED_PROOF_ID`, API port, and
`ESHU_E2E_CONSOLE_PORT` for concurrent proof runs. The console port defaults to
`5180`; override it when that port is serving a retained hands-on evidence site.
The helper validates the identifier, binds the sidecar and credential CLI
to the same schema-first `search_path`, and refuses to reuse an existing proof
container. The browser runner scopes screenshots, trace, and JSON report to the
same proof ID. Cleanup never acts on another proof, the retained Compose project,
or its volumes.

The runner reads its browser-session inputs and `ESHU_E2E_API_BASE` from the
process environment. Compose env files are Compose input and are never sourced
as shell programs; export the small required browser-runner input set explicitly.
For a bounded diagnostic, `ESHU_E2E_ROUTE_PATHS` accepts comma-separated exact
eligible paths such as `/relationships,/code-graph`. Requested order is
preserved, including repeated paths, so `/code-graph,/catalog,/code-graph`
captures cold and warm navigation in one authenticated session. Unknown,
ineligible, or empty selections fail closed. Omit the variable for the complete
auth-eligible route catalog; a filtered run is diagnostic evidence, not a
replacement for the full acceptance gate.
`ESHU_E2E_AUTH_MODE=bearer` plus `ESHU_E2E_API_KEY` remains available only for
bounded operator diagnostics; that mode excludes Profile/Admin and does not
satisfy the retained browser-session acceptance gate. Credentials are never
hard-coded. Artifacts in `e2e-artifacts/` are local proof only and are
gitignored — never commit screenshots, traces, credentials, or local keys.

Every durable report also requires a non-secret proof manifest supplied through
`ESHU_E2E_PROOF_ID`, `ESHU_E2E_SOURCE_HASH`, `ESHU_E2E_RUNNER_HASH`,
`ESHU_E2E_API_IMAGE_DIGEST`, `ESHU_E2E_API_VERSION`,
`ESHU_E2E_NORNIC_IMAGE_DIGEST`, `ESHU_E2E_NORNIC_VERSION`,
`ESHU_E2E_NODE_VERSION`, `ESHU_E2E_PLAYWRIGHT_VERSION`, the launched browser
version, `ESHU_E2E_CORPUS_ATTESTATION`, and
`ESHU_E2E_CORPUS_REPOSITORY_COUNT`. The retained helper derives the runtime
versions and records them with a runner hash that includes the root dependency
manifest and lockfile. It never writes keys, passwords, or encryption secrets
into the report. The runner validates the declared repository count against the
same-run authoritative inventory. The corpus attestation is an operator-supplied
label and is reported explicitly as not authoritatively validated; it is not a
derived corpus identity. `ESHU_E2E_CORPUS_IDENTITY` remains a deprecated fallback
when the attestation variable is unset.

Retained-corpus runs also require real, local-only anchors for workflows that
cannot discover a bounded target through the public UI: `ESHU_E2E_INCIDENT_ID`,
`ESHU_E2E_SERVICE_NAME`, `ESHU_E2E_SECRETS_SCOPE_ID`, and
`ESHU_E2E_CLOUD_SCOPE_ID`. The Cloud Drift four-surface proof additionally
requires a real retained AWS scope in `ESHU_E2E_AWS_SCOPE_ID`. Semantic Search
requires a repository and query that produce retained results in
`ESHU_E2E_SEMANTIC_REPOSITORY_ID` and `ESHU_E2E_SEMANTIC_QUERY`; a successful
empty response does not satisfy that workflow. Do not commit their values. The
runner fails closed
when an anchor is missing, when a parameterized route cannot be reached from a
real retained link, or when a route renders neither its authoritative data
shape nor its exact documented empty state. Vulnerability detail coverage
therefore requires a real advisory collector result; a fabricated advisory ID
or generic page shell does not satisfy the gate.

## Browser-Auth E2E Gate

`npm run console:e2e:auth` is the exhaustive identity-policy companion to the
retained browser-session route gate above. It proves the FIRST-RUN auth flow
itself (issue #4971 phase 2, epic #4962
closer) — the setup wizard, session-cookie login, and the `require_sso`
guardrail — rather than route rendering against an already-provisioned
corpus stack. It additionally exercises OIDC, SSO guardrails, MFA, and leakage
contracts against its own empty stack.

Unlike `console:e2e`, this gate owns its own Compose stack lifecycle end to
end (`scripts/run-auth-e2e.sh`): it always brings up a fresh
`docker-compose.e2e.yaml` stack (isolated Compose project
`eshu-e2e-auth` by default, so it never touches a developer's own
manually-started `eshu-e2e` stack) and always tears it down with `down -v`,
because the acceptance items only mean something starting from zero local
identities — reusing a long-lived stack could mask a real "dead-end login
form" regression behind a leftover admin session. Set
`ESHU_KEEP_COMPOSE_STACK=true` to skip teardown for debugging.

The runner (`e2e/runAuthE2E.ts`) reuses the same console-reachability
approach as `runConsoleLiveE2E.ts`: the host Vite dev server, proxying
`/eshu-api` to the stack's host-mapped API port via `ESHU_DEV_PROXY_TARGET`.
See that file's header comment for the full acceptance-item breakdown. The
gate now covers all six acceptance items on a fresh stack: first-run wizard,
generated-credential claim/consumption, OIDC provider add/test/enable through
the real UI, non-admin SSO login with `/admin` 403 gating, `require_sso`
enforcement with local break-glass, and the negative-secret-leakage scan
(`e2e/authE2ELeakage.ts`).

The `item5_guardrail_rejects_premature_enable` step now passes: it asserts the
guardrail correctly rejects (400) enabling `require_sso` before a provider has
a passing test and an admin has completed an SSO sign-in. (Its earlier
failure caught a real, pre-existing gap — missing scoped-route allowlist
entries for the sign-in-policy admin routes — which was fixed in #5004/#5006;
the E2E likewise surfaced and drove fixes for several other shipped bugs.)

```bash
npm run console:e2e:auth
```

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

### Dead Code count and filter contract

The Dead Code route treats `POST /api/v0/code/dead-code` as a bounded candidate
window, not a corpus-wide aggregate. Its candidate, repository, estimated-LOC,
kind, and confidence counts describe only the rows returned by the current
response. The repository breakdown is an on-demand view of that same window;
it must not be labeled as a complete affected-repository inventory when the
response is truncated.

Keep `display_truncated` and `candidate_scan_truncated` visible as separate
conditions. The first means the filtered display was clipped at the requested
limit; the second means the bounded raw candidate scan stopped before exhausting
the selected labels. The combined legacy `truncated` field does not erase that
distinction.

Repository choices reuse the authenticated session-owned repository catalog and
show both a friendly name and canonical identifier. Language choices combine
the response's advertised `analysis.dead_code_language_maturity` keys with
languages observed in the returned rows. Selecting a supported candidate kind
must issue the exact server-side `candidate_kind`; resetting to all kinds must
remove it.

Only private/live mode owns the bounded Dead Code POST and its repository and
language selectors. Demo mode renders its model fixtures without issuing that
live request or exposing server-only selectors.

Keep the Dead Code route shell eager so navigation can render useful content
without waiting for a route-level module hop. The repository coverage tile,
hidden breakdown implementation, and candidate-row renderer may remain small
lazy chunks; mount the hidden breakdown boundary during the initial render so
opening it is synchronous after the route becomes interactive. This boundary
keeps the main console bundle within its enforced budget without moving the
whole route behind a loading screen.

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
Repository mode uses the caller-authorized repository catalog as a searchable
canonical selector. Selecting a new repository immediately clears prior-scope
evidence, drops obsolete scope/baseline URL state, and performs one bounded
generation lookup before loading the new comparison. File samples show a human
path first while retaining the raw stable fact key as diagnostic detail.

## Service Evidence Graph

The Service Story page (`/service-story`, `/service-story/:serviceName`) renders
the bounded `service_story` visualization packet as an interactive code-to-cloud
graph. It is purely source-backed:

- It fetches `GET /api/v0/services/{service_name}/story`, then derives the packet
  with `POST /api/v0/visualizations/derive` (`view: "service_story"`). The derive
  route is a side-effect-free transformation, so the console performs no
  client-side graph synthesis.
- Node types, source-proven roles, categories, relationships, and truth labels
  come only from the packet. The workload is the sole service anchor; source,
  deployment-configuration, proven runtime-instance, and downstream roles use separate
  lanes. Missing collector lanes are never backfilled with invented nodes.
- Repository observations reconcile only through the packet's privacy-safe
  canonical repository key, never by label. When canonical identity is absent,
  role and hashed scope disambiguation remain visible. Reconciled observations
  retain every role, hashed scope, and evidence handle in the inline evidence
  panel; the graph uses the deterministic highest-priority role for layout.
- Relationship rows lead with human endpoint labels, roles, and a readable verb; opaque `viznode:*` endpoint IDs remain secondary diagnostic detail.
- `limits` and `truncation` stay visible: a truncated subgraph shows known
  dropped node/edge counts, and uses a count-free bounded-subset message when
  the source did not expose exact counts.
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
