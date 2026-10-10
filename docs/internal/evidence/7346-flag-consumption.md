# 7346 (part 2) — file_import_cycles consumes the import flags

Validation record for the second of two PRs that deliver issue #7346, split from
#6851. Part 1 (`7346-cycle-paging-budget.md`) fixed the pager, bounded the walk,
and threaded the enumeration state. This part makes the cycle reader use the
`type_only`, `deferred` and `inferred` properties the parser (#7344) emits and
the projector (#7345) writes onto `IMPORTS` edges.

## Design (arbiter-reviewed)

- **Five edge states.** `runtime` (all three flags written, none set),
  `type_only`, `deferred`, `inferred`, and **`unknown`**: any of the three
  properties is missing or not a boolean. Only an edge written before the flags
  existed is unknown, because the projector writes all three as explicit
  booleans on every edge.
- **A null flag is never runtime.** The reader projects `rel.type_only`,
  `rel.deferred` and `rel.inferred` raw with no `coalesce`, and classifies in Go
  with a three-way decode. A lenient boolean helper would turn a null into false
  and silently count a legacy edge as proven runtime.
- **Exclusion before the walk.** Type-only and deferred edges are dropped: a
  cycle closed through an import that never runs at load time is not a load-time
  cycle. Unknown edges stay in, because dropping them would hide real cycles
  and exclusion needs proof. Inferred edges stay in.
- **A cycle is labelled by its weakest edge.** `ambiguous` if any edge is
  inferred, else `flags_unknown` if any edge is unknown, else `runtime`. Each
  `cycle_edges` item carries `flag_state` (`runtime`, `inferred`, `unknown`).
  `coverage.cycle_edge_flags` counts the deduplicated edges by class before
  anchor filtering: considered, type-only excluded, deferred excluded, inferred,
  unknown.
- **Duplicate rows fold.** The reader matches `Module` by name while the writer
  keys it on (name, language), so two graph edges can reach the reader as one
  (file, module) pair. They fold to the strongest proof: any runtime row wins,
  then unknown, then inferred; only when every row is type-only or deferred is
  the edge excluded. When several edges feed one collapsed hop, runtime wins over
  inferred, which wins over unknown.
- No request parameter to include type-only or deferred edges was added: nothing
  asks for it, and it would widen the wire contract and double the test matrix.

## Failing first

All of these failed against the reader as it was (it ignored the flag columns),
with the behavioral messages below, not compile errors:

| Test | Before |
| --- | --- |
| `TestCycleFlagsExcludeTypeOnlyAndDeferredEdges` | `len(cycles) = 1, want 0` for a cycle closed only through a type-only or deferred edge |
| `TestCycleFlagsLabelEachCycleByItsWeakestEdge` | `cycle_label = <nil>` for every case (runtime, ambiguous, flags_unknown, partly written flags, inferred beside unknown) |
| `TestCycleFlagsKeepAnAlternateRuntimePath` | 2 cycles, want only the runtime `a<->c` cycle |
| `TestCycleFlagsFoldDuplicateEdgeRows` | excluded and mislabelled rows for every fold case |
| `TestCycleFlagCoverageCountsEveryEdgeClass` | `coverage.cycle_edge_flags` absent |
| span test | the five flag-count attributes missing from the handler span |

## Graph truth

`TestFileImportCyclesFlagTruthAgainstARealGraph` seeds a repository with twelve
`IMPORTS` edges, eleven carrying the projector's flag properties and one legacy
edge written with none, and reads them through the production `imports.CycleRows` and
response shaper: a runtime cycle (all flags explicitly false), a cycle closed only
through a type-only edge, one closed only through a deferred edge, a legacy cycle
with one no-flag edge, an inferred cycle, and the Python relative-import fallback
`./xi_b`, which never equals a module name and so must close nothing.

| Backend | Result |
| --- | --- |
| NornicDB, pinned `ghcr.io/eshu-hq/nornicdb-amd64-cpu` v1.3.3 (secondary) | PASS at the first implementation build; not re-run at this head (secondary backend, #7331). Cycles: `rt_a` runtime, `lg_a` flags_unknown, `in_a` ambiguous; no type-only, deferred or `./x` cycle. Coverage: considered 12, type-only excluded 1, deferred excluded 1, inferred 2, unknown 1 |
| Neo4j, digest-pinned `neo4j:2026-community@sha256:eabfbb04...` (Kernel 2026.08.1, primary) | PASS at the head rebased onto the merged carriage change (#7345): `ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=neo4j go test ./internal/replay/offlinetier -run 'TestFileImportCyclesFlagTruthAgainstARealGraph|TestCanonicalImportEdgesGraphTruth' -count=1 -v` on a throwaway tmpfs container, exit 0 (1.62 s for this test). Cycles: `in_a` ambiguous, `lg_a` flags_unknown, `rt_a` runtime; no type-only, deferred or `./x` cycle. Coverage: considered 12, type-only excluded 1, deferred excluded 1, inferred 2, unknown 1, identical to the NornicDB run. Data and logs ran on tmpfs because the Docker VM disk was full |

The NornicDB run answers the question the design left open: a relationship
property that was never written comes back from the pinned NornicDB as a null
that the reader classifies as `flags_unknown`, not as false. The test seeds edges
in the writer's property shape and does not go through the canonical writer. #7345 has
since merged, and its live test `TestCanonicalImportEdgesGraphTruth`, which does go through
the production writer and reads explicit booleans back, passes on the same pinned Neo4j at this
head in the same run. The two tests are not joined into one writer-to-reader case.

## Order independence

The fold is a maximum over a total order, and the hop collapse decides on proof
before line number. Both were first asserted only with the stronger row last and
every row on line 1, so a reversed fold passed. `TestCycleFlagsFoldDoesNotDependOnRowOrder`
and `TestCycleFlagsHopCollapsePrefersTheStrongerProof` feed each pair in both orders with the
stronger row on line 9 and the weaker on line 1. Three overlay mutations that
survived the earlier tests now fail: the fold returning the last row (4 failing
lines), the hop rank preferring unknown (7), and the hop state check removed (7).
Each mutated file was restored byte-identical.

The dedupe used to keep the earliest-line row and overwrite only its state, so a
type-only import's line could sit beside a runtime state. The kept row now comes
from the surviving state, and the line assertions in the two tests failed first
(`line_number = 1, want 9`).

## Performance Evidence:

Query shape: `FileImportCycleEdgeRowsCypher`, a bounded read
`(Repository {id})-[:REPO_CONTAINS]->(File)-[rel:IMPORTS]->(Module)` with
`LIMIT $scan_limit` (25,001), now projecting three more raw relationship
properties (`rel.type_only`, `rel.deferred`, `rel.inferred`). Backend:
digest-pinned `neo4j:2026-community`. Index and constraint state: `Repository.id`
and `File.path` uniqueness constraints and the `Module.name` lookup index.
Input cardinality: Python-shaped synthetic data at the largest measured corpus
size (626 files, 4,522 edges) and at the scan limit (2,500 files, 25,000 edges),
with 10% of edges flagged type-only, 10% deferred, 10% inferred, 10% with no flag
properties (legacy) and the rest explicit false. Measured with `PROFILE`, old
statement (the same text without the three columns) against new, on both the
scoped-grant and the unscoped shape.

| Shape | Rows | DB hits old | DB hits new | Delta | Per row |
| --- | --- | --- | --- | --- | --- |
| scoped grant, 4,522 edges | 4,522 | 39,310 | 52,876 | 13,566 | 3.0 |
| unscoped, 4,522 edges | 4,522 | 23,101 | 36,667 | 13,566 | 3.0 |
| scoped grant, 25,000 edges | 25,000 | 212,504 | 287,504 | 75,000 | 3.0 |
| unscoped, 25,000 edges | 25,000 | 120,005 | 195,005 | 75,000 | 3.0 |

Every shape returns identical rows apart from the three added columns, has no
`Eager` operator, and adds exactly three DB hits per returned row, which is the
bound the design review set (three times the row count). That is a 35% to
63% increase in DB hits for this statement, depending on the shape, and it is inherent: the flags are extra
properties that must be read.

The operator tree is **not** byte-identical, and the design review's pass
criterion asked for an identical tree. The new plan has exactly one additional
operator, a `CacheProperties` that caches the three added property reads,
placed after the sort-and-limit stage. In each of the four shapes the diff of
old against new is exactly that one inserted operator, and the access-path
operators are identical (`NodeUniqueIndexSeek`, then `Expand(All)` twice, with
`Filter` between): no new scan, no new expand, no eager operator. This is
reported as measured; the criterion was not relaxed to make it pass.

Laptop smoke figures, superseded by the remote run recorded under "Remote wall-time result" below. The same runs on the shared laptop
measured medians of 116 ms to 155 ms at 4,522 edges and 646 ms to 775 ms (scoped)
at 25,000 edges, an increase of roughly 18% to 34% across the four shapes (unscoped
4,522 edges 123.8 to 145.7 ms is the low end). That laptop was under heavy contention
from other work, so these are smoke figures, not accepted timing.

The route's checked-in latency contract is the capability matrix's p95 for `symbol_graph.import_dependencies`:
1,500 ms per call in the supported production profile and 1,000 ms in `local_authoritative` and
`local_full_stack` (`specs/capability-matrix.v1.yaml`), the same 1.5-second interactive SLO that the #5561
evidence proved per call. The fetch is one component of that call; the Go walk is bounded at 250,000 steps
with a 250 ms design ceiling (part 1), and row shaping is measured, not assumed.

The criterion for the remote run was fixed by an arbiter before it ran, so it is not chosen after the data.
The remote rig (a dedicated 16-CPU Linux host, the pinned Neo4j with the production schema) models the
deployed profile, so the 1,500 ms production figure gates.

- **G1, deterministic.** The remote `PROFILE` gives exactly `hits_new - hits_base = 3 x rows` (three property
  reads per returned row), equal row counts, no `Eager` operator, an operator set that differs from the
  base only by the one `CacheProperties` operator, and an identical access path.
- **G2, relative wall time.** For each of the four shapes, both the ratio of medians and the mean of the
  same-round ratios stay at or below that shape's DB-hit ratio (1.3451 scoped and 1.5872 unscoped at 4,522
  edges; 1.3529 scoped and 1.6250 unscoped at 25,000 edges) plus the control bound derived from two A/A-only sets
  on the same rig. Validity: rule PD holds, at least nine valid rounds per shape, and a derived bound of at
  most 0.15; otherwise the run is void, not a code result.
- **G3, absolute, fetch only.** At both 25,000-edge shapes the maximum valid round of the new statement is at
  most 1,250 ms, which is the 1,500 ms production p95 minus the walk's 250 ms ceiling; with 9 to 18 valid rounds
  the nearest-rank p95 is the maximum, so the gate is stated as the maximum. The base statement is judged
  against the same bar so a breach is attributed: base and new both over is a pre-existing breach to record and
  file, with no claim that the SLO holds; base under and new over means this change crosses the SLO, and the work
  stops for profiling. The 4,522 shapes are reported against the same bar and not gated. Median and the cold
  first execution are reported beside it.
- **G4, per call.** The in-process time of the handler's import-dependency read (fetch, cycle build and walk),
  cold and warm, at the 25,000-edge graph for `file_import_cycles` scoped and unscoped, is at most 1,500 ms. It is
  measured through a test-local reader that has none of the production read policy (deadlines, retries,
  telemetry), so it is the query and walk cost, not the full server path.

The 1,000 ms local-profile rows are reported as findings only (whether the 25,000-edge fetch maximum fits
750 ms and the per-call time fits 1,000 ms), because the rig does not model those profiles. A breach of G2, G3 or
G4 with G1 holding is an unexplained cost to diagnose, not a disclosure item. The handler wraps no timeout of its
own; the fetch runs through the import-dependency rows path into the Neo4j reader, whose per-read deadline is
`querycontract.DefaultGraphReadTimeout` (10 s), with slow-read logging above 1 s. The remote-validation artifact
cited for the production capability row (`prod-import-dependencies`) is a compose end-to-end run whose form is
checked and which binds no statement; it is not refreshed here, and this note does not claim it re-validates the
changed statement.

### Remote wall-time result

Rig: the dedicated 16-CPU Linux host, the pinned `neo4j:2026-community` image (digest
`sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`, 8 GiB heap and page cache),
the production schema from `bootstrap-data-plane`, base statements from merge-base
`6ec0073d0980e9a44af632b2ed3187cd3987f6f1` and new statements from head
`c6b7dc2bb3600e9b0f8f7746bfa686d8f372e272` (a pre-rebase commit that the branch no longer contains; the
statement text is identical at the rebased head, where the unscoped statement's `cypher_sha256` in
`go/internal/queryplan/testdata/hot-cypher.yaml` is `d7c9e82091dd8d35f42604a0fc4a94f0f2550f75aacf1b10f3ccd26048e276ce`),
both dumped from the real `FileImportCycleEdgeRowsCypher` (the unscoped statement built with `AllScopes: true`). Rule PD held: load1 2.39 at start, 2.94 at end, 3.02 the
in-run maximum of the 1 s samples, against a limit of 8. Each statement text had a 15 s discarded warm-up. The
control bound came from two A/A sets (72 pooled ratios, range 0.9473 to 1.0159, SD 0.0084) and is B = 0.0778;
every shape had 9 valid rounds from 9 attempts. The A/B rounds ran in a rotating Latin-square order.

| Shape (edges, variant) | G1: hits new - base | G2: ratio of medians / same-round mean (limit) | Fetch max new / base | G3 |
| --- | --- | --- | --- | --- |
| 4,522 scoped | 13,566 = 3 x 4,522 | 1.0429 / 1.0374 (1.4229) | 0.103 s / 0.100 s | reported |
| 4,522 unscoped | 13,566 = 3 x 4,522 | 1.0369 / 1.0371 (1.6650) | 0.103 s / 0.099 s | reported |
| 25,000 scoped | 75,000 = 3 x 25,000 | 1.0348 / 1.0329 (1.4307) | 0.557 s / 0.542 s | pass, bar 1.25 s |
| 25,000 unscoped | 75,000 = 3 x 25,000 | 1.0384 / 1.0368 (1.7028) | 0.560 s / 0.539 s | pass, bar 1.25 s |

Wall time rose 3.5% to 4.3% at the median while DB hits on this rig rose 58.7% (23,101 to 36,667 at 4,522
edges) and 62.5% (120,005 to 195,005 at 25,000 edges), identically for scoped and unscoped: this rig's planner
applies the grant at the repository seek, so the grant adds no per-row cost. The extra property reads are cheap
next to the index seek, expand and sort. The G2 limits use the pre-registered DB-hit ratios H from the laptop
`PROFILE` (1.3451 and 1.3529 scoped, 1.5872 and 1.6250 unscoped), which for the scoped shapes are stricter than
this rig's own ratio, so the gate was not loosened; the laptop scoped figures came from a different plan choice.
In all four shapes the
new plan differs from the base plan by exactly one added `CacheProperties`, with the same access path and no
`Eager`. The cold first execution was 0.197 s and 0.170 s (4,522 scoped, unscoped) and 0.615 s and 0.599 s
(25,000 scoped, unscoped) for the new statement.

G4, per call, in process: the walk was driven to its budget by a ring fixture (626 files, 24,414 edges, every
edge resolving in-repo, no cycle of length 8 or less, so the run reports `rows=0`, `stop=step_budget`,
`steps=250000`), and this was asserted by the evaluator. Scoped: cold 0.311 s, warm maximum 0.277 s.
Unscoped: cold 0.297 s, warm maximum 0.251 s, both against 1.5 s. On the random 25,000-edge graph, which has
no cycle and so reports `rows=0`, `steps=0`, the same calls took 0.264 s to 0.270 s cold and at most 0.251 s
warm (report-only; that graph does not exercise the walk). Report-only findings: the 25,000-edge fetch maximum
fits 0.750 s and every per-call time fits 1.0 s, so the 1,000 ms local profiles would also hold on this rig,
which does not model them.

`eval_reader.py` (G1 to G4 as fixed above) reported PASS. What this does not show: it is a synthetic
Python-shaped graph on one rig, not the deployed corpus, and the per-call figure uses a test-local reader
without the production deadlines, retries or telemetry.

Two earlier remote attempts at this head were void under the pre-registered validity rule (derived control
bound 0.2079 and 0.2077 against the 0.15 cap) and are not used for any claim. The passing run is the third.
After the second void attempt (its A/A ratios ranged 0.8652 to 1.0176 with a pooled SD of 0.0243, consistent with
cold executions) the harness gained a 15 s discarded warm-up per statement text; the second void attempt's
harness had a single discarded execution. The criterion, the evaluator's gates and the validity rule did not
change, and the second void attempt's A/B figures, which back no claim, pointed the same way (ratio of medians
1.035 to 1.037).
The other tenants on the host were not quiet: the per-second container CPU samples show foreign containers
using up to about 2.9 cores in bursts (host busy cores mean 3.0, maximum 5.9 of 16), and the run met rule PD and
the validity rule; it is stated here rather than hidden. The Neo4j query log could not be made
to record on the pinned image (only `db.logs.query.enabled` and `db.logs.query.threshold` are accepted and the
log stayed empty), so per-statement server-side timing is not part of this evidence.

## Benchmark Evidence:

No new benchmark. The walk is unchanged for a strongly connected graph; the
added work is a state decode per edge row and a fold per duplicate key, both
linear in the row count and allocation-light. The walk's own cost is pinned by
part 1's step counts and benchmark. The reader's wall time is measured on the remote rig (see above).

## Observability Evidence:

The `query.import_dependency_investigation` span gains, for `file_import_cycles`,
`eshu.import_dependencies.cycle_edges_considered`, `cycle_flags_unknown`,
`cycle_inferred_edge_count`, `cycle_type_only_excluded` and
`cycle_deferred_excluded`. An operator reading a trace sees how much of an
answer rests on edges that predate the flags and how many edges the flags
removed, without reading the payload. A handler test drives the real route and
asserts all five; the response carries the same counts as
`coverage.cycle_edge_flags`. OpenAPI, the MCP tool description, the HTTP
reference and the codemodel README are updated to the same contract.

## Real-corpus proxy step count

The 250,000-step walk budget from part 1 was set by a wall-time ceiling, not by a margin
over a real import graph, and an arbiter condition on #7346 asked for that margin before this
part merges. The named corpus (`trident-automation`, 4,522 edges over 626 files, 172 resolved
in-repo) is a QA graph that this lane may not read, so the measurement uses real public
Python repositories as a labelled proxy. An arbiter accepted that on these terms: each proxy is
chosen by reader-resolved edge count (at least 172), its rows are built through the projector's
module-naming rule, and the bar is steps x 10 at most 250,000 on every qualifying proxy at
`max_cycle_length` 8.

Method: the merged reader (`BuildFileImportCycleRows`) run in process over the head Python parser's
imports folded per (file, module), with the three flag columns false (a run with them omitted gave
identical cycles and steps on every qualifying repo). 56 repositories were cloned, 38 parsed and 6
qualify; the discarded ones are listed with their resolved counts in the local report, and most
resolve almost nothing because a packaged library's imports are `./`-prefixed paths the reader never
matches to a module name.

| Repository | .py files | Resolved edges | Cyclic components (largest) | Length 5 cycles / steps | Length 8 cycles / steps |
| --- | --- | --- | --- | --- | --- |
| django/django | 2,932 | 696 | 0 | 0 / 0 | 0 / 0 |
| tensorflow/models | 2,723 | 576 | 0 | 0 / 0 | 0 / 0 |
| NVIDIA/DeepLearningExamples | 2,592 | 824 | 1 (17 files) | 26 / 2,104 | 26 / 4,184 |
| ansible/ansible | 1,845 | 979 | 0 | 0 / 0 | 0 / 0 |
| micropython/micropython-lib | 509 | 256 | 0 | 0 / 0 | 0 / 0 |
| python/cpython | 2,364 | 729 | 0 | 0 / 0 | 0 / 0 |

Pinned commits of the six: django `5a4511adb247a44a1cada11fe4763abba0a42663`, tensorflow/models
`3c4ccb467af565394c58eca434c5bfb12dcd0b3a`, NVIDIA/DeepLearningExamples
`729963dd47e7c8bd462ad10bfac7a7b0b604e6dd`, ansible `a900ea9d0c3665d4013d305dabe6d0af2c3231f8`,
micropython-lib `4fa59bd6a5916783e8503e9f2339627c8cffa5bf`, cpython
`39e5ed7fea8364dc30efd3fa5d68644606b0eac8`. The probe was a throwaway Go test in a detached worktree that
ran the head Python parser over each clone and called the merged reader; it was not committed.

The bar holds on all six: every run stopped with reason `none`, and none stopped on `step_budget`.
The worst case is NVIDIA/DeepLearningExamples (also the densest graph, 4,415 collapsed file hops) at 4,184
steps at length 8, about 1.7% of the budget and a 60x margin. This is weak evidence and is stated as
such: only one of the six proxies has a cyclic component at all, so on the other five the reader walks
nothing. It shows the budget is not the binding constraint on these graphs; it does not show how
`trident-automation` behaves. The resolved edges on the proxies are mostly standard-library names
(`datetime`, `json`, `typing`, `io`) that collide with a same-named file elsewhere in the tree. That is a pre-existing accuracy risk the proxy data exposed, not one this change introduces: the reader turns `import json` into a hop to any repository file named `json.py` that itself imports something (a file only becomes a hop target if it is the source of at least one edge in the fetch), which can report a false cycle and inflates the resolved-edge counts these proxies qualified on. The matcher is deliberately not widened or narrowed here. The
replay of the exact corpus stays an open follow-up on #7346 (one read-only query and a CSV export, then a
replay through the reader), and the merge does not wait for it. Wall time per step was not measured on
this data.

## Limits stated plainly

- **Legacy edges.** Edges on files that never change stay without flag
  properties under delta-only operation until that file changes or a full
  generation runs. Until then a cycle through such an edge is labelled
  `flags_unknown`, not `runtime`; that is the point of the state, and it means a
  result can include a cycle the flags would have excluded.
- **`ambiguous` is not reachable end to end today.** The parser's inferred source
  is the `./x` fallback, and the reader matches a target by exact module name
  against the importing file's own module, so `./x` never equals a module and an
  inferred Python edge cannot close a cycle. The label is exercised on seeded
  edges. The matcher was deliberately not widened here: doing so risks false
  cycles and is a separate change.
- The NornicDB leg (secondary, #7331) was not re-run at this head (see Graph truth), and its wall time was not
  measured; the timing above is Neo4j only.
- **The console does not show the labels yet.** `apps/console/src/api/codeImports.ts`
  normalizes cycle rows and drops `cycle_label` and `flag_state`, so a cycle still
  renders without its label. Rendering them is a separate console change; it is not
  part of this reader change.
- `codemodel` now holds 45 non-test files under its existing justified
  `//nolint:dirgate` marker, whose reason text is updated. A subpackage split is
  a package move and out of scope for this lane.
- Two `internal/query` tests pin the import-dependency statement family
  (`TestImportDependencyQueryplanVariantsStayComplete` and
  `TestHandlerQueryplanProductionVariantFamiliesStayExplicit`). They failed on the
  changed statement text and were missed by scoped runs that left the root
  `internal/query` package out. The variant counts (280 and 488) are unchanged; only
  the two family hashes moved, and both were refreshed to the values the tests report.
- The `hot-cypher` manifest hashes for `QP-CODE-IMPORT-CYCLES` are refreshed
  because the statement text changed on purpose (three added projections); it is
  not a relocation.
