# CALLS degree floor: raising `nonHotCorpusMaxCALLSDegree` (#6649)

`go/internal/queryplan/source_coverage.go`'s `nonHotCorpusMaxCALLSDegree`
floors `max_degree` for every `degree_bounded`/`depth_bounded` non-hot
disposition in `testdata/query-source-coverage.yaml`. The value was 8,
measured over the 31 synthetic B-7 staged corpus fixtures
(`scripts/lib/golden-corpus-fixtures.sh`) rather than real code. This record
raises the floor to 1125, the maximum CALLS degree measured over both
directions on a real, cross-language, 804-repository corpus, and records the
method, the distribution, and the PROFILE evidence that the covered reads
stay cheap at that degree.

## Method

- Host: ops-qa. Backend: Neo4j Community 2026.08.1
  (`backend_version: 2026.08.1-community`). Commit: `5583d45`. Measured
  2026-09-26.
- Corpus: 804 repositories, 540k `Function` nodes, 356,547 `CALLS` edges.
- The `CALLS` writer (`go/internal/storage/cypher/canonical_code_call_edges.go`
  and `canonical.go:128`) uses `MERGE (source)-[rel:CALLS]->(target)`, which
  allows at most one `CALLS` edge per ordered pair. A plain relationship
  count per node therefore already equals the distinct-callee count
  (out-degree) or distinct-caller count (in-degree) with no extra `DISTINCT`
  required.
- Both directions were measured because one floor constant covers both
  `degree_bounded` read directions (`nonHotCorpusMaxCALLSDegree`'s doc
  comment); the floor is the larger of the two so it does not certify a
  fan-out below the direction it actually serves.
- Per-language breakdown restricts to `:Function` nodes for the same
  MERGE-dedup reason.
- Repo size does not predict max degree: the top out-degree (521) and a
  near-top in-degree (466) both trace to a 36-61-file repository, while
  several other high-degree anchors trace to a 3,886-file repository. High
  fan-out/fan-in is a code-shape property (generic dispatch, builder-style
  setter chains), not a corpus-size artifact.

## Distributions (both directions)

| direction | nodes | mean | p50 | p90 | p99 | p99.9 | max |
|---|---|---|---|---|---|---|---|
| out-degree | 128,964 | 2.71 | 1 | 5 | 21 | 53 | **521** (ledger:6649-calls-out-degree-max) |
| in-degree | 163,032 | 2.14 | 1 | 4 | 16 | 54 | **1125** (ledger:6649-calls-in-degree-max) |

The prior floor of 8 sits below p90 for every language in the per-language
breakdown below and roughly two orders of magnitude below the real
cross-language maximum in both directions.

## Per-language maxima (`:Function` nodes)

| language | out-degree max | in-degree max |
|---|---|---|
| javascript | **521** | 672 |
| java | 194 | **1125** |
| php | 60 | **1050** |
| tsx | 42 | 128 |
| typescript | 53 | 321 |
| python | 74 | 133 |
| groovy | 21 | 78 |
| go (349-function sample) | 13 | 32 |

JavaScript dominates the out-degree tail; Java and PHP dominate the
in-degree tail. The go sample here (349 functions in this corpus) is not
comparable to #6556's go/ast-based sizing of this repo's own module (73,893
functions, p99 19, max 95): different methodology (parsed AST fan-out vs.
graph CALLS degree) and different population (one Go module vs. a
349-function cross-repo sample), so neither figure supersedes the other —
both simply confirm Go's fan-out is far below the JavaScript/Java/PHP tail
that drives this floor.

## PROFILE evidence: the covered reads stay cheap at the new floor

All cells below ran against the pinned Neo4j binary (2026.08.1-community) on
the ops-qa corpus, per the `cypher-query-rigor` proof requirement (test
against Neo4j, not NornicDB, for the accuracy/performance contract).

| Reader | Direction / anchor | Plan | Time | DB hits | Rows |
|---|---|---|---|---|---|
| `callChainCandidateOneHopRows` (`codequery/callers.go`) | outgoing, out-degree-521 anchor | `Union` of `NodeUniqueIndexSeek`s (one per label) | 27 ms | 2,616 | 521 |
| `transitiveRelationshipsGraphRow` (`codequery/relationship_handlers.go`), `CALLS*1..10` | incoming, in-degree-1125 anchor | `VarLengthExpand(Pruning,BFS,All)` | 23-44 ms | 26,831 | 2,203 (depth 10) |
| `transitiveRelationshipsGraphRow`, `CALLS*1..10` | outgoing, out-degree-521 anchor | `VarLengthExpand(Pruning,BFS,All)` | 3-34 ms | 4,015 | (depth 10) |

The depth-10 incoming cell (worst-case wall time 44 ms) is recorded as
ledger:6649-transitive-depth10-profile. Both readers reach a per-label
unique index seek (`NodeUniqueIndexSeek`) or a pruning BFS
(`VarLengthExpand(Pruning,BFS,All)`), so cost tracks the reachable set the
planner actually walks, not `degree^depth`. This is the evidence behind
"raise the number, no code change needed" for
`nornicDBCallChainOneHopRows`, `callChainCandidateOneHopRows`, and
`transitiveRelationshipsGraphRow`.

## NornicDB-only note: `nornicDBTransitiveOneHopRows` is unmeasured here

`nornicDBTransitiveOneHopRows` (`go/internal/query/codequery/transitive_walk.go`)
only runs when the backend is NornicDB; the backend dispatch lives in
`relationship_handlers.go:338`. Its Cypher anchors `source`/`target` with a
bare property `WHERE` and no label, so on Neo4j the same statement plans a
full `CALLS` relationship-type scan (~400 ms, ~700k db hits) regardless of
the anchor's actual degree — a materially different, not-degree-bounded
shape. Per the repo's Neo4j-proof rule (test against Neo4j, not NornicDB),
this symbol's own NornicDB-branch behavior was not re-measured in this pass;
raising `max_degree` for its registry row keeps the same disposition class
this PR found it in, but does not certify that its NornicDB-branch cost is
actually bounded by degree. That follow-up is tracked in #7300: either anchor
`source`/`target` on the same labeled pattern the call-chain readers already
use, or register the symbol hot and profile its NornicDB-branch cost
directly, before treating its `degree_bounded` classification as fully
proven end to end.

## Registry changes

`go/internal/queryplan/testdata/query-source-coverage.yaml` rows moved from
`max_degree: 8` to `max_degree: 1125` in lockstep with the constant:

- `(*CodeHandler).nornicDBCallChainOneHopRows`
- `(*CodeHandler).callChainCandidateOneHopRows`
- `(*CodeHandler).transitiveRelationshipsGraphRow` (`depth_bounded`, `max_depth` unchanged at 10)
- `(*CodeHandler).nornicDBTransitiveOneHopRows`

One derived-but-unenforced number also referenced the old floor:
`(*CodeHandler).runWrapperGraphRows`'s `keyed_support`/`bounded_key_batch`
row carried `max_results: 400` with the comment `# 400 = 50 keys x corpus
CALLS degree 8`. This bound is not validator-enforced against
`nonHotCorpusMaxCALLSDegree` (the `keyed_support` class only requires
`max_results > 0`), but it is a documentation claim derived from the old
floor, so it was recomputed honestly: `50 x 1125 = 56,250`. This does not
change the row's class or its validation outcome, only the audit bound's
stated value. `docs/internal/evidence/6834-code-divergence-theory.md` cited
the same `50 keys x corpus CALLS degree 8, audit max_results 400` figure and
was updated to the new numbers in the same change.

## Seeded-violation RED/GREEN

1. Bumped only `nonHotCorpusMaxCALLSDegree` (8 -> 1125) in
   `source_coverage.go`, before touching the YAML. `go test
   ./internal/queryplan/... -count=1` failed in
   `TestHotCypherManifestCoversEveryProductionQueryCall`, naming exactly the
   four registry rows still carrying `max_degree: 8`:
   `nornicDBCallChainOneHopRows`, `callChainCandidateOneHopRows`,
   `transitiveRelationshipsGraphRow`, `nornicDBTransitiveOneHopRows`.
2. Moved those four rows to `max_degree: 1125`
   (`transitiveRelationshipsGraphRow` keeping `max_depth: 10`). Reran the
   same command: green.
3. `source_coverage_degree_test.go`'s existing
   `TestValidateSourceCoverageRejectsIncompleteDegreeBoundedEvidence` table
   test uses `nonHotCorpusMaxCALLSDegree - 1` (not a hardcoded literal), so
   it automatically re-targets the new floor (1124) and still fails with the
   same `requires max_degree` message at the raised floor -- confirmed by a
   direct run of that test after the constant change, before and after the
   YAML edit.

Full commands and output are in the PR/commit history for this branch
(`perf/6649-calls-degree-floor`).

No-Regression Evidence: no Cypher text changed. This PR edits a Go constant,
its doc comment, four `max_degree` values and one derived `max_results`
comment/value in a static YAML manifest, and prose in two Markdown files.
`go test ./internal/queryplan/...` and
`go test ./internal/query -run 'Queryplan|QueryPlan|SourceCoverage'` are the
relevant regression proof; both pass at the raised floor (see the
seeded-violation section above for the exact RED/GREEN transition).

No-Observability-Change: this PR adds no API route, graph query, graph
write, metric, span, runtime knob, queue work, or provider call. It only
raises a static validator floor and its registry data.
