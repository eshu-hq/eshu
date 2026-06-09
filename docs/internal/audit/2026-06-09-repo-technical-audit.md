# Eshu Repository Technical Audit — 2026-06-09

Read-only audit of the full repository at commit `8ffe611`. Every finding cites
the file and line inspected. Claims labeled **[fact]** were verified directly;
claims labeled **[judgment]** are assessments. No code was modified.

## Executive Summary

Overall health grade: **B+**. The Go core is unusually disciplined for any
team, let alone a solo-maintained project: clean package boundaries, a
production-hardened queue/lease model, race-detected tests and multi-backend
e2e on every PR, and exactly one TODO in ~439K lines of Go. The grade is not
an A because the shipped TypeScript surfaces (console + site) have zero CI
gates, the repository violates its own 500-line rule on its most critical
ingestion files, and the deployment seam ships dev-grade defaults (hardcoded
Helm password, `0.0.0.0` binds, no HTTP server timeouts). Top three risks:
(1) frontend regressions can merge silently because no TS typecheck or test
runs in CI; (2) per-package process overhead (~1,818 doc files, 122 unindexed
scripts) grows linearly with the codebase and is the main threat to solo-
maintainer sustainability; (3) production deployments inherit insecure
defaults unless operators know to override them. Top three opportunities:
a one-file CI change closes the entire frontend gap; splitting three god
files de-risks the ingestion path that everything downstream depends on;
making Helm fail on the default password makes the secure path the default
path.

## Repo Map

**Purpose.** Self-hosted code-to-cloud context graph: ingests repos, infra
config, registries, and cloud observations into Postgres facts, materializes
a canonical graph (NornicDB default, Neo4j compatible), and serves it via
HTTP API, MCP, and CLI (`README.md:1-30`, `docs/public/architecture.md`).

**Stack.** Go 1.26 backend (~439K LOC, 6,759 files, 58 internal packages,
34 binaries in `go/cmd/`), Postgres + graph backend, React 19/Vite 5
site + console (`package.json`, `apps/console/`), Helm + Docker Compose
deployment, MkDocs documentation, protobuf via buf, Cloudflare Pages site.

**Maturity.** Production-intent platform, pre-broad-adoption: single
contributor (134 commits in the last 90 days, all by one author — `git
shortlog`), 2,000+ PRs merged, heavily agent-driven development governed by
`CLAUDE.md`/`AGENTS.md` and 11 project skills.

**Data flow.** sync → discover → parse → emit facts (Postgres) → enqueue →
reducer claims work (`SELECT ... FOR UPDATE SKIP LOCKED` + leases) → graph +
content projection → bounded reads through `GraphQuery`/`ContentStore` ports.

**Key directories.**

| Path | What it is |
| --- | --- |
| `go/cmd/` | 34 binaries: CLI, api, mcp-server, ingester, reducer, bootstrap-index, ~20 collectors |
| `go/internal/collector/` | Git sync, discovery, snapshotting, archive preflight |
| `go/internal/parser/` | Language adapters, tree-sitter, SCIP |
| `go/internal/reducer/` | Cross-domain materialization, shared projection, queue drain |
| `go/internal/storage/{postgres,cypher,neo4j}/` | Facts/queue/content; backend-neutral Cypher writes; Neo4j adapter |
| `go/internal/query/` | HTTP read surface, OpenAPI, capability/truth contracts |
| `deploy/helm/eshu/` | Split-service Kubernetes chart |
| `docs/public/` (230 files), `docs/internal/` (51) | MkDocs site; ADRs and design docs |
| `apps/console/`, `src/` | Read-only graph console; Cloudflare Pages site |
| `scripts/` | 122 verification/monitoring shell scripts |
| `terraform_providers/`, `go/internal/terraformschema/schemas/` | Provider schema corpus (21 tracked .gz, ~2.5MB) |

**Surprises.** One TODO in the entire Go tree
(`go/internal/storage/postgres/workflow_control_sql.go:52`) **[fact]**; 147
AWS SDK service modules in `go/go.mod` (justified by the AWS collector)
**[fact]**; 604 packages each carrying a `doc.go` + `README.md` + `AGENTS.md`
triple **[fact]**; the default graph backend is a niche third-party project
(`github.com/orneryd/nornicdb v1.0.45`).

## Audit Report

### Architecture & design

- **[fact, High] The repo violates its own 500-line rule on its most
  critical files.** 29 non-test Go files exceed 500 lines (`CLAUDE.md`
  mandates "MUST keep files under 500 lines"). Worst on the hot ingestion
  path: `go/internal/collector/git_snapshot_native.go` (1,066),
  `go/internal/storage/postgres/ingestion.go` (1,039),
  `go/cmd/bootstrap-index/main.go` (974),
  `go/internal/collector/git_source.go` (865),
  `go/internal/reducer/repo_dependency_projection_runner.go` (836).
  Consequence: the files every fact flows through are the hardest to test
  and review. (`go/internal/telemetry/instruments.go` at 3,479 lines is a
  data registry and a defensible exception.)
- **[fact, Medium] Graph-backend default contradiction plus panic in a
  request path.** `go/internal/query/code.go:56-58` treats an empty
  `GraphBackend` as Neo4j; `go/internal/runtime/data_stores.go:82-84` and
  `go/internal/query/contract.go:35-43` (and `docs/public/architecture.md:
  178-181`) define empty = NornicDB. The same method panics on parse failure
  (`code.go:61`) instead of returning an error. A mis-wired handler would
  silently assume the wrong backend or crash the API process.
- **[fact, strength] Boundaries are clean.** Query handlers depend on
  `GraphQuery`/`ContentStore` ports, not drivers; no reverse imports from
  storage→query or parser→storage were found; reducer does not import query.
- **[judgment, Low] Internal package sprawl.** 58 packages include several
  proof/bench-flavored ones (`vulnerabilityparity`, `vulnerabilityparityproof`,
  `searchbench`, `storageeval`). Fine today; worth a periodic cull.

### Code quality

- **[fact, strength] Debt is near zero.** 1 TODO, no `FIXME`/`HACK` density,
  no `_ = err` swallows found, `%w` wrapping is consistent, handlers route
  errors through `WriteError`.
- **[fact, Low] A handful of panics outside initialization.** Verified:
  `query/code.go:61` (covered above). `reducer/shared_projection.go:189`
  panics on `json.Marshal` of plain strings — documented as unreachable and
  acceptable. `collector/awscloud/redaction.go:142` is a `must*()` init-time
  constructor over constant inputs — idiomatic, not a defect.
- **[fact, Low] Cypher built with `fmt.Sprintf`** in
  `go/internal/storage/cypher/edge_writer_sql.go:128-134`. Interpolated
  values are allowlist-validated labels and hardcoded literals — not
  exploitable today, but the pattern is fragile to future edits.

### Security

- **[fact, High] Hardcoded default Neo4j password in Helm values.**
  `deploy/helm/eshu/values.yaml:831` ships `password: change-me`. A team
  installing the chart without overriding it runs a graph database with a
  known credential. The chart should refuse to render (or generate a random
  secret) when the default is left in place.
- **[fact, Medium] Services bind `0.0.0.0` by default.**
  `go/internal/runtime/config.go:23-24` defaults `ESHU_LISTEN_ADDR` to
  `0.0.0.0:8080` and metrics to `0.0.0.0:9464`. API reads are bearer-token
  protected (`go/internal/query/auth.go`, constant-time compare), but
  metrics/status surfaces leak operational detail on all interfaces.
- **[fact, Low] Default MinIO creds (`minioadmin`) in a test compose file**
  (`docker-compose.tier2-tfstate.yaml:119-120,150-151`). Test-fixture only.
- **[fact, strengths]** Constant-time bearer auth with public-path allowlist
  (`go/internal/query/auth.go`); archive-bomb/zip-slip protections with entry,
  size, and compression-ratio caps
  (`go/internal/collector/archivepreflight/preflight.go`); non-root (uid
  10001) multi-stage Docker image (`Dockerfile`); pprof forced to localhost
  (`go/internal/runtime/pprof.go:71-73`). No shell-injection or
  string-concatenated SQL patterns found in `storage/postgres`.

### Testing

- **[fact, High] The TypeScript surfaces have zero CI gates.**
  `.github/workflows/test.yml` contains no Node step; `package.json:10-14`
  defines `test`, `typecheck`, `console:test`, `console:typecheck` that CI
  never runs, despite ~54 TS test files existing. Frontend type errors and
  test failures merge silently.
- **[fact, Medium] No coverage measurement anywhere.** No `-coverprofile`
  or coverage service in any workflow. For a repo whose culture is
  evidence-based, the absence of a coverage trend is an odd blind spot.
- **[fact, Medium] Three logic packages have zero tests:**
  `go/internal/repositoryidentity` (repo identity normalization — feeds
  deduplication), `go/internal/tfstatewarning`, `go/internal/vulnsource`.
- **[fact, Medium] Parser is the test-ratio outlier:** 37K test lines vs 52K
  code lines (0.72x) while sibling core packages run 1.0–4.0x. Parsing is
  the front door for all fact emission.
- **[fact, strengths]** `go test ./... -race` blocks every PR
  (`test.yml:63-65`); e2e runs on every PR against both NornicDB and Neo4j
  (`e2e-tests.yml`, fail-fast disabled); integration tests gate on
  `ESHU_POSTGRES_DSN` and skip cleanly; sampled tests assert behavior (SQL
  shape, telemetry fields), not just absence of error; only 12 `time.Sleep`
  calls across all tests.

### Performance & concurrency

- **[fact, strength] The queue model is production-hardened.** Claims use
  `FOR UPDATE SKIP LOCKED` with lease `claim_until` fencing
  (`go/internal/storage/postgres/reducer_queue_claim_query.go`), enqueue is
  idempotent via `ON CONFLICT (work_item_id) DO NOTHING`
  (`reducer_queue.go:28`), ack/fail require matching `lease_owner`
  (`reducer_queue.go:43,69`), heartbeat rejection surfaces lease loss
  (`reducer_queue.go:291-297`), and all reducer goroutines are context-aware
  and joined (`go/internal/reducer/service.go:123-170`,
  `service_batch.go:39-241`). ADR `docs/internal/design/1289-queue-substrate-
  evaluation-gate.md` documents the proof scenarios.
- **[fact, Medium] Claim-time gating scales with queue depth.** The semantic
  inflight `COUNT(*)` subquery (`reducer_queue_claim_query.go:56-65`) and
  per-domain readiness `EXISTS` checks (`reducer_queue_batch.go:104-223`)
  evaluate per candidate row before `LIMIT`. Documented as a deliberate
  correctness-over-speed tradeoff in ADR 1289; still the first place to look
  when claim latency grows on large scopes.
- **[fact, Medium] No `http.Server` timeouts and no per-query graph
  timeouts.** The Neo4j driver sets connection-level timeouts
  (`go/internal/runtime/data_stores.go:75-77`) but per-query deadlines rely
  entirely on caller contexts; the API server sets no
  `ReadTimeout`/`WriteTimeout`/`IdleTimeout`. A slow client or runaway
  traversal can pin worker goroutines indefinitely.
- **[fact, Medium] Queue/audit retention is operator-dependent.**
  `fact_work_items` and `fact_replay_events` clean up only via generation
  cascade (`schema/data-plane/postgres/005_fact_work_items.sql:4-5`,
  `006_fact_work_item_audit.sql`); no automatic scope GC exists. Long-lived
  scopes grow without bound.
- **[fact, Low] Max-attempt enforcement lives in handlers, not the claim
  query** — a row past `MaxAttempts` is still claimable if a handler
  misbehaves.

### Dependencies

- **[fact, Low] 147 `aws-sdk-go-v2/service/*` modules** (`go/go.mod`) —
  justified by the AWS collector; cost is build time and binary size.
- **[judgment, Medium] Bus-factor on the default graph backend.**
  `github.com/orneryd/nornicdb v1.0.45` is a niche single-maintainer
  project, yet it is Eshu's default canonical store. Neo4j compatibility is
  the mitigation; keep the conformance gate (`go/internal/backendconformance`)
  authoritative.
- **[fact, Low]** No `.golangci.yml` — CI lints with upstream defaults
  (`test.yml:52-57`); three transitive YAML libs and jwt v4+v5 coexist
  (Kubernetes ecosystem; no collision). Lockfiles present and in sync.

### DevEx & operations

- **[fact, Medium] 122 scripts in `scripts/` with no index or README.**
  Naming is consistent (`verify_*`, `test-verify-*`, `monitor_*`) but
  purposes are opaque to anyone but the author.
- **[fact, Low] Setup docs omit tools CI requires.** `CONTRIBUTING.md` never
  mentions buf (needed for `proto/` work) or the mkdocs toolchain; both fail
  in CI for a contributor who follows the docs. Also trivial: CI installs
  mkdocs via `pip` (`test.yml:71`) while `CLAUDE.md` prescribes `uv run`.
- **[fact, strengths]** CI is otherwise exemplary: lint, build, race tests,
  Helm lint, strict docs build, whitespace check, custom evidence gates
  (`test.yml`), Docker/Helm publish, macOS job, daily cron. Telemetry is a
  frozen contract with `eshu_dp_` instruments
  (`go/internal/telemetry/instruments.go`).

### Documentation

- **[fact/judgment, High] The per-package doc-triple is the largest
  maintenance liability in the repo.** 604 × (`doc.go` + `README.md` +
  `AGENTS.md`) ≈ 1,818 files — over 80% of all 1,599 markdown files. Quality
  is currently high (spot-checked 14 packages: 100% present, well-written)
  and a CI gate verifies presence (`scripts/verify-package-docs.sh`), but
  presence ≠ accuracy, and every package change now implies three doc
  reviews. This is process overhead growing linearly with code size.
- **[fact, strength] Public docs are accurate where checked.** CLI flags
  (`go/cmd/eshu/root.go:44-46` vs `docs/public/reference/cli-reference.md`),
  env-var references, and architecture claims match code; the one
  contradiction found is the backend default in `query/code.go` (above).
  Docs build `--strict` on every PR.

### The ugly parts, ranked

1. Frontend entirely outside CI (`test.yml` — no Node step).
2. Hot-path god files: `git_snapshot_native.go`, `ingestion.go`,
   `bootstrap-index/main.go` — the repo's own rule, broken where it matters
   most.
3. `password: change-me` shipping in Helm values (`values.yaml:831`).
4. No HTTP server or per-query graph timeouts.
5. 1,818 doc-triple files + 122 unindexed scripts of pure process weight.

## Improvement Strategy

**Theme 1 — One quality regime for every shipped surface.** The Go core has
gates for everything; the TS console and site have none. Target: CI fails on
TS typecheck/test failures exactly as it does for Go. Principle: if it ships,
it gates.

**Theme 2 — The repo must obey its own constitution on the hot path.**
CLAUDE.md's 500-line rule and no-panic discipline are violated precisely
where correctness matters most (ingestion, bootstrap). Target: no
runtime-logic file on the collector→reducer path above ~500 lines; zero
panics reachable from request/work handling. Principle: rules earn trust by
applying to the hardest code first.

**Theme 3 — Secure-by-default at the deployment seam.** Defaults today
(password, binds, timeouts, retention) assume a competent operator reads
everything. Target: `helm template` fails on default credentials; servers
ship with timeouts; retention has a documented automated path. Principle:
the lazy path must be the safe path.

**Theme 4 — Cap process overhead so a solo maintainer can keep the pace.**
Doc triples and scripts grow O(packages). Target: drift detection (the
`.eshu-doc-state/stale.jsonl` mechanism + `eshu-folder-doc-keeper` skill)
becomes the enforced loop, a generated index covers `scripts/`, and the
AGENTS.md layer is audited for genuinely scoped content vs. boilerplate.
Principle: automate what governance demands, or shrink the demand.

**Explicitly not fixing:** AWS SDK breadth (product-justified); claim-query
readiness `EXISTS` cost (documented ADR-1289 tradeoff — revisit only with
measurements); transitive YAML/jwt duplication (ecosystem reality); macOS
`-race` omission (platform limitation); the `Sprintf` Cypher builder beyond
a comment (validated allowlists make rewrite low-payoff).

**Done looks like:** CI red on TS errors; zero >800-line files on the
ingestion path and a tracked burn-down to 500; `helm template` errors on
`change-me`; `http.Server` timeouts set in all serving binaries; coverage
published per PR with a ratchet on `parser`; zero High findings open.

## Task Plan

### Quick wins (do immediately)

| ID | Task | Effort |
| --- | --- | --- |
| T1 | Frontend CI gate | S |
| T2 | Go coverage reporting in CI | S |
| T6 | Fix backend-default mismatch + panic in `query/code.go` | S |
| T9 | `scripts/README.md` index + CONTRIBUTING setup additions | S |

### Milestone 0 — Safety net

| ID | Task | Files/areas | Acceptance | Effort | Risk | Deps |
| --- | --- | --- | --- | --- | --- | --- |
| T1 | Add Node job to CI running `typecheck`, `test`, `console:typecheck`, `console:test` | `.github/workflows/test.yml` | PR with a TS type error fails CI | S | Low | — |
| T2 | Add `-coverprofile` + artifact/summary to Go test step | `test.yml` | Coverage % visible per PR | S | Low | — |
| T3 | Characterization tests around `git_snapshot_native.go` and `ingestion.go` current behavior (fixtures already exist in `tests/fixtures/`) | `go/internal/collector`, `go/internal/storage/postgres` | Refactor in T8 can run against frozen behavior snapshots | L | Low | — |

### Milestone 1 — Critical fixes (security & correctness)

| ID | Task | Files/areas | Acceptance | Effort | Risk | Deps |
| --- | --- | --- | --- | --- | --- | --- |
| T4 | Helm refuses default Neo4j password: `fail` in template when `change-me` and no existing secret; document override | `deploy/helm/eshu/values.yaml:827-833`, templates, deploy docs | `helm template` with defaults errors; CI helm-lint passes | M | Medium (breaks lazy installs — intended) | — |
| T5 | Add `http.Server` Read/Write/Idle timeouts to api/mcp/status servers; audit `sourcecypher` executor for per-query deadline propagation | `go/internal/app`, `go/internal/runtime`, `go/internal/storage/cypher` | Timeouts configurable, defaults set; evidence marker per repo gate | M | Medium | — |
| T6 | `query/code.go:56-62`: default empty → NornicDB via `ParseGraphBackend`, return error instead of panic | `go/internal/query/code.go` | Regression test for empty/invalid backend; no panic path | S | Low | — |
| T7 | Unit tests for `repositoryidentity`, `tfstatewarning`, `vulnsource` | `go/internal/{repositoryidentity,tfstatewarning,vulnsource}` | Behavior-asserting tests; packages no longer zero-coverage | M | Low | T2 |

### Milestone 2 — High-leverage improvements

| ID | Task | Files/areas | Acceptance | Effort | Risk | Deps |
| --- | --- | --- | --- | --- | --- | --- |
| T8 | Split the three hot-path god files into <500-line single-purpose modules (snapshot phases; ingestion fact vs queue handling; bootstrap phase orchestration) | `git_snapshot_native.go`, `ingestion.go`, `cmd/bootstrap-index/main.go` | All files <500 lines; characterization tests green; no-regression evidence per repo gate | XL (one L per file) | High | T3 |
| T9 | `scripts/README.md` categorized index; add buf + mkdocs setup to `CONTRIBUTING.md` | `scripts/`, `CONTRIBUTING.md` | New contributor path passes without CI surprises | S | Low | — |
| T10 | Queue/audit retention: scope GC job or documented cron + `fact_replay_events` retention policy | `go/internal/storage/postgres`, `schema/`, operate docs | Retention behavior documented and testable; depth gauge alert documented | M | Medium | — |
| T11 | Commit explicit `.golangci.yml` pinning the lint contract | `go/.golangci.yml` | CI uses committed config | S | Low | — |

### Milestone 3 — Quality & polish

| ID | Task | Files/areas | Acceptance | Effort | Risk | Deps |
| --- | --- | --- | --- | --- | --- | --- |
| T12 | Parser coverage uplift toward sibling-package ratio, prioritizing dispatch and error paths | `go/internal/parser` | Ratio ≥0.9x or documented rationale | L | Low | T2 |
| T13 | Measure claim latency at depth (10K+ rows); optimize semantic-gate COUNT only if measured hot | `reducer_queue_claim_query.go`, ADR | Before/after evidence per ADR-1289 rules | M | Medium | — |
| T14 | AGENTS.md layer audit: dedupe boilerplate triples into inherited scope where harness allows | `go/**/AGENTS.md`, doc-keeper skill | Doc-file count reduced or drift automation proven | M | Low | — |
| T15 | Env-var the MinIO creds in tier2 compose; add default-credential grep to CI hygiene step | `docker-compose.tier2-tfstate*.yaml`, `test.yml` | No literal default creds in compose | S | Low | — |

### Top-3 implementation sketches

**T1 — Frontend CI gate.** Add a `frontend` job to `test.yml`:
`actions/setup-node@v4` (node 22, npm cache), `npm ci`, then the four
existing package.json scripts. Gotchas: `console:*` uses its own vite
config — run from repo root as scripted; Playwright is in devDeps but no
e2e script exists, so do not install browsers (skip `npx playwright
install`) until a real e2e target exists; keep the job parallel to the Go
job so wall time doesn't grow.

**T4 — Helm password hard-fail.** In the neo4j secret template, wrap with
`{{- if and (not .Values.neo4j.auth.existingSecret) (eq
.Values.neo4j.auth.password "change-me") }}{{ fail "set
neo4j.auth.password or provide an existing secret" }}{{- end }}`. Gotchas:
preserve upgrade path for existing installs that overrode the value (no-op
for them); update `docs/public/deploy/kubernetes/` in the same PR per the
docs-lockstep rule; e2e compose paths don't use the chart, so CI impact is
helm-lint only — add a `helm template` negative test to
`scripts/test-verify-*` style gate.

**T8 — Split the god files.** Order: `ingestion.go` first (most downstream
risk, best test surface), then `git_snapshot_native.go`, then
`bootstrap-index/main.go`. Approach: extract by existing phase seams —
ingestion splits into fact-write vs queue-enqueue vs status modules;
snapshot splits per phase (enumerate, filter, hash, emit); bootstrap main
splits into a `phases` package mirroring the documented 4-phase pipeline
(`docs/internal/agent-guide.md:183-189`). Pure moves + unexported seams
first, behavior changes never in the same PR. Gotchas: the repo's
performance-evidence gate (`scripts/verify-performance-evidence.sh`) will
trigger on touched hot-path files — include `No-Regression Evidence:`
markers with a fixture-corpus run; package doc triples must be updated in
the same PR (`eshu-folder-doc-keeper`).

## Open Questions

1. **Is the console a shipped production surface or an internal tool?**
   Determines whether T1 should also grow a real Playwright e2e (and whether
   Playwright stays a dependency at all).
2. **NornicDB ownership:** is `orneryd/nornicdb` effectively a first-party
   fork you control, or a true third-party dependency? Answer changes the
   bus-factor risk rating and whether pinning + conformance gating is
   sufficient.
3. **Scope lifecycle intent:** should scope GC/retention be automatic
   (a reducer-owned reaper) or remain an operator runbook task? T10 design
   depends on this.
4. **Doc-triple appetite:** is the per-package `AGENTS.md` layer earning its
   keep under your current harnesses, or can scoped instructions be hoisted
   to directory-tree roots? This is the single biggest lever on maintenance
   overhead.
5. **Performance contract numbers:** is there a target claim latency /
   bootstrap wall time for a reference corpus? T13's stop threshold needs a
   number, per the repo's own evidence rules.
