# #8659 graph retraction: evidence note

Retract a retired repository's graph and finish the purge (Deliverable 2
step 2c and its 2b/2b'/2q/2z seams, #7766 follow-up). Lane branch
`drive/8659-graph-retract`. The operator trigger is unchanged: this lane
builds the tested machinery the staged runner will drive (arbiter verdict:
graph phase between fence and purge, tombstone-captured ids, staged seams;
verdict recorded in the lane handoff).

## Query response discipline (cypher-query-rigor)

- G5f anchor: `(f:File)` by `repo_id`, `file_repo_id` RANGE index (new).
  Input: one repo id. Output: its projector-owned files. Largest fan-out:
  files per repository (bounded by the checkout).
- G5p anchor: `(p:Parameter)` by `path` prefix, `parameter_path` RANGE
  index (new). Input: one root prefix plus nested prefixes. Output:
  Parameters under the root minus nested checkouts.
- G6 anchor: `(r:Repository)` by `id`, existing UNIQUE constraint. Input:
  one repo id. Output: one node. No index change needed.
- Backend: pinned Neo4j (local `neo4j` image ballot), NornicDB covered by
  the shared `file_repo_id` index and its existing
  `nornicdb_parameter_path_lookup` (kept, not duplicated: a NornicDB
  `parameter_path` entry would re-backfill every existing store).
- Verification: EXPLAIN before/after on the pinned backend (below), live
  retract test, live sweep test, live Postgres tests.

## Performance Evidence:

Prove-theory-first EXPLAIN shims ran on live Neo4j before any statement
was built (same backend and data both runs):

| Statement | Before | After |
|---|---|---|
| G5f File retract read half | `NodeByLabelScan f:File` + Filter | `NodeIndexSeek RANGE f:File(repo_id)` + Filter |
| G5p Parameter retract read half | `NodeByLabelScan p:Parameter` + Filter | `NodeIndexSeekByRange RANGE p:Parameter(path) STARTS WITH $root` + Filter |
| G6 demote read half | `NodeUniqueIndexSeek UNIQUE r:Repository(id)` | unchanged (already anchored) |

The parameterized `$evidence_source` form of G5f plans identically to the
literal form (same RANGE seek, filter after). Equivalence: the indexes
change the access path only; the live retract test asserts the exact
post-state row counts.

Index write-cost bound: `file_repo_id` is maintained per File MERGE, the
same shape as the accepted `function_repo_id` / `directory_repo_id`
retract indexes. `f.repo_id` is immutable per node (a File belongs to one
repository for its whole life), so every write after the first maintains
the value already held. `parameter_path` is Neo4j-only
(`neo4jRetirementLookupIndexes`): it adds the prefix seek where the
composite uniqueness constraint leads with `name` and cannot serve a
path-only predicate. NornicDB keeps its existing
`nornicdb_parameter_path_lookup` for the same label and property, which
already seeks that shape there, so no backend ever holds two indexes on
`Parameter.path` and no existing NornicDB store re-backfills.

Purge scale measurement (AC8, committed as
`TestRetirementPurgeScaleMeasurementLive`, live Postgres): one-transaction
fence+purge at 100 generations / 10k facts and at 3,280 generations /
328k facts on the same container:

- small: fence 3.7ms, purge 35.5ms (0.3ms/gen)
- large: fence 2.7ms, purge 956.7ms (0.3ms/gen)
- scaling: 32.8x generations, 27.0x purge wall (sub-linear)

Verdict: keep the one-transaction 2d at observed scale. A 957ms purge of
the largest observed retirement needs no batching seam; re-measure (one
command: the test above) if a retirement exceeds ~10x this shape. Walls
are same-host relative (shared host); the scaling ratio is the claim.

## Observability Evidence:

No-Observability-Change:
`eshu_dp_repository_retirements_total{outcome}`,
`eshu_dp_repository_retirements_purged_total`. This lane adds stores,
statement builders, and closed tables but no emitting stage: the trigger
funnel counters above still diagnose every operator path, and
`scripts/verify-telemetry-coverage.sh` passes with no new untracked
stages.

The 3am readback for the graph phase is queryable state plus specified
log keys, all built here:

- `CountPreBarrierLeases` returns the blocking count with the oldest
  lease named (domain, partition, owner, claim epoch) for the 2b' wait
  log line.
- `ReassertScopesQuiesced` returns the four 2q counts (live generations,
  actionable reducer rows, live reducer/projector leases) for the refuse
  log line.
- Tombstone columns `intent_barrier_at`, `graph_repo_root`,
  `graph_scope_ids`, `graph_nodes_deleted`,
  `graph_relationships_deleted`, and the 2z `failure_class` /
  `failure_details` carry the phase state; the retire route doc's new
  inspection note names them.

## Per-AC proof

- AC1 (no projector-owned node after complete): live
  `TestRetirementGraphRetractLive` runs the exact P10 forms
  (`MATCH (n) WHERE n.repo_id=$id RETURN labels(n), count(n)` zero rows
  for three retired ids; edge census one foreign-owned incoming edge).
- AC2 (demote + sweep reap after TTL, injected clock): live
  `TestRetirementDemotedRepositoryReapedAfterTTLWithInjectedClock`
  (mark at T0, survive at T0+30m, reaped at T0+61m with a 1h TTL, no
  sleeps; canonical node never marked).
- AC3 (G5p + shared-root skip+record): builder test pins the NOT term
  and fail-closed root; live test detects the shared root through
  `RetirementSharedRootQuery` (parent shape: the retired root nested
  under the live repository's path, the only shape the
  `repository_path` uniqueness constraint admits; the equal-path arm is
  unit-pinned), skips G5p, and asserts both Parameters survive.
- AC4 (Files without Repository node): live test retracts R3's file,
  which G5 cannot reach; builder test pins "no Repository anchor".
- AC5 (intents deleted before retraction + pre-barrier lease wait):
  `TestRetirementBarrierWaitsPreBarrierLeaseLive` (RED first),
  heartbeat-epoch and expiry tests; lease-domain firewall
  `TestRetirementLeaseDomainsMatchLeaseWriters` enumerates every
  non-test `ClaimPartitionLease` caller from source.
- AC6 (reducer rows cannot reopen graph writes): `ReassertScopesQuiesced`
  (2q) with live reopened-row detection plus the predicate pin test
  against the fence; chain 2q (this lane) -> 2b/2b' (this lane) -> 2c
  (this lane) -> 2d purge delete + terminal gate (shipped) -> 2d'
  settle (runner work, specified in the handoff).
- AC7 (every Evidence-listed table dispositioned):
  `TestRetirementTableCensusClassifiesEveryScopeKeyedTable` (live join
  against the migrated schema, both directions) plus the seeded-violation
  RED/GREEN pair.
- AC8 (3,280-generation measurement vs ADR 2248 bounds): above.
- AC9 (residue census before complete, fail-closed): anchored
  `BuildRetirementResidueCensus` arms (all seek; entity labels beyond
  File/Directory/Function rely on G5, proven by the exact-P10 live
  acceptance, recorded here as the known boundary) plus the Postgres
  census join; runner wiring (refuse-and-remark on nonzero) is runner
  work.
- AC10 (migration adds `graph_repo_root`): migration 208 (+
  `graph_scope_ids` per the arbiter's D2) and migration 209
  (`claimed_at`), both applied live (227/227) with manifest, digest, and
  parity pins.
