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
| `required-gates.yml` | Live ruleset audit only (fan-in and aggregate job deleted #10584; aggregate disabled 2026-11-03) | The ruleset requires only `go-core-complete` and `go-race-complete`, read directly with no publisher. Restore the trusted-publisher trust boundary if an aggregator returns. |
| `scorecard-example-conformance.yml` | Blocking separate example module | Main Go module does not cover this; retain. |
| `sdk-go.yml` | Blocking two separate SDK modules | Not duplicate main-module builds; retain. |
| `security-scan.yml` | Blocking gosec/govulncheck; advisory Trivy/Nancy | Preserve blocking vulnerability checks; schedule costly advisory scans where signal supports it. |
| `static-contract-gates.yml` | Thirty-plus mainly blocking static contracts, advisory root-cause guard | Highest maintenance duplication: registry, filters, commands and check labels. Derive scheduling/matrix metadata from registry in follow-up; retire checks only with demonstrated replacement proof. |
| `test.yml` | Blocking Go core/race/docs; separate lint plugin module | Primary Go authority; plugin module is distinct, not redundant. |
| `verify-agent-hygiene.yml` | Five blocking agent/process contracts | Retain current enforcement; evaluate maintenance against concrete failure history. |
| `verify-ci-gate-registry.yml` | Blocking ownership/ruleset consistency and self-tests | Necessary while registry is authoritative; retain. |
| `verify-replay-tier.yml` | Offline real Neo4j replay (blocking; retargeted 2026-11-06, #10223) | Distinct backend replay proof; retain the job. No longer part of the NornicDB legs. |

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
four ways, each read from the committed files with no list of exceptions:

1. A workflow line selects `nornicdb` (a matrix value under any key,
   `ESHU_GRAPH_BACKEND`, `--backend nornicdb`, or an expression that picks it
   for the key `backend`, `graph_backend`, `graph_service` or
   `ESHU_GRAPH_BACKEND`), assigns `ESHU_GRAPH_BACKEND=nornicdb` or
   `COMPOSE_PROFILES=nornicdb` inside a command, brings up the `nornicdb`
   compose service or profile (also through a `-f` file whose name carries
   `nornicdb`), or `docker run`s (`docker container run`, `podman run`) a
   NornicDB image or runs one with `uses: docker://...`. A step `name:` is a
   label, not a command, so "Clean up nornicdb volumes" is not a bring-up. A job
   `services:` or `container:` image that names NornicDB counts too. The same
   markers are read over the workflow-level lines outside `jobs:` (a top-level
   `env:`) whenever the workflow has a live job, and a workflow input that
   defaults to `nornicdb` counts when a live job reads `inputs.<name>`. A
   workflow whose jobs are all off is not read that way. Names are
   matched ignoring case. Lines are read after `\` continuations are joined,
   and a shell variable set to a literal earlier in the same step is
   substituted (`svc=nornicdb` then `up -d "$svc"`). Workflows are `*.yml` and
   `*.yaml`, and the indentation is read from the file, not assumed.
2. A `*.sh` or `*.py` file the job runs, or any script that file runs in turn,
   does the same, or exports `ESHU_GRAPH_BACKEND=nornicdb`. The walk has no
   depth cap and a script cycle ends it. A shell line is split at `&&`, `;`,
   `|` and `then` outside quotes, `sudo`, `timeout N`, `env` and the like are
   dropped, and a `bash -c '...'` body is read, so `cd x && docker ...` and
   `sudo docker run ...` count. A statement counts only when it is the command,
   and a Python file is parsed for spawn calls, so a fixture or regex that
   quotes a command is skipped.
3. A local composite action the job calls (`uses: ./.github/actions/<name>`),
   or an action that one calls, does either of the above in its `action.yml` or in
   a script it runs.
4. A `--shard "${{ matrix.X }}/N"` run whose matrix value
   `live_backend_shards.partition_runs` gives NornicDB runs.

Each message names the workflow, the job, and the file and line. A workflow
with no block-style `jobs:` mapping, or a job written inline without `uses:`,
fails too, because the guard cannot read it. So does a workflow, script, action,
or registry file that cannot be decoded as UTF-8: the message says "cannot
read" and names the file (the `call-*` jobs in `test.yml` are
inline reusable-workflow calls and are read as text).

Stated limits, each pinned by a test in
`scripts/test-verify-nornicdb-ci-off-forms.py` so that closing one changes the
test and this list together:

- A backend or service name computed at run time is not resolved: `$(...)`, a
  function argument, or a `${X:-nornicdb}` default assigned to a variable first.
- A compose file's own default service (`docker compose -f <file> up` with no
  service named) is not opened. The disabled e2e cell (commented out in
  `e2e-tests.yml`) paired the deploy repo's `docker-compose.yaml` with
  `graph_service: nornicdb`, so a job that ran that file bare could start
  NornicDB; the guard reads the matrix value, not the file.
- An expression that picks the backend is read only on the four keys above; a
  `${{ ... 'nornicdb' ... }}` under another matrix key is not.
- A command substitution inside a double-quoted string is part of that string.
- An environment assignment inside a Python script, a reusable workflow's jobs
  as part of its caller (the callee file is scanned on its own), and remote
  actions or images.
- A YAML anchor and alias (`x: &b nornicdb`, then `ESHU_GRAPH_BACKEND: *b`) is
  not resolved.
- A script with no `.sh` or `.py` extension, a Makefile target (`make live`), and
  a Go test helper that execs docker are not opened.
- Quoting nested more than two levels deep (a `bash -c` string inside two other
  quoted strings, with escaped quotes) is not parsed. The `_MAX_NESTING` cap of
  4 is never reached because the quote matcher stops first.
- A name piped to a command (`echo nornicdb | xargs docker run`) is not joined
  to that command.
- A dead step (`if: false`) inside a live job is flagged, because only a
  job-level `if` switches a job off. That errs on the safe side.

The guard also fails a disabled job whose registry row is still blocking. Restoring the
replay tier's `if`, or putting shard 1 back in the live-backend matrix, fails it
whatever the exact-text pins around those lines say.

| Leg | Workflow and job | How it is off | Registry row |
| --- | --- | --- | --- |
| Offline replay tier | `verify-replay-tier.yml` / `replay-tier` | `if: ${{ false }}` | `replay-tier`: manual, non-blocking, `disabled:` marker (#10223) |
| E2E NornicDB cell | `e2e-tests.yml` / `test` | matrix cell commented out | `e2e-tests`: `test (nornicdb)` removed from `check_names` |
| Golden corpus NornicDB cell | `golden-corpus-gate.yml` / `corpus-gate` | matrix cell commented out | `golden-corpus-gate`: manual, non-blocking, no `check_names` |
| Differential NornicDB captures | `golden-corpus-gate.yml` / `differential-capture`, `differential` | matrix is `[neo4j]`; staging names `neo4j`; backend-diff step is `if: ${{ false }}`; writer coverage reads Neo4j only | `golden-corpus-differential`: stays blocking (Neo4j only) |
| Live-backend NornicDB leg | `live-backend-tests.yml` / `live-backend-shards`, `live-backend` | shard matrix is `[2, 3, 4]`; aggregate runs `expand --backend neo4j` and `--skip-shard 1/4` | `live-backend-tests`: stays blocking (Neo4j shards) |
| Read-API latency cell | `read-api-latency-gate.yml` / `latency-gate` | `if: ${{ false }}` (2026-11-03) | `read-api-latency-gate`: manual, non-blocking |

The Neo4j cells stay on and blocking. The job names (`differential nornicdb vs
neo4j`, `live-backend (nornicdb and neo4j)`) are unchanged so pinned check names
do not move. Go unit tests of `go/internal/storage/nornicdb` and the
`race-graph-writes.yml` run are not CI legs and stay on. The Ifá determinism
gates and scheduled proofs already run on Neo4j per #10236 (on main), so there
was nothing to switch off here. Turning the replay
tier off leaves 167 replay-coverage surfaces unproven (457/457 to 290/457); the
coverage gate reports them as "Proof off" instead of failing. A proof gate counts
as off only when its registry row carries the `disabled:` marker (a `reason` and
a tracking `issue`, on a manual, non-blocking row) and the workflow job it names
is `if: ${{ false }}`; `ci-gates` drift check 16 and the coverage gate both check
the pair. Flipping a gate to manual and non-blocking without the marker still
fails the coverage gate. Six `class: ci`, `backends: nornicdb` live tests (for
example `go/internal/query/nornicdb_answer_truth_live_test.go`) and the replay
tier's `dedicated_ci` NornicDB entries no longer run in any job.

To re-enable everything, revert in this order and run each check after its step:

1. Restore each workflow edit from the table. Put back the commented matrix cell
   or the original `if` kept in the comment next to it.
2. Restore the registry rows (tier, `blocking`, `check_names`), then run the
   registry doc generator and the registry validator
   (`scripts/generate-ci-gates-doc.sh`, `scripts/verify-ci-gates-registry.sh`).
3. Restore the pinned shapes the guards compare against:
   `scripts/lib/nornic_conformance_runner.py` (e2e matrix) and the stage-step
   name in `scripts/lib/golden-corpus-capture-leg-cases.sh`. Pass `nornicdb
   neo4j` to `stage-differential-capture.sh`. (The replay-tier pins —
   `scripts/lib/replay_tier_routes.py` and its digests — stay on the Neo4j
   shape from #10223; re-enabling NornicDB does not move the replay tier.)
4. Delete the `nornicdb-ci-off` registry row, its two steps in
   `verify-agent-hygiene.yml`, `scripts/verify-nornicdb-ci-off.py`, its two
   scanner modules (`scripts/lib/nornicdb_ci_off_scan.py` and
   `scripts/lib/nornicdb_ci_off_shell.py`), and its four test files
   (`scripts/test-verify-nornicdb-ci-off*.py`). Do this last: until then the guard refuses any live NornicDB leg.
5. The replay-coverage dashboard needs no replay-tier step: #10223 restored the
   tier on Neo4j and the dashboard is 457/457. (If a future owner order switches
   a proof gate off again, regenerate the dashboard with
   `cd go && go test ./cmd/replay-coverage-gate/ -update-dashboard`; while a
   proof-gate row is off, its surfaces report as unproven "Proof off".)
6. Run the focused tests the disabling PR listed, then open a normal PR.
