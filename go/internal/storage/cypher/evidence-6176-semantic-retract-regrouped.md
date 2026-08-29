# Evidence: #6176 semantic retract returns to the grouped transaction

## What changed

`SemanticEntityWriter.WithSequentialRetract()` is gone, along with the
`sequentialRetract` field, the branch in `WriteSemanticEntities` that held
retract statements out of the grouped list, the separate autocommit dispatch
loop, and the reducer wiring that turned the mode on for NornicDB
(`go/cmd/reducer/neo4j_wiring.go`).

Retract and upsert now share one transaction on any executor that implements
`GroupExecutor`. Executors that expose only `Execute` — NornicDB's default
`ExecuteOnlyExecutor`, and test stubs — keep the per-statement fallback they
already had, in the same order.

## Why it is safe now

The split existed for one defect: grouped `DETACH DELETE` statements
under-apply on NornicDB v1.1.11, leaving semantic nodes such as `Variable` in
the graph with nothing reporting a failure (#4367,
`evidence-4367-semantic-retract-dispatch.md`). #5323 measured the grouped
retract correct on 1.2.1 and 1.2.2 against a 1.1.9 control that still fails.
`docs/internal/evidence/6176-grouped-semantic-retract-version-floor.md` added
the v1.1.11 measurement the earlier work skipped and set the floor at 1.2.1.
The repository owner has since settled the compatibility question: v1.2.3 —
the build the deployment actually runs — is the supported backend, and it is
above the floor.

Two facts narrow the blast radius:

- With `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` unset (the default;
  `neo4j_wiring.go` returns false) the NornicDB semantic executor is
  `ExecuteOnlyExecutor`, which hides `GroupExecutor`, so this path already
  dispatched one statement at a time and the removal changes nothing there.
  The behaviour change lands on the grouped-writes opt-in, whose
  `TimeoutExecutor` does implement `ExecuteGroup`.
- The semantic retract Cypher is the `MATCH (n:Label) WHERE n.path IN
  $file_paths ... DETACH DELETE n` shape, not the `UNWIND`-batched
  `MATCH ... DELETE` that no-ops inside a NornicDB managed transaction. That
  second shape is the separate #4902 trap recorded in
  `docs/public/reference/nornicdb-pitfalls.md`; this retract was never in it.

## No-Regression Evidence:

- **Backend / version:** `timothyswt/nornicdb-cpu-bge:v1.2.3`, which
  **self-reports `1.2.2`** in its own startup logs (`"service":"nornicdb"`,
  `"version":"1.2.2"`) — the tag name is not the version, so the reported value
  is what this record binds to. Started with
  `NORNICDB_ASYNC_WRITES_ENABLED=false`, embeddings and search off, Bolt on host
  port 17802, `ESHU_GRAPH_BACKEND=nornicdb`, database `nornic`.
- **Input shape:** one repo, two `Variable` entities in two files; gen1 upserts
  both, gen2 delta-retracts one file path. The committed live regression
  `TestReducerSemanticVariableRetractGraphTruth`
  (`go/internal/replay/offlinetier/delta_tier_reducer_semantic_variable_retract_live_test.go`).
- **Before (sequential retract, the code as it stood):** `-count=1`, exit 0,
  1 PASS. The harness works against this container.
- **After (grouped retract, this change):** `-count=20`, exit 0, **20 PASS,
  0 FAIL, 0 SKIP**. The gen2 in-scope `Variable` is gone (`count = 0`), the
  out-of-scope `Variable` survives, and both `File` nodes survive.
- **The grouped route is really exercised:** the test now asserts
  `exec.(cypher.GroupExecutor)` before writing and fails if it does not hold.
  Without that assertion `WriteSemanticEntities` would silently take its
  per-statement fallback and a green run would say nothing about grouped
  dispatch.
- **Mutation proof — the live assertion bites.** Dropping the delta retract
  emission (`subs=1`, `go vet` exit 0 on the mutant) turned the live run RED:
  exit 1, 3 FAIL of 3 at `-count=3`, every one reporting
  `gen2: in-scope Variable retracted: count = 1, want 0`. Restored, exit 0.
- **Mutation proof — the inverted unit assertions bite.** Re-introducing the
  sequential split (`subs=1`, `go vet` exit 0) turned 5 tests RED across both
  lanes: the three writer-level dispatch tests plus both reducer factory tests.
  Restored, exit 0.
- **Throughput:** no measurable change to defend. The retract emits at most one
  bounded statement per semantic label (~11), once per generation per file-path
  set. Moving those ~11 statements from separate autocommit round-trips into the
  transaction the upserts already open removes round-trips rather than adding
  them; the high-cardinality per-label `UNWIND` upsert path is untouched.
- **Concurrency:** this narrows a window rather than widening one. Before, a
  concurrent reader could observe the retracted nodes as absent between the
  retract commit and the upsert commit, and convergence depended on the
  reducer's idempotent retry. Now retract and upsert commit or roll back
  together, so a failed write leaves the previous generation intact instead of a
  partially-retracted graph. The writer remains idempotent and the reducer's
  requeue is unchanged.
- **Concurrency, the part an earlier draft of this bullet missed:** the
  reducer's requeue was never the retry that mattered here. `ExecuteGroup`
  absorbs a commit-time UNIQUE conflict **in place**, and it only did so for a
  group whose every statement contained `MERGE`. Folding the retract in made
  the group mixed and silently dropped that in-place retry, so a concurrent
  canonical MERGE on the same semantic uid would have dead-lettered instead of
  converging. That is fixed in the same PR — see "Keeping the group retryable"
  below — not left to the requeue.

## Which statements the replay-safety guard accepts

`allStatementsAreReplaySafe` decides whether a commit-time UNIQUE conflict on a
grouped write is retried in place or left terminal. It sits on the shared
`RetryingExecutor.ExecuteGroup` path, so it classifies **every**
`OperationCanonicalRetract` emitter in the repository, not only the semantic
writer this record measures. `isIdempotentRetractStatement` therefore has to be
true of the whole population, not just of the one writer that motivated it.

It was not. The guard accepted any retract that opened on `MATCH` and wrote
only through `DELETE`/`REMOVE`, while the doc comment claimed a replay "removes
the same parameter-bound set". A live statement broke that claim:
`canonicalNodeRetractParametersCypher` is a non-drain retract that reaches the
grouped path with `p.generation_id <> $generation_id` — a predicate matching
every generation EXCEPT this writer's. A Parameter committed by a concurrent
writer on a different generation between the failed attempt and the replay is
newly in range, so the replayed `DETACH DELETE` removes a node the first
attempt never saw.

The guard now also requires the predicate to be bounded BY the bound parameters
rather than by their complement. Enumerating the statements this reclassifies,
by building the real writers' groups and classifying each under both the old
and new rule:

| group | before | after |
| --- | --- | --- |
| semantic full-repo retract + upsert | retryable | **retryable** |
| semantic delta retract + upsert | retryable | **retryable** |
| canonical node, full refresh (files + dirs + entities) | terminal | terminal |
| canonical node, files but no entities | terminal | terminal |
| canonical node, delta retract | terminal | terminal |
| canonical node, repo with no files/dirs/entities | retryable | **terminal** |

The shapes newly refused are the negation/complement family:
`canonicalNodeRetract{Files,RemovedFiles,Directories,Parameters}Cypher`, the
per-label entity retracts, `canonicalNodeRepositoryPathCleanupCypher`, the
tfstate generation retracts, `retractStaleCodeTaintEvidenceByUIDsCypher` and
its interproc sibling, and the Kubernetes namespace `coalesce(...) <> $gen`
retract — thirteen Cypher shapes in all.

Only one measured group actually changes classification, and the cost is
bounded three ways. Every writer in that list except the canonical node writer
dispatches its retract standalone through `Execute` (`dispatchRetract`), so the
group classifier never sees it. The canonical node writer's group does carry
them, but on any materialization with files or entities that group is already
terminal because the same group carries `UNWIND ... MATCH ... DELETE`
containment-refresh statements. And the one group that does change — a
repo-scoped full refresh with zero files, directories and entities — was
terminal on `main` too, where `allStatementsAreMerge` refused every group
containing a retract. This narrows a widening introduced by this PR; it cannot
regress against the base.

The semantic writer's own retract, the shape this PR measured and the reason
the widening exists, uses positive `IN $params` membership and stays retryable.

Accuracy is ranked above performance here deliberately. A refused group loses a
retry it might not have needed and takes dead-letter redrive, which costs time
and is visible. Accepting a predicate whose match set grows under concurrency
costs graph truth silently.

## No-Observability-Change:

No instrument, span, log, or status surface is added, removed, or renamed. The
same statements carry the same `OperationCanonicalRetract` / statement-metadata
summaries and the same `WrapRetryableNeo4jError` retry classification; only the
transaction they are dispatched in changed. One consequence worth an operator's
attention: a retract failure now surfaces as a failure of the whole semantic
write group rather than as a standalone retract error, so the error text reads
`write semantic entities:` where it previously read `retract semantic entities:`.

## Keeping the group retryable

`RetryingExecutor.ExecuteGroup` retries a commit-time UNIQUE conflict or a
relationship-snapshot conflict only when the group is safe to replay, and the
old test for that was "every statement contains MERGE". The semantic retract
contains none, so the group this change creates would have failed the gate: the
race that `ExecuteGroup` exists to absorb — a concurrent canonical writer
committing the same uid first — would have surfaced as
`Neo.ClientError.Transaction.TransactionCommitFailed` and dead-lettered the
work item.

The gate now tests the property the replay actually needs. A NornicDB commit
failure rolls the whole transaction back rather than tearing it, so the replay
restarts from the pre-group state; what it needs is that every statement
converges when run again. `allStatementsAreReplaySafe` (in
`go/internal/storage/cypher/writer.go`, beside the `Statement` type it reasons
about) accepts two shapes: a MERGE-shaped statement, unchanged, and a
predicate-scoped retract — `OperationCanonicalRetract` on Cypher that opens
with `MATCH` and whose only write clauses are `DELETE` or `REMOVE`, so a second
run removes the same parameter-bound set.

Everything else still keeps the group terminal, which is the narrowness the old
gate bought by refusing every mixed group: a `CREATE` duplicates on replay, an
accumulating `SET` double-applies, and a row-driven `UNWIND ... MATCH ...
DELETE` is the shape that no-ops inside a managed transaction and must never be
replayed as though it had applied.

Nothing was serialized to get here. The concurrent writers stay concurrent and
the conflict is absorbed by replaying an idempotent group.

- **Regression, failing first:** `TestSemanticEntityWriterGroupedRetractConvergesOnCommitUniqueConflict`
  drives the production `SemanticEntityWriter`, wired as
  `go/cmd/reducer/neo4j_wiring.go` wires it for NornicDB, through the production
  `RetryingExecutor` over a group executor that fails the first commit with the
  UNIQUE-conflict body. Against the code as this branch first stood it failed
  with `write semantic entities: Neo4jError:
  Neo.ClientError.Transaction.TransactionCommitFailed (commit failed:
  constraint violation ...)` on both the full-repo and the delta variant. It
  now passes with `ExecuteGroup calls = 2` (one conflict, one replay), and it
  asserts the group really is mixed so it cannot pass for the wrong reason.
- **Mutation proofs — every guard bites.** Five single-substitution mutants,
  `go vet` exit 0 on each before the test run:

  | Mutant (`subs=1`) | Result |
  | --- | --- |
  | drop the retract branch from `allStatementsAreReplaySafe` | RED — 7 subtests, including both writer-level convergence cases |
  | drop the non-idempotent-clause guard | RED — all 5 clause cases (CREATE, SET, UNWIND, CALL, FOREACH) |
  | drop the `OperationCanonicalRetract` requirement | RED — `delete_not_labelled_as_a_retract` |
  | drop the leading-`MATCH` requirement | RED — `retract_that_does_not_open_on_MATCH` |
  | accept any retract-labelled statement without a DELETE/REMOVE clause | RED — `retract_operation_on_a_non-delete_statement` |

## The full-repo retract, measured rather than inferred

The live proof above exercises the DELTA retract (`WHERE n.path IN
$file_paths`), whose retracted paths are disjoint from the upserted rows, so no
uid is ever deleted and recreated inside one transaction. The full-repo retract
(`semanticRetractStatements` -> `WHERE n.repo_id IN $repo_ids ... DETACH
DELETE`) always overlaps: every row the generation keeps is deleted by the
retract and re-MERGEd by the upsert in the same grouped transaction. That is a
different read-your-writes question and it was previously inferred from the
delta number, not measured.

`TestSemanticEntityWriterLiveNornicDBFullRepoRetractRecreatesSameUID`
(`go/internal/storage/cypher/semantic_entity_full_repo_retract_live_test.go`)
measures it. gen1 writes two `Variable` rows for one repo; gen2 does a
full-repo write carrying only one of them, with a changed property value. The
assertions separate the three ways this can go wrong instead of collapsing them
into one count: the dropped uid must be gone (the retract applied), the kept
uid must exist exactly once with its gen2 value (the re-MERGE saw the delete
and landed), its `File` containment edge must not be duplicated, and both
`File` nodes must survive.

- **Backend / version:** `timothyswt/nornicdb-cpu-bge:v1.2.3`, self-reporting
  `"version":"1.2.2"`, started with the `docker-compose.yaml` NornicDB
  environment (`NORNICDB_ASYNC_WRITES_ENABLED=false`, embeddings and search
  off) on host Bolt port 17811.
- **Result:** `-count=20`, exit 0, **20 PASS, 0 FAIL, 0 SKIP**.
- **The grouped route is really exercised:** the test asserts the live executor
  implements `cypher.GroupExecutor` before writing, so it cannot pass through
  `WriteSemanticEntities`'s per-statement fallback.
- **Mutation proof:** dropping the retract emission from `WriteSemanticEntities`
  (`subs=1`, `go vet` exit 0 on the mutant) turned it RED, 3 FAIL of 3 at
  `-count=3`, each reporting `dropped Variable count after gen2 = 1, want 0` —
  the same symptom shape the v1.1.11 defect reports.

One result worth recording because it is easy to misread. The same test, and a
throwaway variant of it using the delta predicate, both pass 20/20 against the
v1.1.11 container (`@sha256:51b6174a`, host Bolt port 17813) too, and the
mutation above goes RED there as well, so the harness is live and biting on
that backend. This harness therefore does **not** reproduce the under-
application that
`docs/internal/evidence/6176-grouped-semantic-retract-version-floor.md`
measured through the `internal/replay/offlinetier` shim. That is a difference
between two harnesses, not a contradiction of the floor: it is **not** evidence
that v1.1.11 is safe, and the 1.2.1 floor stands on the offlinetier
measurement, unchanged.

```bash
cd go
ESHU_SEMANTIC_ENTITY_NORNICDB_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb \
ESHU_NEO4J_DATABASE=nornic NEO4J_URI=bolt://localhost:17811 \
NEO4J_USERNAME=neo4j NEO4J_PASSWORD=nornicdb \
go test ./internal/storage/cypher/ \
  -run 'TestSemanticEntityWriterLiveNornicDBFullRepoRetractRecreatesSameUID' \
  -count=20 -v; echo $?
```

Exit codes captured directly, never after a pipe. Pass and fail counts came
from counting `--- PASS:` / `--- FAIL:` lines, because a `-run` filter matching
nothing also exits 0.

## Merge ordering: satisfied, #6313 has landed

This removal was only safe once #6313 moved the chart off the backend where
the grouped retract under-applies. That has happened: `#6313` merged as
`a281fad7523b`, this branch is rebased onto it, and
`deploy/helm/eshu/values.yaml:1110` now reads
`tag: "v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74"`.
The `v1.1.11@sha256:51b6174a` pin this section was originally written against
is gone, so the combination it warned about — bundled v1.1.11 plus the
grouped-writes opt-in with the sequential retract removed — is no longer
reachable from this chart.

The exposure is narrow and stays stated rather than assumed. With
`ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` unset — the default — the NornicDB
semantic executor is `ExecuteOnlyExecutor`, which hides `GroupExecutor`
entirely, so nothing on the default path changes. The opt-in is the only route
affected, and `go/cmd/reducer/AGENTS.md` already documents it as
conformance-only: "Enable it only for conformance runs, not production."

## Not covered by this change

The other v1.1.11-era retract workarounds are untouched and unmeasured on
1.2.2 — repo-dependency retract
(`EdgeWriter.executeRepoDependencyRetractStatementsSequential`), the code-call
per-label retracts, the SQL relationship sequential writes, and the
Drain-marked canonical edge retracts. #6176 sequences the semantic writer
first on purpose; each of the others needs its own live measurement before it
can be unpicked. The `deploy/helm/eshu/values.yaml` NornicDB pin is also
unchanged and still names v1.1.11 — that is #6313's to move, and the merge
order it forces is under "Merge ordering" above.
