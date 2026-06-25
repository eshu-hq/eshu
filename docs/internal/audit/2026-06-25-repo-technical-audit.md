# Eshu Repository Technical Audit — 2026-06-25 (Re-Audit)

Read-only re-audit of `origin/main` at commit `0f4c8a8c`. This supersedes the
2026-06-09 audit (`docs/internal/audit/2026-06-09-repo-technical-audit.md`).
Findings are labeled **[fact]** (verified against files) vs **[judgment]**
(assessment), with file:line citations and severity. No code was modified.
Findings are cross-referenced to the 105 open GitHub issues (18 epics) where
they map. Research fan-out used Sonnet sub-agents; the core 20% (query,
reducer, storage, mcp, search, auth) got deep review — parser internals,
individual awscloud services, and the ask/answer pipeline got lighter review.

## Executive Summary

Overall health grade: **A−**, up from B+ in December. In ~1,080 commits over
~6 months the project closed most of the prior audit's High findings and
converted the rest into a disciplined, labeled epic backlog (A–Y, M). Verified
fixes: HTTP server timeouts (`cmd/api/main.go:87`), per-write graph timeouts +
a new production-grade `graphbackpressure` gate, generation retention with
audited FK-cascade pruning, a real frontend CI gate (`frontend.yml`), and a
five-job security-scan workflow (Trivy/gosec/govulncheck/nancy) — all blocking
PRs. The codebase grew hard: 58→92 internal packages, 34→40 binaries, +33K
non-test Go LOC, adding SSO (OIDC + SAML), scoped tokens, and a full
vector/hybrid semantic-search stack. **Top 3 risks:** (1) the product is going
multi-tenant but tenant isolation is application-layer only — no Postgres RLS,
no Cypher tenant scope, no tenant middleware (Epic M, all open); (2) the Helm
chart still ships `password: change-me` with no schema constraint blocking it
(#3756); (3) `internal/query` has become a 661-file / 127K-LOC god package that
forces auth packages to import the HTTP layer. **Top 3 opportunities:** finish
Epic M before GA to make isolation defense-in-depth; a handful of S-effort
security fixes (Helm schema `pattern`, OIDC rate limiter, the `query/code.go`
panic) clear the security epics; carving an `internal/auth` package out of
`query` unblocks the next phase of modularization. The team's own issue
framing slightly overstates two test debts (see Audit §Testing). This is a
healthy, fast-moving codebase whose main risk is now scaling governance and
tenant isolation to match feature velocity, not foundational quality.

## Repo Map

**Purpose.** Unchanged: self-hosted code-to-cloud context graph serving
repo-aware answers with evidence over CLI, MCP, and HTTP API
(`README.md`, `docs/public/architecture.md`).

**Stack.** Go 1.26.4 (~472K non-test LOC, 92 internal packages, 40 binaries),
Postgres + graph backend (NornicDB `orneryd/nornicdb v1.0.45` default / Neo4j
compatible), React 19 / Vite 7 console + marketing site, Helm + Compose, MkDocs,
14 GitHub Actions workflows.

**Maturity.** Production-track, approaching multi-tenant GA. Single primary
contributor, very high velocity (~1,215 commits/30 days), agent-driven under a
strict governance regime (CLAUDE.md, 11 project skills, per-package doc triples,
proof/evidence CI gates).

**What changed since the last audit (new surfaces).**

| New package cluster | Purpose |
| --- | --- |
| `oidclogin`, `samlauth`, `scopedtoken` | SSO (OIDC/SAML) + scoped bearer tokens |
| `searchembed*`, `searchvector`, `searchhybrid`, `searchrerank`, `searchretrieval` | Vector/hybrid semantic search stack |
| `ask`, `askwiring`, `answerguardrail`, `answernarration` | Natural-language "ask" answering surface |
| `graphbackpressure` | Shared permit-pool gate over graph writes |
| `governanceaudit`, `admissionaudit`, `auditpreflight`, `exposure` | Governance/audit + sink-exposure catalog |
| `serviceintel`, `serviceintelhttp`, `evidencebundle`, `queryplan`, `proofofvalue` | Service dossiers, evidence bundles, plan regression, value scoring |

**New CI workflows (8→14):** `frontend.yml`, `security-scan.yml`,
`golden-corpus-gate.yml`, `verify-telemetry-coverage.yml`,
`generate-operator-dashboard.yml`, `verify-skill-roundtrip.yml`.

**Open backlog shape (105 issues, 18 epics):** security (Epic H), multi-tenancy
(Epic M), reducer modularization (Epic D), test parity (A/P/R/T), observability
(G/U), search quality (Q), perf (B), storage hardening (S2), frontend (F),
plus API versioning (V), lifecycle (W), webhooks (K), release/repro (Y).

## Audit Report

### Resolved since the 2026-06-09 audit (verified)

- **[fact] HTTP server timeouts** — `cmd/api/main.go:87-93` sets
  ReadHeader/Write/Idle; `mcp/server.go:135-141` sets them with `WriteTimeout:0`
  for SSE (annotated). Prior High → closed.
- **[fact] Per-write graph timeouts + backpressure** —
  `storage/cypher/timeout_executor.go` (30s default, env-tunable) plus the new
  `graphbackpressure` gate wrapping both canonical and materializer write paths
  outermost, with telemetry (`GraphWriteBackpressureEngaged/WaitDuration`).
  Prior Medium → closed (partly #3652).
- **[fact] Queue/generation retention** —
  `storage/postgres/generation_retention.go` prunes superseded generations with
  bounded batches and a `generation_retention_events` audit table. Prior Medium
  → mostly closed (caveat below).
- **[fact] Frontend CI gate** — `frontend.yml` runs typecheck + vitest + build
  for both marketing site and console, ESLint, and `npm audit`, blocking PRs.
  Prior High → closed.
- **[fact] Security scanning** — `security-scan.yml` runs Trivy fs+image,
  govulncheck (package mode), gosec, and license-check with exit-code-1 gates.
  New defense the prior audit recommended.
- **[fact] Reducer modularization (Epic D)** — reducer is now ~396 single-
  concern non-test files; prior god files `git_snapshot_native.go` and
  `storage/postgres/ingestion.go` no longer appear in the top size cohort.
- **[fact] Auth strengths retained** — `query/auth.go:386` constant-time
  compare; `__Host-` session+CSRF cookies; scoped tokens stored hash-only.

### Security

- **[fact, High] Helm still ships `password: change-me`** —
  `deploy/helm/eshu/values.yaml:1062`. A `values.schema.json` now exists but the
  `neo4j.auth.password` field (≈line 1401) is typed `string` with **no**
  `pattern`/`minLength`/`not` constraint, so a verbatim install authenticates to
  Neo4j with a known credential. Tracked **H1 #3756** — schema added, the
  blocking constraint is the missing half.
- **[fact, High/judgment] Multi-tenant isolation is application-layer only.**
  No `ROW LEVEL SECURITY`/`CREATE POLICY` anywhere in
  `schema/data-plane/postgres/` (M1 #3751); no tenant predicate or ancestor
  node in `storage/cypher/` graph queries (M2 #3752); no tenant middleware —
  scoping is a 500-line per-route allowlist in `query/auth_scoped_routes.go`
  with no automated route-coverage gate (M3 #3753 / M4 #3754). Today every
  handler applies `tenant_id`/`scope_id` filters correctly, but a single missed
  filter on a new route exposes cross-tenant data with no DB-layer safety net.
  This is the headline new risk as the product goes multi-tenant. Whole of
  **Epic M (#3731)** — open.
- **[fact, Medium] No rate limiter on OIDC login endpoints** —
  `query/oidc_login_handler.go` start/callback are public routes that each do a
  state-row write + outbound IdP call per request; unauthenticated flood creates
  DB rows / amplifies into IdP quota. Tracked **H3 #3758**.
- **[fact, Medium] `0.0.0.0` default binds** — `runtime/config.go:26-27`,
  `deploy/helm/.../deployment.yaml:45`, `values.yaml:1048,1075`. Metrics (9464,
  public path) and NornicDB ports exposed pod-wide. Tracked **H2 #3757**.
- **[fact, Low-Medium] SAML signature path correct, test gap remains** —
  `query/saml_verifier.go` delegates to crewjam (full XML-DSIG, conditions,
  `AllowIDPInitiated:false`), adds replay reservation + 1 MiB body cap. No
  end-to-end test feeds a tampered/forged-signature XML, so a future refactor
  could silently weaken it. Tracked **H5 #3759**.
- **[fact, Low] NornicDB default `NORNICDB_NO_AUTH: "true"`** —
  `values.yaml:1077`; cluster-internal + NetworkPolicy-gated, but undocumented
  posture with no warning comment. New finding.
- **[fact, strengths]** No SQL injection (all dynamic SQL uses `$N` placeholders
  or allowlist-validated labels), no Cypher injection (closed-set label maps),
  OIDC nonce/state hash-only with single-use `ConsumeState`, OIDC cross-tenant
  lock in `resolveProviderContext`, `safeReturnPath` open-redirect mitigation,
  pod `runAsNonRoot`+`readOnlyRootFilesystem`+`drop ALL`, gated/observable
  `InsecureSkipVerify`.

### Architecture & design

- **[fact, High] `internal/query` is now the god package** — 661 non-test files
  / 127K LOC (verified `wc -l`) in one package spanning every read surface plus
  auth types. `scopedtoken` and `oidclogin` must import `internal/query` just
  to use `AuthContext`/`OIDCLogin*` types (`scopedtoken/identity.go:40`,
  `oidclogin/service.go:74`) — an auth implementation depending upward on the
  HTTP layer. **Untracked**; the natural fix (extract `internal/auth`) also
  unblocks further modularization. Epic D (#3740) currently targets reducer, not
  query.
- **[fact, Medium] `query/code.go:69` still panics on backend parse failure**
  inside an HTTP-handler path, and `code.go:65` defaults empty backend to Neo4j
  while `runtime/data_stores.go:82` and `query/contract.go` default empty to
  NornicDB — same contradiction flagged in the prior audit, still unresolved.
  `contract.go:247` similarly panics mid-request on an unregistered capability.
- **[fact, Medium] Cross-layer coupling:** `graphbackpressure/
  materializer_backpressure.go:9` imports `internal/reducer` to satisfy a
  `reducer.CypherExecutor` interface (infra→domain upward dep; motivated by
  #3652). `storage/postgres`→`reducer` type imports are acceptable (no cycle:
  reducer never imports storage).
- **[fact/judgment, Medium] Three reducer functions exceed 200 lines** —
  `workload_materialization_handler.go:119` Handle (254),
  `cross_repo_resolution.go:70` Resolve (233),
  `code_call_materialization_index.go:16` buildCodeEntityIndex (238). Tracked
  under Epic D §T8 nolint markers. `cmd/bootstrap-index/main.go` is 1,144 lines
  (genuinely complex 4-phase pipeline; `runPipelined` 198 lines, no panic
  recovery around its goroutine fan-in).
- **[judgment, strength]** The 34 new packages are coherently bounded; the
  search cluster layers cleanly; port discipline holds (reducer imports no
  storage).

### Testing

- **[fact, High] Three logic packages still have zero tests** (verified):
  `repositoryidentity` (repo identity/dedup), `tfstatewarning`, `vulnsource`.
  Carried over unfixed from the prior audit.
- **[fact, High] 41 of ~83 MCP `tools_*.go` have no companion test**, and the
  ~42 that do are schema/registration-contract tests, not handler-behavior
  tests — query-execution correctness is unexercised in CI. Tracked **Epic T
  #3737**.
- **[fact, Medium] No Go coverage measurement** anywhere — no `-coverprofile`,
  no codecov, no threshold gate in any of the 14 workflows. Carried over.
- **[fact, Medium] `macos.yml` omits `-race`** (`macos.yml:41`); Linux
  `test.yml:119` enforces it. Apple-silicon-specific races go undetected.
- **[fact, judgment] Two backlog test-debt items are mis-scoped** (worth
  correcting before spending effort): Epic A (#3736) cites awscloud "8.5%"
  but that counts only the flat top-level dir (mostly `constants_*.go` tables);
  the real tree is ~32% with every one of 134 service subdirs holding tests.
  Epic P (#3735) "0-test parsers" (kotlin/swift/php/csharp/scala/c) actually
  have extensive **parent-level** engine tests (e.g. 13 `engine_kotlin_*`
  files). The debt is smaller than the issue titles imply.
- **[fact, strengths]** 14 workflows, most blocking PRs; `golden-corpus-gate.yml`
  diffs a full 5-fixture pipeline run against golden snapshots; `e2e-tests.yml`
  runs both graph backends every PR; Go tests assert behavior not just
  execution; `verify-telemetry-coverage.yml` gates span/metric coverage; flaky
  `time.Sleep` use is low and mostly in fakes/bounded polls.

### Performance & concurrency

- **[fact, Medium] `fact_work_items` rows are never deleted** — terminal
  (`completed`/`failed_terminal`/`superseded`) rows persist forever
  (`schema/data-plane/postgres/005_fact_work_items.sql`); generation retention
  prunes other tables but not this one. CTEs filter them from live reads but the
  heap grows unbounded, taxing status scans over months. Fits **Epic S2 #3742**.
- **[fact, Low-Medium] API/MCP JSON bodies lack `MaxBytesReader`** —
  `query/handler.go:94` and `mcp/server.go:247` decode `r.Body` unbounded; a
  multi-MB body can OOM a constrained pod. (Webhook + SAML paths *do* cap.) Fits
  Epic S2.
- **[fact, Low-Medium] `searchhybrid/index.go:179` embeds with
  `context.Background()`** at index-build time in the request path — safe today
  (persisted vectors + local hash embedder) but a latent leak if a slow external
  document embedder is wired in. Fits **Epic Q #3743**.
- **[fact, Low] Shared `runtime/http_server.go:64`** (admin/metrics/pprof) sets
  only `ReadHeaderTimeout` — fine for internal listeners, hardenable under S2.
- **[fact, strengths]** Queue model remains production-hardened (SKIP LOCKED +
  leases + fencing + generation supersession, context-aware goroutine triad);
  search is bounded end-to-end (limit ≤100, required timeout, overflow surfaced,
  approximate→exact fallback); pools well-defaulted and tunable.

### Dependencies

- **[fact, Low] Healthy.** Go 1.26.4; only two `replace` directives (local SDK
  path + legitimate tree-sitter-elixir rename); 147 modular AWS service modules
  (correct approach); jwt v5, go-jose v4, pgx v5.9.2, x/net 0.55 all current;
  frontend React 19 / Vite 7 / TS 5.7 current. **[judgment]** NornicDB remains a
  single-maintainer module dependency as the default backend — the Neo4j
  conformance gate is the mitigation. Re-run `govulncheck`/Dependabot for live
  CVE state (not verifiable from source here).

### DevEx, operations & documentation

- **[judgment, Low] Governance overhead is the scaling cost.** Per-package doc
  triples, proof-evidence gates, and the epic regime are high-quality but grow
  O(packages); this is sustainable only because tooling enforces them. No new
  defect — flagged as the thing to keep automating. Docs build `--strict` on
  every PR; operator dashboard drift is a CI failure.

### The ugly parts, ranked

1. Multi-tenant isolation has no DB/graph-layer enforcement while the product
   moves to multi-tenant (Epic M — all open).
2. `password: change-me` still installable (#3756).
3. `internal/query` god package + the `query/code.go` handler panic (untracked).
4. Three zero-test logic packages + 41 untested MCP tools.
5. `fact_work_items` unbounded growth.

## Improvement Strategy

**Theme 1 — Tenant isolation must become defense-in-depth before GA.** Target:
Postgres RLS on every multi-tenant table, a tenant ancestor/predicate in graph
writes+reads, one HTTP tenant middleware, and a CI gate that fails when a new
route isn't classified (public / shared / tenant-scoped). Principle: isolation
that depends on every handler being perfect is not isolation. This is Epic M;
treat it as the release blocker.

**Theme 2 — Close the security epic with cheap, high-certainty fixes.** Helm
schema `pattern` rejecting `change-me`, OIDC login rate limiter, SAML
tampered-signature test, `0.0.0.0`→`127.0.0.1` defaults. Principle: the lazy
deploy path must be the safe one. Mostly S effort (Epic H).

**Theme 3 — Make the query layer obey the architecture the reducer now does.**
Extract `internal/auth` (and likely `internal/identity`) so auth packages stop
importing the HTTP layer, then continue splitting `query` by domain as Epic D
did for reducer. Replace the two `query` handler panics with error returns.
Principle: the rule that earned the reducer its health applies to query too.

**Theme 4 — Turn test *presence* into test *confidence*.** Add coverage
measurement (even report-only) to make blind spots visible, give the three
zero-test packages and the 41 MCP tools behavior tests, and re-scope Epics A/P
to their real (smaller) debt so effort lands where it matters. Principle:
measure before grinding.

**Explicitly not fixing now:** AWS SDK breadth (correct modular design);
NornicDB single-maintainer risk beyond the conformance gate; macOS `-race`
(platform limitation — document it); the `bootstrap-index/main.go` size
(genuinely complex, lower payoff than query split); doc-triple overhead (keep
automating, don't dismantle).

**Done looks like:** RLS + tenant middleware + route-coverage gate green (zero
unclassified routes); `helm template` errors on the default password; zero
reachable panics in `query`; `internal/auth` exists and auth packages no longer
import `query`; coverage published per PR; zero open High findings.

## Task Plan

### Quick wins (high impact, S effort — do immediately)

| ID | Task | Issue |
| --- | --- | --- |
| Q1 | Add `pattern`/`not` to `values.schema.json` rejecting `change-me`; document override | H1 #3756 |
| Q2 | Fix `query/code.go:65-69` — default empty→NornicDB, return error not panic | — |
| Q3 | OIDC login per-IP rate limiter on start/callback | H3 #3758 |
| Q4 | Flip `0.0.0.0`→`127.0.0.1` defaults + restricted NetworkPolicy | H2 #3757 |
| Q5 | `MaxBytesReader` on API/MCP JSON bodies | S2 #3742 |
| Q6 | Add `-coverprofile` (report-only) to `test.yml` | — |

### Milestone 0 — Safety net

| ID | Task | Files/areas | Acceptance | Effort | Risk | Deps |
| --- | --- | --- | --- | --- | --- | --- |
| T1 | Coverage measurement in CI | `test.yml` | Coverage % per PR, no threshold yet | S | Low | — |
| T2 | Behavior tests for `repositoryidentity`, `tfstatewarning`, `vulnsource` | those 3 pkgs | Each has behavior-asserting tests | M | Low | T1 |
| T3 | Per-route tenant-coverage audit script + CI gate (classify every route) | `query/auth_scoped_routes.go`, new script | CI fails on unclassified route | M | Low | — |

### Milestone 1 — Critical (security & correctness)

| ID | Task | Acceptance | Effort | Risk | Deps |
| --- | --- | --- | --- | --- | --- |
| T4 | Helm password hard-fail (Q1) | `helm template` errors on default | S | Med (breaks lazy installs, intended) | — |
| T5 | Postgres RLS on all multi-tenant tables (M1 #3751) | RLS policies + tenant GUC; tests prove cross-tenant denial | L | High | T3 |
| T6 | Cypher tenant predicate/ancestor + linter (M2 #3752) | graph reads/writes tenant-scoped; lint rule blocks unscoped | L | High | T3 |
| T7 | HTTP tenant middleware injecting tenant_id (M3 #3753) | context-propagated tenant_id; handlers read from it | M | Med | T3 |
| T8 | OIDC rate limiter (Q3) + SAML tampered-sig test (H5 #3759) | flood-tested limiter; forged-sig test fails closed | M | Low | — |
| T9 | Replace `query/code.go` + `contract.go` panics with errors | no reachable panic in query handlers | S | Low | — |

### Milestone 2 — High leverage

| ID | Task | Acceptance | Effort | Risk | Deps |
| --- | --- | --- | --- | --- | --- |
| T10 | Extract `internal/auth`/`identity`; auth pkgs stop importing `query` | no `oidclogin`/`scopedtoken`→`query` import | L | Med | — |
| T11 | Continue `query` package split by domain (Epic D-style) | top files <500 lines; no behavior change + golden-corpus green | XL | Med | T10 |
| T12 | MCP tool behavior tests for the 41 untested tools (Epic T #3737) | each tool has a handler-behavior test | XL | Low | T1 |
| T13 | `fact_work_items` retention/archival (S2 #3742) | bounded terminal-row pruning + audit | M | Med | — |

### Milestone 3 — Quality & polish

| ID | Task | Acceptance | Effort | Risk |
| --- | --- | --- | --- | --- |
| T14 | Re-scope Epics A/P to real debt; target genuine gaps | issues updated; targeted tests added | M | Low |
| T15 | Reducer >200-line function splits (Epic D §T8) | 3 functions <120 lines; no-regression evidence | M | Med |
| T16 | `runtime/http_server.go` Write/Idle timeouts; macOS `-race` documented | timeouts set; rationale noted | S | Low |
| T17 | `searchhybrid` index-build embedding respects request context | parent ctx cancels embed | S | Low |

### Top-3 implementation sketches

**T5 — Postgres RLS.** Add a `app.tenant_id` GUC set per connection by the
tenant middleware (T7), then `ENABLE ROW LEVEL SECURITY` + `CREATE POLICY
USING (tenant_id = current_setting('app.tenant_id')::uuid)` on each multi-tenant
table. Gotchas: the reducer/ingester write paths run without an HTTP request —
they need a service role that either sets the GUC explicitly per scope or holds
`BYPASSRLS`; pick per-scope GUC, not bypass, so projection writes are still
constrained. Prove with a two-tenant integration test that a missing GUC denies
all rows (fail-closed) rather than leaking. Land behind a feature flag and run
the golden-corpus gate to catch projection regressions. Sequence before T6 so
the relational layer is proven first.

**T3 — Route tenant-coverage gate.** Enumerate every registered route at test
time (reuse the router registration table), assert each is in exactly one of
{public, shared-token, tenant-scoped} sets, fail CI otherwise. Gotchas: this is
the cheapest durable defense and a prerequisite for T5–T7 — it converts "did we
remember to scope this route" from a review burden into a build failure. Keep
the classification next to the route registration, not in a separate file that
drifts.

**T10 — Extract `internal/auth`.** Move `AuthContext`, `AuthMode`,
`ScopedTokenResolver`, and the `OIDCLogin*`/SAML wire types out of
`internal/query` into `internal/auth`; have `query` import `auth` (reversing
today's direction). Gotchas: do it as a pure move with no behavior change so the
golden-corpus and e2e gates stay green; expect a large mechanical import churn;
update the per-package doc triples in the same PR (the docs gate will flag
them). This unblocks T11 (splitting the rest of `query`).

## Open Questions

1. **Multi-tenant GA timeline?** Epic M's priority (and whether T5–T7 are
   release blockers vs fast-follows) depends on when the first external tenant
   lands. This is the single most important scoping input.
2. **Is per-scope GUC or a service bypass role acceptable for the
   reducer/ingester write paths under RLS?** Determines T5's design.
3. **NornicDB ownership** — first-party-controlled or true third-party? Changes
   the bus-factor rating on the default backend.
4. **Should Epics A and P be re-scoped now** that their stated ratios are
   mis-measured, to avoid spending L effort on already-covered surfaces?
5. **Target retention horizon for `fact_work_items`** (and a status-query
   latency SLO) — T13 needs a number.
