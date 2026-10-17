# Deployable-Unit Reopen Changed-Evidence Gate

Issue #8283. Every deferred-maintenance pass re-opened every succeeded
`deployable_unit_correlation` item on its scope's replay floor, and in steady
state almost every replay reproduced the same graph. A succeeded item is now
re-opened only when something it read can have changed.

## Decision record

- **Build order.** The #8283 design comment of 2026-10-16 22:11Z replaced the
  #8082 close-comment order (readiness-aware reopen first). The
  changed-evidence gate is built alone, for `deployable_unit_correlation`
  only. A listing filter on the readiness gates is rejected: the fence and
  canonical gates are corpus-wide, and the same transaction re-opens
  `deployment_mapping` first. The readiness-aware reopen goes to a separate
  issue, opened only if the deferral share stays above 25 % with this gate
  deployed.
- **Fingerprint basis.** An arbiter ruling amended the 22:11Z generation-id
  basis. The fingerprint is a digest of the sorted `resolved_id` set the
  handler's fused read selects. A generation-id basis misses rows appended
  under an already-active generation. A re-run `deployment_mapping` writes
  into the relationship generation of its scope generation (same id) through
  `insertResolvedSQL` (`ON CONFLICT DO NOTHING`), and
  `activateResolutionGenerationSQL` (`ON CONFLICT DO UPDATE SET status =
  'active'`) leaves that id in place.
- **Digest.** The ruling named md5. The repo gate
  `TestProductionSQLHasNoMD5Calls` (#6753) rejects md5 in production SQL,
  because OpenSSL FIPS rejects it. The digest is
  `encode(sha256(convert_to(...)), 'hex')`, the same form as
  `changed_since_sql.go`.
- **Same-transaction hazard: option (i), a one-pass bound.** A
  `deployment_mapping` re-opened in the same transaction appends its rows
  after the gate ran, so the next pass re-opens an item that changed that
  way. Today's unconditional reopen has the same window whenever the item
  runs first. A pass runs only after a drain that committed something
  (`RunDeferredRelationshipMaintenanceAfterShardDrain`). If no later pass
  runs, the item keeps its old graph until one does. The exact
  completion-driven wake is the follow-up #8294.

- **Write-time loss is repaired by verification; later loss is not
  detected.** The fingerprint covers the resolved Postgres input, not the
  graph. The canonical upsert MATCHes both endpoint `Repository` nodes, so a
  write whose endpoint commits later binds nothing and still reports success.
  The CI determinism cell showed exactly that (see "Verified success" below).
  The handler therefore records its fingerprint only after it reads every
  admitted edge back. A loss at write time fails the run and the item stays
  open. A loss after a verified success is the remaining exposure: if another
  writer later removes a `CORRELATES_DEPLOYABLE_UNIT` edge or its target node,
  an item whose input is unchanged is not re-opened until the input changes
  or the logic version is bumped. Before the gate, every wave re-merged those
  edges. #8316 tracks that class. #8315 tracks the NornicDB semantics of the
  pre-write target probe, whose multi-clause shape the verification probe
  does not reuse. Graph-side truth otherwise stays with its owners: the
  canonical-node quiescence gate keeps the handler's writes behind
  repository projection, and dual-write reconciliation
  (`go/internal/storage/cypher/generation_reconciliation.go`) retracts edges
  whose denormalized `generation_id` or `resolved_id` drifted from Postgres.
  To force a re-run, delete the item's fingerprint row: a missing row always
  re-opens on the next pass.
- **A logic change needs a version bump.** Before the gate, a deployed logic
  fix reached every item in the next wave. Now an unchanged item keeps its old
  output until `DeployableUnitCorrelationLogicVersion` is bumped. The bump rule
  sits on the handler type and names the code path it covers: the handler,
  candidate extraction, `ExtractDeployableUnitCorrelationRows` and
  `applyResolvedDeploymentSources`, the rule packs and engine, the
  admission-decision mapping and the edge row shape. No source-hash guard
  was added. It cannot tell an output-changing edit from a comment or a
  refactor, so it would teach reviewers to re-pin the hash, and a guard
  satisfied by its own data is not proof (AGENTS evidence rules).
- **resolved_id identity.** The fingerprint hashes `resolved_id` values, so
  it inherits their identity rules. #8299 tracks a latent `resolved_id`
  collision (the id hashes the batch ordinal, not the repository ids); a fix
  that changes how ids are minted changes every fingerprint once, which only
  re-opens items.

## Shape

- **Side table.** Migration 193 adds `deployable_unit_reopen_fingerprints`:
  one row per succeeded item with `repo_ids`, `input_fingerprint`,
  `logic_version`, `claimed_at` and `recorded_at`. It has a cascading FK to
  `fact_work_items`, and the PK on `work_item_id` serves the cascade.
- **Fused read.** `GetFingerprintedResolvedRelationshipsForReposWithCorpusFence`
  is derived from `listResolvedByReposWithCorpusFenceSQL` by markers. Its
  by-repos rows move into a MATERIALIZED `input_rows` CTE that also carries
  `resolved_id`. The fingerprint is `backfill.ResolvedInputDigestSQL` over
  those same rows, in the same statement snapshot.
- **Success write.** The handler records the read-time value after every
  graph write. The write is fenced on the exact claim (`work_item_id`,
  `last_attempt_at`, a live `claim_until`, `FOR NO KEY UPDATE`) and keeps the
  newest claim.
- **Gated listing.** `backfill.ListDeployableUnitReopenCandidatesQuery` is
  the floored listing plus a LEFT JOIN on the fingerprint table and an
  `input_changed` column. It re-opens an item when:
  - the fingerprint row is missing;
  - the logic version moved;
  - a recompute through `ResolvedInputFingerprintSQL` differs.
- **Where the gate applies.** The whole pass, the #7584 partition-scoped pass
  (through a derived listing) and `eshu-bootstrap-index` all use the gated
  listing. The other three correlation domains are unchanged.
- **Zero candidates.** A zero-candidate item records `'{}'` and the
  `empty-resolved-input` sentinel, so a change in resolved input never
  re-opens it.

## Measurements

QA environment, read-only, while it was re-ingesting from scratch. These are
not a deployed before and after.

- **QA wave 2026-10-16 23:17Z.** 786 items re-opened.
  `eshu_dp_reducer_executions_total{deployable_unit_correlation,status=deferred}`
  stayed at 2785 for the whole wave, so there were 0 deferrals. The
  run-duration sum went from 1186.9 s to 1365.9 s (about 179 handler-seconds),
  over about 3 minutes of wall time (23:17:00 to 23:19:54).
- **The 2785 lifetime deferrals** came from the initial ingest. Every
  relationship generation was created between 22:10Z and 22:20Z, and none
  since, although two waves each re-opened 55 `deployment_mapping` items.
- **Readiness filter.** It would have skipped 0 of 786 on QA. The ruling's
  production figure is 0 of 799.

Performance Evidence: local PostgreSQL 18.6, on the #6740 seed (5000 active
scopes, 10000 relationship generations, 200000 resolved rows, 20000 succeeded
work items). Each run used 10 repository ids, jit off, three warmups and nine
paired runs.

| Statement | Execution (median, range) | Shared hit |
| --- | --- | --- |
| shipped fused read, fence complete | 5.419 ms (5.390-5.483) | 660 |
| fingerprinted fused read, fence complete | 5.624 ms (5.587-5.708) | 660 |
| shipped fused read, fence incomplete | 3.746 ms (3.744-3.752) | 580 |
| fingerprinted fused read, fence incomplete | 3.933 ms (3.927-3.943) | 580 |

- **Cost of the fingerprint.** +0.205 ms with the fence complete and
  +0.187 ms with it incomplete. It was slower in 9 of 9 pairs each, with
  identical buffers.
- **Rejected InitPlan form.** It re-read the rows: 6.889 ms and 1038 buffers
  (+1.25 ms, +378).
- **Plan.** The by-repos access path is kept: a BitmapOr over
  `resolved_relationships_source_repo_idx` and `_target_repo_idx`,
  hash-joined to `relationship_generations`, now feeding the CTE.

Listing, same seed. Every item has a fingerprint row, and none changed:

| Listing | 5000 items | 786 items (QA scale) |
| --- | --- | --- |
| ungated (`ListSucceededReducerWorkItemsByDomainQuery`) | 35.7 ms, 10361 hit | 15.4 ms, 10361 hit |
| gated (`ListDeployableUnitReopenCandidatesQuery`) | 259.2 ms, 219378 hit | 50.3 ms, 43439 hit |

- **Per item.** The fingerprint SubPlan costs 0.039 ms, and the hash on
  `relationship_generations` is built once. The gated listing returned
  `input_changed = false` for 5000 of 5000.
- **Trade at QA scale.** +34.9 ms of listing replaces 786 reopen round trips
  plus 786 reducer executions (about 179 handler-seconds in the QA wave).
- **Trade at 5000 items.** +223.5 ms replaces 5000 of each.

Write costs:

- **Success write.** One claim-fenced upsert per success, 0.134-0.143 ms each.
  800 upserts took 0.20 s of wall time in one session. In steady state the
  gate stops the re-runs, so most waves write almost none.
- **Retention cascade.** Measured on the production bootstrap schema at
  base 271d54e54 (after #7420 dropped the key-sidecar FKs, which sat on
  `fact_records`; `fact_work_items` still has exactly two cascading FK
  children, `fact_replay_events` and this table). Seed: 400000 work items
  over 4000 generations, 4000 fingerprint rows. Deleting 50 generations
  (5000 cascaded work items), 8 paired runs:
  - without the table, 48.99-50.18 ms; with it, 78.43-79.87 ms;
  - delta +28.7 to +30.8 ms (median 29.4), about 5.9 µs per deleted work item
    and about 101 µs per pruned generation at QA's 17.2 work items per
    generation;
  - the FK's own trigger is 13.7 ms over 5000 calls (2.7 µs each); the rest
    is AFTER-trigger queue overhead and the slower work-item cascade step.
  An earlier measurement on a reduced synthetic schema gave +16.5 ms; the
  bootstrap-schema figure above supersedes it.
- **Retention row count.** `generationRetentionRowCountsQuery` does not count
  the new grandchild, so it under-counts by at most one row per pruned item.
  The follow-up is #8300.

## Concurrency

The gate takes no lock. It reads the fingerprint table, and the reopen UPDATE
is the same per-row `ReopenSucceeded` as before. The success write locks the
one work-item row, then the fingerprint row. A retention cascade takes them in
the same order, so the two cannot close a cycle.

Live proofs in `go/internal/storage/postgres` (isolated schema, PostgreSQL
18.6), also run under `-race`:

- **Concurrent passes.** Four maintenance passes on separate connections
  finished with no error and the exact changed/unchanged split.
- **Write versus reclaim.** In 40 rounds, a stale claim's write ran before,
  after and concurrently with a reclaim plus the fresh write. Every round
  ended on the fresh claim's row, and the fresh write was never rejected.
- **Write versus cascade.** In 40 rounds of a write racing the retention
  cascade, there were 0 deadlocks and no orphan row.

## Mutations

Each mutation was applied to production SQL, run, then restored:

| Mutation | Tests that fail |
| --- | --- |
| skip whenever a row exists | R1, R2, R4, R5, new matching scope, G3, G2, both C2 cases |
| missing row skips | R3 |
| generation-id basis | R2, G3, C2 rows appended, G2 |
| ignore logic version | R4 |
| recompute at write time | both C2 cases (stored value is not the read-time value) |
| drop the claim fence | C1 stale-claim write |
| drop the monotonic guard | older-token write |
| drop both guards | the reclaim race |
| listing drops the target match | D1 differential |
| targeted pass ungated | targeted-pass gate |

On the ungated tree, G1 failed with
`duc-a status after maintenance = "pending", want "succeeded"` and
`eshu_dp_correlation_reopen_skipped_total{deployable_unit_correlation} = 0, want 2`.

Observability Evidence: the gate adds three signals.
`eshu_dp_correlation_reopen_skipped_total{domain,reason="input_unchanged"}`
counts skips on the whole pass. `skipped_unchanged` is added to the
`relationship.backfill.reopen` block span and log line (whole and
partition-scoped passes) and to the `reducer_work_items_reopened` line.
`eshu_dp_correlation_reopened_total` keeps its meaning: items actually
re-opened. A wave whose skipped count equals its candidate count saw no
changed input. A rejected success write fails the run with
`ErrExecutionClaimRejected`, on the fenced-claim path the reducer service
already handles.

## Verified success

Root-Cause Evidence: the CI determinism cell failed deterministically. Its
artifact logs show the app edge write at 07:09:07.198 with `executed_rows=1`,
while `repo_dependency` created the deploy `Repository` stub at 07:09:09.589.
The canonical upsert MATCHes both endpoints
(`storage/cypher/canonical_deployable_unit_edges.go`), so the MERGE wrote
nothing, the handler succeeded and recorded its fingerprint, and the gate
skipped every later pass (`skipped_unchanged=4`). Main passed that cell only
through the unconditional reopen.

The fix (arbiter ruling on #8312): after `WriteEdges` and before the
admission decisions, `EdgeVerifier` reads every admitted edge back. The edge
writer implements it with one single-pattern probe per distinct pair:

```cypher
MATCH (:Repository {id: $source_repo_id})-[rel:CORRELATES_DEPLOYABLE_UNIT]->(:Repository {id: $target_repo_id})
WHERE rel.evidence_source = $evidence_source
RETURN 1 LIMIT 1
```

On a miss, two `MATCH (:Repository {id: $repo_id}) RETURN 1 LIMIT 1` probes
classify it. An absent endpoint is the non-counting
`shared_edge_target_not_ready` class, under the #7268 elapsed bound. Both
endpoints present is the new counting class
`deployable_unit_edge_write_unverified`, which dead-letters at the budget. A
probe fault counts. An executor without probes leaves the run unverified: it
succeeds and records no fingerprint. No logic-version bump: verification
changes no written edge or decision.

Performance Evidence: `PROFILE` on the pinned Neo4j image
(`neo4j:2026-community@sha256:eabfbb04…`, native arm64, auth off), seeded
with 20000 `Repository` nodes under the `repository_id` uniqueness
constraint and 10000 edges: 8000 one-to-one pairs plus 2000 edges into one
hot deploy repository. Three warm runs per case, same plan every run.

| Case | Plan | DB hits | Rows |
| --- | --- | --- | --- |
| edge present, ordinary pair | `NodeUniqueIndexSeek` source, `Expand(All)`, `Filter` | 7 | 1 |
| edge present, target in-degree 2000 | same | 7 | 1 |
| edge absent, target in-degree 2000 | same | 5 | 0 |
| source with no out-edges | same | 4 | 0 |
| endpoint node probe | `NodeUniqueIndexSeek` | 2 | 1 |

The probe anchors on the source's unique id and expands its out-edges, so
its cost follows the source's out-degree (one per deployment repository),
not the target's in-degree. Each admitted edge costs one probe, about 1 ms
round trip through `cypher-shell`; a miss adds two node probes. NornicDB was
NOT_CHECKED locally: the pinned NornicDB image is amd64-only and fails with
`exec format error` on this aarch64 host. The live proof below runs on
NornicDB in the live-backend CI job.

Live proof: `deployable_unit_verify_live_test.go` (class `ci`, both
backends) seeds run-unique repositories and checks target absent, both
present with no edge, and an edge written by the real `WriteEdges` path. An
edge owned by another evidence source reads as missing. Neo4j local run:
3 of 3 PASS in 1.6 s. Removing the `evidence_source` filter fails the
edge-present case.

Mutations, each applied, run, and restored:

| Mutation | Fails |
| --- | --- |
| skip verification | records-only-after-verified, both unverified classes, no-verifier, unavailable |
| record regardless of the verdict | no-verifier, unavailable |
| verify after the record | both unverified classes, no-verifier, unavailable |
| writer treats `missing_target` as present | missing target, missing source |
| handler treats `missing_target` as success | unverified `missing_target` |

Observability Evidence: `eshu_dp_deployable_unit_edge_verify_total{outcome}`
(`present`, `missing_target`, `missing_edge`, `probe_error`), the `deployable
unit edge not verified after write, the run records no reopen fingerprint`
WARN with `verdict`, `source_repo_id`, `target_repo_id` and
`sample_intent_id`, and `deployable_unit_edge_verify_outcome`,
`deployable_unit_edges_verified` and `deployable_unit_edges_probed` on the
handler span.

Determinism cell, rerun locally on the pinned Neo4j image with
`ESHU_GRAPH_BACKEND=neo4j` (the standalone `deployable_unit_edges` cell of
`scripts/verify-ifa-determinism.sh`, a fresh stack): PASS, `ifa assert-edges:
domain=deployable_unit_edges expected=1 edges matched exactly`, 0 residual
work items, all four items succeeded with fingerprint rows, 140 s cell wall
time. In this sample the app item ran after the deploy stub existed, so no
verification miss fired: the run shows convergence with the gate, not the
race. The race path is covered by the hermetic handler tests and the live
probe proof. The NornicDB cell runs in CI; it was NOT_CHECKED locally for the
image reason above.

## NOT_CHECKED

- **Deployed before and after.** Wave CPU-seconds, wall time,
  skipped-unchanged per wave, `eshu_dp_correlation_reopened_total` per domain,
  and the forced-change canary on a deployed corpus all wait on a deployed
  image.
- **Over-admit rate of the shipped basis.** How often a deployed wave
  re-opens an item whose `resolved_id` set changed but whose edges did not
  (an appended row that matches the item's repositories without changing its
  admitted edges). Not measured; it needs a deployed wave with per-item output
  comparison.
- **Golden-corpus gate.** Not run against the gate. If it shows the
  deployable-unit edge set changed, the completion-driven wake (#8294) comes
  into this work.
