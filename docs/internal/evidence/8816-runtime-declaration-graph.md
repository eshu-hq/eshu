# Runtime declarations in the graph (#8816)

This note records the proof behind the `runtime_declaration_projection`
reducer domain: the schema, the write and retract Cypher, and the retract
scope. The domain projects `runtime_declaration.version` facts (#8815) as
`Repository-[:DECLARES_RUNTIME]->RuntimeDeclaration`.

## Graph model

- One `RuntimeDeclaration` node per declaration, keyed on
  `(repo_id, relative_path, line_number, runtime, order_index)`. That is the
  graph key the #8815 arbiter ruling fixed: a pin-file line with several
  tokens (`nodejs 20.11.1 18.19.0`) holds one node per token.
- `runtime_declaration_identity`, a composite uniqueness constraint on those
  five properties, backs the MERGE on Neo4j. NornicDB drops composite
  constraints, so `nornicdb_runtime_declaration_repo_id_lookup` keeps its MERGE
  off a label scan.
- `runtime_declaration_runtime_major`, a range index on
  `(runtime, version_major)`, backs the version-threshold read the query tool
  (#8817) will issue.
- Every other property is `SET`. A nullable version part stays null: an alias
  such as `lts/iron` never gets a `version_major`.

## Retract scope

- A full generation retracts every declaration of each repository it covers,
  then writes its facts. The repository fact names the repository, so a
  generation whose last pin file was deleted still retracts.
- A delta generation carries facts only for the pin files it changed
  (`EmitRuntimeDeclarationFactsForCandidates`), so it retracts only the
  declarations whose `relative_path` is in the repository fact's
  `delta_relative_paths` or `delta_deleted_relative_paths`. A whole-repository
  retract there would erase every unchanged pin file's declarations.
- Each path list is bound to its own repository (`UNWIND $scopes`), because a
  pin-file path such as `.nvmrc` is the same in every repository.
- The retract is skipped only on the first attempt of a scope's first
  generation. A retry always retracts.

## Theory proof (before the change)

Performance Evidence: on the pinned Neo4j image
(`neo4j:2026-community@sha256:eabfbb04...`, server 2026.08.1, one throwaway
container), 10,000 `RuntimeDeclaration` nodes across 1,000 repositories with
`runtime_declaration_identity` and `runtime_declaration_runtime_major`
applied. `node` is 4 of every 12 runtimes and 1 in 10 declarations is an alias
with no `version_major`; 2,003 declarations match `node` below 20.

| Statement | Plan | DB hits | Time (warm) |
| --- | --- | ---: | --- |
| `runtime = 'node' AND version_major < 20`, `ORDER BY` the five-part key, `LIMIT 50` | `NodeIndexSeek` on `runtime_declaration_runtime_major`, 2,003 index rows, `Top` 50 | 10,066 | 2 ms plan + 4 ms rows (cold 107 + 21 ms) |
| Same query with a keyset cursor (`version_major > 15 OR (= 15 AND key > cursor)`) | two `NodeIndexSeek`s on the same index, `Top` 50 | 8,062 | 1 + 3 ms |
| Bounded range `version_major >= 15 AND < 20` | `NodeIndexSeek`, 1,001 rows | 5,056 | 1 + 3 ms |
| 500-row `UNWIND` upsert (production shape) | `MergeUniqueNode` (unique-index seek), `Merge` for the edge | 6,518 first run, 13,280 replay | 106 ms first run (cold), 24 ms replay |
| Whole-repository retract, one repository | `NodeUniqueIndexSeek(Locking)` on `Repository.id`, `Expand(All)` | 102 | under 1 ms |
| Path-scoped retract, one repository and one path | same plan | 86 | under 1 ms |

The upsert replay left the counts unchanged (10,428 nodes and 10,428 edges
before and after), and the path-scoped retract kept the other file's 12 nodes
of that repository. The read cost grows with the number of matching
declarations, not with the label, because the seek is on the composite index.

## Repository-scale check

Run after the implementation, at review request, on the same image. The same shapes at 50,000 declarations across 5,000 repositories (10 per
repository) on the same image stay index-driven: no plan holds
`NodeByLabelScan` or `AllNodesScan`. The threshold query reads 10,002 matching
index rows (50,061 db hits, 1 + 29 ms warm); the 500-row production upsert
plans `MergeUniqueNode` and `Expand(Into)` (19,408 db hits, then 14,416 on
replay); the whole-repository retract seeks `Repository.id` (52 db hits for one
repository, 2,620 for 50); the delta retract over 50 repositories with 2 paths
each takes 2,224 db hits and keeps each repository's other files. The full
table is in `docs/public/reference/cypher-runtime-declarations.md`.

## Proof of the change

- Unit tests for the writer, the handler, the retract scope and the
  idempotency replay case; the handler test runs under `-race`.
- `go/internal/storage/cypher/edge/writer/runtime_declaration_live_test.go`
  drives the production handler over the production writer on the pinned
  Neo4j: idempotent replay, an in-place edit, a full-generation retract, a
  delta delete that keeps the unchanged file, and 8 repositories projecting at
  once with the same pin-file paths. Local `-race` runs took 1.17 to 1.65 s.
  `runtime_declaration_retract_live_test.go` adds the retract edge cases: a
  delta that shrinks `.tool-versions` from 4 declarations to 1 (no stale
  higher line or token survives), a rename (old path deleted, new path
  written), a full generation after the last pin file is deleted (0 owned
  nodes and edges; a node from another evidence source survives), and one
  path retract over two repositories with crossed path lists.
- Eight mutants fail the tests (`go test -overlay`): a delta treated as a
  full generation, a path retract without the repository anchor, a
  zero-filled `version_major`, keeping blank-runtime facts (the Ifá guard), a
  full retract without the evidence-source filter, a scope that ignores
  deleted paths, a skipped delta retract, and full-generation repositories
  taken only from rows instead of the repository fact.
- The replay cost scenario
  (`go/internal/replay/costcounting/runtime_declaration_cost_test.go`) pins
  one write batch per intent; writing one row per call exceeds the budget.

## Ifá coverage scope

The family is a direct materialization (`WriteRuntimeDeclarations`), so it
belongs in the direct half of the Ifá ledger. This change lands the offline
half: the cataloged Odù `odu:ifa-runtime-declaration-family`, the
`resolveRuntimeDeclarationMaterializedEdges` vacuity guard, the hand-derived
four-edge fixture, and the registry entry (registered from
`runtime_declaration_family.go` because `single_type_families.go` is past the
500-line cap). `go test ./internal/ifa ./internal/ifa/materializededges
./cmd/ifa` and `./internal/reducer/...` pass, and keeping blank-runtime facts
fails the guard with an extra edge.

The live half stays waived on #6228 for both proof gates, `ifa-determinism`
and `ifa-fault-injection`. Those matrices are Docker live gates on fixed ports,
so they cannot run locally under the lane rules, and CI must not be their first
run. The live enrollment follows the code_taint_evidence live leaf (fee10c8346):
a cassette, a registry row, drive and assert callbacks, the fault trio cells,
and an `assert-edges` endpoint rule for the keyless `RuntimeDeclaration` label.

No-Regression Evidence: the change adds a domain and touches no existing
write path. The only shared effect is one more `shared_followup` fact per
repository generation, which moves the content-version pin from 342 to 343
facts and the content version to `git-collector.v14`.

Observability Evidence: the domain runs as a standard reducer execution
(`eshu_dp_reducer_executions_total`, `eshu_dp_reducer_run_duration_seconds`),
quarantines a malformed fact into `eshu_dp_reducer_input_invalid_facts_total`,
and its statements run through `cypher.InstrumentedExecutor`
(`eshu_dp_neo4j_query_duration_seconds`, `eshu_dp_neo4j_batch_size`,
`eshu_dp_neo4j_batches_executed_total`) with phase `runtime_declaration`. The
INFO log `runtime declaration projection completed` carries the declaration
count, the retract mode counts, whether the retract was skipped, and the
quarantine count.
