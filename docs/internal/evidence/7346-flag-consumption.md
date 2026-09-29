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
`IMPORTS` edges carrying the projector's flag properties, plus one legacy edge
written with none, and reads them through the production `imports.CycleRows` and
response shaper: a runtime cycle (all flags explicitly false), a cycle closed only
through a type-only edge, one closed only through a deferred edge, a legacy cycle
with one no-flag edge, an inferred cycle, and the Python relative-import fallback
`./xi_b`, which never equals a module name and so must close nothing.

| Backend | Result |
| --- | --- |
| NornicDB, pinned `ghcr.io/eshu-hq/nornicdb-amd64-cpu` v1.3.3 (secondary) | PASS. Cycles: `rt_a` runtime, `lg_a` flags_unknown, `in_a` ambiguous; no type-only, deferred or `./x` cycle. Coverage: considered 12, type-only excluded 1, deferred excluded 1, inferred 2, unknown 1 |
| Neo4j, digest-pinned `neo4j:2026-community` (primary) | NOT_CHECKED for this test. The container would not start on a host under load (JVM aborted or failed startup with a load average of 14 to 32 from other lanes' work). It ran earlier the same day on a quieter host. Re-run before merge |

The NornicDB run answers the question the design left open: a relationship
property that was never written comes back from the pinned NornicDB as a null
that the reader classifies as `flags_unknown`, not as false. The test seeds edges
in the writer's property shape and does not go through the canonical writer; a
writer-driven case should join it once #7345 has merged.

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
at 25,000 edges, an increase of roughly 20% to 34%. That laptop was under heavy
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
- **Neo4j leg pending** for the new live test (see Graph truth), and wall time is
  NOT_CHECKED.
- `codemodel` now holds 45 non-test files under its existing justified
  `//nolint:dirgate` marker, whose reason text is updated. A subpackage split is
  a package move and out of scope for this lane.
- The `hot-cypher` manifest hashes for `QP-CODE-IMPORT-CYCLES` are refreshed
  because the statement text changed on purpose (three added projections); it is
  not a relocation.
