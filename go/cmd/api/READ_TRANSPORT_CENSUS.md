<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# eshu-api read-transport census (Leaf B input, prep only)

Refs #9669, part of #6707. Status: preparation only — no cutover has
occurred, no behavior changes in this artifact, and no in-tree code moves.

## What this is

One row per live handler family served by the `eshu` API binary, with a
per-family disposition for the eshu-api extraction, plus the consumer,
build, and gate inventory Leaf B/C executors need. The served-route source
of truth stays [CUTOVER_OWNERSHIP.md](CUTOVER_OWNERSHIP.md); the drift test
`read_transport_census_test.go` fails when a live family has no row here,
or when a row here names no live family. Route counts below are read from
that file and are informational only.

## Method (exact commands, run in the worktree root)

```bash
rg -c '^\| `(GET|POST|PATCH|PUT|DELETE)' go/cmd/api/CUTOVER_OWNERSHIP.md
rg -ln 'internal/query"' go --glob '*.go' | rg -v '_test\.go' | rg -v '^(go/cmd/api|go/internal/query)/'
rg -ln 'cmd/api' go specs --glob '*.go' --glob '*.yml' | rg -v '^go/cmd/api/'
rg -n 'eshu-api' Dockerfile
cat specs/ci-pr-workflows.d/read-api-latency-gate.yml
rg -n 'CoreReadContractsVersion' go --glob '*.go' | rg -v '_test\.go'
find go/internal/query/openapi -name '*.go' | wc -l
```

Result at census time: 278 route rows across 66 families; 156 OpenAPI
fragment files; `CoreReadContractsVersion = "v0"`.

## Disposition rule (executor proposal, needs Leaf B confirmation)

The arbiter §3 text is not in this repo, so the mapping below applies the
five named dispositions by the channel/persistence columns of
[CUTOVER_OWNERSHIP.md](CUTOVER_OWNERSHIP.md) as follows. Leaf B confirms or
reorders; the drift test pins only coverage, not the mapping.

- **Never satellite**: a family with any `mutation` row. HTTP shape may
  move; the persistence in the survivor column stays core-owned and is
  reached through a versioned admission contract.
- **Dispatch until Leaf B-D**: pure-`read` rows served by a handler that
  holds a writer store (Status, SupplyChain). Reads dispatch/proxy to core
  until the read side is separated from the writer handle.
- **Native now**: first-wave native surface — store-free or already
  isolated at the `wireAPI` seam. Proves the transport and parity harness.
- **Native next in order**: pure reads whose parity proof follows another
  family (aggregate/count/inventory pairs, resolvers, bundles over sources).
- **Native anytime**: remaining pure-reader families; no ordering constraint.

## Per-family disposition (66 families)

| Family | Routes | Channel | Disposition | Survivor / ordering note |
| --- | --- | --- | --- | --- |
| Admin | 16 | read+mutation | Never satellite | Postgres recovery/reindex/dead-letter/queue stores; via admission contract |
| AdminIdentityMutations | 5 | mutation | Never satellite | Identity stores (roles, IdP mappings); via AdminIdentityAdmission |
| AdminIdentityReads | 8 | read | Native anytime | Reader ports only |
| AdminInputInvalidFacts | 1 | read | Native anytime | Reader ports only |
| AdminProviderConfigMutations | 6 | mutation | Never satellite | Provider-config stores; via ProviderConfigMutationAdmission |
| AdminProviderConfigReads | 3 | read | Native now | Reader ports only; already `wireAPI`-isolated |
| AdminRetirement | 1 | mutation | Never satellite | Retirement marker/fencer/purger stores; via RetirementAdmission |
| AdminTerminalFailure | 3 | read+mutation | Never satellite | Terminal-failure ack store + replay ledger; via admission contract |
| APIRouter | 4 | read | Native now | Mux-level liveness, doc UI, spec; no store |
| Ask | 1 | read | Native now | Reader ports only; already `wireAPI`-isolated |
| AuthProviders | 1 | read | Native now | Reader ports only; already `wireAPI`-isolated |
| BrowserSessions | 4 | read+mutation | Never satellite | Browser-session store; lifecycle via admission contract |
| CICD | 3 | read | Native next in order | Count/inventory parity follows base list route |
| CLISupport | 5 | read | Native now | Pure function of request body; no store reads or writes |
| Capabilities | 1 | read | Native now | Capability-matrix probe surface |
| CloudInventory | 1 | read | Native anytime | Reader ports only |
| CloudRuntimeDrift | 2 | read | Native anytime | Reader ports only |
| Code | 27 | read | Native anytime | Reader ports only |
| CodeownersOwnership | 1 | read | Native anytime | Reader ports only |
| Compare | 1 | read | Native next in order | Compares Status-family outputs; follows Status dispatch |
| ComponentExtensions | 2 | read | Native anytime | Reader ports only |
| Content | 7 | read | Native anytime | Reader ports only |
| Dependencies | 1 | read | Native anytime | Reader ports only |
| Documentation | 6 | read | Native next in order | Count/inventory parity follows base list route |
| Entities | 7 | read | Native anytime | Reader ports only |
| Evidence | 5 | read | Native next in order | Bundle/citations parity follows source families |
| ExtractionReadiness | 2 | read | Native anytime | Reader ports only |
| FactSchemaVersions | 2 | read | Native anytime | Reader ports only |
| Freshness | 3 | read | Native anytime | Reader ports only |
| GitHubLogin | 2 | mutation | Never satellite | Login-state/session stores; via OIDCLogin-style admission |
| GraphEntityInventory | 1 | read | Native anytime | Reader ports only |
| IaC | 11 | read | Native anytime | Reader ports only |
| Images | 1 | read | Native anytime | Reader ports only |
| Impact | 13 | read | Native anytime | Reader ports only |
| Incident | 1 | read | Native anytime | Reader ports only |
| Infra | 9 | read | Native next in order | Count/inventory parity follows base list route |
| InvestigationWorkflows | 2 | read | Native next in order | Resolve route follows Playbooks family |
| Kubernetes | 1 | read | Native anytime | Reader ports only |
| Language | 1 | read | Native anytime | Reader ports only |
| LocalIdentity | 16 | read+mutation | Never satellite | Identity/session/token stores; issuance via admission contract |
| Metrics | 1 | read | Native anytime | Reader ports only |
| OIDCLogin | 2 | mutation | Never satellite | Login-state/session stores; via OIDCLoginAdmission |
| ObservabilityCoverage | 1 | read | Native anytime | Reader ports only |
| PackageRegistry | 7 | read | Native next in order | Count/inventory parity follows base list route |
| Playbooks | 2 | read | Native anytime | Reader ports only |
| Profile | 1 | read | Native anytime | Reader ports only |
| Repositories | 12 | read | Native next in order | Language-inventory parity follows base list route |
| RuntimeDeclarations | 1 | read | Native anytime | Reader ports only |
| SAML | 3 | read+mutation | Never satellite | Login-state/session stores; via SAMLLoginAdmission |
| SecretsIAM | 5 | read | Native anytime | Reader ports only |
| SemanticEvidence | 2 | read | Native anytime | Reader ports only |
| SemanticSearch | 1 | read | Native anytime | Reader ports only |
| ServiceCatalog | 1 | read | Native anytime | Reader ports only |
| ServiceIntelHTTP | 1 | read | Native now | Reader ports only; already `wireAPI`-isolated |
| SessionList | 1 | read | Native anytime | Reader ports only |
| Setup | 4 | read+mutation | Never satellite | Bootstrap stores; first-run flow via SetupAdmission |
| SignInPolicyMutations | 1 | mutation | Never satellite | Sign-in-policy store; via admission contract |
| SignInPolicyReads | 2 | read | Native now | Reader ports only; already `wireAPI`-isolated |
| SSMHostInventory | 1 | read | Native anytime | Reader ports only |
| Status | 18 | read | Dispatch until Leaf B-D | Writer handle held for governance audit reads; needs audit-read contract |
| SupplyChain | 19 | read+mutation | Dispatch until Leaf B-D | 18 reads share the suppression handler; suppressions row is the Never-satellite survivor via SuppressionAdmission |
| SurfaceInventory | 1 | read | Native anytime | Reader ports only |
| TagHistory | 1 | read | Native anytime | Reader ports only |
| TerraformConfigStateDrift | 1 | read | Native anytime | Reader ports only |
| Visualization | 1 | read | Native anytime | Reader ports only |
| WorkItems | 1 | read | Native anytime | Reader ports only |

## Consumer census (every importer of `go/internal/query`, non-test)

| Consumer | Uses from `query` | Replacement path |
| --- | --- | --- |
| `go/cmd/api` (in-tree handlers, wiring) | Handlers, `Mount`, ports, envelope helpers | Moves to eshu-api as the native handler set; reads via versioned core contracts |
| `go/cmd/reducer` (wiring, lookups) | `GraphQuery` port and related wiring types | Import ports from `querycontract` (core-owned); drop handler imports; reducer stays core |
| `go/cmd/eshu` (CLI service, graph, investigation) | Profiles, backends, packet render helpers | eshu-cli HTTP client against eshu-api; local modes via core contracts |
| `go/cmd/golden-corpus-gate` | `EnvelopeMIMEType` | Contract constant from eshu-sdk/api client; gate retargets eshu-api base URL |
| `go/cmd/capability-inventory` | `OpenAPISpec`, probe registry | Fetch OpenAPI/capabilities from eshu-api; probes run against the satellite |
| `go/internal/mcp` (dispatch, router, summaries) | `ResponseEnvelope`, filters, views | Adapt to versioned read contracts; MCP calls eshu-api transport (Leaf C/D lane) |
| `go/internal/cli` (investigation, support, reports) | `TruthEnvelope`, `ErrorEnvelope`, packet helpers | eshu-cli HTTP client against eshu-api; `localsupervisor` embedded stores stay core-local |
| `go/internal/ask/engine` | `AnswerPacket`, `ResponseEnvelope`, truth classes | Moves with the Ask family (Native now); core keeps no ask engine import |
| `go/internal/serviceintelhttp` | HTTP helpers, selectors | Moves with ServiceIntelHTTP (Native now) |
| `go/internal/reportbundle`, `answerquality`, `answernarration` | Pure packet/score shapes | Hoist to eshu-sdk or the eshu-api contract module |
| `go/internal/scopedtoken`, `oidcbearer`, `oidclogin`, `githublogin` | Auth/session helpers, provider config | Versioned auth/session contracts; identity stores stay core |
| `go/internal/facts`, `evidencecontinuity` | Doc shapes, gate triggers — string mentions only, zero AST-exact imports (`facts/docs/documentation.go` + `facts/semantic/semantic.go` `NoReducerEvidence` strings kept per arbiter; `evidencecontinuity/gatetriggers.go` doc example neutralized) | Fact schemas to eshu-sdk; triggers re-point at eshu-api evidence routes |
| `go/internal/cli` docs/verify, compparity | Doc parity assertions | Re-point at eshu-api OpenAPI/spec surface |

Direct `go/cmd/api` importers outside that tree (non-test) are doc/closure
references (`askwiring`, `cigates`, `cli/support`) and admin credential
flows (`cli/admin`): the former re-point at eshu-api docs, the latter at
admission-contract clients.

## Build and gate inventory

- `Dockerfile` builds one image containing `eshu-api` from `./cmd/api`
  alongside every other binary; there is no api-only stage. Replacement:
  an eshu-api repo Dockerfile owning the api build, and a core image that
  drops the `eshu-api` binary.
- `specs/ci-pr-workflows.d/read-api-latency-gate.yml` triggers on `go/**`,
  the route-budget files, and compose files (markdown excluded except the
  lockstep list, which includes this census's sibling
  `go/cmd/api/CUTOVER_OWNERSHIP.md`). Replacement: eshu-api repo owns the
  latency gate against a pinned core image; core keeps the contract-registry
  and static-contract gates.
- Live/e2e gates touching the API surface: `e2e-tests.yml`,
  `live-backend-tests.yml`, `live-postgres-readiness*.yml`,
  `golden-corpus-gate.yml`. Replacement: the Leaf B cross-repo parity
  harness replays golden-corpus and replay coverage against an eshu-api
  deployment instead of the in-tree binary.

## Leaf B input: contract pin and harness scope

- Pin: `CoreReadContractsVersion = "v0"` in
  `go/internal/query/querycontract/api_boundary.go`. It versions the
  `GraphQuery` (two methods) and `ContentStore` (frozen set in
  `api_boundary_test.go`) ports; the Postgres `ReadStore` half is pinned by
  `TestReadStoreContractIsVersioned` in `storage/postgres/db`. Any breaking
  port change must bump the version in the same diff.
- A cross-repo transport plus parity harness must cover, per family:
  response-envelope negotiation (`Accept: application/eshu.envelope+json`
  via `WriteSuccess`/`WriteErrorEnvelope`, `TruthEnvelope`/`ErrorEnvelope`
  shapes in `querycontract/http.go` and `truth.go`), pagination (bounded
  `limit` params, offset-ordered pages with `page_position`, resume cursor
  with `truncated` and `TruthLevelPartial`), capability/profile gating
  (`unsupported_capability`, truth labels), OpenAPI parity (156 fragment
  files under `go/internal/query/openapi/paths`, the operation-id
  convention test, `apicontract_drift_test`), authz parity
  (`auth_scoped_routes_completeness`, fail-closed
  `BrowserSessionRoutePolicy`), and the single suppression-mutation
  admission proof for the SupplyChain survivor row.
- Relay-forever evidence (presented, not decided; arbiter rules against
  it): for a permanent proxy — read handlers are pure functions of the
  versioned ports, so a proxy preserves behavior with no handler move. Against
  it — the repo plan forbids non-core Postgres/graph handles (rules 2-4),
  the latency gate measures the in-process path, `AuthMiddleware` placement
  is a security boundary owned with the binary, and a proxy keeps two
  deploys for one truth owner. Leaf B decides.

## Open questions for Leaf B/C executors

1. Confirm or reorder the Native now first wave (eight families proposed).
2. Decide the Status audit-read contract: new read admission or a scoped
   `ReadStore` extension.
3. Decide the SupplyChain handler split point (reads native, suppressions
   admission) versus dispatching the whole family.
4. Confirm Admin read rows stay Never satellite with their mutation
   siblings, or split them into a read admission.
5. Confirm the relay-forever rejection with the arbiter ruling recorded.
