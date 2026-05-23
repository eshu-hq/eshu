# NornicDB Behavior and Pitfalls Reference

This page is the operational companion to
[NornicDB Tuning](nornicdb-tuning.md). It records NornicDB behaviors that have
affected Eshu integration and proof work.

Use it to avoid rediscovering the same failure shape. Still check the current
NornicDB source before patching.

## How To Use This Page

1. Read the matching section before patching NornicDB or routing around a
   suspected NornicDB bug.
2. Validate the behavior against the current `NornicDB-New` checkout that built
   the image under test.
3. Check upstream docs and release notes for the pinned `NORNICDB_IMAGE`.
4. If the current reproduction differs, update this page with the reproduction,
   observed shape, and either the root cause or open question.

NornicDB changes quickly. A documented behavior may already be fixed in the
binary you are testing.

## Pitfall: Recreating Single-Property `UNIQUE` Constraints On A Live Store

### Observed shape

On a running NornicDB instance with existing nodes:

1. `DROP CONSTRAINT <name>` succeeds.
2. `CREATE CONSTRAINT <name> FOR (n:Label) REQUIRE n.prop IS UNIQUE` succeeds.
3. A later write that matches an existing node can fail commit with a uniqueness
   violation against the matched node itself.

The row remains readable. `MATCH (n {prop: value}) RETURN id(n)` still finds it.

### Hypothesis

The value-cache rebuild can register existing values with one node ID shape
while transactional validation compares another. The commit path then treats the
matched node as another node with the same unique value.

Verify this against the current `NornicDB-New` source before relying on the
hypothesis.

### Eshu implications

- Do not use drop/create constraint cycles as a live-stack debug experiment.
  Tear down the dedicated graph volume and start fresh.
- Do not change Eshu schema bootstrap to rerun `CREATE CONSTRAINT` after graph
  writes. Schema DDL belongs before writes.
- If a read/update of an existing node fails with a false `UNIQUE` violation,
  check this pitfall before changing writer logic.

### Validation

Use an isolated Compose project: run data-plane schema bootstrap, write one
node for a label with a uid-style unique constraint, drop and recreate that
constraint through the Bolt HTTP endpoint, then reissue a `MATCH ... SET`
against the same node. Tear the stack down after the experiment.

## Pitfall: Concurrent `MERGE` Can Lose At Commit-Time `UNIQUE`

### Observed shape

Two concurrent writers can run the same canonical `MERGE` for a uid. Both may
plan a create, one commits, and the other loses at commit with a uniqueness
violation such as:

```text
Neo4jError: Neo.ClientError.Transaction.TransactionCommitFailed
(commit failed: constraint violation:
 Constraint violation (UNIQUE on TerraformResource.[uid]):
 Node with uid=<X> already exists (nodeID: <Y>))
```

That is normal concurrent `MERGE` behavior. Re-executing the same MERGE after
the winning commit should match the existing node.

### Eshu status

Eshu handles this in `go/internal/storage/cypher/retrying_executor.go`.
`RetryingExecutor.ExecuteGroup` retries commit-time unique conflicts when every
statement in the group is MERGE-shaped. Mixed groups are not retried because
re-executing non-MERGE statements after partial success can be unsafe.

The retry classifier recognizes current NornicDB commit-time unique wrapping.
If an upgrade changes the error shape, extend
`isNornicDBCommitTimeUniqueConflict` and add a regression test.

### Eshu implications

Do not serialize workers to hide this race, and do not add preflight `MATCH`
checks as the fix for canonical MERGE re-projection. Route canonical projection
through the retrying phase-group executor. If the error reappears, verify
`retryable_error_test.go` and `retrying_executor_test.go` before changing queue
or worker knobs.

## Pitfall: Persisted Graph Store Fails To Reopen After Dictionary Corruption

### Observed shape

A NornicDB-backed Eshu graph store can fail before Bolt or HTTP readiness with:

```text
failed to load persisted schema: schema: rebuild unique values:
decode node: property key id <id> not in dictionary for namespace "nornic"
```

When this happens, API and MCP graph-backed reads cannot recover until the graph
backend opens or the graph volume is rebuilt.

### Eshu recovery contract

For Eshu, NornicDB graph data is rebuildable projection state. Source systems,
repository snapshots, collector facts, workflow state, content, and Postgres
queues are the durable inputs.

Supported response:

1. Preserve the broken graph volume or logs when forensic evidence matters.
2. Recreate only the NornicDB data directory or PVC.
3. Run data-plane schema bootstrap before graph writes resume.
4. Replay projection work from stored facts or recollect from source systems.
5. Verify API/MCP health and queue-zero with `GET /api/v0/index-status`.

Do not delete Postgres unless the accepted recovery plan is full source
recollection. Do not make Eshu silently delete graph data at startup.

### Upstream follow-up

Track this upstream when the current binary still reproduces it. The database
should classify the corruption signal and restore large node sets in bounded
chunks rather than one oversized transaction.

## When To Patch NornicDB

Patch NornicDB only when evidence supports one of these:

- a correctness fix for NornicDB itself
- a measured NornicDB performance win that generalizes beyond one Eshu symptom
- a measured Eshu runtime win proven by focused and corpus-level evidence

Before drafting a patch:

1. Write a failing test in `NornicDB-New`.
2. If the bug does not reproduce in NornicDB isolation, investigate Eshu first.
3. Build the patched binary into a unique image tag and pin that image only in
   the relevant test or Compose overlay.
4. Never overwrite a shared production image tag for a local experiment.
