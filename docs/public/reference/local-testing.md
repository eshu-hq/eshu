# Local Testing Reference

This page is the verification map for engineers and agents changing Eshu. For
first-time setup, use [Run Locally](../run-locally/index.md). For operator
checks, use [Operate Eshu](../operate/index.md) and
[Health Checks](../operate/health-checks.md).

Use the smallest gate that proves the touched behavior, then run the hygiene
checks required by the files you changed. Do not call work ready without citing
the commands you actually ran.

Changing `go/internal/ifa`, `go/cmd/ifa`, or anything else the conformance
platform covers? See [Run the proof suite](../guides/run-the-proof-suite.md)
and [The Ifá conformance platform](../concepts/ifa-conformance-platform.md)
for that platform's own commands and layers.

## Before You Push

Use this fixed promotion order before opening or updating a PR:

1. Complete TDD and the focused proof for every touched surface, including any
   applicable frontend, security, runtime, or Ifa gates.
2. Run a preliminary full `eshu-code-review` of the rebased diff. If it reports
   any P0, P1, or P2 finding, fix every finding, rerun affected focused proof,
   and repeat the full review. Do **not** run `make pre-pr` while findings remain.
3. Once the preliminary verdict is `P0=0, P1=0, P2=0` and the branch is
   otherwise ready to push, run `make pre-pr` exactly once as the late promotion
   gate. Use `make pre-pr-full` here instead when the risk tier requires the
   whole-module race lane.
4. Run a final full `eshu-code-review` against the exact post-preflight diff.
   Make no edits before push; any diff change invalidates the verdict and
   restarts this sequence at focused proof.

`make pre-pr` is the one-command local mirror of the credential-free CI gates,
so format, exactness, race, contract, docs, and (for Go changes) security
failures are caught in a single local pass instead of across multiple
~20-minute CI rounds:

```bash
make pre-pr            # or: bash scripts/dev/pre-pr.sh
```

CI remains the authoritative, non-bypassable source of truth — but it should
rarely be the *first* place you learn about a credential-free failure. Two
expectations are firm:

- **Exactness gates are blocking** when matching code, spec, fixture, cassette,
  or generated-contract inputs change. `make pre-pr` selects and runs them.
- **Race gates are blocking** when Go implementation code changes. `make pre-pr`
  runs the targeted/scoped race lane; `make pre-pr-full` adds the whole-module
  `go test ./... -race`, and CI runs the authoritative full race gate.

Frontend- and security-heavy lanes are not in `make pre-pr` (they need Node, the
network, or are slow); run `make frontend-preflight` / `make security-preflight`
when you touch those surfaces. None of these silently skip: a gate that cannot
run locally prints why and names the CI gate that remains authoritative.

It runs gofumpt and golangci-lint over the **whole** module (catching
cross-package consequences a changed-package run misses, such as code that
becomes unused when a sibling package changes), `go build` and `go vet` over the
whole module, `go test` on the packages changed versus `origin/main`, the
500-line file cap and package-docs gates, and — driven by the gate registry
(#4214) — the **selected credential-free exactness and telemetry contract gates**
for your changed paths (OpenAPI, route coverage, edge source-tool coverage,
evidence continuity, fact-kind registry, contract source-of-truth, parser
relationship kit, query-plan regression, scale corpus/benchmark, capability
budget, collector entrypoints, skill roundtrip, telemetry coverage, operator
dashboard, and so on). You no longer have to remember which verifier matches
your change — the changed-path selector picks them. A docs-only or no-op change
runs none of them. Docker/NornicDB/Postgres/credentialed gates remain CI-only
and are printed (with a reason), never run locally. Integration suites that need
Postgres or NornicDB are not run here — use the focused Compose gates below for
those.

It also runs a **race lane** for Go changes (#4215): the targeted graph-write
race set (the registry's `race-graph-writes` gate, mirroring
`.github/workflows/race-graph-writes.yml`) when a graph-write package changes,
plus a scoped `go test -race` on any other changed Go packages. The
Postgres-backed reducer-contention race gate is reported CI-only, not run
locally. **CI remains the authoritative blocking race gate** (whole-module
`go test ./... -race`); for a local whole-module race before a high-risk PR:

```bash
make pre-pr-full      # pre-pr + `go test ./... -race`
```

For frontend changes, a separate focused preflight mirrors `.github/workflows/frontend.yml`
(#4216) — root-site and console typecheck/test/build, console a11y (critical +
serious block), the ESLint flat config, npm audit (high/critical block), the
per-page console e2e, and changed-file Prettier — selected by changed path:

```bash
make frontend-preflight      # or: bash scripts/dev/frontend-preflight.sh
```

These gates need Node and installed dependencies; if `node_modules` is missing
the npm commands fail loudly (run `npm ci` first) rather than skipping silently.

For dependency or deploy changes, a security preflight mirrors the
credential-free `security-scan.yml` jobs (#4217) — whole-module gosec,
govulncheck, nancy, and an optional Trivy filesystem scan — selected by changed
path:

```bash
make security-preflight      # or: bash scripts/dev/security-preflight.sh
```

govulncheck and nancy need network for their advisory databases; Trivy is
optional and the `trivy-fs` gate prints setup guidance and defers to CI when
`trivy` is not installed (never a silent pass). **CI remains authoritative** for
SARIF uploads, the Trivy image scan, and release/package security checks — those
stay CI-only.

To see exactly which credential-free CI verifiers apply to the paths you
changed — and why — use the gate selector:

```bash
# Show which gates would run for this branch (with explanations):
bash scripts/dev/select-gates.sh --base origin/main --tier pre-pr --explain

# Run them:
bash scripts/dev/run-selected-gates.sh --base origin/main --tier pre-pr

# Verify the registry itself is consistent (refs exist on disk):
bash scripts/verify-ci-gates-registry.sh

# Also verify hooks/pre-pr/workflows have not drifted from the registry:
bash scripts/verify-ci-gates-registry.sh --drift
```

The registry lives at `specs/ci-gates.v1.yaml`. Gates marked CI-only (no local
command) are always printed with a reason but never executed locally.

The `--drift` check (#4220) keeps `.pre-commit-config.yaml` and
`.github/workflows/` in lockstep with the registry: every local pre-commit hook
must map to a gate's `hook_id` or a declared `hygiene_hooks` entry, and every
workflow must be referenced by a gate or listed in `non_gate_workflows` with a
reason. It runs in pre-commit (the `gate-registry-drift` hook) and in CI
(`verify-ci-gate-registry.yml`), so adding a workflow or hook without registering
it fails fast. (Reconciling `make pre-pr`'s step set against the registry is
[#4214](https://github.com/eshu-hq/eshu/issues/4214), which makes `pre-pr.sh`
registry-driven via the gate selector instead of a hard-coded step list.)

### CI workflow shape

The CI side is consolidated and path-filtered (#4218) so a PR runs only the
gates its changed paths select:

- **Always-on (runs on every PR, including docs-only):** agent hygiene, the
  ci-gate registry drift check, the docs build + Helm lint + whitespace
  (`docs-helm-hygiene` in `test.yml`), and — because a docs path can still carry
  a leaked secret or a stale published claim — Trivy's filesystem secret/IaC scan
  (`trivy-fs`) and the capability `-mode docs` guard (`capability-verify`). These
  are the due-diligence gates a documentation change still needs.
- **Path-selected (blocking):** the static contract verifiers — OpenAPI, route
  coverage, edge source-tool coverage, evidence continuity, skillgen roundtrip,
  telemetry coverage, operator dashboard, and contract source-of-truth — are
  consolidated into one matrix workflow, `static-contract-gates.yml`, whose
  `changes` job runs each only when its registry paths change. The golden-corpus,
  replay, race, and reducer-contention gates remain path-filtered and blocking.
- **Path-selected (heavy):** the whole-module Go build/lint/vet/test and the
  sharded `go-race` lanes (`test.yml`), the two-OS binary build (`build.yml`),
  the Go-source security scanners (`govulncheck`/`gosec`/`nancy` in
  `security-scan.yml`), the Go MCP drift jobs (`mcp-tool-count`/`mcp-test-suite`),
  end-to-end tests, and macOS CI all **skip a docs-only PR** — one whose every
  changed file is under `docs/**`, a root-level `*.md`, or `mkdocs.yml`. `build.yml`
  skips via a `pull_request` `paths-ignore`; the mixed workflows (`test.yml`,
  `security-scan.yml`, `mcp-schema-drift.yml`) skip per-job via a `changes` gate,
  so their always-on jobs above keep running. A package doc under `go/**/*.md`
  still counts as code, and any PR that mixes docs with code runs the full set.
  `main`, the nightly schedule, and tag pushes run everything unconditionally as
  the backstop. The `go-race-complete` umbrella reports green when the matrix is
  skipped, so it stays a stable check name that is safe to mark required without
  stranding a docs-only PR.
- **Advisory:** the benchmark regression check (`BENCH_REGRESSION_ENFORCE=false`)
  and the changed-file Prettier check do not block merge.
- **CI-only / release-only:** Trivy image scan, GHCR/package publication, and
  release-attestation checks require credentials and never run locally or on a
  normal PR.

Exactness and race gates stay **blocking** when their matching code, spec,
fixture, or generated-contract inputs change — consolidation changed where they
run, not whether they block.

### What `make pre-pr` selects, by change type

`make pre-pr` always runs the whole-module Go gates (gofumpt, golangci-lint,
build, vet, file cap) plus the focused changed-package tests; the table is what
the changed-path selector *adds* on top. You never have to remember the matching
verifier — the selector picks it.

| You changed | `make pre-pr` additionally runs | Also run |
| --- | --- | --- |
| Docs only (`docs/**`, `*.md`) | nothing extra (no exactness/race selected) | docs build (pre-push) |
| Frontend only (`src/**`, `apps/console/**`) | nothing backend | `make frontend-preflight` |
| Parser (`go/internal/parser/**`) | parser relationship kit, accuracy golden gate, scoped race | — |
| Reducer / storage (`go/internal/reducer/**`, `storage/**`) | query-plan regression, scale gates, **targeted graph-write race** | reducer-contention is CI-only (Postgres) |
| Collector (`go/internal/collector/**`) | edge source-tool coverage, evidence continuity, scale corpus | — |
| API / MCP (`go/internal/query/**`, `go/internal/mcp/**`) | OpenAPI surface, route coverage, MCP schema drift, capability budget, operator dashboard | — |
| Facts / contracts (`go/internal/facts/**`, `specs/*.v1.yaml`) | fact-kind registry, contract source-of-truth, evidence continuity | — |
| `go.mod` / `go.sum` | nothing extra (the whole-module Go gates always run) | pre-push runs changed-file gosec; `make security-preflight` runs whole-module gosec, govulncheck, nancy |
| Deploy / runtime (`Dockerfile`, `deploy/**`, `docker-compose*`) | — | `make security-preflight` (Trivy fs); golden-corpus + e2e are CI-only (Docker) |

Run `bash scripts/dev/select-gates.sh --base origin/main --tier pre-pr --explain`
to see exactly what your branch selects and why.

## Common Compose Environment

When running commands directly against the default local Compose stack:

```bash
export ESHU_GRAPH_BACKEND=nornicdb
export NEO4J_URI=bolt://localhost:7687
export NEO4J_USERNAME=neo4j
export NEO4J_PASSWORD=change-me
export DEFAULT_DATABASE=nornic
export ESHU_NEO4J_DATABASE=nornic
export ESHU_CONTENT_STORE_DSN=postgresql://eshu:change-me@localhost:15432/eshu
export ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:15432/eshu
```

For `docker-compose.neo4j.yml`, use `ESHU_GRAPH_BACKEND=neo4j` and database
`neo4j` instead.

## What To Run

| Change area | Use this page |
| --- | --- |
| Onboarding first-answer dogfood proof | [First five minutes benchmark](local-testing/first-five-minutes-benchmark.md) |
| Cross-surface answer-quality dogfood proof | [Answer Quality Scorecard](local-testing/answer-quality-scorecard.md) |
| Ask Eshu API + SSE + guardrail local proof | [Ask Eshu Local Proof](local-testing/ask-eshu-local-proof.md) |
| Remote all-collector Compose proof | [Remote collector E2E](local-testing/remote-collector-e2e.md) |
| Confluence, Jira, vulnerability source, and live registry smokes | [Collector live smokes](local-testing/collector-live-smokes.md) |
| Cassette, replay, and no-provider proof authoring | [Cassette and replay proof](cassette-replay.md) |
| Normal package, Compose, graph, Terraform-state, webhook, and docs gates | [Verification gates](local-testing/verification-gates.md) |
| Discovery report loop for noisy repositories | [Discovery advisory playbook](local-testing/discovery-advisory.md) |
| Worker knobs, pprof, and phase CPU profile capture | [Profiling and concurrency](local-testing/profiling-and-concurrency.md) |
| Postgres pool, queue, hot-table, or search-index write pressure | [Postgres tuning](postgres-tuning.md) |

## Quick Verification Matrix

| If you touched | Minimum verification |
| --- | --- |
| CI gate registry (`specs/ci-gates.v1.yaml`), `internal/cigates`, `cmd/ci-gates`, `.pre-commit-config.yaml`, `scripts/dev/pre-pr.sh`, or `.github/workflows/*` | `cd go && go test ./internal/cigates ./cmd/ci-gates -count=1`, `bash scripts/verify-ci-gates-registry.sh --drift`, and `bash scripts/test-verify-ci-gates-registry.sh` |
| Answer-quality scorecard criteria, CLI, or docs | `cd go && go test ./internal/answerquality -count=1`, `cd go && go test ./cmd/eshu -run 'TestAnswerQualityScorecardCommand' -count=1`, and the docs build |
| Ask Eshu answer path, guardrail, or local proof | `scripts/test-verify-ask-eshu-local-proof.sh`, `scripts/verify-ask-eshu-local-proof.sh`, and the docs build (see [Ask Eshu Local Proof](local-testing/ask-eshu-local-proof.md)) |
| Competitive parity gate criteria, CLI, or docs | `cd go && go test ./internal/competitiveparity -count=1`, `cd go && go test ./cmd/eshu -run 'TestCompetitiveParity|TestRootCommandIncludesCompetitiveParity' -count=1`, `cd go && go run ./cmd/eshu competitive-parity validate --repo-root .. --json`, and the docs build |
| Portable evidence bundle schema, CLI, or docs | `cd go && go test ./internal/evidencebundle -count=1`, `cd go && go test ./cmd/eshu -run 'TestEvidenceBundle|TestRootCommandIncludesEvidenceBundle' -count=1`, and the docs build |
| Performance contract thresholds (`local-performance-envelope.md`, `reducer-claim-latency-gate.md`, `hybrid-retrieval-production-gate.md`) or `go/internal/perfcontract` | `cd go && go test ./internal/perfcontract -count=1` (doc↔code lockstep; fails if a documented threshold drifts from its in-code value) and the docs build if a doc threshold changed |
| Remote remediation benchmark wrapper | `bash scripts/test-verify-remote-e2e-remediation-benchmark.sh` |
| Remote full-corpus degradation report classifier | `bash scripts/test-verify-remote-e2e-degradation-report.sh` |
| Docs, `CLAUDE.md`, `AGENTS.md`, or README files | `bash scripts/test-verify-docs-build-changed.sh` and `bash scripts/verify-docs-build-changed.sh` (changed-path mkdocs build, mirrors pre-push hook); also `cd go && go run ./cmd/capability-inventory -mode docs` and `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml` for a full build |
| GitHub workflow or CodeQL setup guidance | `scripts/test-verify-codeql-setup.sh` and `scripts/verify-codeql-setup.sh` |
| CLI/runtime wiring | `cd go && go test ./cmd/eshu ./cmd/api ./cmd/mcp-server -count=1` |
| Pre-change impact or developer change plan API/MCP/CLI surface | `cd go && go test ./internal/query ./internal/mcp ./cmd/eshu -run 'TestDeveloperChangePlan|TestPreChangeImpact|TestChangePlan|TestFetchChangePlan|TestRunChangePlan|TestResolveRouteMapsDeveloperChangePlan|TestOpenAPIDeveloperChangePlan' -count=1` |
| Status/admin or completeness contract | `cd go && go test ./internal/status ./internal/query ./cmd/api -count=1` and `cd go && go vet ./internal/status ./internal/query ./cmd/api` |
| Replatforming plan, ownership-packet, or rollup API/MCP surface | `cd go && go test ./internal/mcp -run TestReplatforming -count=1` (see [Verification gates → Replatforming API/MCP parity proof](local-testing/verification-gates.md#replatforming-apimcp-parity-proof)) |
| Parser platform or collector snapshot flow | `cd go && go test ./internal/parser ./internal/collector/discovery ./internal/collector -count=1` |
| Terraform provider-schema evidence or relationship extraction | `cd go && go test ./internal/terraformschema ./internal/relationships ./internal/storage/postgres -count=1` |
| Parser, language-query, dead-code maturity, or relationship contribution docs | `scripts/verify-parser-relationship-kit.sh` plus the focused parser, query, relationship, or docs gate for the touched surface. |
| Compose, Helm, or deployable runtime shape | `cd go && go test ./cmd/api ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer -count=1` and `helm lint deploy/helm/eshu` |
| Argo CD or GitOps overlay rendered shape | `scripts/test-verify-gitops-rendered-diff-preflight.sh` and `scripts/verify-gitops-rendered-diff-preflight.sh --overlay deploy/argocd/overlays/aws --values values.private.yaml` |
| Hosted Helm install, upgrade, or rollback proof | `scripts/test-verify-hosted-helm-rollout-proof.sh`, `scripts/verify-hosted-helm-rollout-proof.sh --out-dir .proof/helm-install`, and `helm lint deploy/helm/eshu` |
| Hosted API/MCP auth, secret, or exposure posture | `scripts/test-verify-hosted-security-posture.sh`, `scripts/verify-hosted-security-posture.sh -f values.eshu.yaml`, and `helm lint deploy/helm/eshu -f values.eshu.yaml` |
| Hosted NetworkPolicy egress posture | `scripts/test-verify-hosted-network-policy-egress.sh`, `scripts/verify-hosted-network-policy-egress.sh -f values.eshu.yaml`, and `helm lint deploy/helm/eshu -f values.eshu.yaml` |
| Hosted governance local proof posture | `scripts/test-verify-hosted-governance-proof.sh`, `scripts/verify-hosted-governance-proof.sh`, and the docs build. This includes local no-policy governance status, no-provider semantic status, and no-provider semantic queue planning. |
| Hosted governance remote Compose proof | `scripts/test-verify-hosted-governance-remote-compose-proof.sh`, `scripts/verify-hosted-governance-remote-compose-proof.sh`, and `scripts/verify-hosted-governance-remote-compose-proof.sh --runtime` after the remote Compose stack is running |
| Hosted governance proof artifact | `scripts/test-verify-hosted-governance-proof-artifact.sh` and `scripts/verify-hosted-governance-proof-artifact.sh --input governance-proof.json --output-json governance-proof.summary.json --output-markdown governance-proof.summary.md` |
| Hosted governance Helm proof | `scripts/test-verify-hosted-governance-helm-proof.sh`, `scripts/verify-hosted-governance-helm-proof.sh --out-dir .proof/governance-helm --values values.eshu.yaml`, and `helm lint deploy/helm/eshu -f values.eshu.yaml` |
| Hosted governance negative leakage proof | `scripts/test-verify-hosted-governance-negative-leakage-proof.sh` and `scripts/verify-hosted-governance-negative-leakage-proof.sh --manifest leakage-proof.json --output-json leakage-proof.summary.json --output-markdown leakage-proof.summary.md` |
| Hosted auth audit and revocation proof | `scripts/test-verify-hosted-auth-audit-proof.sh` and `scripts/verify-hosted-auth-audit-proof.sh --input auth-audit-proof.json --output-json auth-audit-proof.summary.json --output-markdown auth-audit-proof.summary.md` |
| Hosted governance retention-state proof | `scripts/test-verify-hosted-governance-retention-proof.sh` and `scripts/verify-hosted-governance-retention-proof.sh --input retention-proof.json --output-json retention-proof.summary.json --output-markdown retention-proof.summary.md` |
| Hosted-growth Postgres fact and queue proof | `scripts/test-verify-hosted-growth-postgres-proof.sh` and `scripts/verify-hosted-growth-postgres-proof.sh --input hosted-growth-proof.json --output-json hosted-growth-proof.summary.json --output-markdown hosted-growth-proof.summary.md`; the proof must include the #4044 `fact_records` growth breakpoint fields: fact-family growth, index bloat, graph-write pressure, query plans, retention lag/prune cost, and the partition/archive/split/retention/defer decision |
| Hosted backup, restore, or graph-rebuild proof | `scripts/test-verify-hosted-backup-restore-proof.sh` and `scripts/verify-hosted-backup-restore-proof.sh --input restore-proof.json --output-json restore-proof.summary.json --output-markdown restore-proof.summary.md` |
| Compose-to-Kubernetes runtime parity | `scripts/test-verify-compose-helm-runtime-parity.sh`, `scripts/verify-compose-helm-runtime-parity.sh`, and `helm lint deploy/helm/eshu` |
| `docs/public/deploy/kubernetes/storage.md` "Bundled NornicDB" values example | `scripts/test-verify-storage-doc-bundled-nornicdb-example.sh` and `scripts/verify-storage-doc-bundled-nornicdb-example.sh` — renders the embedded `neo4j.auth` example through `helm template` so a stale `secretName`/`password` pairing fails locally instead of only when a reader copy-pastes it |
| Hosted ops dashboard or alert pack | `scripts/test-verify-hosted-ops-alert-pack.sh`, `scripts/verify-hosted-ops-alert-pack.sh`, and `helm lint deploy/helm/eshu` |
| Accuracy golden gate (complexity, resolvers, correlation), or per-language cyclomatic complexity, cross-repo call resolvers, or correlation precision | `scripts/verify_accuracy_golden_gate.sh` (or `cd go && go test ./internal/accuracygate -count=1`); update `go/internal/accuracygate/testdata/baseline.json` only to raise a floor after a measured improvement (see [Accuracy Golden Gate](accuracy-golden-gate.md)) |
| Facts-first indexing, queue, or resolution flow | `cd go && go test ./internal/projector ./internal/reducer ./internal/storage/postgres -count=1` |
| Recovery, replay, or repair controls | `cd go && go test ./internal/recovery ./internal/runtime ./internal/status -count=1` |
| Hot-path Cypher, graph writes, queues, workers, leases, batching, or runtime knobs | `scripts/test-verify-performance-evidence.sh`, `scripts/verify-performance-evidence.sh`, `scripts/test-verify-query-plan-regression.sh`, and `scripts/verify-query-plan-regression.sh` |
| Graph backend query-plan fixture contract | `scripts/test-verify-query-plan-regression.sh` and `scripts/verify-query-plan-regression.sh` |
| Scale-lab representative corpus, privacy, metric, or threshold contract | `bash scripts/test-verify-scale-corpus-suite.sh` and `bash scripts/verify-scale-corpus-suite.sh` |
| Scale benchmark artifact, threshold, backend, commit, or before/after proof contract | `bash scripts/test-verify-scale-benchmark-artifact.sh` and `bash scripts/verify-scale-benchmark-artifact.sh` |
| Go micro-benchmark CI workflow (`.github/workflows/bench.yml`) or its runner `scripts/run-go-benchmarks.sh` | `bash scripts/test-run-go-benchmarks.sh` (static contract + fast hermetic functional check); `bash scripts/run-go-benchmarks.sh` for the full credential-free benchmark sweep that the workflow uploads as an artifact |
| Benchmark regression gate (`scripts/verify-bench-regression.sh`, `testdata/benchmarks/baseline.txt`, the weekly `bench-baseline-refresh` workflow) | `bash scripts/test-verify-bench-regression.sh` (static contract + synthetic benchstat parser checks); `bash scripts/verify-bench-regression.sh` compares the current run against the committed baseline with `benchstat` (advisory by default — set `BENCH_REGRESSION_ENFORCE=true` to block; baseline is recaptured on ubuntu-latest weekly via `scripts/refresh-bench-baseline.sh`) |
| Capability-matrix p95 latency or max-scope budget proof contract | `bash scripts/test-verify-capability-budget-proof.sh` and `bash scripts/verify-capability-budget-proof.sh` |
| B-7 golden end-to-end corpus gate (any pipeline phase: collector/parser/projector/reducer/query/storage, the B-10 cassettes, or the B-12 snapshot) | `cd go && go test ./cmd/golden-corpus-gate -count=1` and `bash scripts/test-verify-golden-corpus-gate.sh` (unit + static contract); `bash scripts/verify-golden-corpus-gate.sh` for the full live run (needs Docker; runs bootstrap + B-10 cassettes + reducer drain and diffs the B-12 snapshot — see [Golden Corpus Gate](local-testing/golden-corpus-gate.md)) |
| B-11 macro per-phase wall-clock baseline (`testdata/golden/e2e-baseline.json`) or the refresh script | `cd go && go test ./cmd/golden-corpus-gate -count=1` and `bash scripts/test-refresh-e2e-baseline.sh` (unit + static contract); `bash scripts/refresh-e2e-baseline.sh` recaptures the baseline on the enforcement host (needs Docker — see [Golden Corpus Gate → Macro per-phase regression (B-11)](local-testing/golden-corpus-gate.md#macro-per-phase-regression-b-11)) |
| Cassette or replay authoring docs, contributor conformance flow, input tape, parser fixture, authz replay catalog, or replay proof boundary | `cd go && go test ./conformance -count=1`, `cd go && go test ./internal/replay/... -count=1`, `bash scripts/test-verify-replay-coverage-gate.sh`, `bash scripts/verify-replay-coverage-gate.sh --blocking`, and the docs build (see [Cassette and Replay Proof](cassette-replay.md)) |
| API/MCP/CLI golden response-shape change or B-12 `query_shapes` update | `(cd go && go test ./conformance ./internal/replay/... ./cmd/replay-coverage-gate ./internal/replaycoverage -count=1)`, `bash scripts/test-verify-replay-coverage-gate.sh`, `bash scripts/verify-replay-coverage-gate.sh --blocking`, `(cd go && go test ./cmd/golden-corpus-gate -count=1)`, `bash scripts/test-verify-golden-corpus-gate.sh`, and `bash scripts/verify-golden-corpus-gate.sh` when the committed B-12 snapshot or query shapes change; `query_shapes.http`, `query_shapes.mcp`, and `query_shapes.cli` are asserted by the B-7 gate |
| C-1/C-8/C-9/C-10 replay coverage manifest + lockstep gate (a new implemented-lane collector, fact-kind read surface, CLI read surface in `query_shapes.cli`, parser, capability claim, product claim, authorization permission family/scoped route, required scenario type, the coverage manifest, or `go/internal/replaycoverage`) | `cd go && go test ./cmd/replay-coverage-gate ./internal/replaycoverage -count=1` and `bash scripts/test-verify-replay-coverage-gate.sh` (unit + static contract); `bash scripts/verify-replay-coverage-gate.sh --blocking` runs the same blocking gate CI enforces over the source-of-truth registries, runs the focused `authz-scoped-route-tests` query proof, and fails on any supported surface/scenario_type pair lacking a replay scenario while writing the C-7 coverage report with per-axis and per-scenario-type percentages. Omit `--blocking` only for local exploratory/advisory reports. Static, credential-free, Docker-free — composes with the golden-corpus-gate, replay tier, parser fixtures, capability inventory, Go race tests, authz scoped-route tests, and budget proof gates that actually run the scenarios it counts |
| #5335 read-surface consumer-existence gate (`specs/language-feature-parity-ledger.v1.yaml` `read_surfaces`, `specs/fact-kind-registry.v1.yaml` `read_surface`, or `go/internal/query/impact_blast_radius.go`) | `cd go && go test ./internal/mcp ./internal/query ./internal/replaycoverage ./cmd/api -count=1` — see [Read-Surface Consumer-Existence Gate](read-surface-consumer-existence-gate.md) |
| New collector family, provider, scanner, or hosted collector runtime | `scripts/test-verify-collector-authoring-gate.sh` and `scripts/verify-collector-authoring-gate.sh` |
| Generated collector entrypoint manifest or generated collector command files | `scripts/test-verify-collector-entrypoints-generated.sh` and `scripts/verify-collector-entrypoints-generated.sh` |
| Skillgen roundtrip baseline (`skill-fragments/`, `expected/`, `go/cmd/skillgen/`, `go/internal/extensions/skillgen/`, `specs/surface-inventory.v1.yaml`, or the gate script itself) | `cd go && go build ./cmd/skillgen/...` and `bash scripts/test-verify-skill-roundtrip.sh` plus `bash scripts/verify-skill-roundtrip.sh` (and the docs build when the matrix or skillgen doc changes) |
| Evidence-continuity matrix, evidence-centric capability rows, or API/MCP proof coverage | `scripts/test-verify-evidence-continuity.sh`, `scripts/verify-evidence-continuity.sh`, and `cd go && go test ./internal/evidencecontinuity -count=1` |
| Fact-kind registry contract, fact schema-version metadata, or generated fact registry docs | `scripts/test-verify-fact-kind-registry.sh`, `scripts/verify-fact-kind-registry.sh`, and `cd go && go test ./internal/facts ./cmd/fact-kind-registry -count=1` |
| New or changed Go package under `go/internal` or `go/cmd` | `scripts/test-verify-package-docs.sh` and `scripts/verify-package-docs.sh` |
| New or changed telemetry registration, pipeline stage, or `docs/public/observability/telemetry-coverage.md` row (the X2 coverage gate) | `scripts/test-verify-telemetry-coverage.sh` and `scripts/verify-telemetry-coverage.sh` (and the docs build when the X1 doc changes) |
| New or changed operator dashboard, dashboard panel, or `docs/public/observability/dashboards/eshu-operator-overview.json` (the X4 dashboard generator) | `scripts/test-generate-operator-dashboard.sh` and `scripts/generate-operator-dashboard.sh` (re-generates the committed dashboard JSON; the test mirror asserts the committed artifact matches) |
| Go source, comments, package contracts, or generated docs | `cd go && golangci-lint run ./...` |
| Root marketing site (Cloudflare Pages) | `npm test` (unit) plus `npm run site:review` (desktop + mobile browser gate documented in the repo-root `CLOUDFLARE_PAGES.md`) |
| Repo hygiene gates | `git diff --check` |

## Remote Collector E2E Compose Proof

Use [Remote collector E2E](local-testing/remote-collector-e2e.md) when changing
`docker-compose.remote-e2e.yaml` or hosted collector recovery.

Before accepting a remote collector E2E run, also run the hosted runtime-state
gate in [Remote E2E Runtime State](remote-e2e-runtime-state.md). It verifies
the API, MCP server, ingester, resolution engine, workflow coordinator, hosted
collectors, and checkpointed queue-zero signal.

Use [Remote Remediation Benchmark](local-testing/remote-remediation-benchmark.md)
to rerun the known CVE/package to owner/remediation packet proof and capture
public-safe wall time, queue, fact-count, graph-write, and API/MCP parity
artifacts.

## Secrets/IAM Activation Proof

No-Regression Evidence: issue #2430 remote-validation proof on 2026-06-16
against NornicDB with data-plane schema bootstrapped first. Baseline live
writer conformance failed scoped retract with
`SecretsIAMServiceAccount survived retract: count = 1`. After changing
secrets/IAM graph retracts from list/`UNWIND` mutation predicates to one
scalar `scope_id` cleanup statement per label/scope and executing retracts
sequentially, `ESHU_SECRETS_IAM_GRAPH_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb go
test ./internal/storage/cypher -run '^TestSecretsIAMGraphWriterLiveConformance$'
-count=1 -v` passed in 0.066s. Sanitized readback counted all four
`SecretsIAM*` node families and all five `SECRETS_IAM_*` relationship families
at one before retract, and the sensitive-property spot check reported
`suspicious_values=0`. The same target passed the focused reducer, cypher
storage, and reducer command packages, and the flag-on reducer startup emitted
the `secrets/IAM graph projection ENABLED` warning and reached the normal worker
startup log after the projection truth contract moved from output-only
`canonical_asset` to source evidence `observed_resource`.

No-Observability-Change: the activation fix adds no metric name, metric label,
worker, queue domain, runtime knob, backend branch, or graph-write route.
Secrets/IAM graph writes and retracts still flow through
`SecretsIAMGraphWriter` statement metadata with `phase=secrets_iam_graph`,
entity labels, existing executor error wrapping, existing graph-write
spans/metrics, and the existing reducer flag-on warning.

## Helm Workspace Setup PVC Retry Proof

No-Regression Evidence: baseline chart rendering ran `workspace-setup` as root
with `drop: ALL`, copied `.eshuignore` directly to the final PVC path, and then
ran `chown`, which failed on the default persistent-volume smoke and was not
retry-safe after the final file existed. The fixed render runs as UID/GID
`10001`, keeps `drop: ALL` with no added capabilities, creates `/data/.eshu`
and `/data/repos`, and replaces `.eshuignore` through a temp file on the same
data mount before `mv -f`. Verification covered
`go test ./internal/runtime -run TestHelmWorkspaceSetupInitIsPersistentVolumeRetrySafe -count=1`,
`go test ./internal/runtime -count=1`, `helm lint ./deploy/helm/eshu`,
Compose/Helm runtime parity, and a remote Docker proof on linux/amd64 using the
runtime base image with `--cap-drop ALL`, `--read-only`, UID/GID `10001`, and
the same persisted data/config/tmp mount shape; both first and retry setup
reported `ok`. Terminal queue and row counts are not applicable because this
change runs before any Eshu process starts.

No-Observability-Change: the setup change adds no metric, span, structured log,
status field, queue, graph write, worker, lease, batch, or runtime data
contract. Operators diagnose it through Kubernetes init-container state, pod
events, and the existing ingester probes after startup.

## Two-Team K8s Governance Proof

`scripts/run-k8s-two-team-governance-proof.sh` deploys the Helm chart to a local
Kubernetes cluster (OrbStack), provisions two teams' scoped tokens via a mounted
read-only Secret, and asserts cross-scope isolation live through the API and MCP:
each team sees only its own repositories, the other team's repository is absent,
out-of-grant single-repo selectors return 403, unauthenticated requests return
401, and the restricted NetworkPolicy egress is applied. `helm uninstall` plus
namespace delete run on success and failure. `scripts/verify-hosted-governance-proof.sh`
runs the verifier self-test (good plus bad fixtures) as part of the aggregate
gate.

The chart hooks that enable this proof — `api.extraVolumes` /
`api.extraVolumeMounts` and the matching `mcpServer.*` values — are additive and
default to `[]`, so an operator that does not opt in renders a byte-identical
runtime. `deploy/helm/eshu/ci/governance-two-team-k8s.values.yaml` is test-only
and is not part of a shipped runtime profile.

No-Regression Evidence: the chart hooks are opt-in, empty-by-default Pod volume
mounts; they add no Cypher, graph write, worker claim, lease, batch, queue, or
concurrency knob and do not change the default-rendered Deployment runtime. Live
proof on OrbStack Kubernetes v1.34.8 (single node): two-team scoped reads stay
isolated (each team count=1, other team's repo absent, API/MCP parity),
out-of-grant selector 403, unauthenticated 401, NetworkPolicy restricted egress
applied; all pods reached Ready and the namespace was torn down clean. The
scoped-token authorization itself is the unchanged graph/SQL already exercised by
the merged scoped-read suites.

No-Observability-Change: the proof reads existing spans, metrics, status, and the
documented `/api/v0/repositories` and MCP responses; no telemetry, metric label,
span, or status field is added or altered by the chart hooks.

No-Regression Evidence: bundled NornicDB Helm render proof on Kubernetes 1.32
showed the Deployment preserves the pinned image entrypoint, sets the
`NORNICDB_ADDRESS` wildcard bind address, and exposes the charted HTTP and Bolt
ports through the Service. A Linux amd64 Docker proof with the same pinned
backend image and entrypoint-preserving environment reached HTTP health and
accepted a Bolt TCP connection through published ports. This changes only the
Kubernetes startup contract for the bundled graph backend; it does not change
Eshu queue workers, graph query text, reducer batching, or API/MCP read paths.

No-Observability-Change: the bundled NornicDB chart fix keeps the existing HTTP
health probes, named `http` and `bolt` container ports, and Service targetPorts.
Operators still diagnose the path through pod readiness, container logs, Service
endpoints, and the existing graph-backed Eshu readiness checks.

## Discovery Advisory Playbook

Use [Discovery advisory](local-testing/discovery-advisory.md) when a repository
is slow, unexpectedly large, or timeout-heavy. This is diagnostic evidence, not
a stable API contract.

## Demo Compose Stack Proof

Use `scripts/verify-demo-compose-answers.sh` to prove the credential-free demo
stack (`docker-compose.demo.yaml`) converges the corpus and answers the five
`specs/demo-first-answers.v1.yaml` questions over HTTP. It is failing-test-first
(red before the overlay exists, green after): it boots the stack with zero
credential env, asserts each question over HTTP with no `Authorization` header,
then runs `docker compose down -v --remove-orphans` and asserts zero leftover
containers, volumes, or networks. Two grep gates run before the boot: no
`:?`-required env var in any demo compose file, and no `*_TOKEN` /
external-provider `*_API_KEY` / cloud-credential env anywhere in the demo path.

No-Regression Evidence: the demo overlay changes no runtime behavior of the
existing services. It replays the same fixture corpus, cassette collectors, and
`bootstrap-index`/reducer/projector binaries the B-7 golden-corpus gate already
proves on every PR (`scripts/verify-golden-corpus-gate.sh`), on the pinned
NornicDB backend `timothyswt/nornicdb-cpu-bge:v1.1.9`. Baseline: the golden
gate's ~900s wall-clock budget over its 20-repo/17-collector corpus. The demo
runs a smaller manifest-declared subset (6 repos, 9 cassette collectors), and
the proof script drains every queue to terminal — its final maintenance pass
asserts zero `fact_work_items` residual and zero `shared_projection_intents`
non-terminal rows — before asserting the five answers. The concurrency knobs the
one-shot orchestrator sets (`ESHU_LISTEN_ADDR`/`ESHU_METRICS_ADDR` ephemeral
ports so its concurrent reducer and projector do not collide, plus a per-drain
settle) match the golden gate's own settings and affect only the orchestrator
container; no default-stack behavior changes.

No-Observability-Change: the demo overlay adds no new metrics, spans, or log
fields. It reuses the existing bootstrap-index, reducer, projector, API, and MCP
telemetry; orchestrator drain progress is plain stdout, and queue-drain state is
read from the existing `fact_work_items` and `shared_projection_intents` tables.

## Process Profiling

Use [Profiling and concurrency](local-testing/profiling-and-concurrency.md)
for `ESHU_PPROF_ADDR`, concurrency knobs, and phase CPU capture.

## Docs And Hygiene

Docs, `CLAUDE.md`, `AGENTS.md`, and README changes require:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

### Docs-Change Pre-commit and Pre-Push Gate

Instead of running mkdocs on every commit, the `docs-build-staged` pre-commit hook
inspects the git index and only invokes mkdocs when staged files under `docs/`,
root `README.md`, `AGENTS.md`, `CLAUDE.md`, `.opencode/agent/*.md`,
`.agents/skills/*.md`, or the mkdocs config itself have changed. The
`docs-build-changed` pre-push hook does the same against the branch diff.

Both hooks use the same verifier as a standalone command:

```bash
bash scripts/verify-docs-build-changed.sh          # branch-mode: diff vs origin/main
bash scripts/verify-docs-build-changed.sh --staged # staged-mode: git index only
```

When no trigger-path files are changed, the verifier exits 0 with a skip message
and does not invoke mkdocs. When trigger files are changed, it runs the same
`uv run --with mkdocs ... mkdocs build --strict --clean` command CI uses.

A hermetic self-test is available:

```bash
bash scripts/test-verify-docs-build-changed.sh
```
