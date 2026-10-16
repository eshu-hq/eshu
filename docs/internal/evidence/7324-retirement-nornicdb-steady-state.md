# #8173: Attributed Retirement Deletes The Live Node On NornicDB (Steady State)

Follow-up to `7324-repository-path-conflict-retirement.md`, correcting its
"NornicDB probe outcome" section: the rewritten statement deletes correctly
on NornicDB only when a real conflict exists. In steady state (same id,
same path) it DETACH DELETEs the live node. Root cause, probe matrix, and
fix below. Base: branch head 45f1acb62 on 261ca79b8. Backends: ambient
`nornicdb` (Bolt 127.0.0.1:38689, database `nornic`) and `neo4j`
(127.0.0.1:38687, database `neo4j`), production schema applied by the
harness (`repository_id` and `repository_path` unique constraints present).

## CI signature

PR #8173 live-backend run 38809650867, shard 1/4 (NornicDB):
`TestLiveRepositoryRetryInterleavedWithWorkloadMaterialization` (all 6
subtests) and `TestLiveRepositoryZombieAttemptKeepsReducerEdges` (trial 0)
fail with `reducer edges = map[]`. The same file on shard 4/4 (Neo4j)
passes (status 0). The `UNIQUE on Repository.[id] ... already exists`
WARNs in the log are expected #7304 transient noise from the passing
50-session MERGE race, not the failure.

## Interleaving (root cause)

Steady-state retry, same repo id and path, on NornicDB:

1. `repository_cleanup` runs `MATCH (r:Repository {path: $path}) WHERE
   r.id <> $repo_id`, which should match nothing.
2. The new shape appends `OPTIONAL MATCH (r)-[rel]-(other) WITH r,
   collect(...) AS dropped` before `DETACH DELETE r RETURN dropped`.
   NornicDB routes multi-clause compositions into an interpreter that
   drops the WHERE guard (same family as the `RETURN DISTINCT` literal
   and WITH-position pitfalls in
   `docs/public/reference/nornicdb-pitfalls.md`): the DELETE fires on the
   path-matched live node (zombie repro log: `nodes_deleted=1
   relationships_deleted=28`), and the RETURN projection yields the
   literal string `"dropped"`, which parses to nil (`attribution=false`).
3. Phase C `MERGE (r:Repository {id})` re-creates the emptied node;
   directories/files rewire CONTAINS; reducer edges (never re-armed) stay
   lost. `relationshipTypeCounts` reads the recreated node: `map[]`, or
   the materializer-only partial map when the hook ran after the cleanup.

Retry/rollback semantics versus the old summary-counter path: the old
shape is single-clause (`MATCH..WHERE..DETACH DELETE`, no RETURN), so
NornicDB honors the guard and the steady state is a no-op. The behavior
change is the statement shape, not record consumption: the probe below
shows correct summary counters with the record consumed, and the delete
happens server-side at RUN regardless.

Ruled out with code reads plus the probe: Bolt `Next`-before-`Consume`
ordering (counters and records behave; innocent); a sanity-check DETACH
DELETE at `canonical_node_writer.go:91` (no such statement exists);
retry re-matching a deleted node (nothing is re-matched; the live node
itself is deleted); the 50-session MERGE race (zombie trial 0 is fully
sequential and still wipes; the race test passes).

## Probe matrix (live NornicDB, isolated nonce fixture)

| variant | steady state | result |
| --- | --- | --- |
| V1 old shape (no RETURN) | 0/0 expected | `records=0 nodesDeleted=0`, node kept |
| V2 new shape | 0/0 expected | `records=1 nodesDeleted=1 relsDeleted=1`, node GONE, record value `"dropped"` |
| V4 new shape minus RETURN | 0/0 expected | `records=0 nodesDeleted=1 relsDeleted=1`, node GONE |
| V5 `DETACH DELETE` + `RETURN r.id` | 0/0 expected | `records=1 (null) nodesDeleted=0`, node kept |

V4 isolates the trigger: `OPTIONAL MATCH` + `WITH collect` between the
guard and the delete, with or without RETURN. V5 shows a single-clause
delete with a RETURN stays correct.

## Fix (partition, no serialization)

The attributed shape is Neo4j-only; NornicDB writers emit the bare shape
(`canonicalNodeRepositoryPathCleanupBareCypher`, byte-identical to the
pre-split text, proven 0/0 above). Same statement position, same anchor,
guard, params, and summary key; same-statement attribution and
at-most-once reporting are unchanged on Neo4j. Bare entries flow through
the existing unattributed path (`attribution=false`, only
`eshu_dp_canonical_repository_retirements_total` moves). No worker,
batch, timeout, or retry change.

Files: bare constant + MUST-gate comment (`canonical_node_cypher.go`);
`repositoryRetirementBare` flag + `WithBareRepositoryRetirement`
(writer + options); branch in `buildRepositoryCleanupStatements`;
capture/report recognize both shapes (`canonical_node_writer_retract.go`);
seam contract stays attributed-only (`write_counts.go`); Rule 1 guard
allows both constants (guard test); bare selected in
`storagenornicdb.ConfigureCanonicalWriter` (projector, ingester) and the
bootstrap NornicDB branch; live harness writers follow the backend under
test. Hermetic pins: bare emission + params, capture of the bare shape,
wiring mutation-checked, guard RED/GREEN extended.

## Rerun rates

- Bare base 261ca79b8, NornicDB zombie: 3/3 PASS.
- Branch head pre-fix, NornicDB zombie: local 0/1 FAIL + CI 0/1 FAIL
  (sequential trial 0 fails, so not a timing flake).
- Branch head post-fix, NornicDB: zombie 3/3 PASS, interleaved 6/6 PASS,
  full `^TestLiveRepository` file PASS except the three ledger-pinned
  `backends: neo4j` attribution files (fail on NornicDB on base too:
  `TestLiveRepositoryPathConflictRetirementReportsDroppedIncomingEdges`
  0/1 on base — pre-existing, by design).
- Branch head post-fix, Neo4j: zombie + interleaved PASS; all three
  attribution files PASS on both executor shapes (split preserved).

## Observability Evidence

`canonical repository retired` with `attribution=false` on NornicDB bare
retirements (outcome counter only); unchanged attributed WARN/INFO with
`dropped_incoming`/`dropped_outgoing` and the split counter on Neo4j.
No new query, transaction, retry, or lock domain: the bare shape is the
pre-split statement, so no new performance proof is owed beyond the live
runs above (zombie ~0.9s, interleaved ~0.8s, full file ~67s on NornicDB
including the 58s MERGE race).

## Remaining risk

An untuned hand-built writer pointed at NornicDB still emits the
attributed shape (default). All production NornicDB wirings select bare;
the constants and option carry MUST-level comments. A NornicDB release
past the upstream multi-clause fix could re-enable attribution there;
until then no NornicDB attribution is claimed.
