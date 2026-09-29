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
| NornicDB, pinned `ghcr.io/eshu-hq/nornicdb-amd64-cpu` v1.3.3 (secondary) | PASS at the first implementation build; NOT re-run at the final head (the pinned image is not local and the Docker VM disk was full). Cycles: `rt_a` runtime, `lg_a` flags_unknown, `in_a` ambiguous; no type-only, deferred or `./x` cycle. Coverage: considered 12, type-only excluded 1, deferred excluded 1, inferred 2, unknown 1 |
| Neo4j, digest-pinned `neo4j:2026-community@sha256:eabfbb04...` (Kernel 2026.08.1, primary) | PASS at the head that carries the order-independence fix (3.69 s). Cycles: `in_a` ambiguous, `lg_a` flags_unknown, `rt_a` runtime; no type-only, deferred or `./x` cycle. Coverage: considered 12, type-only excluded 1, deferred excluded 1, inferred 2, unknown 1, identical to the NornicDB run. Data and logs ran on tmpfs because the Docker VM disk was full |

The NornicDB run answers the question the design left open: a relationship
property that was never written comes back from the pinned NornicDB as a null
that the reader classifies as `flags_unknown`, not as false. The test seeds edges
in the writer's property shape and does not go through the canonical writer; a
writer-driven case should join it once #7345 has merged.

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
bound the design review set (three times the row count). That is about a 35%
increase in DB hits for this statement and it is inherent: the flags are extra
properties that must be read.

The operator tree is **not** byte-identical, and the design review's pass
criterion asked for an identical tree. The new plan has exactly one additional
operator, a `CacheProperties` that caches the three added property reads,
placed after the sort-and-limit stage. In each of the four shapes the diff of
old against new is exactly that one inserted operator, and the access-path
operators are identical (`NodeUniqueIndexSeek`, then `Expand(All)` twice, with
`Filter` between): no new scan, no new expand, no eager operator. This is
reported as measured; the criterion was not relaxed to make it pass.

Wall time: NOT_CHECKED on a valid test bed. The same runs on the shared laptop
measured medians of 116 ms to 155 ms at 4,522 edges and 646 ms to 775 ms (scoped)
at 25,000 edges, an increase of roughly 18% to 34% across the four shapes (unscoped 4,522 edges 123.8 to 145.7 ms is the low end). That laptop was under heavy
contention from other work and the dedicated remote was unreachable, so these are
smoke figures, not accepted timing, and no claim is made about the interactive
1.5 s bound at the scan limit. The increase in DB hits says the wall-time cost
is real and about proportional; the remote run must confirm the bound holds.

## Benchmark Evidence:

No new benchmark. The walk is unchanged for a strongly connected graph; the
added work is a state decode per edge row and a fold per duplicate key, both
linear in the row count and allocation-light. The walk's own cost is pinned by
part 1's step counts and benchmark. Timing is NOT_CHECKED (see above).

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
in-repo) is an ops-qa graph that this lane may not read, so the measurement uses real public
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

The bar holds on all six: every run stopped with reason `none`, and none stopped on `step_budget`.
The worst case is NVIDIA/DeepLearningExamples (also the densest graph, 4,415 collapsed file hops) at 4,184
steps at length 8, about 1.7% of the budget and a 60x margin. This is weak evidence and is stated as
such: only one of the six proxies has a cyclic component at all, so on the other five the reader walks
nothing. It shows the budget is not the binding constraint on these graphs; it does not show how
`trident-automation` behaves. The resolved edges on the proxies are mostly standard-library names
(`datetime`, `json`, `typing`, `io`) that collide with a same-named file elsewhere in the tree. The
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
- The NornicDB leg was not re-run at the final head (see Graph truth), and wall time is
  NOT_CHECKED.
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
