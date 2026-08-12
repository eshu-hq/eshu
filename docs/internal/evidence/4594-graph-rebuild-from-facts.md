# Graph rebuild from facts: what a wipe-and-rebuild actually restores

Evidence for #4594. Run on 2026-08-12 with
`scripts/verify-graph-rebuild-from-facts.sh` against the local Compose stack.

The headline: the rebuild command works, the queue drains clean, and the graph
comes back **incomplete**. Source-local structure returns exactly. Twelve of
seventeen reducer materialization domains never re-run, so the graph they build
stays missing. An operator following the disaster-recovery runbook today gets a
graph that is short 73 nodes and 384 relationships out of 2,504 and 3,289.

That is a finding about the product, not about the script. The script asserts the
contract #4594 asks for, and the contract does not hold yet.

## What was run

```text
scripts/verify-graph-rebuild-from-facts.sh
  ESHU_KEEP_COMPOSE_STACK=true ESHU_DR_SKIP_INTERRUPT=true
```

Sequence: index the corpus, snapshot the graph, stop the writers, delete the
`nornicdb_data` volume, reapply graph schema with
`ESHU_GRAPH_SCHEMA_FORCE_REAPPLY=true`, restart the writers, `POST
/api/v0/admin/recover-generations` with `all_scopes: true`, drain to terminal,
snapshot again.

## Conditions

| Field | Value |
| --- | --- |
| Corpus | `tests/fixtures/ecosystems`, 1.4 MB, 361 files |
| Scopes | 67 active, each with an active generation |
| Facts | 3,866 `fact_records` |
| Graph before wipe | 2,504 nodes, 3,289 relationships |
| Backend | NornicDB, image `eshu-nornicdb-pr290:3722b483c02c` |
| Postgres | the Compose default, preserved across the wipe |
| Machine | Apple M4 Pro, 12 logical CPUs, 64 GiB, macOS 26.5.2 |
| Load average at drain completion | 13.20 (1 min) |
| Eshu commit | `056249265` |

The corpus is a fixture pack, well below the smallest scale-lab slot. Treat every
number here as a proof that the mechanism runs, not as a deployment-scale
rebuild time.

## Timing

```text
graph_rebuild_seconds=26 (0m26s)
  scopes_enqueued=67 fact_records=3866 nodes=2504
  load_at_finish=13.20 17.66 21.84
```

The clock runs from the `recover-generations` response to the moment
`fact_work_items` holds nothing `pending`, `claimed`, or `running` and nothing
outside `succeeded`/`superseded`. Wipe and schema reapply sit outside it.

Load was 13.2 on 12 CPUs because a gate was running in a sibling worktree, so 26
seconds is an upper bound rather than a clean figure. For an operator sizing a
recovery objective that is the safe direction to be wrong in, but it is not a
baseline anyone should compare a future run against.

## What came back, and what did not

Repository (67), File (351), Function (801), Class (211), and Directory (85) all
returned at exactly their pre-wipe counts. So did most of the source-local
structure.

Missing nodes, 73 in total:

| Label | Before | After |
| --- | ---: | ---: |
| `Module` | 228 | 180 |
| `EvidenceArtifact` | 14 | 0 |
| `Variable` | 5 | 0 |
| `Platform` | 5 | 3 |
| `CodeownerTeam` | 2 | 0 |
| `CloudAction` | 1 | 0 |
| `Environment` | 1 | 0 |

Missing relationships, 384 in total. The large ones:

| Type | Before | After |
| --- | ---: | ---: |
| `CALLS` | 116 | 0 |
| `CONTAINS` | 2,368 | 2,288 |
| `INHERITS` | 36 | 0 |
| `REFERENCES` | 33 | 0 |
| `EVIDENCES_REPOSITORY_RELATIONSHIP` | 14 | 0 |
| `HAS_DEPLOYMENT_EVIDENCE` | 14 | 0 |
| `DEPLOYMENT_SOURCE` | 12 | 0 |
| `DEFINES` | 11 | 0 |

Sixteen smaller families went to zero as well, among them
`CORRELATES_DEPLOYABLE_UNIT`, `DECLARES_CODEOWNER`, `PROVISIONS_PLATFORM`,
`QUERIES_TABLE`, `HAS_COLUMN`, `EXECUTES`, and `INVOKES_CLOUD_ACTION`.

## Why: reducer domains that never re-run

`refinalize` inserts `stage='projector', domain='source_local'` rows. Those
re-run and rebuild source-local structure. Reducer work is a separate matter, and
the rebuild only reaches part of it.

### Correction: `updated_at` was the wrong signal

The classification below was originally made by grouping reducer rows by domain
and asking whether the latest `updated_at` fell inside the rebuild window. That
only proves a row was touched. A re-run increments `attempt_count`, so that is
the column to read, and reading it changes the answer.

Re-measured on a second run of the same script (2026-08-12, Eshu `880d2e7d5`,
same corpus, idle machine), after the rebuild drained:

```text
SELECT attempt_count, count(*) FROM fact_work_items WHERE stage='reducer' GROUP BY 1;
 1 | 1301
```

`attempt_count` is 1 on all 1,301 reducer rows, including all 259
`workload_materialization` rows. **No pre-existing reducer row was re-driven by
the rebuild — not five domains, not four, zero.**

What the five domains actually did was gain *new* rows. Comparing the catalog
before and after the rebuild:

| Domain | Rows before | New rows during rebuild |
| --- | ---: | ---: |
| `eshu_search_document` | 0 | 133 |
| `deployment_mapping` | 67 | 67 |
| `workload_identity` | 67 | 67 |
| `workload_materialization` | 192 | 67 |
| `service_catalog_correlation` | 1 | 1 |

Those rows escaped `ON CONFLICT (work_item_id) DO NOTHING` because their ids
differ. Same scope, same generation, different id:

```text
original: ..._deployment_mapping_deployment_sql_comprehensive
rebuild:  ..._deployment_mapping
```

The entity-key suffix is absent on the re-derived intent. That is its own defect
— a projection run should derive the same intent id from the same facts — and it
is the only reason those five domains produced any work at all.

Three more domains muddy the original method from the other side.
`code_import_repo_edge`, `container_image_identity` and
`deployable_unit_correlation` had `updated_at` bumped during the rebuild window
with no new rows and no `attempt_count` increase. An `updated_at` reading would
call them re-run; they were not.

So the headline number survives at the domain level — five domains did execute
handlers during the rebuild — but nothing in the original method supported it,
and the mechanism is "new rows under unstable ids", not "the domain re-ran".

The two-column table below is kept because the mapping from domain to missing
graph structure is correct and is what the fix was built against. Read the left
column as "produced some work", not "re-ran".

| Re-ran during the rebuild | Did not |
| --- | --- |
| `deployment_mapping` | `code_call_materialization` |
| `eshu_search_document` | `code_import_repo_edge` |
| `service_catalog_correlation` | `codeowners_ownership` |
| `workload_identity` | `container_image_identity` |
| `workload_materialization` | `crossplane_satisfied_by_materialization` |
| | `deployable_unit_correlation` |
| | `inheritance_materialization` |
| | `platform_infra_materialization` |
| | `semantic_entity_materialization` |
| | `shell_exec_materialization` |
| | `sql_relationship_materialization` |
| | `submodule_pin` |

The twelve on the right kept their `succeeded` rows from the original indexing
run, with `updated_at` around `15:34:2x`, roughly forty seconds before the
rebuild began. They were never re-enqueued, so their handlers never ran, so the
graph structure they own was never rebuilt.

The mapping to the missing structure is one-to-one:

- `code_call_materialization` owns `CALLS` — 116 edges, all absent.
- `inheritance_materialization` owns `INHERITS` — 36 edges, all absent.
- `codeowners_ownership` owns `CodeownerTeam` and `DECLARES_CODEOWNER` — absent.
- `platform_infra_materialization` owns `Platform` and `PROVISIONS_PLATFORM` —
  `Platform` down 2, edges absent.
- `deployable_unit_correlation` owns `CORRELATES_DEPLOYABLE_UNIT` — absent.
- `semantic_entity_materialization` owns `EvidenceArtifact` and
  `EVIDENCES_REPOSITORY_RELATIONSHIP` — absent.
- `shell_exec_materialization` owns `EXECUTES`, `CloudAction`, and
  `INVOKES_CLOUD_ACTION` — absent.
- `sql_relationship_materialization` owns `QUERIES_TABLE`, `HAS_COLUMN`,
  `REFERENCES_TABLE`, `READS_FROM`, `WRITES_TO` — absent.

Every missing family belongs to a domain that did not re-run. No missing family
belongs to a domain that did.

## The fix: reset the dedup state a rebuild has to get past

Three pieces of Postgres state outlive a graph wipe, and each one tells the
pipeline the work is already done.

1. **Succeeded reducer work items.** Re-projection re-derives the intents, but
   `ON CONFLICT (work_item_id) DO NOTHING` drops each one against its succeeded
   row.
2. **Shared projection intents with `completed_at` set.** The partition workers
   drain only `completed_at IS NULL`, and the upsert's `COALESCE` never reopens
   a completed row.
3. **Graph projection phase rows.** They assert canonical nodes are committed.
   After a wipe that is false, and the edge Cypher is `MATCH`-only, so work
   admitted on that stale answer matches nothing, writes nothing, and still acks
   `succeeded`.

Both dedup guards are left byte-identical — `git diff origin/main` shows no
change to `reducer_queue.go` or `shared_intents_upsert.go`. They are correct for
ordinary operation, where every shard drain, reopen, and retry depends on
completed work staying completed. The reset instead lives in
`RecoveryStore.RefinalizeScopeProjections`, scoped to the generations that
refinalize is rebuilding, in the same transaction as the projector re-enqueue.
All four statements render one shared affected-generation subquery, so they
cannot disagree about which scopes are in scope.

The reducer rows are **deleted**, not reset to `pending`. Two reasons. A pending
row is claimable immediately, before the projector re-run that owns its inputs
has committed anything, so a handler could write into a wiped graph and ack
succeeded — the same silent incompleteness the change exists to fix. And a blind
status rewrite fails outright:

```text
ERROR:  new row for relation "fact_work_items" violates check constraint
        "fact_work_items_container_image_identity_v2_status_check"
```

That constraint ties `status` to `container_image_identity_v2_authorized_status`,
a coupled column family a status-only reset does not carry. Deleting restores
first-ingest causality: the work exists again only once its producer has run.

The delete is scoped to `succeeded`. Claimed and running rows hold live leases a
rebuild must not yank; `dead_letter` and `failed` rows belong to the replay
endpoint and contributed nothing to the pre-wipe graph.

### The premise this rests on, checked

Deleting is only safe if re-projection re-derives the complete reducer catalog.
If any domain's intents came from somewhere else, deleting would lose it with
nothing to re-insert — strictly worse than the bug. Measured directly: delete
every succeeded reducer row for the active generations, refinalize, drain, and
compare the catalog per domain. All seventeen domains came back, sixteen at
exactly their prior row count. Only `eshu_search_document` differed (133 → 66),
and it accumulates per run rather than converging on a fixed count; it owns no
graph nodes or edges in the missing set.

## What the fix restores

| Metric | Pre-wipe | Before the fix | After the fix |
| --- | ---: | ---: | ---: |
| Nodes | 2,504 | 2,431 | 2,499 |
| Relationships | 3,289 | 2,905 | 3,263 |
| `CALLS` | 116 | 0 | 115 |
| `INHERITS` | 36 | 0 | 36 |
| `REFERENCES` | 33 | 0 | 33 |
| `CONTAINS` | 2,368 | 2,288 | 2,368 |

The missing set drops from 73 nodes and 384 relationships to 5 and 26.

## Cost

Clearing the dedup state takes a rebuild from re-driving a few hundred rows to
re-driving the whole catalog.

| Measure | Before the fix | After the fix |
| --- | ---: | ---: |
| `graph_rebuild_seconds` | 15 s | 25 s |
| Reducer rows re-driven | 335 (new rows only) | 1,034 (deleted and re-derived) |
| Shared intents re-drained | 0 | 586 |
| Readiness phase rows cleared | 0 | 609 |
| Load average at completion | 2.41 | 5.19 |

Read that as indicative, not controlled. The two runs sat at different machine
loads (2.41 against 5.19 on 12 CPUs), so some of the 10-second difference is the
machine rather than the change. The direction is not in doubt — roughly three
times the reducer work and 586 shared intents that previously did nothing — but
anyone quoting a precise multiplier from these two numbers is over-reading them.

The cost lands only on refinalize. Ordinary indexing, shard drains, reopens, and
retries are untouched, which is the whole reason the reset is scoped to the
recovery path instead of the guards.

## Reproducing

```bash
scripts/verify-graph-rebuild-from-facts.sh
```

It exits non-zero on the count mismatch above. That is the current true state.

Performance Evidence: `graph_rebuild_seconds=26 (0m26s)` for 67 scopes and 3,866
`fact_records`, measured between the `recover-generations` response and queue
terminal on NornicDB `eshu-nornicdb-pr290:3722b483c02c`, Apple M4 Pro / 12 CPU /
64 GiB, load 13.20 at completion, queue terminal with zero retrying and zero
dead-letter rows. No prior measurement exists for this operation, so this is a
first data point rather than a before/after; it is an upper bound because of the
concurrent load. The path it exercises is a bounded INSERT of one work item per
active scope (67 rows here) plus the existing projector drain, and it adds no
query to any read path.

Observability Evidence: the rebuild is visible through the existing projector
queue metrics and `GET /api/v0/index-status`, and every request is recorded in
the `admin_replay_requests` ledger with its reason and idempotency key. The one
new signal is the `bootstrap.graph.force_reapply` warning log, which fires when
an operator overrides the graph-schema marker; the run above shows it followed by
`bootstrap.graph.applied`, which is how you tell the override took effect rather
than being silently ignored.
