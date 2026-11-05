<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# eshu-api read-transport spec (Leaf B, CoreReadContracts v0)

Refs #9669, part of #6707. Status: spec + in-tree conformance skeleton
only — no serving-path behavior change, no cutover, no traffic flip, no
deploy, no removal. Relay-forever is ruled out per the arbiter ruling
(§4/Leaf B): this lane builds the transport path, not a permanent proxy.

## What this is

The exact cross-repo surface the satellite eshu-api repo must implement
so its Native-now wave is byte-and-shape conformant with the in-tree
`eshu` API binary at `CoreReadContractsVersion = "v0"`. The conformance
skeleton is [read_transport_conformance.go](read_transport_conformance.go)
with red-first tests in
[read_transport_conformance_test.go](read_transport_conformance_test.go)
(run with `-tags conformance`; the red is expected until the satellite
probe body lands, and is NOT fixed by editing the test).

Inputs (read-only, authoritative where cited):

- [CUTOVER_OWNERSHIP.md](CUTOVER_OWNERSHIP.md) — served-route source of
  truth (278 route rows, 66 families).
- `go/internal/query/querycontract/api_boundary.go` — the v0 pin
  (`CoreReadContractsVersion = "v0"`; GraphQuery 2 methods, ContentStore
  frozen set in `api_boundary_test.go`; Postgres ReadStore half pinned by
  `TestReadStoreContractIsVersioned` in `storage/postgres/db`).
- `../eshu-9669-census/go/cmd/api/READ_TRANSPORT_CENSUS.md` — per-family
  disposition census (lives in the census worktree; not copied here).

## Contract pin (v0)

The satellite reads core truth only through the versioned ports. Any
breaking port change must bump `CoreReadContractsVersion` in the same
diff; the satellite tracks the pin, never the live port shape:

- `GraphQuery`: `Run`, `RunSingle` (2 methods).
- `ContentStore`: frozen set in `api_boundary_test.go` (35 methods).
- Postgres `ReadStore`: pinned beside the surface it pins.
- Read runtimes display core truth; they never take direct table or queue
  handles. The single bounded exception (authenticated, all-scopes-only
  vulnerability-suppression mutation) stays core-owned behind a versioned
  admission contract — it is NOT part of this read-transport surface.

## Transport rules (apply to every family below)

### Envelope negotiation

- A request carrying `Accept: application/eshu.envelope+json` (constant
  `querycontract.EnvelopeMIMEType`) gets the envelope shape via
  `WriteSuccess` / `WriteErrorEnvelope` / `WriteContractError`:
  `ResponseEnvelope{Data, Truth, Error}` with `TruthEnvelope` and
  `ErrorEnvelope` as defined in `querycontract/truth.go`.
- Without the header, the satellite must serve the legacy plain payload
  (`WriteJSON(data)` on success, `{"error","detail"}` on failure) —
  byte-compatible with the in-tree fallback.
- `TruthEnvelope` fields (`level`, `capability`, `profile`, `basis`,
  `backend`, `freshness`, `reason`, `truncated`, `omissions`) keep exact
  JSON names and `omitempty` behavior; a complete page carries no
  `truncated` key at all.

### Pagination

- Bounded `limit` via `ParseBoundedLimit` semantics: blank means the
  route default; non-integer or outside `[1, max]` is 400 with
  `limit must be an integer in [1, %d]`.
- `offset` via `ParseOffset` semantics: blank means 0; negative or
  non-numeric is 400 with `offset must be a non-negative integer`.
- Pages are offset-ordered with `page_position`; a cut page sets
  `data.next_offset = offset + limit` (`NextOffset`) and the matching
  `truncated` signal; a work-budget cut additionally reports a resume
  cursor with `truncated: true` at `TruthLevelPartial`.

### Capability / profile gating

- Unknown capability for the active profile answers
  `unsupported_capability` with `ErrorProfiles{Current, Required}` via
  `WriteContractError`; the required profile comes from
  `RequiredProfile` (default `ProfileLocalFullStack`).
- Success truth is built by `BuildTruthEnvelope` (capability-matrix
  ceiling + basis normalization); the satellite must not invent levels.

### OpenAPI-fragment parity

- One fragment per route under `go/internal/query/openapi/paths/<family>/`
  (156 files at census time); the satellite spec must carry the same
  paths, operationIds (lowerCamelCase, unique — see
  `openapi_operation_id_convention_test.go`), parameters, and response
  shapes for every Native-now route.
- `apicontract` goldens must match the live types
  (`TestApicontractGoldensMatchLiveTypes`); placeholder refs must not
  answer 503 (`TestOpenAPISpecNotImplementedResponsesAvoidServiceUnavailable`).

### Auth placement

- Auth wraps the FULL mux after the route surface is mounted
  (`mountRuntimeSurface` before `AuthMiddleware` in `wireAPI`); data
  routes are unreachable without auth when a token is configured.
- The public surface stays public: `/healthz`, `/readyz`,
  `/admin/status`, `/metrics` (plus `GET /health` in the wave below).
- Credential kinds: Bearer [REDACTED] (`ResolveAPIKey`; empty token
  means dev-open), scoped tokens, dashboard browser sessions under
  `BrowserSessionRoutePolicy` (zero value is fail-closed).
- Under `hosted_multi_tenant` (or an unrecognized governance mode) an
  all-scope credential is refused with 403 on grant-bound,
  deployment-scoped, and transitive routes.
- Layer order outside-in: deprecation/sunset headers → v1→v0 prefix
  rewrite (before auth, so route classification sees the v0 path) → auth
  → OIDC rate limit → checkpoint → instrumented mux.
- Scoped-route completeness (`auth_scoped_routes_*` tests) must hold for
  every ported route: each route's grant classification travels with it.

### Error envelopes

- Stable `ErrorCode` vocabulary (`unsupported_capability`, `ambiguous`,
  `unauthenticated`, `invalid_argument`, `not_found`,
  `permission_denied`, `backend_unavailable`, `backend_timeout`,
  `index_building`, `scope_not_found`, `service_not_found`,
  `capability_degraded`, `overloaded`, `internal_error`,
  `documentation_read_model_unavailable`,
  `component_registry_unavailable`); no new codes without a core-side
  addition in the same contract bump.
- `Retry-After` is sent ONLY on a 503 whose verdict is marked retryable
  (graph outage, stale/timed-out Postgres reader, unreachable identity
  store); permanent 503s (unconfigured backend) carry no hint. `Retryable()`
  is the reader. Status 499 marks caller-cancelled (log/metric signal
  only; never a body contract).

## Native-now wave (first conformance target, 18 routes / 8 families)

| Family | Route | OpenAPI fragment | Auth / parity note |
| --- | --- | --- | --- |
| APIRouter | `GET /api/v0/openapi.json` | paths/status/routes.go | Public spec bytes; must equal in-tree spec for the wave |
| APIRouter | `GET /health` | \<mux-health\> | Public liveness; no envelope, no auth |
| APIRouter | `GET /api/v0/docs` | \<doc-ui\> | Doc UI shell; no versioned contract |
| APIRouter | `GET /api/v0/redoc` | \<doc-ui\> | Doc UI shell; no versioned contract |
| Capabilities | `GET /api/v0/capabilities` | paths/catalog/capabilities.go | Capability-matrix probe surface; gates every other family |
| CLISupport | `POST /api/v0/answer-quality/score` | paths/cli/score.go | Pure fn of body; no store; envelope + plain both |
| CLISupport | `POST /api/v0/investigation/packet/render` | paths/cli/render.go | Pure fn of body; no store |
| CLISupport | `POST /api/v0/report/bundle/capture` | paths/cli/capture.go | Pure fn of body; no store |
| CLISupport | `POST /api/v0/report/bundle/validate` | paths/cli/validate.go | Pure fn of body; no store |
| CLISupport | `POST /api/v0/service/report/compose` | paths/cli/compose.go | Pure fn of body; no store |
| Ask | `POST /api/v0/ask` | paths/search/ask.go | Reader ports only; scoped-route authz travels |
| ServiceIntelHTTP | `GET /api/v0/services/{service_name}/intelligence-report` | paths/service/intelligence_report.go | Reader ports only; `wireAPI`-isolated |
| AuthProviders | `GET /api/v0/auth/providers` | paths/auth/routes.go | Reader ports only; `wireAPI`-isolated |
| SignInPolicyReads | `GET /api/v0/auth/sign-in-policy` | paths/auth/sign_in_policy.go | Public read; reader ports only |
| SignInPolicyReads | `GET /api/v0/auth/admin/sign-in-policy` | paths/auth/sign_in_policy.go | Admin read; scoped-route gate travels |
| AdminProviderConfigReads | `GET /api/v0/auth/admin/provider-configs` | paths/auth/admin_provider_configs.go | Admin reads; `wireAPI`-isolated |
| AdminProviderConfigReads | `GET /api/v0/auth/admin/provider-configs/{provider_config_id}` | paths/auth/admin_provider_configs.go | Admin read; same gate |
| AdminProviderConfigReads | `GET /api/v0/auth/admin/provider-configs/{provider_config_id}/revisions` | paths/auth/admin_provider_configs.go | Admin read; same gate |

The frozen copy of this table lives in `read_transport_conformance.go`
(`nativeNowWave`) and is pinned by `TestNativeNowWaveFrozen`. A route
added to or removed from the wave fails the pin until spec, skeleton,
and table move together.

## Explicit TBDs (satellite repo implements)

1. Satellite handler set for the 18 routes against the v0 ports (no new
   store handles; provider-config/sign-in-policy readers via versioned
   read contracts).
2. Probe body of `RunReadTransportConformance`: per-route envelope/plain
   negotiation checks, pagination-boundary checks, `unsupported_capability`
   checks, OpenAPI-fragment diff, scoped-route authz matrix replay.
3. Golden-corpus + replay-coverage replay against a satellite deployment
   (replacing the in-tree binary target in `e2e-tests.yml`,
   `live-backend-tests.yml`, `golden-corpus-gate.yml` scope).
4. Latency-gate ownership: satellite repo owns the latency gate against a
   pinned core image; core keeps contract-registry + static-contract gates.
5. Status audit-read contract (Leaf B-D): new read admission vs scoped
   `ReadStore` extension — blocks Status/SupplyChain dispatch, NOT this wave.
6. SupplyChain split point (reads native vs whole-family dispatch) — same,
   not this wave.

## Open questions for the satellite implementer

1. Confirm the 8-family Native-now wave order, or reorder with reason.
2. Base-URL + credential injection for the conformance runner (env shape).
3. OpenAPI hosting path for the satellite spec (`/api/v0/openapi.json`
   stays the comparison point — confirm).
4. Docker split: satellite Dockerfile owning the api build; core image
   dropping the `eshu-api` binary (census build inventory).
