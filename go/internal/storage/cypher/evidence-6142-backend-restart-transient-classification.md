# Evidence: a backend restart's transients were terminal (#6142)

## Symptom

`scripts/verify-ifa-fault-injection.sh` cell
`restart-backend-between-phase-groups` went red on PR #6142 (workflow run
32048449301 attempt 1, job 95441720646, head `49d75a4b1`): the cell's canonical
graph digest diverged from the fault-free baseline while every liveness signal
in the same cell reported healthy — 4/4 drains PASS, `residual=0`,
`dead_letter=0`, `ifa assert-edges` exact, restart sentinel non-vacuous. A
re-run of the same job at the same commit (attempt 2, job 95453325823) passed,
so the cell is intermittent rather than standing red.

## Reproduction

Reproduced locally on 2026-08-17 (macOS, 12 cores, Docker 29.4.0, NornicDB
`eshu-nornicdb-pr290:3722b483c02c`, Compose project `eshu-repro-6142`, ports
15942/7901/7988) with a scratch harness that runs the gate's own `cell_baseline`
and then repeated `restart-backend-between-phase-groups` cells, recording each
digest instead of dying on the first mismatch.

The fault-free baseline is deterministic on this host: it canonicalized to
`280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052` on every
run, byte-identical to CI's baseline digest for the failing run — 707 edges,
678 nodes, GCP relationship edges exactly 63 per `acme-demo-gcp-00..07` plus 123
for `supply-chain-demo-project`.

Restart cells failed on an otherwise idle machine. One restart produced three
dead letters at once, all `projection_bug`, all at `attempt_count = 1`:

```
gcp_resource_materialization     acme-demo-gcp-01  dead_letter  projection_bug
gcp_resource_materialization     acme-demo-gcp-03  dead_letter  projection_bug
  write canonical cloud resource nodes: Neo4jError: Neo.ClientError.Statement.SyntaxError
  (UNWIND MERGE chain create failed: checking node existence: reading node: DB Closed)

gcp_relationship_materialization acme-demo-gcp-02  dead_letter  projection_bug
  write canonical gcp relationship edges: Neo4jError: Neo.ClientError.Statement.SyntaxError
  (UNWIND MERGE chain relationship create failed: start node nornic:565f703f-… does not exist)

gcp_relationship_materialization acme-demo-gcp-01  pending  attempt_count 0
gcp_relationship_materialization acme-demo-gcp-03  pending  attempt_count 0
```

An earlier restart produced the commit-side variant:

```
gcp_resource_materialization  supply-chain-demo-project  dead_letter  projection_bug
  write canonical cloud resource nodes: Neo4jError:
  Neo.ClientError.Transaction.TransactionCommitFailed
  (commit failed: badger commit failed: Writes are blocked, possibly due to DropAll or Close)
```

Each time the drain then sat at a non-zero residual until the gate's 4-minute
budget expired: `drains: still polling after 4m0s (fact residual=…)`, `1
required-fail`. `gcp_resource_materialization` publishes the
`cloud_resource_uid` canonical-nodes-committed phase, so every relationship
intent behind a dead-lettered one waits on a readiness gate that can never open.

## Root cause

A backend restart interrupts a canonical write at one of several points, and
NornicDB reports them under codes that do not say "the backend went away".
Before this change only three points were classified:

| point | error | before |
|---|---|---|
| process gone | `*neo4jdriver.ConnectivityError` | retryable |
| WAL closed before begin | `TransactionStartFailed` / `failed to write WAL tx begin: wal: closed` | retryable (#5989) |
| **store closing, commit refused** | `TransactionCommitFailed` / `…badger commit failed: Writes are blocked, possibly due to DropAll or Close` | **terminal** |
| **store closed under a statement** | `Statement.SyntaxError` / `UNWIND MERGE chain create failed: checking node existence: reading node: DB Closed` | **terminal** |
| **endpoint unreadable mid-chain** | `Statement.SyntaxError` / `UNWIND MERGE chain relationship create failed: start node nornic:<uuid> does not exist` | **terminal** |

The last three reached the reducer as plain `*neo4jdriver.Neo4jError` values,
`reducer.IsRetryable` returned false, and `ReducerQueue.failIntent` dead-lettered
attempt 1 as `projection_bug`. Two of the three arrive under
`Neo.ClientError.Statement.SyntaxError` — the code a genuinely malformed query
uses — which is why the code alone cannot classify them and the message has to.

These are misclassifications, not races. Once a restart lands on one of these
points the dead-letter follows every time; only *whether* it lands there is
timing-dependent, which is why the gate is intermittent and why a larger, faster
reference host does not expose it.

### What this does NOT explain: CI's truncated edge set

CI's failing cell had `dead_letter=0` and a converged drain. Every shape fixed
here dead-letters. So none of them produces CI's signature, and this section
records what was measured about the leading alternative rather than leaving a
plausible story standing.

**The observation stands.** Measured against the locally reproduced baseline
(which carries CI's baseline digest), of the 114 distinct GCP relationship types
`supply-chain-demo-project` holds:

```
cut = GCP_route_next_hop_vpn_tunnel
  66 types sort BELOW the cut  ->  0 lost anything
  48 types sort AT/AFTER it    -> all 48 lost records (51 records)
  exceptions in either direction: 0
```

A strict contiguous suffix, and a real one: the canonical dump sorts by node
content digest, not by relationship type (`graphdump.byCanonicalBytes`, `from`
first), so clustering in type space cannot be a `diff -u` alignment artifact —
unlike the artifact's "51 removed / 50 added" record framing, which is only the
cheapest line alignment and must not be read at record level.

**The reading it suggested is rejected by measurement.** Because
`GCPCloudResourceEdgeWriter.WriteCloudResourceEdges` emits one statement per
type in `sort.Strings(cypherTypes)` order, a suffix-shaped loss looks exactly
like a statement group that applied a PREFIX and still reported success. It is
not what this backend does. Probed against the pinned image by restarting the
backend under a long transaction built from the real upsert constant:

```
reached=4597/20000 statements executed before the restart landed
driverError=Neo4jError: Neo.ClientError.Statement.SyntaxError
            (DB::Get key: "\x01system:migration:legacy_data" err: DB Closed)
survived=0/20000
```

Full rollback, loud failure. Nothing was left behind. A companion probe ran 60
per-type statements in one transaction on a healthy backend: 60/60 applied. And
the reducer genuinely takes that grouped single-transaction path —
`reducerNeo4jExecutor` defines `ExecuteGroup` unconditionally so the
`GroupExecutor` assertion in `dispatch` succeeds, and
`ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` gates only *semantic* writes
(`cmd/reducer/multi_cloud_runtime_drift_wiring.go`), not this writer.

**So the cause of CI's truncated edge set is unidentified.** Not the three
shapes fixed here (all dead-letter; CI had none). Not a torn group (measured,
rejected). Not supersession — that requires a newer active generation for the
same scope (`supersedeInactiveReducerGenerationsCTE`, which does sweep
`dead_letter`), and this cell drives exactly one generation per scope, which the
run's own `skip_retract=true` lines assert. Anyone picking this up next should
start from that unexplained signature, not from the prefix story.

### Measured backend behaviour (pinned image, throwaway probe)

Run against `eshu-nornicdb-pr290:3722b483c02c` using the real
`canonicalGCPCloudResourceEdgeUpsertCypherFormat` constant, reading
`ResultSummary.Counters()`:

| case | ContainsUpdates | RelationshipsCreated | PropertiesSet | edges in graph |
|---|---|---|---|---|
| A both endpoints present | true | 1 | 0 | 1 (correct) |
| B target endpoint absent | false | 0 | 0 | 0 (correct, by design) |
| C idempotent rewrite of A | false | 0 | 0 | 1 (correct) |
| D 1 valid + 1 missing endpoint | true | 1 | 0 | 1 of 2 rows |

Two things worth keeping. First, case A writes: there is no #5652-style silent
drop on this statement. Second, and more reusable — **B and C are byte-identical
on every counter** while B wrote nothing and C correctly rewrote an existing
edge. NornicDB reports `PropertiesSet=0` even when the `SET` ran against an
existing relationship. So counter-based write-accounting cannot distinguish
"wrote nothing" from "nothing needed writing" on this backend; detecting a lost
write needs a read-back or a returned row count, not `Counters()`.

## Fix

Three narrow classifiers, each requiring an error code AND a distinguishing
message so a real Cypher syntax error or a genuine constraint violation stays
terminal:

- `isNornicDBStoreClosingCommitFailure` (`retryable_error.go`) — the commit-side
  twin of #5989's transaction-start guard.
- `isNornicDBStoreClosedStatementFailure` (`retryable_error.go`) — the `DB
  Closed` body. No statement-shape guard: a closed store answered a read, so
  nothing was written.
- `isNornicDBRelationshipCreateMissingEndpoint` (`retrying_executor.go`) — folded
  into `isNornicDBRelationshipSnapshotConflict` beside its update-side sibling,
  so it inherits that path's existing MERGE-shape gate. Both bracketing
  fragments are required; matching the `create failed` prefix alone also
  swallows `create failed: not found`, which
  `TestRetryingExecutorDoesNotBroadenRelationshipSnapshotRetry` deliberately
  keeps terminal — an earlier draft did exactly that and the existing guard
  caught it.

The commit-side and `DB Closed` shapes are deliberately kept out of
`classifyTransientNeo4jError`, so no transaction body is replayed in place. A
commit failure leaves an outcome this process cannot observe, which is why that
function already excludes the driver's own `CommitFailedDeadError`. Durable
queue replay needs no such observation: it re-runs the whole handler, whose
canonical writers are MERGE-shaped upserts, and whose relationship handlers
force the prior-generation retract once `AttemptCount > 1`.

Retry stays bounded. `graph_write_timeout` is not in
`nonCountingReducerRetryFailureClasses` (all readiness classes), so a backend
that never returns still dead-letters once `maxAttempts` is spent — later than
before, but not never.

No worker count, batch size, lease, conflict domain, Cypher shape, statement
batching, transaction scope, or phase order changes.

## Verification

Regressions first, red before green, in
`retrying_executor_backend_restart_commit_test.go` and
`retrying_executor_backend_closing_test.go`: queue-retryability through the real
`CloudResourceNodeWriter` dispatch for the commit-side and `DB Closed` shapes,
in-place replay plus queue-retryable exhaustion for the missing-endpoint shape,
and fail-closed controls for a real syntax error, a `DB Closed` body under an
unrelated code, `create failed: not found`, and a non-MERGE group.

```text
cd go && go test ./internal/storage/cypher ./internal/reducer ./cmd/reducer \
  ./internal/projector ./internal/storage/postgres -count=1   # exit 0
cd go && go vet ./internal/storage/cypher/...                 # exit 0
bash scripts/verify-performance-evidence.sh                   # exit 0
```

No-Regression Evidence: backend NornicDB `eshu-nornicdb-pr290:3722b483c02c` over
the shared Cypher/Bolt contract; input shape = the fault-injection gate's six
driven cassettes, 13 `fact_work_items`, `ESHU_REDUCER_WORKERS=4`; conflict domain
= per-scope canonical `uid` MERGE under concurrent reducer workers. The change
alters only the Go error *type* returned on an already-failing path — no new
query, no extra round trip, no measurable handler cost.

Live coverage, stated at the granularity it was actually obtained. The
fault-free baseline reproduced digest `280a8824…` on six consecutive runs. Five
post-fix restart cells held that digest exactly, against a pre-fix cell that
dead-lettered and blew the gate's 4-minute drain budget; one of those five
recovered four `ConnectivityError`s during its restart window and still matched.
What is NOT claimed: no post-fix cell has yet been observed hitting one of the
three newly classified shapes and recovering from it live. Those shapes were
each observed live BEFORE the fix, with the durable `fact_work_items` rows
quoted above, and the classification flip is pinned by regression tests driving
the real writer dispatch; the end-to-end recovery of each specific shape is left
to the gate itself, which exercises this cell on every run.

Observability Evidence: a restart-interrupted write is now recorded on a
`retrying` row under `failure_class=graph_write_timeout` (via
`neo4jRetryableError.FailureClass`) rather than a `dead_letter` row under
`projection_bug`, and feeds the same producer write-timeout backpressure signal
every other transient graph write does. The 3 AM question — "did the graph
backend bounce, or did the projector emit a bad write?" — previously had the
wrong answer recorded in `fact_work_items.failure_class`.

## Known gaps

Two, named rather than left implied. An earlier revision of this file listed a
third — "the write is still not atomic", asserting that an interrupted GCP
relationship edge write leaves a partial scope until a replay repairs it. That
was written before the backend was probed and is **false on this path**: the
statements go out as one grouped transaction and a restart rolls the whole thing
back (`survived=0/20000` above). It is recorded here rather than silently
deleted because it was the reasoning the fix was originally justified by, and
the justification that survives is the weaker, correct one — replay is safe
because the backend discards everything, not because a replay sweeps a partial.

**CI's truncated edge set is still unexplained.** See the section above. The
three shapes fixed here all dead-letter; CI's failing cell had `dead_letter=0`
and a converged drain. This change removes three real ways the restart cell
fails. It is not known to be the change that makes CI's specific failure stop.

**A genuine missing-endpoint bug now reports a different class.** If a start
node is absent for a real reason — a projection ordering defect rather than a
backend teardown — that write is now retried and, once the budget is spent,
dead-lettered as `graph_write_timeout` instead of `projection_bug`. The item
still dead-letters and `failure_message` still carries NornicDB's exact
"start node … does not exist" text, so it remains diagnosable, but the triage
class is less specific than before. This is the same trade the update-side
sibling has always made, and it is the deliberate direction: mislabelling a
restart as a projection bug broke recovery outright, while mislabelling a
projection bug as a graph-write timeout only delays a dead letter that still
arrives with its cause attached.
