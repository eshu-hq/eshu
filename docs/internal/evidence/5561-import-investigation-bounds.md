# #5561 Import Investigation Bounds

## Scope

`POST /api/v0/code/imports/investigate` had one query-plan registration for a
repository and source-file request, while the API and MCP schemas expose 244
valid query and filter combinations. The retained failure was four consecutive
60-second timeouts. A 21-repository control returned in 5.7 ms or less because
it had no `IMPORTS` edges, so it was not useful performance evidence.

The finished path keeps one connected Cypher `MATCH` per graph read. Module
investigation anchors the exact module name first, resolves a bounded
repository-and-file membership set, then reads import or call candidates by
file path and rejects repository-path collisions before paging. Cycle
investigation reads one bounded ordered
`IMPORTS` edge set and reconstructs reciprocal Python cycles in Go. Internal
candidate reads request 25,001 rows and return HTTP 422 when the 25,000-row
ceiling is exceeded.

## Theory proof

The proof graph used NornicDB v1.1.11 and synthetic data only:

- 50 repositories
- 25,000 Go files with populated `IMPORTS` edges
- 500 reciprocal Python import edges in one repository, representing 250 known
  cycles
- shared source and target modules across all 50 repositories
- one `CALLS` pair per repository
- duplicate package edges in one repository

The table compares the old and candidate shapes on the same data. Row sets were
compared after stable ordering.

| Shape | Old | Candidate | Result |
| --- | ---: | ---: | --- |
| repository + source file | 0.030885 s | 0.008004 s | exact, 2 rows |
| source file across repositories | 6.949743 s | 0.152878 s | exact, 50 rows |
| target module across repositories | 6.997891 s | 0.391717 s | exact, 50 rows |
| source module across repositories | 7.050102 s | 0.593105 s across two bounded reads | exact, 100 rows |
| repository-scoped cross-module call | 30.2 s | 0.048838 s | exact, 1 row |
| Python cycle query | about 0.72 s and 0 cycles | 0.0346 s and 250 cycles | expected correctness delta |

The old cycle query was not an accuracy baseline: pinned NornicDB returned no
cycles for a graph with 250 known cycles. The candidate result was checked
against the seeded reciprocal edge set instead of claiming equality with the
wrong output.

An index on `File.relative_path` was measured and rejected. The indexed
source-file candidate took 0.205233 seconds versus 0.152878 seconds without the
index, so this change adds no graph schema or write cost.

The retained cold graph later exposed two rejected membership encodings. A
list-of-maps predicate returned no rows on NornicDB. A scalar-key candidate took
1.355841 seconds and about 1.99 seconds with membership; separate repository and
path lists took 2.474787 seconds end to end. Reversing the connected membership
pattern to anchor `Module.name` first reduced membership from 0.810174 seconds
to 0.010174 seconds. The complete source-module read then took 1.068615 seconds
and returned the same 101 candidate rows.

Package paging also reproduced the old duplicate-page defect. Paging raw import
edges repeated the same logical module on later pages. `RETURN DISTINCT` over
repository, module, and language removed the overlap before `SKIP` and `LIMIT`.
Source-module package reads apply the same logical ordering in Go after exact
repository-path filtering.

A 5,000-row membership microbenchmark measured the old linear lookup at
128,527,417 ns/op and the production indexed lookup at 522,208 ns/op. Both matched all
5,000 rows. Production uses the indexed repository-path key, avoiding quadratic
work at the 25,000-row ceiling.

## Finished production proof

The tagged test drives `CodeHandler.importDependencyRows` twice for each
representative shape. Both calls use the same data and exact production
builders. The checked-in interactive SLO is 1.5 seconds per call.

| Production shape | Cold | Immediate repeat | Returned rows |
| --- | ---: | ---: | ---: |
| repository + source file | 0.001672 s | 0.000212 s | 3 |
| target module | 0.634905 s | 0.000812 s | 50 |
| source module | 0.991016 s | 0.001884 s | 101 |
| package imports | 0.032970 s | 0.002562 s | 201 |
| file import cycles | 0.012614 s | 0.003439 s | 201 |
| cross-module calls | 0.007863 s | 0.000563 s | 1 |

The 201-row values are the requested 200-row page plus one truncation sentinel.
The separate cycle-edge proof read 500 edges, reconstructed the requested first
page of exact cycles, and completed in 0.027751 seconds. A second production
read using exact source file, target file, source module, and target module
filters returned the expected single cycle. A graph restart followed by the
cross-module test alone completed in 0.039012 seconds, so its cold result does
not depend on a preceding module query warming the graph.

## Query-plan coverage

The reachability test enumerates all 244 requests accepted by validation and
maps them to 140 distinct production Cypher texts. Cycle direction filters now
run after reciprocal reconstruction, so six previously distinct edge queries
correctly share one candidate shape. Those texts join the
hash-frozen safe production family. The hermetic Neo4j gate profiled 21 handler
entries, 22 legacy Cypher entries, and 454 safe variants: 497 shapes in total.
No plan used `AllNodesScan`, `CartesianProduct`, or an unbounded expansion.

Pinned NornicDB returned no plan object for `PROFILE`. The NornicDB result is
recorded as unavailable rather than treated as a pass; exact shared query text,
the live wall-time gate, and the hermetic Neo4j planner cover the two proof
surfaces separately.

## Commands

```text
cd go
go test ./internal/query ./internal/queryplan -count=1
go test ./internal/query -run '^$' -bench BenchmarkImportDependencyScopeLookup \
  -benchtime=1x -count=1
go test -tags live_import_cycle_proof ./internal/query \
  -run 'TestLive(FileImportCyclesBoundedEdgeScan|ImportDependencyRepresentativeShapes)' \
  -count=1 -v
cd ..
scripts/verify-query-plan-profile.sh
```

The focused query and query-plan suites passed. The scoped-cycle tagged
NornicDB rerun passed in 3.783 seconds, and the 497-shape hermetic profile gate
passed in 14.522 seconds.
