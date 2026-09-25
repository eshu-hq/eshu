# NornicDB re-pin to plain upstream main `f2163176` (#7014)

Change: the default NornicDB backend pin moves from the eshu-hq self-built
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1`
(upstream `6ac958a9` plus the unmerged orneryd/NornicDB#502 patch) to
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-6915-f2163176@sha256:a41fa912b0ac85aa8383d3095237347201fa66bc5c8ab644ce869a6c799c44be`
(amd64 child `sha256:7cabadf4380389b27d96129ae79dd548cb17021010ff1a30bf7c87d231c76f21`,
arm64 child `sha256:67e3c82c6ff4f3389903a0dd69488a95253379ba44851d669fe94882fa1602e8`),
an eshu-hq build of plain upstream orneryd/NornicDB `main` at commit
`f2163176` (post #492/#512/#519, still carrying #491/#498/#501 and the #500
ORDER BY fix; see "Root cause and upstream fix" below).
The same 41 files move together as in the prior re-pins
([6915-nornicdb-fix500-repin.md](6915-nornicdb-fix500-repin.md),
[6162-nornicdb-fix499-repin.md](6162-nornicdb-fix499-repin.md)). Prior evidence
notes stay untouched as historical records.

Superseded first attempt: the branch first pinned
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-6915-c4de1c5c@sha256:76dd5f9b016db047ba867b69b13e4b2dd0f7b90c2764059476821ba4ce52274a`
(amd64 child `sha256:79a171850c586fb495a2d7cc66916c200bff8d27a15ab798fdc83f15e41720fb`,
arm64 child `sha256:3a4649d64116f410d10729091b1e94e05e03e94c01339c7eb1ec12e366d5c372`).
That candidate went RED on the live golden-corpus gate (5 required-fails, the
mutualPing call-chain regression root-caused to orneryd/NornicDB#519 below)
and was replaced by `f2163176`, which carries the upstream #519 fix. The
`c4de1c5c` narrative is kept below as the audit trail for that rejected
candidate, not as the description of this PR's pin.

## What the image is

An eshu-hq build of plain upstream orneryd/NornicDB `main` at commit
`f2163176`, with no Eshu-carried patch on top. Built with
`docker buildx build --platform linux/amd64,linux/arm64 -f docker/Dockerfile.amd64-cpu`
and pushed to the public eshu-hq GHCR package `nornicdb-amd64-cpu`.

### Superseded first attempt: `c4de1c5c` (rejected — RED gate, #519)

The rejected candidate was an eshu-hq build of plain upstream
orneryd/NornicDB `main` at commit `c4de1c5c`, with no Eshu-carried patch on
top. Built with
`docker buildx build --platform linux/amd64,linux/arm64 -f docker/Dockerfile.amd64-cpu`
and pushed to the public eshu-hq GHCR package `nornicdb-amd64-cpu`.

Provenance chain on `main` as of `c4de1c5c`:

- [orneryd/NornicDB#492](https://github.com/orneryd/NornicDB/pull/492) (parser
  refactor, TCK at 100%) merged 2026-09-23 14:56Z, followed by
  [#512](https://github.com/orneryd/NornicDB/pull/512) (regression fixes)
  merged 2026-09-23 16:43Z.
- Main already carried
  [#491](https://github.com/orneryd/NornicDB/pull/491) (`a427a468`, conjunct
  index-seek fix), [#498](https://github.com/orneryd/NornicDB/pull/498)
  (`3f997e04`, numID counter floor on open), and
  [#501](https://github.com/orneryd/NornicDB/pull/501) (`6ac958a9`, Close
  waits for in-flight durable writes).
- The #500 ORDER BY key fix: the regression file from the closed
  [orneryd/NornicDB#502](https://github.com/orneryd/NornicDB/pull/502), run
  against `c4de1c5c`, passes 26 of 27 subtests. #502 was closed as superseded
  by #492's parser rewrite rather than merged as its own patch.

## #502 regression-file result and the DISTINCT watch item

The one non-matching subtest is
`RETURN DISTINCT e.name AS name ORDER BY e.id` (ordering by a non-projected
key under `DISTINCT`): main at `c4de1c5c` now rejects it with
`variable e is not defined`, matching Neo4j's own behavior of refusing an
unprojected `ORDER BY` key after `DISTINCT`. On the prior `fix-500-e022384c`
pin (`main` + the standalone #502 patch) the same statement was still
accepted and silently sorted on storage order, so this is a strictly more
correct rejection, not a new divergence. A repo-wide search found every
`RETURN DISTINCT … ORDER BY` statement Eshu emits already orders on a
projected alias, so nothing in the shipped query surface hits the new
rejection; the gates below are the authoritative proof for hot-path Cypher
usage.

## Sweep

Repinned all 41 files that referenced `fix-500-e022384c` (`rg -l
fix-500-e022384c` before the sweep): `docker-compose.yaml`, Helm values
(`deploy/helm/eshu/values.yaml`,
`deploy/helm/eshu/ci/governance-two-team-k8s.values.yaml`), the replay-tier
and k8s two-team governance proof scripts and their fixtures, the Ifá
fault-injection diagnostics hard-coded image assert
(`scripts/lib/ifa_fault_injection_diagnostics.sh`), the 13 live-test
docker-run pins under `go/`, `go/internal/runtime/compose_nornicdb_image_test.go`,
and the docs enumerated in the prose-fix commit for #7014 (`docker-compose.md`,
`storage.md`, `helm-routing-and-storage-values.md`,
`docs/internal/design/430-nornicdb-graph-search-split.md`, and this note's
sibling `values.yaml` comment). All now read "eshu-hq build of plain upstream
main `c4de1c5c`", dropping every "#502 not yet merged upstream" /
"self-built ... plus the ORDER BY key fix" phrasing that described the
superseded patch stack.

## Gate results (this change)

Focused, non-live proof (all green, captured 2026-09-23):

- `cd go && env -u GOROOT go test ./internal/runtime/ -run NornicDB -count=1` — PASS.
- `bash scripts/test-verify-replay-tier.sh` — PASS.
- `bash scripts/test-verify-k8s-two-team-governance-proof.sh` — PASS.
- `bash scripts/test-k8s-two-team-governance-provenance.sh` — PASS.
- `bash scripts/test-verify-ifa-fault-injection.sh` (the harness that sources
  `scripts/lib/test-ifa-fault-injection-diagnostics-cases.sh` and
  `scripts/lib/test-ifa-fault-injection-diagnostics-fake-docker.sh` as
  fixtures; those two files are not standalone entry points) — PASS: "49
  cells, 4 shards, exact cover proven", "pin-helper behaviour check: 28
  helper(s) executed", "test-verify-ifa-fault-injection: pass".
- `cd go && env -u GOROOT go vet -tags integration` across the packages
  holding the 13 live-test image pins (`cmd/golden-corpus-gate`,
  `cmd/reducer`, `internal/query/...`, `internal/reducer/code/value/...`,
  `internal/runtime`, `internal/storage/cypher`) — clean.
- `cd go && env -u GOROOT go test ./cmd/golden-corpus-gate/ -count=1` (the
  golden-corpus-mirror static/hermetic contract test, no Docker) — PASS.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml` — PASS (pre-existing
  unrelated nav warning for `reference/local-testing/pre-pr-execution.md`
  only).
- `git diff --check` — clean.

## Live golden-corpus gate: RED — real regression, reproduced 3x

**`bash scripts/verify-golden-corpus-gate.sh` FAILS against `fix-6915-c4de1c5c`.**
The default gate ports (Postgres 15432, NornicDB Bolt 7687/HTTP 7474) were held
by a foreign, unrelated stack the whole drive (worktree
`eshu-worktrees/6843-graph-only-labels`'s `par6843-battery` compose project,
and separately a long-lived `eshu-6820-pg` container on 15432), and at times a
genuine concurrent `ci-gates` process was live elsewhere on the machine
(`pgrep -x ci-gates` matched real, short-lived processes from other sessions
more than once). Waiting did not clear the port hold (it is a persistent
container, not a finishing gate run), so every run below used the sanctioned
isolated lane: `ESHU_POSTGRES_PORT=15532 NEO4J_BOLT_PORT=7788
NEO4J_HTTP_PORT=7575 GATE_API_PORT=18081 GATE_MCP_PORT=18092` (all five
verified free with `lsof` immediately before each run),
`ESHU_LIVE_GATE_LOCK_DIR` pointed at a private scratch directory, and a
unique `COMPOSE_PROJECT_NAME` per run so nothing touched another session's
stack. Never used `ESHU_SKIP_LIVE_GATE_LOCK` or `CLAUDE_HOOK_ALLOW`. Every
run's own compose project was confirmed torn down (`docker compose ls`)
before the next one started; `par6843-battery` was never touched.

Three independent runs, same tested commit (`2d4cb9ad6f60e2dc0042307113c89ab5bbddcc97`),
same result each time — this is a stable regression, not a flake:

1. `env -u GOROOT ESHU_POSTGRES_PASSWORD=*** ESHU_NEO4J_PASSWORD=*** ESHU_POSTGRES_PORT=15532 NEO4J_BOLT_PORT=7788 NEO4J_HTTP_PORT=7575 GATE_API_PORT=18081 GATE_MCP_PORT=18092 ESHU_GRAPH_BACKEND=nornicdb bash scripts/verify-golden-corpus-gate.sh` — exit 1, `566 pass, 5 required-fail, 1 advisory-warn`.
2. Same command plus `ESHU_LIVE_GATE_LOCK_DIR=<scratch>/live-gate-lock-7014 COMPOSE_PROJECT_NAME=gate7014b7` (isolated lane) — exit 1, `565 pass, 5 required-fail, 2 advisory-warn`.
3. The `golden-corpus-differential` gate's own local command (`specs/ci-gates.v1.yaml` id `golden-corpus-differential`, `COMPOSE_PROJECT_NAME=gate7014diff`): `rm -rf /tmp/eshu-diff-corpus && ESHU_DIFFERENTIAL_CAPTURE=1 ESHU_DIFFERENTIAL_CAPTURE_DIR=/tmp/diff-capture/nornicdb ESHU_REPOS_DIR=/tmp/eshu-diff-corpus ESHU_GRAPH_BACKEND=nornicdb bash scripts/verify-golden-corpus-gate.sh && ... (neo4j leg) && ... (backend-diff, statement-coverage)` — exit 1, `565 pass, 5 required-fail, 2 advisory-warn` on the NornicDB leg (the `&&` chain short-circuited there: **the Neo4j leg and the `-phase=backend-diff`/`-phase=statement-coverage` steps never ran**).

Every run failed on the identical 5 required checks, all one symptom — a
Dart mutual-recursion CALLS-relationship name lookup (function `mutualPing`,
repo `dart_comprehensive` / `repository:r_ed3a9bab`) that comes back empty or
404 through the API/MCP query layer, while the underlying graph write is
correct:

```
[FAIL] POST /api/v0/code/relationships?assert=direct-callees: "outgoing" has 0 results, want >= 1
[FAIL] POST /api/v0/code/relationships?assert=direct-callers: "incoming" has 0 results, want >= 1
[FAIL] POST /api/v0/code/relationships?assert=transitive-callees: HTTP 404 from /api/v0/code/relationships?assert=transitive-callees
[FAIL] POST /api/v0/code/relationships?assert=transitive-callers: HTTP 404 from /api/v0/code/relationships?assert=transitive-callers
[FAIL] mcp:find_function_call_chain: "chains" has 0 results, want >= 1
```

Evidence this is a query-resolution regression, not data loss: in the same
run, `rc-11: (Function)-[:CALLS]->(Function) count=25, want >= 1` PASSED,
`edge_count_CALLS: 29, snapshot range [29,200000]` PASSED, and
`sl-dart-calls-recursion: (Function {language="dart"})-[:CALLS]->(self)
count=2, want [2,2]` PASSED — the CALLS edges exist and the Dart
self-recursion count is exactly right. Only the name+repo_id-anchored lookup
(`ResolveRelationshipsNameTarget` /
`go/internal/query/codemodel/code_relationships_resolution.go`, calling
`reader.SearchEntitiesByName`) and the transitive/MCP call-chain paths that
depend on it come back empty. The direct and transitive requests hit the
same anchor resolution but fail two different ways (200/empty vs 404),
consistent with a single upstream anchor-match miss surfacing through two
different response-shaping code paths, not two independent bugs.

Advisory-only, not blocking: `phase_maintenance_drains` ran 80s against a
25s baseline / 30s ceiling in both non-differential runs — plausibly this
machine's sustained background load (multiple foreign compose stacks and
intermittent sibling `ci-gates` runs throughout the drive), not evidence of
a NornicDB regression on its own.

**Not proven:** whether this same golden-corpus-gate assertion passed on the
prior `fix-500-e022384c` pin. The assertion entries were added to
`testdata/golden/e2e-20repo-snapshot.json` in `15134de7a1` (the only commit
touching those lines, already an ancestor of this branch) as a *required*
check, so it should have been green in CI before this repin; that was not
independently re-verified against the old image in this drive to avoid
spending more shared-machine gate time chasing a confirmed regression.

## Divergence disposition: REGRESSION, not a stale-allowlist retirement

**This is not a backend-divergence-allowlist item.** It is a same-run,
same-backend required-fail against the golden snapshot, orthogonal to the
`specs/backend-divergence-allowlist.v1.yaml` NornicDB-vs-Neo4j comparison
mechanism. No allowlist entry has been retired and no new divergence entry
has been added — the allowlist file is unmodified — because the
`golden-corpus-differential` gate's own chain never reached its
`-phase=backend-diff`/`-phase=statement-coverage` steps (run 3 above stopped
at the NornicDB leg). The two #6915 allowlist entries this repin was
expected to retire remain unretired; that retirement cannot happen until the
call-chain regression above is fixed and the differential gate can actually
run to completion.

Per the binding disposition rule, this is recorded as a regression to fix,
not suppressed, papered over, or worked around by relaxing an assertion.
**#7014/#6915 is NOT gate-proven and this pin should not roll out until the
call-chain query regression is root-caused and fixed** (most likely
candidate given the timing: the orneryd/NornicDB#492 parser rewrite changing
entity/property-match or Cypher planning behavior in a way that breaks
`SearchEntitiesByName`-style anchor lookups; not independently confirmed in
this drive).

No-Observability-Change: no metric, span, log field, or status contract
changes. The image swap is observable through the existing backend image
assertions and the (blocked) differential gate.

No-Regression Evidence: N/A for this section — the live golden-corpus gate
is RED, not green; see above. The focused Go, replay-tier, k8s governance,
and Ifá fault-injection fixture suites in the previous section are unchanged
in shape and still green on the new pin; that is a narrower proof than the
live golden-corpus/differential gates and does not substitute for them.

## Neo4j leg: the same 5 assertions PASS

At the owner's request, ran the golden-corpus gate's Neo4j leg (same isolated
lane: `ESHU_POSTGRES_PORT=15532 NEO4J_BOLT_PORT=7788 NEO4J_HTTP_PORT=7575
GATE_API_PORT=18081 GATE_MCP_PORT=18092`, `ESHU_LIVE_GATE_LOCK_DIR` under a
private scratch directory, `COMPOSE_PROJECT_NAME=gate7014neo4j`):

```
env -u GOROOT ESHU_POSTGRES_PASSWORD=*** ESHU_NEO4J_PASSWORD=*** \
  ESHU_POSTGRES_PORT=15532 NEO4J_BOLT_PORT=7788 NEO4J_HTTP_PORT=7575 \
  GATE_API_PORT=18081 GATE_MCP_PORT=18092 \
  ESHU_LIVE_GATE_LOCK_DIR=<scratch>/live-gate-lock-7014 \
  COMPOSE_PROJECT_NAME=gate7014neo4j \
  ESHU_DIFFERENTIAL_CAPTURE=1 ESHU_DIFFERENTIAL_CAPTURE_DIR=/tmp/diff-capture/neo4j \
  ESHU_REPOS_DIR=/tmp/eshu-diff-corpus ESHU_GRAPH_BACKEND=neo4j \
  bash scripts/verify-golden-corpus-gate.sh
```

Exit 0. `summary: 568 pass, 0 required-fail, 4 advisory-warn`,
`PASS: B-7 golden corpus gate green (elapsed 327s, budget ceiling 1800s)`.
The 5 assertions that fail on the NornicDB leg all pass on Neo4j, verbatim:

```
[PASS] POST /api/v0/code/relationships?assert=direct-callees: "outgoing" has 1 results; item fields [] present; json paths [], values [name repo_id], object matches [outgoing[]], and mutual-exclusion checks 0 present
[PASS] POST /api/v0/code/relationships?assert=direct-callers: "incoming" has 1 results; item fields [] present; json paths [], values [name repo_id], object matches [incoming[]], and mutual-exclusion checks 0 present
[PASS] POST /api/v0/code/relationships?assert=transitive-callees: "outgoing" has 1 results; item fields [] present; json paths [], values [], object matches [outgoing[]], and mutual-exclusion checks 0 present
[PASS] POST /api/v0/code/relationships?assert=transitive-callers: "incoming" has 1 results; item fields [] present; json paths [], values [], object matches [incoming[]], and mutual-exclusion checks 0 present
[PASS] mcp:find_function_call_chain: "chains" has 1 results; item fields [] present; json paths [], values [chains[].chain[].name cross_repo end repo_id start], object matches [], and mutual-exclusion checks 0 present
```

This confirms the regression is NornicDB-specific (the `c4de1c5c` build, not
a fixture or Eshu-side bug): the same golden corpus, the same Dart
mutual-recursion fixture, the same API/MCP handlers behave correctly against
Neo4j and incorrectly against the new NornicDB pin. Compose project
`gate7014neo4j` was torn down cleanly by the gate script; confirmed via
`docker compose ls` afterward (only the unrelated, foreign `par6843-battery`
project remains).

### `-phase=backend-diff` attempt: ran, surfaced one signal, one caveat

Ran `cd go && go run ./cmd/golden-corpus-gate -phase=backend-diff
-diff-left=/tmp/diff-capture/nornicdb -diff-right=/tmp/diff-capture/neo4j
-diff-allowlist=../specs/backend-divergence-allowlist.v1.yaml` against the
NornicDB capture from the earlier `golden-corpus-differential` attempt
(captured before that run's NornicDB leg failed, `/tmp/diff-capture/nornicdb`)
paired with this run's fresh Neo4j capture. Exit 1:
`summary: 0 pass, 1 required-fail, 0 advisory-warn`,
`gate failed: 1 required check(s) did not pass`.

The one failure is the stale-allowlist guard, not a new divergence:

```
[FAIL] nornicdb_vs_neo4j: apply divergence allowlist: divergence allowlist entry 12
("MATCH (e:Function {uid: $entity_id})-[rel:CALLS]->(target) RETURN 'outgoing' as direction, ...
ORDER BY target.uid LIMIT $row_limit"): matched no divergence in this run (stale)
```

That entry (`specs/backend-divergence-allowlist.v1.yaml` line 126, tier
`missing`, owner `query`, tracked under #6906 — a NornicDB uid-anchored CALLS
enrichment strategy vs Neo4j's transitive-path strategy, unrelated to #6915's
ORDER BY issue or to the mutualPing regression above) no longer shows a
divergence in this comparison.

**Caveat, and why this is reported but not acted on:** this pairing is NOT
the registry's intended paired run — the NornicDB capture came from a
separate, earlier gate invocation (a different container instance, corpus
staging, and generation ids) than the Neo4j capture, stitched together
after the fact because the registry's own chained command cannot get past
its failing NornicDB leg (see above). A `-phase=backend-diff` result built
from two independently-staged captures is weaker evidence than one built
from the registry's single co-run, so this entry is reported as a
**candidate** stale entry, not retired. `specs/backend-divergence-allowlist.v1.yaml`
remains unmodified in this change. Retiring it should wait for a clean,
single-invocation paired differential run once the mutualPing regression no
longer blocks the NornicDB leg from completing.

## Root cause and upstream fix: orneryd/NornicDB#519, repin to `f2163176`

**Root cause identified.** The mutualPing regression above is
[orneryd/NornicDB#519](https://github.com/orneryd/NornicDB/issues/519), "LIMIT
after a MATCH that joins an earlier variable drops rows": since `7bccfec5`
("fix: converge list executor", part of the #492 parser-refactor line), a
query whose last `MATCH` reuses a variable bound by an earlier `MATCH` and
ends in `RETURN ... LIMIT n` (no `ORDER BY`) can return fewer rows than it
should, down to none — exactly the shape of the name+repo_id-anchored CALLS
lookup used by `POST /api/v0/code/relationships` and
`find_function_call_chain`. `6ac958a9` (the prior `fix-500-e022384c` pin's
base) returns the right rows, and so does Neo4j, matching the RED-on-`c4de1c5c`
finding above.

Upstream fixed #519 in main at `f2163176`. Independently verified (by the
session that built and published the new image, not re-verified live in this
worktree): the regression test goes RED on the parent commit and GREEN on the
fix, the full `pkg/cypher` suite passes, and a local arm64 build of `f2163176`
runs the Eshu golden-corpus gate GREEN — `569 pass, 0 required-fail, 3
advisory-warn`, `PASS: B-7 golden corpus gate green (elapsed 288s, budget
ceiling 1800s)`, log at `$SP/nornic519/golden.log`
(`SP=/private/tmp/claude-501/-Users-allen-personal-repos-eshu/568182cc-70e6-429c-9430-67d47808073b/scratchpad`),
with the same 5 previously-failing assertions now passing:

```
[PASS] POST /api/v0/code/relationships?assert=direct-callees: "outgoing" has 1 results; item fields [] present; json paths [], values [name repo_id], object matches [outgoing[]], and mutual-exclusion checks 0 present
[PASS] POST /api/v0/code/relationships?assert=direct-callers: "incoming" has 1 results; item fields [] present; json paths [], values [name repo_id], object matches [incoming[]], and mutual-exclusion checks 0 present
[PASS] POST /api/v0/code/relationships?assert=transitive-callees: "outgoing" has 1 results; item fields [] present; json paths [], values [], object matches [outgoing[]], and mutual-exclusion checks 0 present
[PASS] POST /api/v0/code/relationships?assert=transitive-callers: "incoming" has 1 results; item fields [] present; json paths [], values [], object matches [incoming[]], and mutual-exclusion checks 0 present
[PASS] mcp:find_function_call_chain: "chains" has 1 results; item fields [] present; json paths [], values [chains[].chain[].name cross_repo end repo_id start], object matches [], and mutual-exclusion checks 0 present
```

**Repin.** The default pin moves from
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-6915-c4de1c5c@sha256:76dd5f9b016db047ba867b69b13e4b2dd0f7b90c2764059476821ba4ce52274a`
(amd64 `79a171850c586fb495a2d7cc66916c200bff8d27a15ab798fdc83f15e41720fb`, arm64
`3a4649d64116f410d10729091b1e94e05e03e94c01339c7eb1ec12e366d5c372`) to
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-6915-f2163176@sha256:a41fa912b0ac85aa8383d3095237347201fa66bc5c8ab644ce869a6c799c44be`
(amd64 `7cabadf4380389b27d96129ae79dd548cb17021010ff1a30bf7c87d231c76f21`,
arm64 `67e3c82c6ff4f3389903a0dd69488a95253379ba44851d669fe94882fa1602e8`), an
eshu-hq build of plain upstream orneryd/NornicDB main at `f2163176` (post
#492/#512/#519, still carrying #491/#498/#501 and the #500 ORDER BY fix; self-
reports 1.3.3). The same 41 files moved together, swept with a copy of the
repin helper (`old_*` pointed at the `c4de1c5c` values). Two truncated
digest-prefix occurrences the sweep cannot catch by design (an `rg` example in
`docker-compose.md` and the `wrong-index` mutation in
`test-verify-k8s-two-team-governance-proof.sh`, kept tag-only per the same
precedent as the fix-500 repin) were fixed by hand. `rg -n "c4de1c5c" .`
outside `docs/internal/evidence/` returns nothing.

Prose in `deploy/helm/eshu/values.yaml`, `docs/public/run-locally/docker-compose.md`,
`docs/public/deploy/kubernetes/storage.md`,
`docs/public/deploy/kubernetes/helm-routing-and-storage-values.md`, and
`docs/internal/design/430-nornicdb-graph-search-split.md` now name the pinned
commit as `f2163176` and cite #519.

**Post-repin local proof (this worktree, non-live):**

- `cd go && env -u GOROOT go test ./internal/runtime/ -run NornicDB -count=1` — PASS.
- `bash scripts/test-verify-replay-tier.sh` — PASS.
- `bash scripts/test-verify-k8s-two-team-governance-proof.sh` — PASS (confirms the hand-fixed `wrong-index` mutation still rejects invalid provenance).
- `bash scripts/test-k8s-two-team-governance-provenance.sh` — PASS.
- `bash scripts/test-verify-ifa-fault-injection.sh` — PASS: "49 cells, 4 shards, exact cover proven", 28 pin-helper checks, "test-verify-ifa-fault-injection: pass".
- `cd go && env -u GOROOT go vet -tags integration` across the packages holding the 13 live-test image pins — clean.
- `cd go && env -u GOROOT go test ./cmd/golden-corpus-gate/ -count=1` — PASS.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml` — PASS (same pre-existing unrelated nav warning).
- `git diff --check` — clean.

No live gate was run in this worktree for the repin itself (not requested this
round; the golden-corpus GREEN evidence above comes from the separate session
that built and validated the `f2163176` image, cited by log path, not
reproduced live here).

No-Observability-Change: no metric, span, log field, or status contract
changes from the repin itself.

No-Regression Evidence: the upstream fix was benchmarked/proven by the
publishing session (RED-on-parent/GREEN-on-fix regression test, full
`pkg/cypher` suite, golden-corpus gate); this worktree's own proof is the
focused non-live suite above, unchanged in shape and green on the new pin.

## NornicDB-vs-Neo4j differential on `f2163176`: two runs, second GREEN

Ran the `golden-corpus-differential` registry command (`specs/ci-gates.v1.yaml`
id `golden-corpus-differential`) against `fix-6915-f2163176` on the isolated
lane (`ESHU_POSTGRES_PORT=15533 NEO4J_BOLT_PORT=7789 NEO4J_HTTP_PORT=7576
GATE_API_PORT=18083 GATE_MCP_PORT=18094`, own `ESHU_LIVE_GATE_LOCK_DIR`,
`NORNICDB_PLATFORM=linux/arm64`, `NORNICDB_IMAGE` pinned to the digest above).
Checked no other live gate first each time (`pgrep -x ci-gates`,
`docker compose ls`); tore down cleanly after each run, confirmed via
`docker compose ls`.

**Run 1: RED, but it's the known #6502 flake, not a NornicDB regression.**
The NornicDB leg failed the drain gate: `fact_work_items_residual: residual=2
(dead_letter=2) [container_image_identity/dead_letter/projection_bug=2]
"read container image identity activation epoch: container image identity
generation is not active"` after a 10-minute drain timeout — a different
domain (`container_image_identity`) than the mutualPing regression
(`code_relationships`), never reached the Neo4j leg or backend-diff. At the
time, machine load was 45.19/29.06/22.87 (an ~18-core box), with a concurrent
foreign `ci-gates` process, `par6843-battery`, three `diag7014-*` containers,
and several other persistent stacks all running simultaneously. Per the
owner, this is [eshu-hq/eshu#6502](https://github.com/eshu-hq/eshu/issues/6502),
a known Eshu reducer flake on the golden gate under load, unrelated to
NornicDB.

**Run 2 (after `make pre-push` for #7007 finished and the machine quieted,
load ~23): GREEN through both legs.**

- NornicDB leg: `570 pass, 0 required-fail, 2 advisory-warn`,
  `PASS: B-7 golden corpus gate green (elapsed 268s, budget ceiling 1800s)`.
- Neo4j leg: `568 pass, 0 required-fail, 4 advisory-warn`,
  `PASS: B-7 golden corpus gate green (elapsed 366s, budget ceiling 1800s)`.

`-phase=backend-diff` then ran (exit 1, one finding — the stale-entry guard,
not a new divergence):

```
[FAIL] nornicdb_vs_neo4j: apply divergence allowlist: divergence allowlist entry 43
("MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|...
]->(related:Repository) RETURN ... UNION MATCH (related:Repository)-[rel:...]->
(r:Repository {id: $repo_id}) RETURN related.id AS repo_id, related.name AS repo_name"):
matched no divergence in this run (stale)
```

That entry (`specs/backend-divergence-allowlist.v1.yaml` line 281, tier
`results`, owner `query`, tracked under
[#6916](https://github.com/eshu-hq/eshu/issues/6916) — NornicDB's `UNION`
appending an extra text-named column duplicating `repo_name` for the
repo-dependency edge family) no longer diverges. **Not related to #6915 or
#519** — a different NornicDB dialect quirk, on a Repository `DEPENDS_ON`
family query, not the CALLS/mutualPing shape. No `#6915`-tagged entries
remain in the allowlist to retire (both were already retired in the earlier
`fix-500-e022384c` repin). This is the strongest-evidence backend-diff result
of the drive (a single paired co-run, not stitched captures), so unlike the
earlier candidate finding it is not a correlation artifact. The
`-phase=statement-coverage` step never ran (the chain stopped at
`backend-diff`'s exit 1). The lead's retirement call for this entry is
recorded in the next section.

## Allowlist entry 43 (#6916) retired

The lead's call: retire it. Per the #6991/#6984 repin precedent, a stale
allowlist entry — one that matched no divergence in a green run — is
removed, not kept as a permanent exemption; the file's own `design:` header
states the same rule (`Excuse` fails a stale entry even on a green run).
Removed the 5-line entry (`statement`/`tier`/`reason`/`upstream`/`owner`) for
the Repository-dependency `UNION` statement from
`specs/backend-divergence-allowlist.v1.yaml`; `rg -n "6916"
specs/backend-divergence-allowlist.v1.yaml` now returns nothing.

Validated with the gate's own static/hermetic mirror (the `golden-corpus-differential`
registry entry's `test_command`, no Docker needed since it only parses the
allowlist and exercises the diff/coverage Go code paths):

- `ESHU_POSTGRES_PORT=15532 NEO4J_BOLT_PORT=7788 NEO4J_HTTP_PORT=7575 GATE_API_PORT=18081 GATE_MCP_PORT=18092 bash scripts/test-verify-golden-corpus-gate.sh` (ports relocated only to satisfy the repo's live-gate guard hook's name-pattern match; this mirror does not itself bind them) — `test-verify-golden-corpus-gate: pass`.
- `cd go && env -u GOROOT go test ./cmd/golden-corpus-gate -run 'TestBackendDiff|TestRunBackendDiff|TestStatementCoverage|TestRunStatementCoverage' -count=1` — PASS.
- `cd go && env -u GOROOT go test ./internal/backendconformance -run 'TestComputeStatementCoverage' -count=1` — PASS.
- `cd go && env -u GOROOT go test ./internal/queryplan -run 'TestDiscoverStatementBuilders|TestValidateBuilderManifest|TestStatementBuildersManifestMatchesProduction' -count=1` — PASS.

The retirement is proven by the live differential run cited above (this
worktree, isolated lane, `COMPOSE_PROJECT_NAME=gate7014f2163176diff2`,
`differential-f2163176-run2.log`): NornicDB `570 pass, 0 required-fail`, Neo4j
`568 pass, 0 required-fail`, both green, and `-phase=backend-diff` flagged
this entry alone as stale in that single paired co-run — not re-run after
the edit, since the parser-level mirrors above are what a text-only YAML
removal needs, and the live differential is the expensive proof that already
ran. `specs/backend-divergence-allowlist.v1.yaml` is modified in this change.
