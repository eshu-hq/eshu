# CI workflow inventory and queue optimization

Snapshot: 2026-10-10. This inventory distinguishes merge-blocking product proof
from advisory diagnostics and automation. Recommendations below are follow-up
work, not permission to remove checks in this change.

Update 2026-10-16 (#8288): the owner turned the `main` merge queue off after an
entry sat for more than six hours. The queue figures below (three entries, a
90-minute response timeout, merge-group starts) describe the retired queue and
are kept as the measured history. PRs now merge on the required checks of the
PR head, and no `merge_group` run fires.

## Measured bottleneck

When this was measured, the main ruleset required `go-core-complete`,
`go-race-complete`, and `required-gates-complete` (disabled as a required check
on 2026-10-25). The last status aggregates registry-selected blocking
gates. The retired queue built up to three entries and had a 90-minute response timeout.
All 23 source workflows currently start for each merge group; most jobs started
within seconds to one minute in the inspected sample (400 runs, 18 group heads).
Runner waiting was not the dominant delay in that sample.

Seven completed differential jobs spent 52–61 minutes in four serial capture
legs. Their median sum was 58m11s; median longest leg was 21m1s. Parallel captures
therefore offer a counterfactual median reduction of about 37 minutes in that
stage, excluding cache effects and artifact overhead. This is not a measured
post-change speedup. Example evidence: [capture run 38050189525](https://github.com/eshu-hq/eshu/actions/runs/38050189525),
[38048846477](https://github.com/eshu-hq/eshu/actions/runs/38048846477), and
[38043332390](https://github.com/eshu-hq/eshu/actions/runs/38043332390).

Go build/test took roughly 21–23 minutes; PostgreSQL readiness took 28–37 minutes.
Readiness is advisory in the registry: its duration alone does not establish a
merge-blocking critical path. Differential work blocks only when selected.
PR #7913 separately changes PostgreSQL readiness and is outside this patch.

## Scheduling design

Update 2026-10-28 (#9367): `queue-selection.yml` was a shared scheduling
helper, not an additional required check. Its selection work ran only on
`merge_group` events (any other event emitted `selected=true` and an empty job
list). `main` has had no merge queue since 2026-10-16 (#8288), so that
selection was dead code, and the helper is now deleted: no workflow waits for
selection anymore, and every job runs its payload when its workflow's own
`pull_request`/`push`/schedule triggers select it. The paragraphs below
describe the retired mechanism and are kept as the measured history. They
reused `ci-gates` fixed-base merge-group selection, including renamed
paths and the conservative 300-file truncation rule. The helper unioned the
head registry with the trusted default-branch registry so policy edits could
not suppress work the trusted publisher still expected. The trusted root and
ordered fragments came from one fetched default-branch commit. Missing or
malformed fragments failed closed. Workflow, shared selector, or registry
fragment edits selected all owned gates for the workflow. A static filter edit
selected every static row. Gate names and registry ownership stayed intact.

While it lived, all source jobs waited for selection. Unselected payload steps
skipped expensive checkout, builds, and tests, while stable check jobs
succeeded. Selection failure failed closed, including jobs with `always()`.
Hosted runner and declared service provisioning still occurred for stable check
jobs; selection removed payload work, not all scheduling overhead. Dependencies
executed when any selected downstream owner needed them. Static matrix rows
retained every check name but executed payload only for selected rows. PR and
push payload selection continued to use existing workflow behavior.

The differential used four isolated runners (two independent pairings times two
backends), with fail-fast disabled. Each published a nonempty, attempt-scoped
capture. The required join rejected failed/cancelled producers or missing/empty
captures before running the existing comparison and coverage checks. Backend
comparison retained its existing advisory divergence policy; capture and coverage
failures remained blocking. Runner-local absolute corpus paths stayed consistent.

Capture jobs had 30 minutes, the join 15, and selection 10. The publisher waited
80 minutes in an 85-minute job, below the queue's 90 minutes. This also covered the
longest existing 60-minute payload plus selection and publication margin. Hosted
runner start delays and cold-cache timings still require validation after rollout.

## Every existing workflow

“Blocking” means an owned blocking registry gate, not necessarily every job in
that workflow. Automation workflows are explicitly outside the gate registry.

| File | Purpose / authority | Assessment and follow-up |
| --- | --- | --- |
| `apk-floors-drift.yml` | Daily/manual live Alpine advisory | Useful scheduled drift signal; keep off PR critical path. |
| `bench-baseline-refresh.yml` | Weekly/manual benchmark baseline PR | Useful maintenance; retain outside merge gates. |
| `bench.yml` | Four advisory Go benchmark jobs on PR/main | Consolidate repeated setup; consider scheduled execution and targeted PR opt-in. |
| `build.yml` | Linux/macOS release builds on PR/main/tags | PR compilation overlaps `test.yml`; move release proof toward tags/main while retaining release artifacts. |
| `code-coverage-report.yml` | Advisory full coverage on PR, mirror on main/manual | Path-select package coverage; retain main coverage trend. |
| `deploy-root-docs.yml` | Main/manual docs publishing | Publishing is useful; verify whether duplicate docs build is needed when deploy is enabled. |
| `docker-publish.yml` | Blocking reproducibility, Helm and APK proof; advisory publish/SBOM | Preserve artifact correctness; separate advisory publication cost from PR proof. |
| `e2e-tests.yml` | Blocking Docker end-to-end smoke (Neo4j only; NornicDB cell disabled 2026-11-05) | Distinct runtime integration proof; retain. |
| `factschema-diff.yml` | Blocking fact schema compatibility | Distinct compatibility boundary; retain. |
| `frontend.yml` | Seven blocking frontend gates, advisory SSO/Prettier | Preserve product tests; narrow independent scopes before consolidation. |
| `golden-corpus-gate.yml` | Blocking mirror/Neo4j/Neo4j-only differential (NornicDB corpus cell and capture legs disabled 2026-11-05) | Preserve semantic proof; parallel captures and selected queue payload in this change. |
| `ifa-determinism-gate.yml` | Blocking determinism/dead-letter/fault matrices on Neo4j (owner order 2026-11-05; NornicDB variant removed); advisory mirror and Neo4j-wiring guard | Distinct recovery invariants; retain selected proof. |
| `live-backend-tests.yml` | Blocking live Neo4j shards 2/4 to 4/4 and mirrors (NornicDB shard 1/4 disabled 2026-11-05) | Preserve live integration contracts. |
| `live-postgres-readiness.yml` | Advisory readiness diagnostics | Useful, but long; evaluate #7913 independently. |
| `macos.yml` | Nightly/manual macOS advisory | Platform signal already outside PR; retain. |
| `main-health.yml` | Post-merge and six-hour health watcher | Useful operator signal, not duplicate merge proof. |
| `mcp-schema-drift.yml` | Blocking schema/docs/tool inventory | Preserve compatibility; reduce hand-maintained enumerations through generated schema evidence. |
| `payload-usage-manifest.yml` | Blocking payload consumer manifest | Preserve ownership contract; consider deriving consumer metadata from code. |
| `product-claim-ledger.yml` | Blocking claim evidence, plus weekly audit | Useful truth boundary; replace brittle text assertions with executable contract evidence where possible. |
| `race-graph-writes.yml` | Blocking tagged graph race tests | Investigate actual overlap with whole-module race before deletion; tags may provide unique coverage. |
| `read-api-latency-gate.yml` | Latency/work budgets (NornicDB cell disabled 2026-11-03) plus two comparator mirrors | Preserve performance correctness; benchmark noise and mirrored maintenance are consolidation targets. |
| `reducer-contention-gate.yml` | Blocking contention and supply-chain queries | Distinct concurrency proof; retain. |
| `refresh-cassettes.yml` | Opt-in label/manual credentialed cassette authoring | Necessary authoring automation, not routine PR gating. |
| `replay-coverage-gate.yml` | Three blocking replay/auth coverage gates | Distinct replay/security proof; retain. |
| `required-gates.yml` | Trusted registry aggregator (aggregate job disabled 2026-11-03) and live ruleset audit | The aggregate no longer publishes `required-gates-complete`; the ruleset requires only `go-core-complete` and `go-race-complete`. Preserve the trust boundary if it returns. |
| `scorecard-example-conformance.yml` | Blocking separate example module | Main Go module does not cover this; retain. |
| `sdk-go.yml` | Blocking two separate SDK modules | Not duplicate main-module builds; retain. |
| `security-scan.yml` | Blocking gosec/govulncheck; advisory Trivy/Nancy | Preserve blocking vulnerability checks; schedule costly advisory scans where signal supports it. |
| `static-contract-gates.yml` | Thirty-plus mainly blocking static contracts, advisory root-cause guard | Highest maintenance duplication: registry, filters, commands and check labels. Derive scheduling/matrix metadata from registry in follow-up; retire checks only with demonstrated replacement proof. |
| `test.yml` | Blocking Go core/race/docs; separate lint plugin module | Primary Go authority; plugin module is distinct, not redundant. |
| `verify-agent-hygiene.yml` | Five blocking agent/process contracts | Retain current enforcement; evaluate maintenance against concrete failure history. |
| `verify-ci-gate-registry.yml` | Blocking ownership/ruleset consistency and self-tests | Necessary while registry is authoritative; retain. |
| `verify-replay-tier.yml` | Offline real NornicDB replay (disabled 2026-11-05) | Distinct backend replay proof; retain the job, re-enable with the NornicDB legs. |

The `queue-selection.yml` helper was removed in #9367 (it made 36 workflow
files while it lived; 38 remain). It had no independent
trigger or product gate and was classified as a nongate helper in the registry.

## Redundancy policy

Repeated execution is not automatically redundant: local proof and hosted CI
validate different trust boundaries, separate Go modules need separate builds,
and fresh hosted runners need their own setup. The strongest immediate savings
are parallel capture and skipping unselected queue payload, not deleting checks.

Next, remove duplicated scheduling declarations by deriving static matrix
metadata from the registry. Classify each static assertion by the failure it
prevents, its observed catches, and its replacement behavioral proof. Prefer a
small number of contract tests over assertions that pin incidental YAML/text.
Retire an assertion only when its invariant is covered elsewhere; do not replace
safety with a new manually maintained allowlist. The two clearest workflows
without product CI value are the invitation bot and stale manual bundle job.

A second merge coordinator is not needed to realize these improvements. Revisit
Mergify only after measuring post-rollout queue throughput, selected critical
paths, rebuilds, runner wait, and CI minutes. It cannot shorten a slow test body;
operating two independent merge owners would also require a clear ownership
boundary and matching required-check policy.

## Re-enabling NornicDB CI

By owner order (2026-11-05, #7331) no NornicDB leg runs in CI while Neo4j is the
supported backend. Nothing was deleted: each leg is switched off in place, with
its original condition kept in a comment. The read-API latency cell was turned
off the same way on 2026-11-03. A guard keeps the legs off:
`scripts/verify-nornicdb-ci-off.py` (registry gate `nornicdb-ci-off`). It fails
when a job that can run (its `if` is not `${{ false }}`) starts NornicDB, found
three ways: a workflow line that selects the `nornicdb` backend (matrix cell,
`ESHU_GRAPH_BACKEND`, `--backend nornicdb`), brings up the `nornicdb` compose
service, or `docker run`s a NornicDB image; a `docker run` of a NornicDB image
inside a `scripts/*.sh` file the job invokes; or a `--shard "${{ matrix.X }}/N"`
run whose matrix value `live_backend_shards.partition_runs` gives NornicDB runs.
It also fails a disabled job whose registry row is still blocking. Restoring the
replay tier's `if`, or putting shard 1 back in the live-backend matrix, fails it
whatever the exact-text pins around those lines say.

| Leg | Workflow and job | How it is off | Registry row |
| --- | --- | --- | --- |
| Offline replay tier | `verify-replay-tier.yml` / `replay-tier` | `if: ${{ false }}` | `replay-tier`: manual, non-blocking, `disabled:` marker (#7331) |
| E2E NornicDB cell | `e2e-tests.yml` / `test` | matrix cell commented out | `e2e-tests`: `test (nornicdb)` removed from `check_names` |
| Golden corpus NornicDB cell | `golden-corpus-gate.yml` / `corpus-gate` | matrix cell commented out | `golden-corpus-gate`: manual, non-blocking, no `check_names` |
| Differential NornicDB captures | `golden-corpus-gate.yml` / `differential-capture`, `differential` | matrix is `[neo4j]`; staging names `neo4j`; backend-diff step is `if: ${{ false }}`; writer coverage reads Neo4j only | `golden-corpus-differential`: stays blocking (Neo4j only) |
| Live-backend NornicDB leg | `live-backend-tests.yml` / `live-backend-shards`, `live-backend` | shard matrix is `[2, 3, 4]`; aggregate runs `expand --backend neo4j` and `--skip-shard 1/4` | `live-backend-tests`: stays blocking (Neo4j shards) |
| Read-API latency cell | `read-api-latency-gate.yml` / `latency-gate` | `if: ${{ false }}` (2026-11-03) | `read-api-latency-gate`: manual, non-blocking |

The Neo4j cells stay on and blocking. The job names (`differential nornicdb vs
neo4j`, `live-backend (nornicdb and neo4j)`) are unchanged so pinned check names
do not move. Go unit tests of `go/internal/storage/nornicdb` and the
`race-graph-writes.yml` run are not CI legs and stay on. The Ifá determinism
gates run on NornicDB too; this change does not touch them. Turning the replay
tier off leaves 167 replay-coverage surfaces unproven (457/457 to 290/457); the
coverage gate reports them as "Proof off" instead of failing. A proof gate counts
as off only when its registry row carries the `disabled:` marker (a `reason` and
a tracking `issue`, on a manual, non-blocking row) and the workflow job it names
is `if: ${{ false }}`; `ci-gates` drift check 16 and the coverage gate both check
the pair. Flipping a gate to manual and non-blocking without the marker still
fails the coverage gate. Six `class: ci`, `backends: nornicdb` live tests (for
example `go/internal/query/nornicdb_answer_truth_live_test.go`) and the 31
`dedicated_ci` NornicDB entries (replay tier and Ifá) no longer run in any job.

To re-enable everything, revert in this order and run each check after its step:

1. Restore each workflow edit from the table. Put back the commented matrix cell
   or the original `if` kept in the comment next to it.
2. Restore the registry rows (tier, `blocking`, `check_names`), then run the
   registry doc generator and the registry validator
   (`scripts/generate-ci-gates-doc.sh`, `scripts/verify-ci-gates-registry.sh`).
3. Restore the pinned shapes the guards compare against:
   `scripts/lib/replay_tier_routes.py` (workflow and registry control lines),
   `scripts/lib/nornic_conformance_runner.py` (e2e matrix), the
   `replay_tier_routes.py` digest in `scripts/lib/replay_tier_inventory.json`
   and `scripts/lib/verify-live-tests-ledger.py`, and the stage-step name in
   `scripts/lib/golden-corpus-capture-leg-cases.sh`. Pass `nornicdb neo4j` to
   `stage-differential-capture.sh`.
4. Delete the `nornicdb-ci-off` registry row, its two steps in
   `verify-agent-hygiene.yml`, and `scripts/verify-nornicdb-ci-off.py` with its
   test. Do this last: until then the guard refuses any live NornicDB leg.
5. Regenerate the replay-coverage dashboard
   (`cd go && go test ./cmd/replay-coverage-gate/ -update-dashboard`). With the
   `replay-tier` row blocking again (delete its `disabled:` marker), its 167
   surfaces return from "Proof off" to satisfied (457/457). While the row is off,
   they are reported as unproven.
6. Run the focused tests the disabling PR listed, then open a normal PR.
