# Evidence: orphan sweep redesigned as a Go-side anti-join, no relationship-existence predicate (#5147)

## Problem

The graph orphan sweep (`OrphanSweepStore` in `orphan_sweep.go`) built every
mark/sweep/count statement on `NOT (n)--()`. On both pinned NornicDB backends
(v1.1.11 at `bolt://127.0.0.1:17688`, PR261/compose-default at
`bolt://127.0.0.1:17689`) that predicate never matches: `NOT (n)--()` always
evaluates false, `(n)--()` always evaluates true, and `COUNT { (n)--() } = 0`
always evaluates true (a tautology, not a correct zero-relationship check).
The sweep was therefore a silent no-op: orphans leaked forever, and the
`eshu_dp_graph_orphan_nodes` gauge reported a constant 0 regardless of actual
disconnected-node count. A naive swap to `COUNT{}=0` would not fix this --
it would over-delete the connected graph, since the predicate matches
every node.

## Theory proof (throwaway shims, run before implementation, deleted after)

Shims were run against both `ESHU_CYPHER_BOLT_DSN=bolt://127.0.0.1:17688` and
`bolt://127.0.0.1:17689` (`ESHU_CYPHER_BOLT_DATABASE=nornic`), via the
`openBoltTestRunner`/`runCypherSingle` autocommit path
(`code_evidence_bolt_retract_test.go`), never `runCypherGroup`/`ExecuteWrite`.

1. **S1 (candidates, single-clause read + LIMIT + multi-column RETURN)**:
   `MATCH (n:File) WHERE n.evidence_source IS NOT NULL AND n.path STARTS WITH
   'shim-' RETURN n.path AS key, n.eshu_orphan_observed_at_unix AS observed_at
   LIMIT $limit` returned the correct 3 rows (orphan, connected, peer) with
   both columns bound correctly on both backends.
2. **S2 (connected keys)**: seeded `orphan` (no edge), `connected`
   `-[:CONTAINS]->` `peer` (anchored `MATCH...MERGE`), verified the edge
   visible via an unanchored `MATCH (a)-[r:CONTAINS]->(b) ... RETURN
   count(r)`. The UNWIND-anchored form `UNWIND $keys AS candidate_key MATCH
   (n:File {path: candidate_key})-[r]-(m) RETURN DISTINCT n.path AS key`
   correctly returned exactly `{connected, peer}` and excluded `orphan`, on
   both backends.
   - **First attempt used `UNWIND $keys AS key ... RETURN DISTINCT n.path AS
     key`** (reusing the RETURN alias as the UNWIND binding variable). This
     silently returned zero rows (not an error) on both backends -- a
     shadowing footgun, not a backend limitation. Renaming the UNWIND
     variable to `candidate_key` fixed it. See
     `docs/public/reference/nornicdb-pitfalls.md` ("Every
     Relationship-Existence Predicate Is Mis-Evaluated" > "Pitfall within the
     pitfall").
   - The verbatim full-label fallback form (`MATCH (n:File)-[r]-(m) WHERE
     n.evidence_source IS NOT NULL AND n.path STARTS WITH 'shim-' RETURN
     DISTINCT n.path AS key`) also worked correctly on both backends. **Chosen
     form: the UNWIND-anchored keyed form**, because it is bounded to exactly
     the S1 candidate set (never a broader label scan) and its cost is
     tied to the number of candidates already bounded by `CountLimit`, not to
     total label population.
3. **S3/S4/S5 (key-anchored writes)**: `UNWIND $keys AS candidate_key MATCH
   (n:File {path: candidate_key}) SET/REMOVE/DELETE ...` all applied
   correctly via autocommit `Execute` on both backends -- SET/REMOVE
   round-tripped through a follow-up read, and DELETE removed only the
   orphan while `connected`/`peer` survived.

All shim assertions passed on both `17688` and `17689` after the UNWIND/alias
fix. Shim files were deleted before committing (per repo convention); this
evidence file is the durable record.

## Live discriminating regression (committed, `orphan_sweep_antijoin_live_test.go`)

`TestLiveOrphanAntiJoinReplacesBrokenNotDashDashPredicate` (env-gated on
`ESHU_CYPHER_BOLT_DSN`):

1. **RED subtest** (`historical_not_dash_dash_predicate_is_a_no_op`):
   reproduces the exact origin/main mark/sweep Cypher (`NOT (n)--()`)
   verbatim against a seeded true orphan. Confirmed on both backends: the
   mark statement does not set the marker, and the sweep statement does not
   delete the orphan. This is the RED state -- origin/main's sweep is a
   no-op.
2. **GREEN**: the same test then builds a real `OrphanSweepStore` (autocommit
   `Executor`, bolt `Reader`) with an injected clock and runs two cycles:
   cycle 1 marks the true orphan and a soon-to-be-relinked node; between
   cycles the relinked node gains a `CONTAINS` edge from a peer and the clock
   advances past the 1s TTL; cycle 2 sweeps (deletes) the true orphan,
   clears the relinked node's marker, and leaves the relinked node, the
   original connected node, and both peers present.

Results, both backends identical:

```
17688 (v1.1.11):
  historical_not_dash_dash_predicate_is_a_no_op: PASS
    "confirmed: legacy NOT (n)--() mark did not match the true orphan (no-op)"
    "confirmed: true orphan survives the legacy NOT (n)--() sweep -- origin/main leaks orphans forever"
  cycle 1 result: counts=map[File:3] marked=map[File:3] deleted=map[File:0] skipped=map[File:2]
  cycle 2 result: counts=map[File:1] marked=map[File:0] deleted=map[File:1] skipped=map[File:1]
  PASS (0.14s)

17689 (PR261/compose default):
  historical_not_dash_dash_predicate_is_a_no_op: PASS (same log lines)
  cycle 1 result: counts=map[File:3] marked=map[File:3] deleted=map[File:0] skipped=map[File:2]
  cycle 2 result: counts=map[File:1] marked=map[File:0] deleted=map[File:1] skipped=map[File:1]
  PASS (0.13s)
```

Run with:

```bash
cd go
ESHU_CYPHER_BOLT_DSN=bolt://127.0.0.1:17688 ESHU_CYPHER_BOLT_DATABASE=nornic \
  go test ./internal/storage/cypher -run TestLiveOrphanAntiJoinReplacesBrokenNotDashDashPredicate -count=1 -v
ESHU_CYPHER_BOLT_DSN=bolt://127.0.0.1:17689 ESHU_CYPHER_BOLT_DATABASE=nornic \
  go test ./internal/storage/cypher -run TestLiveOrphanAntiJoinReplacesBrokenNotDashDashPredicate -count=1 -v
```

Final assertions passed on both: orphan deleted (count=0), connected and peer
preserved (count=1 each), relinked node preserved (count=1) with its marker
cleared.

Diagnostic note: two earlier throwaway performance shims (deleted before
commit) had left `test-5147-perf`/`test-5147-scale`-tagged `File` nodes behind
on both backends because their cleanup used the grouped `ExecuteWrite` path
(`boltWriteStatement` / `runCypherGroup`) for a single DETACH DELETE
statement, which is the exact under-application shape #4367/#5116/#5152
documented for grouped single-statement transactions on NornicDB -- the
cleanup silently under-applied. This was caught because it inflated the S1
candidate count and pushed the live regression's specific orphan key past the
`BatchLimit` window on one run. Both backends were manually cleaned with a
direct autocommit `DETACH DELETE` and the live regression then passed
deterministically. Lesson reinforced: even throwaway test cleanup on this
backend must use the same autocommit path production writes use, never
`ExecuteGroup`/`runCypherGroup`, for anything whose application must be
guaranteed.

## Performance ("2 reads, 0 writes" steady-state claim, worst-case scale, and disclosed regression)

No-Regression Evidence: the unit-level steady-state contract is proven by
`go test ./internal/storage/cypher -run
'TestOrphanSweepStoreSteadyStateRunsTwoReadsZeroWrites|TestOrphanSweepStoreNoCandidatesSkipsS2ReadEntirely'
-count=1` -- a label whose only candidate is already connected and unmarked
issues exactly the S1 + S2 reads and zero writes (all three writes
`Skipped`); a label with zero matching nodes at all skips S2 entirely (one
read, zero writes). This generalizes the old design's "steady no-orphan state
runs two cheap reads and issues zero write transactions" claim to the new
anti-join shape.

Worst-case populated-label measurement (throwaway shim, deleted after
recording, run against `17688`): seeded 5,000 `File` nodes (2,500 connected
pairs + 2,500 true orphans, all `evidence_source='test-5147-perf'`):

- **BEFORE** (origin/main's per-cycle count query, `NOT (n)--()`, always a
  no-op): `MATCH (n:File) WHERE evidence_source=... AND NOT (n)--() WITH n
  LIMIT $limit RETURN count(n)` over the 7,500-node candidate pool: **60ms**,
  returned `count=0` (proven no-op -- the old design never did real work here
  regardless of true orphan count).
- **AFTER** (new S1 + S2 anti-join) over the same 7,500-node pool: S1
  (candidates) **20ms** (7,500 rows), S2 (connected keys, UNWIND-anchored on
  all 7,500 candidate keys) **9.66s**, total **9.68s**. Anti-join correctly
  computed 2,500 orphans (`7500 candidates - 5000 connected = 2500`).

Follow-up scaling probe (all-connected keys, isolating S2 cost): 200 keys
14ms, 1,000 keys 194ms, 2,000 keys 811ms, 4,000 keys 3.0s -- consistent
super-linear growth on both backends (784ms at 2,000 keys on `17689` too).
This S2 read cost is bounded today by `OrphanSweepPolicy.CountLimit` (default
10,000) and runs at most once per label per hourly cycle, so it does not
threaten correctness, but a heavily populated label (thousands of
orphan-eligible candidates) could make one sweep cycle take on the order of
10-20 seconds.

### S2 scaling follow-up: alternative shapes measured, chunking adopted

A dedicated follow-up (throwaway shims, deleted after recording, 5,000-node
populated `File` label, 4,000 connected + 1,000 orphan, both `17688` and
`17689`) tried three alternative shapes for the S2 connectivity read:

1. **Bounded IN-list, no UNWIND**
   (`MATCH (n:File)-[r]-(m) WHERE n.path IN $keys RETURN DISTINCT n.path`):
   *worse* than the UNWIND-anchored form at every measured scale (200 keys
   16ms, 1,000 keys 367ms, 2,000 keys 1.45s, 4,000 keys 5.79s, 5,000 keys
   7.22s). Rejected.
2. **Unbounded full-label scan, no key anchor**
   (`MATCH (n:File)-[r]-(m) WHERE <evidence_predicate> RETURN DISTINCT
   n.path`): fast and correct at the tested scale -- 5,000-node population
   27ms, 15,000-node population 200ms (both verified to return the exact
   correct connected-key set, identical to the anchored form's output). But
   it removes `CountLimit`'s bound entirely: its cost is proportional to
   *total* label population matching the evidence predicate (which has no
   index), not to the S1 candidate count. A separate probe that grew a
   synthetic population toward 20,000-40,000 nodes hit a server-side "Txn is
   too big to fit into one request" failure on an unrelated large
   single-statement cleanup and a >2-minute timeout on an unindexed
   full-label scan against the same backend -- direct evidence that
   unbounded operations over an unindexed property at larger populations do
   not stay flat on this backend. Adopting this shape would trade the
   UNWIND form's bounded-but-quadratic cost for an unbounded cost, on
   exactly the "years-old backlog" deployment scenario this bug fix targets.
   Rejected for that boundedness risk, not for a correctness defect.
3. **Chunked UNWIND-anchored form (adopted)**: same query shape as before
   (`BuildConnectedKeysQuery`, still key-anchored, still bounded to exactly
   the S1 candidate set, still no relationship-existence predicate), split
   into `defaultOrphanSweepConnectedKeysChunkSize = 500`-key round trips
   inside `readConnectedKeys`
   (`go/internal/storage/cypher/orphan_sweep_queries.go`). Measured on the
   real `(*OrphanSweepStore).readConnectedKeys` production method at the
   same 5,000-key/4,000-connected scenario, both backends:

   ```
   17688: unchunked 4.746s -> chunked (500/round trip) 0.610s, connected=4000/4000
   17689: unchunked (not re-measured; UNWIND curve matched 17688 throughout
          this investigation) -> chunked (500/round trip) 0.602s, connected=4000/4000
   ```

   A manual sweep of chunk sizes on the unchunked 5,000-key list showed 500
   (10 round trips, ~572-610ms total) beating 1,000 (5 round trips,
   ~950-964ms total) -- more, smaller round trips outperform fewer, larger
   ones, consistent with the underlying per-statement cost being
   super-linear in list length. This is a ~7.7x speedup at the measured
   scale while keeping the exact same bounded, correctness-proven read
   shape -- no full-label scan, no new relationship-existence predicate, no
   change to what "connected" means.

   Decision and operator-facing sizing guidance recorded in
   `docs/public/reference/cypher-performance.md`
   ("Orphan Sweep Anti-Join Connectivity-Read Chunking (#5147)") and
   `docs/public/reference/nornicdb-tuning.md`
   ("Orphan Sweep Candidate And Connectivity-Read Sizing").

New unit coverage (TDD, fake `Reader` counting round trips):
`TestReadConnectedKeysIssuesOneRoundTripAtOrBelowChunkSize`,
`TestReadConnectedKeysChunksAboveChunkSizeAndUnionsResults`, and
`TestReadConnectedKeysChunkPropagatesReaderErrorMidway`
(`orphan_sweep_chunking_test.go`) prove the chunk boundary, the
union/dedup-across-chunks behavior, and that a reader error on a later chunk
is not silently swallowed. `TestLiveOrphanAntiJoinReplacesBrokenNotDashDashPredicate`
was re-run on both `17688` and `17689` after the chunking change and still
passes unchanged, proving the chunked read still discriminates orphan vs.
connected correctly at the scale that matters for correctness.

**Disclosed regression: S1-LIMIT binds on candidates, not on true orphans.**
`BuildCandidateOrphanNodesQuery`'s `LIMIT $limit` bounds how many nodes
matching `evidence_source IS NOT NULL` are considered *at all* in one cycle,
before connectivity is even resolved. If a label has more matching nodes than
`CountLimit`, some of them (including possibly true orphans) will not appear
as S1 candidates this cycle and so cannot be marked/swept until a later cycle
picks them up (Cypher does not guarantee a stable row order across
`LIMIT`-bounded scans). This mirrors the previous design's behavior: the old
`BuildCountOrphanNodesQuery` also bounded its underlying match with `WITH n
LIMIT $limit` before counting, so both designs share this LIMIT-before-full-
convergence property; it is called out explicitly here because the new S1
read makes the bound visible on the candidate set itself rather than hidden
inside a count. Convergence across multiple cycles (proven by
`TestOrphanSweepStoreConvergesAcrossBoundedCyclesForAllDefaultLabels`) still
holds as long as `BatchLimit`-bounded mark/sweep writes keep making forward
progress each cycle.

No-Observability-Change: `OrphanSweepStore` statement summaries and operation
metadata continue to flow through the existing `InstrumentedExecutor` metrics
and spans (statement `Operation` stays `OperationCanonicalRetract` for every
S1/S2/S3/S4/S5 statement, matching the pre-#5147 tagging). The
`eshu_dp_graph_orphan_nodes` gauge, `GraphOrphanNodeCounts`, and the
reducer's per-label count/mark/delete/duration/failure-class structured log
fields are unchanged in shape -- the gauge's *values* now reflect real orphan
counts (previously always 0) rather than the metric contract itself changing.
No metric name, span, log field, queue stage, worker knob, or status field
was added, removed, or renamed.
