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
   any P0, P1, or blocking P2 finding, fix it, rerun affected focused proof,
   and repeat the full review. Do **not** run `make pre-pr` while a blocking
   finding remains.
3. Once the preliminary verdict is `P0=0, P1=0, P2-blocking=0` — every
   deferred P2 tracked in a linked issue with the owner's agreement quoted in
   the PR and named there with its severity-table category, per
   `.agents/skills/eshu-code-review/references/merge-bar.md` — capture a
   `ci-gates review-attest` receipt with the clean preliminary review and exact
   proposed PR claims.
4. With the branch otherwise ready to push, run `make pre-pr` exactly once as
   the late promotion gate. Use `make pre-pr-full` instead when the risk tier
   requires the whole-module race lane, then verify the review receipt. A
   matching receipt replaces the second full semantic review because every
   reviewed input is unchanged. If verification fails, repeat the affected
   proof and full review. Make no edits before push.

`make pre-pr` is the blocking local promotion path for credential-free CI
gates. It catches format, exactness, race, contract, docs, and Go security
failures before a branch enters the hosted queue:

```bash
make pre-pr            # or: bash scripts/dev/pre-pr.sh
make pre-pr-full       # adds advisory registry gates and whole-module race
```

CI remains the authoritative, non-bypassable source of truth — but it should
rarely be the *first* place you learn about a credential-free failure. Two
expectations are firm:

- **Exactness gates are blocking** when matching code, spec, fixture, cassette,
  or generated-contract inputs change. `make pre-pr` selects and runs them.
- **Race gates are blocking** when Go implementation code changes. `make pre-pr`
  runs the targeted/scoped race lane; `make pre-pr-full` adds the whole-module
  `go test ./... -race`, and CI runs the authoritative full race gate.

### Local and hosted ownership

The local and hosted layers have different jobs. Do not remove one because the
other runs a similar command.

| Proof | Mandatory locally | Mandatory in GitHub Actions |
| --- | --- | --- |
| TDD reproduction and focused touched-surface tests | Before review and before `make pre-pr` | Re-run when selected; CI is not the first proof attempt |
| Credential-free promotion checks | `make pre-pr` once on the final reviewed diff; use `make pre-pr-full` only when the proof tier requires it | Re-run independently on the exact PR head |
| Advisory analysis | Outside the default promotion path; run with `make pre-pr-full` or its focused command when useful | May publish evidence but does not block the local promotion stamp |
| Frontend and security-heavy checks | Run the matching preflight when those surfaces change | Blocking path-selected jobs remain authoritative |
| OS-, credential-, service-, or artifact-dependent checks | Run locally or on the dedicated remote validation host when the change contract requires that proof | Mandatory hosted owner; a local or remote pass does not create a GitHub required status |
| Required merge decision | No local command can satisfy it | `go-core-complete`, `go-race-complete`, and `required-gates-complete` must pass |

Deduplicate within a layer: one local invocation should not execute the same
command twice for registry rows owned by the same hosted job, and one hosted job
should not repeat a byte-identical test/gate command. Keep the local-versus-CI
rerun because it proves the final bytes in an independent environment and is
the non-bypassable merge record.

The selected-gate runner separates a verifier from the tests of that verifier.
`local.command` always runs when its gate is selected. A `test_command` still
runs by default, but `make pre-pr` may skip it when the registry declares
`self_test_triggers` and none of those harness paths changed. Entries without
that field stay fail-closed and run both commands. The per-SHA stamp directory
also retains a JSON report with command hashes, run/reuse decisions, skip
reasons, and durations.

Frontend- and security-heavy lanes are not in `make pre-pr` (they need Node, the
network, or are slow); run `make frontend-preflight` / `make security-preflight`
when you touch those surfaces. None of these silently skip: a gate that cannot
run locally prints why and names the CI gate that remains authoritative.

### Documentation-only fast path

For a diff that is provably documentation/specs-only, `make pre-pr` skips the
whole-module `go build`, `go vet`, `gofumpt`, `golangci-lint`, and the race
lane (#5721); the changed-package `go test` lane always runs and is narrowed,
never skipped, to any fixture-consumer package the diff maps to (e.g. a root
`AGENTS.md`/`CLAUDE.md` change selects `./internal/runtime`). The lane's own
path list includes untracked files, so a forgotten `git add` on a new `.go`
file forces FULL rather than riding a skipped build. It also classifies FULL
when the list itself cannot be trusted — an unresolved `origin/main`, any
`git` command that exited non-zero, or a run that could not have recorded such
a failure. See [Pre-PR Documentation Fast Path](local-testing/pre-pr-docs-fastpath.md)
for the classifier's exact allowlist, both fail-closed rules, what still runs
on the fast path, and how it relates to CI's own docs-only skip definition.

Outside the documentation-only fast path above, it runs gofumpt and
golangci-lint over the **whole** module (catching cross-package consequences
a changed-package run misses, such as code that becomes unused when a
sibling package changes), `go build` and `go vet` over the whole module,
`go test` on the packages changed versus `origin/main`, the 500-line Go file
cap and package-docs gates. A direct change to the parent `go/internal/parser`
package expands that focused test target to `./internal/parser/...`, keeping
external child-package tests of the parent Engine contract in the local gate;
child-only parser changes remain package-focused. The preflight also runs —
driven by the gate registry
(#4214) — the **selected credential-free exactness and telemetry contract gates**
for your changed paths (OpenAPI, route coverage, edge source-tool coverage,
evidence continuity, fact-kind registry, contract source-of-truth, parser
relationship kit, query-plan regression, scale corpus/benchmark, capability
budget, collector entrypoints, skill roundtrip, telemetry coverage, operator
dashboard, and the 500-line Markdown cap under `go/` and `docs/`).
You no longer have to remember which verifier matches your change — the
changed-path selector picks them. Docs changes select the applicable docs
gates; a no-op change selects none. Docker/NornicDB/Postgres/credentialed gates remain CI-only
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
make pre-pr-full      # pre-pr + advisory gates + `go test ./... -race`
```

The default promotion path runs blocking registry gates only. Advisory checks
remain available through `make pre-pr-full` and focused commands, so they can
inform a review without withholding the local promotion stamp.

### Exact review attestation

Agents keep the preliminary review packet, verdict, and exact PR title/body in
local files outside the worktree. After the clean full review, capture a receipt:

```bash
go -C go run ./cmd/ci-gates review-attest capture \
  --base origin/main \
  --claims-file "$claims_file" \
  --review-packet "$review_packet" \
  --verdict "$review_verdict" \
  --receipt "$review_receipt"
```

Run the same command with `verify` after `make pre-pr`. A pass means the base,
head, diff, worktree, submodules, claims, packet, and verdict are byte-for-byte
the reviewed inputs. A failure names the changed binding and requires another
full review. This receipt is local proof for the agent workflow; it does not
replace GitHub review or a required hosted check, and `make pre-pr` does not
require community contributors to create one.

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
  changed file is under `docs/**`, a root-level `*.md`, `mkdocs.yml`, or
  `.agents/**`. `build.yml` skips via a `pull_request` `paths-ignore`; the mixed
  workflows (`test.yml`, `security-scan.yml`, `mcp-schema-drift.yml`) skip
  per-job via a `changes` gate, so their always-on jobs above keep running. The
  bare `*.md` negation is **root-anchored** (`README.md`, not nested markdown),
  so `go/**/*.md` still counts as code; `.agents/**` is negated explicitly
  (#5818). Any PR mixing docs with code runs the full set. `main`, the nightly
  schedule, and tag pushes run everything unconditionally. The repository's
  required-status manifest keeps the two existing umbrellas:
  `go-core-complete` (**compilation gate** — whole-module
  `cd go && go build ./...` plus lint/fmt, catching a merge result that does not
  compile though every PR was green, #5814) and `go-race-complete` (**race gate**
  over the sharded `go test -race` matrix). Each stays green when its own lane
  is legitimately skipped, so neither strands a docs-only PR — but each also
  depends on `changes` and fails if `changes` did not, since GitHub marks a job
  `skipped` (not `failed`) when a dependency fails.
- **Trusted blocking-gate aggregate:** the manifest also declares
  `required-gates-complete`. A `workflow_run` publisher executes the policy from
  the default branch, never checks out pull-request code, and evaluates the
  exact pull-request head. It maps changed paths to every registry row marked
  `blocking: true`, then waits for the exact workflow/check names declared by
  those rows. A selected failed, neutral, or timed-out check makes the
  aggregate fail. A selected check that never produced a verdict does not: that
  is infrastructure state, not a gate result, so the aggregate publishes
  `error` with a description naming the re-run rather than claiming a gate
  failed ([#6189](https://github.com/eshu-hq/eshu/issues/6189)). `error` still
  blocks the merge — the ruleset requires `success` — so nothing is waved
  through; the status just stops asserting an outcome that never happened.
  Four shapes qualify: a **cancelled** check, a **stale** one GitHub
  orphaned, one GitHub marked **skipped because the workflow run that
  owned the job was cancelled**, and one **missing entirely because that run
  was cancelled before the job was created** — no check run exists, so nothing
  will ever report it. A check missing for any other reason keeps the
  aggregate waiting rather than resolving, because a gate the registry
  selected whose job simply never ran is a real disagreement, not
  infrastructure noise. The skipped shape is why the aggregate reads run
  conclusions (`actions: read`): GitHub reports the same `SKIPPED` conclusion
  whether a dependency was cancelled or the job's own `if:` excluded it, and
  **a gate skipped for its own reasons still fails the aggregate** — the
  registry selected it for these paths, so a skip the workflow chose is a real
  disagreement about whether it should have run. If the run conclusions cannot
  be read, a skipped gate stays a failure. While a re-run of the cancelled
  workflow is still executing, its conclusion is neither cancelled nor a
  verdict, so the aggregate keeps waiting rather than publishing either answer;
  the re-run's own completion re-triggers it. A cancellation alongside a
  still-running gate keeps waiting, so a gate that goes genuinely red is still
  reported as `failure`. Per-head concurrency
  keeps one aggregate running and retains only the latest pending run without
  cancelling the active status writer. An aggregate that starts posts pending
  before checkout or setup, then reaches a real terminal result; the retained
  run posts pending again before it recomputes. A manual cancellation after
  that first step leaves pending instead of inventing a failure. GitHub cannot
  execute repository code after cancellation before runner allocation, and
  commit statuses have no atomic generation fence, so this workflow does not
  claim protection across that operator/API boundary.
  This closes the gap where a blocking registry row could be visible locally
  yet absent from GitHub's two Go umbrellas.
- **Trust boundary:** the aggregate policy, selector, and status publisher come
  from the default branch and cannot be rewritten by the pull request they
  evaluate. The selected leaf checks remain ordinary pull-request CI: their
  workflow and test code are reviewable changes in the repository. Enforcement
  therefore assumes branch-protected review and does not claim to resist a
  collaborator who can both weaken a leaf check and approve or merge that same
  change. That stronger adversarial boundary requires organization-level
  immutable required workflows or an external GitHub App.
- **Ruleset drift:** rollout retains `go-core-complete` and
  `go-race-complete`, then adds `required-gates-complete` only after its trusted
  publisher is present on the default branch. The scheduled/manual live
  verifier checks that the named owning ruleset is active and scoped to the
  default branch, proves that ruleset itself owns the exact strict context and
  integration-ID manifest, then makes the same comparison against GitHub's
  effective `main` rules. It fails on a missing, extra, or differently owned
  context. GitHub hides bypass actors from the read-only scheduled token, so
  the scheduled audit does not claim to prove their absence; an
  admin-authenticated invocation requires the field and verifies that it is
  empty. The verifier also fails if a merge-queue rule appears: the current
  publisher evaluates pull-request heads, so `merge_group` support must land
  before merge queue is enabled.
- **Advisory:** the benchmark regression check (`BENCH_REGRESSION_ENFORCE=false`)
  and the changed-file Prettier check do not block merge.
- **CI-only / release-only:** PR image-build, reproducibility, and Helm-package
  proof are blocking hosted lanes. The post-publish Trivy image scan,
  GHCR/package publication, and release-attestation checks require credentials
  and cannot block the merge that creates their artifacts.

`blocking: true` is an enforced merge contract when a row's triggers match, not
only a local/preflight label. Exactness and race gates stay blocking when their
matching code, spec, fixture, or generated-contract inputs change —
consolidation changed where they run, not whether they block.

### What `make pre-pr` selects, by change type

`make pre-pr` runs the whole-module Go gates (gofumpt, golangci-lint, build,
vet, file cap) plus the focused changed-package tests for any diff outside the
[documentation-only fast path](local-testing/pre-pr-docs-fastpath.md) above;
the table is what the changed-path selector *adds* on top of those. You never
have to remember the matching verifier — the selector picks it.

| You changed | `make pre-pr` additionally runs | Also run |
| --- | --- | --- |
| Docs only (fast-path-recognized paths — see above) | whole-module Go build/vet/fmt/lint and race lanes SKIPPED; changed-package `go test` still runs, narrowed to any fixture-consumer package (e.g. root `AGENTS.md`/`CLAUDE.md` maps to `./internal/runtime`) and a no-op otherwise; the selected exactness/telemetry/hygiene/docs gates still run, as do file cap and package docs (both no-ops with no changed Go file) | docs build (pre-push) |
| Frontend only (`src/**`, `apps/console/**`) | nothing backend | `make frontend-preflight` |
| Parser (`go/internal/parser/**`) | parser relationship kit, accuracy golden gate, scoped race; a direct parent-package change runs focused tests recursively across `./internal/parser/...` | — |
| Reducer / storage (`go/internal/reducer/**`, `storage/**`) | query-plan regression, scale gates, **targeted graph-write race** | reducer-contention is CI-only (Postgres) |
| Collector (`go/internal/collector/**`) | edge source-tool coverage, evidence continuity, scale corpus | — |
| API / MCP (`go/internal/query/**`, `go/internal/mcp/**`) | OpenAPI surface, route coverage, MCP schema drift, capability budget, operator dashboard, evidence continuity (its spec's proof refs cite tests in these packages) | — |
| CLI (`go/cmd/eshu/**`, `go/internal/cli/**`) | evidence continuity (its spec's proof refs cite tests in these packages) | — |
| CI gate registry helpers (`go/internal/cigates/**`) | evidence continuity (its trigger self-check is built on this package's glob and workflow parsing) | — |
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

See [Quick Verification Matrix](local-testing/quick-verification-matrix.md)
for the full touched-area-to-minimum-verification mapping.

### Performance Evidence Gate

`scripts/verify-performance-evidence.sh` blocks a PR that touches hot Cypher,
graph writes, queues, workers, leases, batching, or runtime knobs unless the
same PR carries its own tracked evidence in a fresh `Performance Evidence:`
(or `Benchmark Evidence:`/`No-Regression Evidence:`) line plus a fresh
`Observability Evidence:`/`No-Observability-Change:` line, in one of the
gate's recognized evidence-file locations. See
[Performance Evidence Gate](local-testing/performance-evidence-gate.md) for
the added-lines requirement, the full list of recognized locations, and the
fixture/vendor/generated exclusions.

### Tagged Build Sweep

`go build`, `go vet` and `go test` all skip a file whose first line is a
`//go:build` constraint, so nothing in the ordinary lane ever compiles it. A
helper it calls can be deleted elsewhere in the package and the file simply
stops compiling, silently, for as long as nobody runs it with its tag on. That
is not theoretical: a live NornicDB proof in `internal/query` lost a helper to
an unrelated refactor and stayed uncompilable through a `make pre-pr` run, a
push, a full CI run, and eight review rounds.

`scripts/verify-tagged-builds.sh` closes that with one `go vet` per distinct
build constraint. It reads the constraints out of the files rather than from a
list, so a tag added later is swept without anyone remembering to add it:

```bash
bash scripts/verify-tagged-builds.sh                  # ./internal/query
bash scripts/verify-tagged-builds.sh --all            # every tagged package
bash scripts/verify-tagged-builds.sh ./internal/query ./cmd/reducer
```

`make pre-pr` runs `--all` whenever the diff touches Go, through the selected
exactness gates rather than a step of its own: the gate is registered as
`tagged-builds` at tier `pre-pr`, so `run-selected-gates.sh` picks it for any
`go/**` diff. CI runs the same pair — mirror then gate — as the
`Verify tagged-builds gate` row of `static-contract-gates.yml`, registered as
`tagged-builds` in `specs/ci-gates.v1.yaml`. Its trigger is `go/**` on purpose:
the change that breaks a tagged file is almost never a change to that file, so
a narrower trigger would reproduce the defect. The sweep is compile-only and
needs no backend, which is what separates it from the tagged suites themselves
— running those usually needs a pinned container, so they stay manual.

The gate accepts one term (optionally negated), terms joined only by `&&`, or
terms joined only by `||`, with no parentheses. Anything outside that — a
parenthesised group, a mix of `&&` and `||`, an unreadable term, or an
alternation that splits into fewer alternatives than its `||` promises — is an
`ERROR` that fails the run, never a skip and never a pass. A `SKIP` there would
be a green run over a file nothing compiled, which is the failure class this
gate exists to remove: `!(tag_a || tag_b)` used to have its parentheses
flattened to spaces, binding the `!` to the first term only, and answered `SKIP`
plus `PASS` without compiling the file either time.

Two shapes are legitimately reported as `SKIP` rather than vetted, and the
summary line prints the skip count so a sweep that is covering less than it
looks like is visible:

- a GOOS or GOARCH term (`windows`, `linux`). The go command takes those from
  the build environment, and forcing one with `-tags` pulls two copies of the
  standard library's platform files into one build. Those files are compiled by
  the ordinary build on their own platform.
- a constraint with no selectable tag left, which is what a negated one
  (`!nolocalllm`) reduces to. The default build already compiles that file.

The self-test builds throwaway modules and asserts both directions, including a
break behind a `&&` constraint that a first-token-only tag reader would miss:

```bash
bash scripts/test-verify-tagged-builds.sh
```

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
