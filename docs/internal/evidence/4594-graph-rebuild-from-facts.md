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
| `graph_rebuild_seconds` | 15 s | 20 s and 25 s, two runs |
| Load average at completion | 2.41 | 3.78 and 5.19 |
| Reducer rows re-driven | 335 (new rows only) | ~1,034 (deleted and re-derived) |
| Shared intents re-drained | 0 | ~590 |
| Readiness phase rows cleared | 0 | ~610 |

All three runs indexed the same corpus and reported 3,866 `fact_records`, so the
workload matches. What does not match is the machine: the runs sat at loads of
2.41, 3.78, and 5.19 on 12 CPUs, and rebuild time tracked load as much as it
tracked the change. Read the direction, not a multiplier — roughly three times
the reducer work plus ~590 shared intents that previously did nothing, landing
somewhere around a third to two thirds more wall time on this corpus.

`graph_rebuild_seconds` also under-counts now. It stops when the work queue is
empty, and the shared backlog can keep running for minutes after that (four, in
the sample above). A recovery-time objective should be sized against both queues
draining, not against this number alone.

The cost lands only on refinalize. Ordinary indexing, shard drains, reopens, and
retries are untouched, which is the whole reason the reset is scoped to the
recovery path instead of the guards.

## Does the rebuild re-resolve, or replay the stored generation?

A proposed explanation for the artifact churn was that the rebuild *re-resolves*
rather than replaying the stored active generation. The identity chain makes that
consequential: `CreateGeneration` digests `time.Now().UnixNano()`
(`storage/postgres/relationship_store.go:117`), `ResolvedRelationshipID` embeds
the generation id (`relationships/models.go:231`), and `repoEvidenceArtifactID`
digests the resolved id (`storage/cypher/edge_writer_row_metadata.go:141`). So if
a rebuild minted a fresh relationship generation, every artifact id would change
and an all-new family would land beside the old one.

Tested directly, dumping `relationship_generations` before and after each of four
refinalizes on a wiped stack:

```sql
SELECT generation_id, created_at FROM relationship_generations ORDER BY created_at;
```

| Point | Rows | Diff vs previous |
| --- | ---: | --- |
| after initial index | 67 | — |
| after refinalize #1 | 67 | identical |
| after refinalize #2 | 67 | identical |
| after refinalize #3 | 67 | identical |

**No new generation is minted.** The rebuild replays the stored generation, which
is what it is supposed to do. Two independent facts confirm it: all 67
`relationship_generations.generation_id` values are exactly the ingestion
`active_generation_id`s that refinalize preserves, and the production path is
`ActivateResolutionGeneration(ctx, intent.GenerationID, scopeID)`
(`reducer/platform_materialization.go:123`) whose SQL is
`INSERT ... ON CONFLICT (generation_id) DO UPDATE`. The wall-clock
`CreateGeneration` has no production caller at all.

So the re-resolution theory is falsified for the within-run rebuild, and it does
not explain the 13 → 19 movement. Section 3 localizes that instead.

This does leave a latent trap worth recording: `CreateGeneration` is dead code
whose identity is wall-clock-derived. If anything ever wires it into the rebuild
path, every evidence artifact id in the graph changes on every recovery.

## What the identity assertion found that counts hid

The first full run under the set-difference assertion reported:

```text
Pass 1 (clean rebuild): nodes differ from the pre-wipe snapshot: 10 missing, 6 extra
Pass 1 (clean rebuild): edges differ from the pre-wipe snapshot: 23 missing, 11 extra
```

The count comparison had been reporting this same corpus as a handful of nodes
short. It was not just short. Three `Module` nodes are **wrong**, not missing:

```text
missing:  Module||basic|||ruby        extra:  Module||basic|||python
          Module||inheritance|||ruby          Module||inheritance|||python
          Module||path|||go                   Module||path|||javascript
```

Same module names, different `lang`. Three out, three in, so `Module` counted
228 before and 228 after and the count gate called it identical. A query asking
which Ruby modules a repository defines gets a different answer before and after
a recovery, and nothing in the old assertion could see it.

The single missing `CALLS` edge is now identified too, and it is cross-repo:

```text
content-entity:e_20bfb893f36a|main|/data/repos/orders-api/main.go|...|go
  ||CALLS||
content-entity:e_d1500a4208a0|Identity|/data/repos/lib-common/common.go|...|go
```

A call from `orders-api` into `lib-common`. That is the reducer→reducer ordering
gap in section 2, named: the edge needs both repositories' canonical nodes
committed, and one pass can drain it before the second repository is there. A
same-repo call would not have this problem, which is why exactly one edge of 116
is affected on this corpus.

`graph_rebuild_seconds` also changes meaning in this run: 341 s (5m41s), against
15-25 s previously. Nothing got slower. The measurement now waits for the shared
edge backlog as well as the work queue, so it finally covers the whole rebuild
rather than stopping at the first queue.

## What still does not match, and why

The verifier still exits 1. The remaining difference has three separate causes,
and only one of them is the rebuild.

### 1. The verifier was snapshotting the graph mid-rebuild

`wait_for_queue_terminal` watched `fact_work_items` only. The shared edge backlog
in `shared_projection_intents` drains on its own worker cycle, so the script
declared the rebuild finished while that queue was still working. Sampled every
10 s during a rebuild:

```text
13:32:40 work_active=0 shared_open=21 rels=3277
...                    (unchanged for 4 minutes)
13:36:32 work_active=0 shared_open=21 rels=3277
13:36:42 work_active=0 shared_open=0  rels=3286
```

Nine relationships arrived after the point the script called it done. Fixed here:
the terminal condition now requires both queues empty. This was a defect in the
verifier, not in the rebuild — it was invisible before, because shared intents
never reopened.

### 2. Two of the three thin edge families were the wait, not an ordering bug

`handles_route` and `runs_in` connect code symbols to `:Endpoint` and `:Workload`
nodes that `workload_materialization` commits under a different acceptance unit,
and their intents live in the shared backlog. Tracking them across three runs
shows the wait was the whole story:

| Run | Waits on both queues | `HANDLES_ROUTE` | `RUNS_IN` | `CALLS` |
| --- | --- | --- | --- | --- |
| 1 | no | 0 of 4 | 0 of 4 | 115 of 116 |
| 2 | no, settled by hand | 2 of 4 | 2 of 4 | 115 of 116 |
| 3 | yes | **4 of 4** | **4 of 4** | 115 of 116 |

Once the verifier waits for the shared backlog, both families come back complete
on a single pass. No ordering fix was needed for them.

`CALLS` is different: 115 of 116 in all three runs, reproducible, and unmoved by
the wait. A second refinalize does recover it (115 → 116), so the missing edge is
one whose endpoints were not both present when its intent drained — a real
within-rebuild ordering gap, one edge wide on this corpus. The reset restores
projector→reducer causality but not reducer→reducer ordering, and this is what
that costs here.

### 3. Correction: repeated refinalize converges, it does not inflate

An earlier revision of this document claimed repeated refinalize inflates without
bound — that `EvidenceArtifact` went 13 → 12 → 19 and "a third and fourth run
would keep inflating them". **That was wrong.** It was an extrapolation from two
data points. Running the third and fourth passes falsifies it:

| Pass | `EvidenceArtifact` | Artifact id set vs previous pass |
| --- | ---: | --- |
| initial index | 13 | — |
| rebuild #1 | 13 | — |
| rebuild #2 | 19 | +6 |
| rebuild #3 | 19 | **0 added, 0 removed** |
| rebuild #4 | 19 | **0 added, 0 removed** |

Passes 3 and 4 were compared by exporting the actual `n.id` set, sorted, and
diffing — not by count. Both diffs are empty, so the artifact identities are
stable and content-derived. There is no inflation defect.

What this actually is, is the same under-production as `CALLS` in section 2: one
rebuild pass produces 13 of the 19 artifacts, a second pass completes the set,
and every pass after that is exactly idempotent. Whether 19 or 13 is the
*correct* number is unresolved — the initial index is not a reliable reference
(section 4).

One methodological note, because it nearly produced a false green. The first
version of this check queried `n.uid`, which `EvidenceArtifact` does not have.
That returned 19 nulls before and 19 nulls after, which diffed clean and looked
like proof of idempotency. The identity property is `n.id`. A set comparison is
only as good as the key it compares.

### 4. The pre-wipe reference is itself nondeterministic

Three runs of this procedure, same corpus, same indexing code path, recorded
three different pre-wipe graphs:

| Run | Nodes | Relationships |
| --- | ---: | ---: |
| 1 | 2,506 | 3,294 |
| 2 | 2,504 | 3,289 |
| 3 | 2,505 | 3,288 |

Diffing run 1 against run 3: `EvidenceArtifact` 15 vs 13, `Module` 228 vs 231,
`Environment` 2 vs 0, and their edges. Those are the same families that show up
in a rebuild comparison, which means part of what reads as a rebuild shortfall is
the indexer's own run-to-run variance.

`EvidenceArtifact` is the clearest case, and it moves on both sides:

| Run | Pre-wipe | Rebuilt |
| --- | ---: | ---: |
| 1 | 15 | — |
| 2 | 13 | 8 |
| 3 | 13 | 9 |
| 4 | 17 | 12 |

Neither column is stable, so the gap between them is not a fixed number either.
`semantic_entity_materialization` is the domain behind all of it, and it is the
same domain that inflates on a repeated refinalize in section 3. One defect
probably explains both: an `EvidenceArtifact` identity that is not a pure
function of the facts. That is a hypothesis from four runs, not a diagnosis — the
identity construction has not been read.

Exact count equality against a snapshot taken from one indexing run cannot pass
reliably until that is understood. The assertion was left alone: it is the right
assertion, and the reference under it is what is not yet stable.

### Caveat on run 4

Run 4 reported 6,285 `fact_records` where runs 1-3 each reported 3,866, so its
Postgres was not fully cleared before indexing — most likely the previous stack
was still up when the teardown ran. Its pre-wipe-versus-rebuilt delta is still
internally valid, because both sides were measured on the same data. Its absolute
totals and its `graph_rebuild_seconds` are not comparable to the other runs and
are not used for the cost table.

## Resumability, measured

The runbook's claim that an interrupted rebuild recovers was asserted, never run.
It has now been run. The verifier's own phase 3 cannot reach it while phase 1
fails, so this was driven by hand against the same stack, immediately after an
uninterrupted rebuild on that stack.

Sequence: wipe the graph, reapply schema, start writers,
`POST /api/v0/admin/recover-generations`, let it build for 12 seconds, then
`docker kill` the ingester, projector, and resolution-engine mid-drain. Restart
them, re-issue the command with a fresh idempotency key, and drain both queues.

At the kill:

```text
work in flight at the kill: 62
shared intents open at the kill: 505
graph at the kill: nodes=522 rels=547
```

The drain after restart, sampled every 15 s:

```text
t+15s  work=160 shared_open=592 nodes=2206 rels=2707
t+45s  work=6   shared_open=138 nodes=2489 rels=3253
t+60s  work=0   shared_open=138 nodes=2489 rels=3253
...                (shared backlog idle for ~3.5 minutes)
t+270s work=0   shared_open=117 nodes=2492 rels=3262
t+300s work=0   shared_open=0   nodes=2510 rels=3307
BOTH QUEUES TERMINAL
```

Residual non-terminal work: none. Zero `dead_letter`, zero `failed`.

Against the uninterrupted rebuild on the same stack, the interrupted one lost
nothing and recovered one edge more:

| Family | Uninterrupted | Interrupted + restarted |
| --- | ---: | ---: |
| `CALLS` | 115 | **116** |
| `EvidenceArtifact` | 12 | 19 |
| `IMPORTS` | 224 | 226 |

Every other label and edge type matched exactly. The `CALLS` edge came back
because the restart issued a second refinalize, which is the same mechanism
section 2 describes. The two inflated families are the known non-idempotency from
section 3, and they are the price of the extra pass — over-production, not loss.

So: an interrupted rebuild converges, drains clean, and does not drop work. It
does inherit the repeat-refinalize inflation, which is one more reason to fix
that defect rather than live with it.

The plateau is worth noting on its own. The shared backlog sat at 138 open
intents for about three and a half minutes with the work queue already empty,
then drained in one burst. Any recovery-time objective has to include that
window; `graph_rebuild_seconds` does not.

## Reproducing

```bash
scripts/verify-graph-rebuild-from-facts.sh
```

It exits non-zero, for the reasons in the section above.

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
