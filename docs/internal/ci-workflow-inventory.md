# CI workflow inventory and queue optimization

Snapshot: 2026-10-10. This inventory distinguishes merge-blocking product proof
from advisory diagnostics and automation. Recommendations below are follow-up
work, not permission to remove checks in this change.

## Measured bottleneck

The active main ruleset requires `go-core-complete`, `go-race-complete`, and
`required-gates-complete`. The last status aggregates registry-selected blocking
gates. The queue builds up to three entries and has a 90-minute response timeout.
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

`queue-selection.yml` is a shared scheduling helper, not an additional required
check. It reuses `ci-gates` fixed-base merge-group selection, including renamed
paths and the conservative 300-file truncation rule. It unions the head registry
with the trusted default-branch registry so policy edits cannot suppress work the
trusted publisher still expects. Workflow or registry edits select that workflow's
owned gates. Existing gate names and registry ownership remain intact.

All source jobs wait for selection. Unselected payload steps skip expensive
checkout, builds, and tests, while stable check jobs succeed. Selection
failure fails closed, including jobs with `always()`. Hosted runner and declared
service provisioning still occur for stable check jobs; selection removes payload
work, not all scheduling overhead. Dependencies execute when
any selected downstream owner needs them. Static matrix rows retain every check
name but execute payload only for selected rows. PR and push payload selection
continues to use existing workflow behavior.

The differential uses four isolated runners (two independent pairings times two
backends), with fail-fast disabled. Each publishes a nonempty, attempt-scoped
capture. The required join rejects failed/cancelled producers or missing/empty
captures before running the existing comparison and coverage checks. Backend
comparison retains its existing advisory divergence policy; capture and coverage
failures remain blocking. Runner-local absolute corpus paths stay consistent.

Capture jobs have 30 minutes, the join 15, and selection 10. The publisher waits
80 minutes in an 85-minute job, below the queue's 90 minutes. This also covers the
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
| `e2e-tests.yml` | Blocking Docker end-to-end smoke | Distinct runtime integration proof; retain. |
| `factschema-diff.yml` | Blocking fact schema compatibility | Distinct compatibility boundary; retain. |
| `frontend.yml` | Seven blocking frontend gates, advisory SSO/Prettier | Preserve product tests; narrow independent scopes before consolidation. |
| `generate-bundle-on-demand.yml` | Manual bundle generator | Retirement candidate: installs Python editable root despite no root Python package and Go runtime. Confirm callers before deletion; currently has stale action syntax. |
| `golden-corpus-gate.yml` | Blocking mirror/Neo4j/differential; advisory NornicDB corpus | Preserve semantic proof; parallel captures and selected queue payload in this change. |
| `ifa-determinism-gate.yml` | Blocking determinism/dead-letter/fault matrices; advisory mirror | Distinct recovery invariants; retain selected proof. |
| `live-backend-tests.yml` | Blocking live backends and mirrors | Preserve live integration contracts. |
| `live-postgres-readiness.yml` | Advisory readiness diagnostics | Useful, but long; evaluate #7913 independently. |
| `macos.yml` | Nightly/manual macOS advisory | Platform signal already outside PR; retain. |
| `main-health.yml` | Post-merge and six-hour health watcher | Useful operator signal, not duplicate merge proof. |
| `mcp-schema-drift.yml` | Blocking schema/docs/tool inventory | Preserve compatibility; reduce hand-maintained enumerations through generated schema evidence. |
| `payload-usage-manifest.yml` | Blocking payload consumer manifest | Preserve ownership contract; consider deriving consumer metadata from code. |
| `post_discord_invite.yml` | Issue/PR invitation bot | No product validation; disable/move candidate if invitations are unwanted. |
| `product-claim-ledger.yml` | Blocking claim evidence, plus weekly audit | Useful truth boundary; replace brittle text assertions with executable contract evidence where possible. |
| `race-graph-writes.yml` | Blocking tagged graph race tests | Investigate actual overlap with whole-module race before deletion; tags may provide unique coverage. |
| `read-api-latency-gate.yml` | Blocking latency/work budgets plus two comparator mirrors | Preserve performance correctness; benchmark noise and mirrored maintenance are consolidation targets. |
| `reducer-contention-gate.yml` | Blocking contention and supply-chain queries | Distinct concurrency proof; retain. |
| `refresh-cassettes.yml` | Opt-in label/manual credentialed cassette authoring | Necessary authoring automation, not routine PR gating. |
| `replay-coverage-gate.yml` | Three blocking replay/auth coverage gates | Distinct replay/security proof; retain. |
| `required-gates.yml` | Trusted registry aggregator and live ruleset audit | Required authority; preserve trust boundary and exact-head status. |
| `scorecard-example-conformance.yml` | Blocking separate example module | Main Go module does not cover this; retain. |
| `sdk-go.yml` | Blocking two separate SDK modules | Not duplicate main-module builds; retain. |
| `security-scan.yml` | Blocking gosec/govulncheck; advisory Trivy/Nancy | Preserve blocking vulnerability checks; schedule costly advisory scans where signal supports it. |
| `static-contract-gates.yml` | Thirty-plus mainly blocking static contracts, advisory root-cause guard | Highest maintenance duplication: registry, filters, commands and check labels. Derive scheduling/matrix metadata from registry in follow-up; retire checks only with demonstrated replacement proof. |
| `test.yml` | Blocking Go core/race/docs; separate lint plugin module | Primary Go authority; plugin module is distinct, not redundant. |
| `verify-agent-hygiene.yml` | Five blocking agent/process contracts | Retain current enforcement; evaluate maintenance against concrete failure history. |
| `verify-ci-gate-registry.yml` | Blocking ownership/ruleset consistency and self-tests | Necessary while registry is authoritative; retain. |
| `verify-replay-tier.yml` | Blocking offline real NornicDB replay | Distinct backend replay proof; retain. |

The new `queue-selection.yml` makes 36 workflow files. It has no independent
trigger or product gate and is classified as a nongate helper in the registry.

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
