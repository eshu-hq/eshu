# 10824: repository dependency count reads the active generation

`repositoryDependencyCount` (`go/internal/query/repository_read_model_summary.go`)
counted `resolved_relationships` rows of every generation. The table is
append-only, so rows of a superseded generation and a pending generation stayed
countable, and so would rows of a generation whose `relationship_generations`
row the retired-repository purge deletes. No code deletes those rows in the
base; #10820 adds the purge, and until it lands a retired repository's
generation stays active and still counts. The statement now joins
`relationship_generations` on `g.status = 'active'`, the predicate the
repository relationship read model and the deployment-evidence read model
already use.

## Behavior proof

`TestRepositoryDependencyCountActiveGenerationLive` (disposable PostgreSQL,
bootstrapped schema). Before the change it failed
(`repo-a dependency count = 6, want 3`); after it passes:

| Repository | Seeded rows | Count |
| --- | --- | --- |
| repo-a | 5 targets in a superseded generation, 3 in the active one (2 overlap), one non-dependency type in the active one | 3 |
| repo-b | rows only in a `pending` generation | 0 |
| repo-c | rows whose generation row was deleted (the state #10820 will create) | 0 |
| repo-d | one active generation, a duplicate target across two types | 2 |
| repo-x | active and superseded generations under two scopes, the two active ones sharing targets | 4 |

`TestRepositoryDependencyCountAcrossScopesLive` (repo-x) requires the distinct
union across both scopes' active generations. It also runs a mutant of the
shipped SQL restricted to one scope (`AND g.scope = 'scope-x1'`), which reads 3,
and fails if the mutant ever agrees with the shipped query. Known limit: a
retired repository whose own generation is deleted still counts rows it holds
under another scope's active generation, because the join is on generation
status, not on the repository's scope. The planned `source_repo_id` cleanup
(#10822) owns that.

## Plan proof

`TestRepositoryDependencyCountPlanLive` explains the shipped statement over
210,000 populated rows (4,000 repositories x 5 generations x 10 rows, plus a hub
repository with 2,000 active rows, 2,000 superseded rows, and 6,000 more
superseded rows), after `VACUUM ANALYZE`. The test fails if the plan
sequentially scans `resolved_relationships`. Local PostgreSQL 18.6, shared
buffers warm; timings are machine-bound.

| Repository | Rows the repo predicate reaches | Plan on `resolved_relationships` | Buffers | Execution |
| --- | --- | --- | --- | --- |
| repo-1 | 50 | Bitmap Index Scan on `resolved_relationships_source_repo_idx`, then a `relationship_generations_pkey` probe per row | 154 | 0.127 ms |
| repo-hub | 10,000 | Bitmap Index Scan on `resolved_relationships_source_repo_idx`, hash join to the active generations | 375 | 5.985 ms |

The pre-change statement used the same index with no generation join. On this
data it read 4 buffers for repo-1 and 178 for repo-hub; the reviewer measured 9
and 181 on their own copy. The join adds one primary-key probe per candidate row
for a small repository (about 3 buffer hits each) and, for a hub, a hash join
against `relationship_generations`; it never adds a scan of
`resolved_relationships`.

Generation-side cost: the hub plan is a Seq Scan on `relationship_generations`
that scans all generation rows (16,001 of the 20,002 removed by the status
filter); the hash holds only the active ones. Nothing deletes
`relationship_generations` rows today, so that scan grows with generation
history, not with scope count. The reviewer measured the hub plan turning into a
Merge Join over the generation primary key, 422,128 buffers and 79.7 ms (repo-1
0.25 ms), after adding 400,000 superseded generation rows, and a throwaway
partial index on `relationship_generations (generation_id) WHERE status =
'active'` bringing it to a Hash Join with an index-only scan, about 7 ms and 248
buffers. The existing relationship read model's outgoing arm costs the same on
the same data (79.6 ms, 422,000 buffers), so this change makes the count pay
what the other readers already pay. No index is added here: it is a schema
change shared by all readers, tracked in #10825.

No-Regression Evidence: `go test ./internal/query -run TestRepositoryDependencyCountPlanLive`
against disposable PostgreSQL keeps the dependency count index-led on
`resolved_relationships_source_repo_idx` (no sequential scan of
`resolved_relationships`) with the active-generation join added.

No-Observability-Change: the read keeps its existing caller spans and metrics;
only the SQL text changed.
