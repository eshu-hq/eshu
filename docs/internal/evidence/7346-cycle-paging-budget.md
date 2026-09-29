# 7346 (part 1) — terminal page, walk bound, and enumeration state for file_import_cycles

Validation record for the first of two PRs that deliver issue #7346, split from
#6851. This part does not depend on the import flags and changes no graph write
and no Cypher. The flag-consuming half (a null flag is its own `unknown` state,
type-only and deferred edges excluded before the walk, a per-cycle label) lands
separately after #7345.

It closes three review findings from the merged #6851 lane:

1. A capped enumeration forced `truncated:true` on every page, including the
   terminal empty one, where `next_offset` echoed the request offset. A client
   paging until `truncated` was false looped on a fixed cursor.
2. The 1,000-cycle cap counts closed cycles only, so a cycle-free dense graph
   never tripped it while the walk stayed exponential in path length.
3. `imports.Rows` discarded the enumeration state with a comment, so a future
   caller could reintroduce a silently partial list.

## Design (arbiter-reviewed)

- `truncated` keeps meaning "the answer is partial": another page exists, or the
  enumeration stopped early. New `has_more` means only "another page exists".
  `next_offset` is a cursor only while `has_more` is true, and null on the last
  page. A capped run therefore stays `truncated:true` on its last page (the list
  is still partial) while the pager ends.
- Only hops inside a strongly connected component can lie on a cycle, so the walk
  is restricted to them (Tarjan). The set of cycles is unchanged; an acyclic
  graph, however dense, has nothing to walk.
- A deterministic step budget counts each examined hop and stops the walk with
  `coverage.cycle_enumeration_stop_reason` = `step_budget`. A time budget was
  rejected because the same request would answer differently on a slower host.
- `BuildFileImportCycleRows`, `imports.CycleRows` and `imports.Rows` return a
  `CycleEnumeration` struct (truncated, stop reason, step budget, steps examined)
  instead of a bare bool, and the handler shapes every query type through one
  path.

## Failing first

Each behavior had a test that failed before the change:

| Test | Before |
| --- | --- |
| `TestCycleResponsePagerTerminatesOnACappedRun` | `next_offset = 1000 does not advance past offset 1000` (the fixed-cursor loop) |
| `TestCycleResponsePagingHasATerminalPageOnACappedRun` | `has_more` absent on every page; terminal page still returned a cursor |
| `TestEnumerateImportCyclesDoesNoWalkOnAnAcyclicGraph` | `StepsExamined = 1514544` to find zero cycles |
| `TestEnumerateImportCyclesStopsAtTheStepBudget` | stop reason `cycle_cap` after 5,988 steps under a budget of 200 (not enforced) |

`TestEnumerateImportCyclesMatchesABruteForceOracle` compares the cycle set with
an independent brute-force listing on 60 seeded random graphs of 3 to 7 files and
passes before and after the component filter, so the filter changes no answer.
The oracle shares no code with the walk or the filter.

## Performance Evidence:

Stage: the in-process cycle walk over one bounded edge fetch (25,000-row scan
limit). No graph read or write changes here.

Step counts at the maximum `max_cycle_length` of 8. Every graph below is a
model; no real corpus edge list was walked (none is available on this machine,
and probing a production cluster is out of bounds for this lane). The corpus
figures used to size the models (4,522 edges over 626 files, of which 172
resolved in-repo) are as reported on #6851 and were not re-verified here:

| Shape | Steps examined | Stop |
| --- | --- | --- |
| Uniform random placement of the largest measured Python corpus shape (626 files, 172 in-repo edges), worst of 200 placements | 9 | none |
| Package-clustered model: 626 files, packages of 16, 2 in-package imports per file | 8,101 | none |
| Package-clustered model: 626 files, packages of 16, 4 in-package imports per file | 16,791 | cycle_cap |
| Package-clustered model: 626 files, packages of 40, 5 in-package imports per file | 38,268 | cycle_cap |
| Layered DAG, 12 layers of 4 files (about 16.7 million paths), before the component filter | 1,514,544 | none |
| Same DAG after the component filter | 0 | none |
| Uniform random, 626 files, all 4,522 imports resolved (stress) | 250,000 | step_budget |
| Uniform random, 626 files, 25,000 edges (scan-limit stress) | 250,000 | step_budget |

The two stress rows stop at the budget by construction. When the budget was still
1,000,000 during derivation those two shapes were observed once to need 371,366
and 604,605 steps before the cycle cap; that output is not asserted by any test
and is recorded only as where the value came from.

Budget value: 250,000 steps, set by the wall-time ceiling and not by a measured
coverage margin. `BenchmarkEnumerateImportCyclesDenseComponent` measured about
530 ns per examined hop (three runs, 531 to 549 ns) on the development laptop,
putting 250,000 steps near 130 ms against a 250 ms ceiling. The provisional value
of 1,000,000 was rejected on this evidence: it would exceed the ceiling at that
cost per hop.

Coverage is modelled, not established. The uniform placement needs at most 9
steps, but it has almost no strongly connected component and says little about
real graphs. The clustered models are the more relevant ones: the budget sits
6.5 times above the densest of them, and their cycle cap (1,000 cycles) bites
before the budget does. No margin over a real corpus is claimed.

Timing is NOT_CHECKED on a valid test bed. The ns/step figure came from a shared,
contended laptop (load average near 25 during the neighbouring benchmark runs) and
is a smoke number, not accepted timing. The dedicated remote was unreachable when
this was written. The wall-time ceiling must be confirmed on the remote before the
budget is relied on for a latency claim. No speedup is claimed.

## Benchmark Evidence:

`BenchmarkEnumerateImportCyclesDenseComponent` (complete digraph of 9 files, max
length 8, `go test -bench ... -benchmem -count=3`): 7,981 steps per op, about
4.24 to 4.38 ms per op, about 6,347 allocs per op. These are local smoke figures
for choosing the budget, not a before/after claim: the walk itself is unchanged
for a strongly connected graph. The change removes work only for the acyclic and
cross-component parts of a graph.

## Observability Evidence:

The `query.import_dependency_investigation` span gains `eshu.import_dependencies.has_more`
and, for `file_import_cycles`, `eshu.import_dependencies.cycle_stop_reason`
(`none`, `cycle_cap`, or `step_budget`, read from the response coverage) and
`eshu.import_dependencies.cycle_steps_examined`. The steps examined let an operator
see a request approach its budget before it becomes a stop; a handler test drives
the real route and asserts all three, and fails if the steps attribute is removed. The
response carries `coverage.cycle_enumeration_stop_reason` and
`coverage.cycle_enumeration_step_budget`, so a partial list says why it is
partial and an operator can see a step-budget stop in the trace without reading
the payload. `docs/public/reference/http-api/code.md`, the OpenAPI operation, and
the MCP tool description are updated to the same contract.

## Console consumer

The console's code-graph page (`apps/console/src/api/codeImports.ts`,
`pages/CodeGraphPageSupport.tsx`) loads only the first page and printed "More
import cycles are available" whenever `truncated` was true. With `truncated`
staying true on a capped run's last page, that text would offer more cycles
where no page exists. The client now reads `has_more` (falling back to the cursor
for an API that predates it), says "More import cycles are available at offset
N" only while `has_more` is true, and otherwise says the list is partial because
the enumeration stopped at its bound. A component test (`says a stopped
enumeration is partial without offering a page that does not exist`) failed on the
old text first. From the repository root, as CI runs it: `npm run console:test`
passes 279 files and 1,790 tests, `npm run console:typecheck` exits 0, and
Prettier's pinned version is clean on the changed files.

## Contract and gates

- `has_more` is additive. `truncated` keeps its meaning for every existing reader
  (it is now true whenever `has_more` is true, and also on a capped run's last
  page); a client that pages on `next_offset` being null now terminates.
- The query-plan manifest pin for `CycleRows` (`source_sha256`) is refreshed
  because its return type changed. The Cypher statement is untouched, so the
  hot-cypher `QP-CODE-IMPORT-CYCLES` row is unchanged.
- `codemodel` holds 43 non-test files, over the 40-file cap, under its existing
  justified `//nolint:dirgate` marker on `doc.go`. Two new files are added
  (`..._components.go`, `..._enumeration.go`) because `..._cycles.go` (475 lines)
  and `..._queries.go` (479 lines) cannot absorb the change under the 500-line
  cap. The marker's stated reason is updated to cover them. Moving the family into
  a subpackage is a package move and is out of scope for this lane.

## Limits stated plainly

- The budget is derived from a synthetic worst case plus a measured real shape;
  its wall-time ceiling is unconfirmed off this laptop.
- This part does not consume import flags. Until the flag half lands, cycles are
  still computed over all stored `IMPORTS` edges with no type-only or deferred
  exclusion and no inferred labelling.
