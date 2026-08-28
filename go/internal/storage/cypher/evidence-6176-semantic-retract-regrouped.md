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

## No-Observability-Change:

No instrument, span, log, or status surface is added, removed, or renamed. The
same statements carry the same `OperationCanonicalRetract` / statement-metadata
summaries and the same `WrapRetryableNeo4jError` retry classification; only the
transaction they are dispatched in changed. One consequence worth an operator's
attention: a retract failure now surfaces as a failure of the whole semantic
write group rather than as a standalone retract error, so the error text reads
`write semantic entities:` where it previously read `retract semantic entities:`.

## Not covered by this change

The other v1.1.11-era retract workarounds are untouched and unmeasured on
1.2.2 — repo-dependency retract
(`EdgeWriter.executeRepoDependencyRetractStatementsSequential`), the code-call
per-label retracts, the SQL relationship sequential writes, and the
Drain-marked canonical edge retracts. #6176 sequences the semantic writer
first on purpose; each of the others needs its own live measurement before it
can be unpicked. The `deploy/helm/eshu/values.yaml` NornicDB pin is also
unchanged and still names v1.1.11 — see the report note under #6296.
