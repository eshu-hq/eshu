# #5554 shared-projection refresh-fence proof

## Contract and theory

The SQL-relationship refresh fence must not carry completion across
generations. An edge may write only when the refresh partition for its exact
`(scope_id, acceptance_unit_id, source_run_id, generation_id, partition_key,
projection_domain)` has completed.

The preliminary hostile review disproved the issue's proposed
same-generation "re-fired row" mechanism: `BuildSharedProjectionIntent`
includes generation and edge identity in a deterministic intent ID, while
`SharedIntentStore.UpsertIntents` preserves an existing `completed_at`. An exact
same-generation retry therefore updates the same completed rows and creates no
pending refresh or edge work. The worker heartbeat also cancels a holder that
cannot renew its partition lease; a second durable intent ID is not the model
for that concurrency path.

The real defect is the generation-blind fence exposed by the #5351 delta
fixture. A throwaway PostgreSQL 18 fixture used the production table and index
shapes with 200,000 background rows plus generation 1 completed and generation
2 pending under a reused source run. The old predicate returned `true`; the
generation-scoped predicate returned `false`. After generation 2 completed, the
new predicate returned `true`.

The live Postgres regression separately constructs production intents and
proves an exact generation-1 retry has the same ID, leaves zero pending rows,
and keeps its completed fence open. Generation 2 has a distinct ID and a closed
fence until its own refresh completes.

## Performance Evidence:

Seven alternating warm `EXPLAIN (ANALYZE, BUFFERS)` samples on the same
200,004-row fixture reported:

- old generation-blind lookup: median `0.038 ms` (range `0.036-0.050 ms`),
  four shared-buffer hits;
- generation-scoped lookup: median `0.042 ms` (range `0.035-0.056 ms`), four
  shared-buffer hits.

Both shapes used one index scan through
`shared_projection_intents_acceptance_lookup_idx`; the new predicate adds only
an in-row `generation_id` filter, with no additional probe, table scan, graph
query, worker-count change, lease change, or serialization.

The rejection threshold was any lost/duplicated edge, an added scan, or a
greater than `0.1 ms` lookup delta. The measured median delta is `0.004 ms` and
the finished worker proof preserves the eight-partition execution shape and
exact output.

## No-Regression Evidence:

The production-shaped cross-generation worker regression failed on unmodified
`main`:

```text
TestProcessPartitionOnceSQLRefreshFenceRedeliveryConverges/next_generation_reuses_source_run
SQL relationship set = [], want exact set [EXECUTES HAS_COLUMN INDEXES MIGRATES QUERIES_TABLE READS_FROM TRIGGERS]
```

After the fix, the following local proofs pass:

```bash
cd go && go test ./internal/reducer ./internal/storage/postgres \
  -run 'TestProcessPartitionOnceSQLRefreshFenceRedeliveryConverges|TestSharedIntentStoreHasCompletedGenerationRefreshFenceHistory|TestSharedIntentStoreHasCompletedAcceptanceUnitSourceRunPartitionDomainIntents' \
  -count=1

cd go && ESHU_SHARED_REFRESH_FENCE_PROOF_DSN="$ESHU_POSTGRES_DSN" \
  go test ./internal/storage/postgres \
  -run '^TestSharedIntentStoreGenerationRefreshFenceAgainstPostgres$' -count=1

cd go && go test -race ./internal/reducer \
  -run '^TestProcessPartitionOnceSQLRefreshFenceRedeliveryConverges$' -count=1

/opt/homebrew/bin/bash scripts/verify-ifa-determinism.sh
```

The concurrency proof keeps eight partitions. It processes every non-refresh
partition before the next generation's refresh partition, then retries them
after the refresh. That is the hostile ordering that lost all seven SQL edge
families before the fix. The exact same-generation retry uses identical
production IDs and stays completed; the cross-generation source-run reuse is
deferred and then converges on the exact seven-edge set. No partition or worker
is globally serialized.

The real-Postgres proof creates an isolated schema and drives
`BuildSharedProjectionIntent`, `UpsertIntents`, `MarkIntentsCompleted`, pending
listing, and the generation fence against PostgreSQL. It verifies exact retry
idempotency and the closed-then-open generation-2 lifecycle without synthetic
intent IDs.

The promoted #5351 live matrix drives the committed SQL baseline cassette,
drains to zero, and asserts the exact seven-edge set. It then drives the gen-2
delta cassette into the same Postgres + NornicDB cell with the reused
`source_run_id`, drains to zero again, and asserts the accumulated exact set:
the unchanged `QUERIES_TABLE` edge survives, `INDEXES` points to
`public.orders`, and the stale `INDEXES -> public.users` edge is absent. All
three worker-count cells matched:

```text
N=1 digest=37406647cfd593e6de18b4d2b8f1501edfec654add22e3d0c9edae680b0c0c74 wall=22s
N=2 digest=37406647cfd593e6de18b4d2b8f1501edfec654add22e3d0c9edae680b0c0c74 wall=14s
N=4 digest=37406647cfd593e6de18b4d2b8f1501edfec654add22e3d0c9edae680b0c0c74 wall=14s
```

Each baseline and delta drain reported zero nonterminal fact work and zero
required nonterminal shared intents. Each pre-delta and post-delta live
`ifa assert-edges` check matched exactly seven edges.

## No-Observability-Change:

No metric, span, log field, status field, queue domain, worker, lease, runtime
knob, or graph query changes. Operators continue to diagnose this path through
`eshu_dp_shared_projection_partition_processing_seconds`,
`eshu_dp_shared_projection_intents_completed_total`,
`eshu_dp_shared_projection_lease_quarantines_total`, the existing per-step
durations, shared-intent backlog rows, and refresh-fence deferred counts.
