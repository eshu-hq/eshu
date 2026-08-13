# Graph rebuild from facts: what a wipe-and-rebuild actually restores

Evidence for #4594. Run on 2026-08-12 with
`scripts/verify-graph-rebuild-from-facts.sh` against the local Compose stack.

**Read this first — the document records a diagnosis and then its fix, in that
order, and the early sections describe a state that no longer holds.**

Where this landed:

- **The original defect is fixed.** A rebuild used to return only source-local
  structure: twelve of seventeen reducer materialization domains never re-ran,
  leaving the graph short 73 nodes and 384 relationships out of 2,504 and 3,289.
  Three pieces of Postgres state outlive a graph wipe and each told the pipeline
  the work was done. Clearing them, scoped to the generations being rebuilt,
  brings every domain back. The shortfall drops to 5 nodes and 26 relationships.
- **The verifier still exits 1**, and that is the correct outcome. Three things
  still differ, and **none of them is the rebuild path this PR touches**: a
  cross-repository `CALLS` edge blocked by the shared-projection readiness gate,
  an `EvidenceArtifact` set that is not a pure function of the facts, and — as a
  consequence of the same resolver defect — a pre-wipe reference that is not
  stable run to run.
- **No time bound is claimed.** The issue asks for one. The measurements here
  cannot support one, and the reasons are recorded rather than papered over.

The assertions were left alone. They assert the contract #4594 asks for, and the
contract does not hold yet. What each remaining failure is, and which of them
needs an owner decision rather than a code change, is in
[Disposition](#disposition-what-each-failure-is-and-who-has-to-act).

Sections below are in the order they were investigated, so a claim in an early
section may be corrected in a later one. Every correction is marked.

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
- `shell_exec_materialization` owns `EXECUTES`, `CloudAction`, and
  `INVOKES_CLOUD_ACTION` — absent.
- `sql_relationship_materialization` owns `QUERIES_TABLE`, `HAS_COLUMN`,
  `REFERENCES_TABLE`, `READS_FROM`, `WRITES_TO` — absent.

Every missing family in that list belongs to a domain that did not re-run, and
no family in it belongs to a domain that did.

### Correction: `EvidenceArtifact` is not a reducer-domain miss

An earlier revision of this list also claimed `semantic_entity_materialization`
owns `EvidenceArtifact` and `EVIDENCES_REPOSITORY_RELATIONSHIP`. **That is
wrong, and it was wrong in a way that pointed at the wrong guard.** Read from
the writer:

- The nodes are written at `storage/cypher/edge_writer.go:196`, inside
  `if domain == reducer.DomainRepoDependency`.
- `DomainRepoDependency` is `"repo_dependency"`, and it is a *shared projection*
  domain (`reducer/shared_projection.go:16`), not one of the seventeen reducer
  materialization domains the table above splits.
- Its intents are emitted by the cross-repo resolver
  (`reducer/cross_repo_intent_row.go:133` sets
  `ProjectionDomain: DomainRepoDependency`), which runs under the
  `deployment_mapping` reducer domain — a domain in the **left** column, one that
  did produce work.

So `EvidenceArtifact` does not fit the "a reducer domain never re-ran" story at
all. Its absence traces to guard **2**, completed `shared_projection_intents`,
which is a different guard with a different fix. The mapping above holds for the
families it lists; this one family was mis-attributed, and the mis-attribution
survived because the count table only ever showed the symptom.

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
tracked the change. Read the direction only — roughly three times the reducer
work plus ~590 shared intents that previously did nothing.

**Do not read a multiplier off this table.** Two independent problems make the
numbers non-comparable, and only one of them was previously recorded:

1. **Three different machine loads.** 2.41, 3.78, and 5.19 on 12 CPUs. A 15 s →
   25 s movement across a doubling of load is not a measurement of the change.
2. **Both columns undercount, by an unbounded amount.** Every number in this
   table was taken before the verifier waited on the shared backlog (the wait
   landed in `98c42d88b`; the table in `e789a6b1f`). They stop when
   `fact_work_items` is empty, and the shared queue has been observed running
   for a further **four minutes** past that point. The undercount is not a fixed
   offset, so it does not cancel between the two columns.

## Is there a defensible time bound? No.

#4594 asks for a timed DR operation with a stated bound. This branch cannot
supply one, and the honest thing is to say so rather than promote a number.

What exists:

| Figure | Definition | Runs | Usable as a bound? |
| --- | --- | --- | --- |
| 15 s / 20 s / 25 s / 26 s | work queue empty only | 4, at 3 different loads | No — undercounts by the shared-backlog tail |
| 341 s (5m41s) | both queues terminal | **1** | No — single sample, one load |

The 341 s figure is the only one that measures the whole rebuild. It is one
sample. A single observation is not a bound, and this repo's own evidence rules
say so.

Three things would each independently sink a bound derived from these runs:

- **The corpus is a toy.** 1.4 MB, 361 files, 67 scopes, 3,866 facts — the doc's
  own conditions table calls it "well below the smallest scale-lab slot." A
  recovery-time objective for a deployment cannot be extrapolated from it. The
  rebuild is dominated by per-scope queue drain, and scope count is the thing
  that changes by orders of magnitude in a real deployment.
- **The definition changed mid-branch**, so the early numbers and the late number
  are not the same measurement.
- **The operation may be two passes.** If the owner resolves failure 1 by
  declaring DR a two-pass operation, the bound roughly doubles. Sizing an RTO
  before that decision is made would produce a number that is wrong either way.

What an operator can rely on today is a shape, not a number: the rebuild is a
bounded INSERT of one work item per active scope, followed by the ordinary
projector and reducer drain, followed by a shared-backlog tail that has been seen
to idle for minutes and then finish in one burst. The tail is the part nobody
should size by intuition.

**To turn this into a bound**, someone has to run the two-queue measurement at
least three times on an unloaded machine at a scale-lab corpus, on one fixed
definition, after the two-pass question is settled. That is a scale-lab task, not
something a fixture corpus on a developer laptop can answer.

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

### Disposition: what each failure is, and who has to act

Read this before deciding what to do with the red gate. None of the three is
fixed by weakening the assertion, and none of them is fixed inside the reset this
PR ships.

| # | Failure | What it is | Who acts |
| --- | --- | --- | --- |
| 1 | One cross-repo `CALLS` edge, 115 of 116 | **Real defect**, outside the rebuild path: the shared-projection readiness gate has no cross-repository key, and the edge write is a silent `MATCH`-only no-op | Owner: either fix the readiness gate, or accept a two-pass DR |
| 2 | `EvidenceArtifact` under-produced on one pass | **Real defect**, outside the rebuild path: the resolver's five-item evidence preview reads fact arrival order | Owner: schedule the resolver ordering fix; it moves the golden snapshot |
| 3 | Nondeterministic pre-wipe reference | **Same root cause as 2** for `EvidenceArtifact`; **unexplained** for `Module` and `Environment` | Owner: the assertion cannot pass until 2 lands; the `Module`/`Environment` variance still needs tracing |

Two of these are the *same* defect wearing different clothes. Failure 2 and the
`EvidenceArtifact` half of failure 3 are both the resolver preview ordering,
proven above. That leaves genuinely three open items, not three independent
causes: the readiness gate, the resolver ordering, and the untraced
`Module`/`Environment` variance.

**The decision this branch cannot make for you.** Failure 1 is the only one with
a cheap operational answer. A second refinalize recovers the edge — the
interrupted-rebuild run below did exactly that by accident. So the owner picks
one of:

- **Fix the readiness gate** so a cross-repository edge waits for both endpoints'
  acceptance units. Correct, and the larger change.
- **Declare DR a two-pass operation** in the runbook, and move the verifier's
  assertion to after pass 2. Cheap, and honest, but it makes every recovery pay
  a second full drain — 341 s became the measured single-pass figure on this
  corpus, so a two-pass DR roughly doubles it.

What the owner must NOT do is relax the assertion to "≤ 1 missing edge is fine."
On this corpus one edge is 100% of the cross-repository calls. The number is
small because the fixture is small, not because the defect is small.

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

The code path was read to confirm this is the mechanism rather than a plausible
story fitted to one number. Three things line up:

1. **The write is `MATCH`-only.** `canonical_code_call_edges.go:68` matches both
   endpoints and merges between them:
   `MATCH (source:Function|Class|File {uid: ...}) MATCH (target:...) MERGE
   (source)-[rel:CALLS]->(target)`. A row whose target uid has no node yields
   zero rows, writes nothing, and raises nothing.
2. **Nothing re-checks it.** `WriteEdges` returns a report whose `writtenRows` is
   the count *submitted*, not the count the backend matched, and the runner then
   unconditionally calls `MarkIntentsCompleted`
   (`reducer/code_call_projection_runner.go:461`). There is no repair queue on
   the code-call family, unlike workload materialization.
3. **The readiness gate only covers the caller's repository.** `code_calls` is
   gated on canonical-nodes-committed, but the key is built from the intent's own
   `AcceptanceKey()`, which falls back to `row.RepositoryID`
   (`reducer/shared_projection.go:264`). No readiness key is ever constructed for
   the *callee's* repository. The design comment at `shared_projection.go:169`
   says so outright: "there is no cross-acceptance-unit dependency to wait on the
   way HANDLES_ROUTE waits on Endpoint materialization."

So a cross-repository `CALLS` intent waits only for its own repository, performs
a silent no-op write if the other repository is not committed yet, and is marked
done in the same pass. That is why exactly one edge of 116 is affected on this
corpus — it is the only cross-repository call in it — and why a second refinalize
recovers it.

This is a pre-existing defect in the shared-projection readiness model, not
something the rebuild introduced. Ordinary indexing has the same gap; the rebuild
only makes it easy to see, because it drains every repository at once.

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

#### The identity construction has now been read, and the defect is located

The previous revision left this as "a hypothesis from four runs, not a diagnosis
— the identity construction has not been read." It has been read, and the
hypothesis was right about the symptom and wrong about the place.

The id itself is pure. `repoEvidenceArtifactID`
(`storage/cypher/edge_writer_row_metadata.go:141`) digests exactly
`resolved_id`, `evidence_kind`, `path`, `matched_value`, plus a Flux
namespace/name suffix. No clock, no random, no map iteration.

The impurity is one hop upstream, in what the artifacts are built *from*.
`aggregateCandidate` (`relationships/resolver.go`) keeps the **first five**
evidence facts it happens to see as the candidate's `evidence_preview`, and
nothing sorts `facts` first:

```go
if len(preview) < 5 {
    preview = append(preview, map[string]any{...facts[i]...})
}
```

`resolvedRelationshipEvidenceArtifacts`
(`reducer/cross_repo_evidence_artifacts.go:26`) then builds the entire artifact
set from that preview and returns early when it is empty. So for any candidate
with more than five evidence facts, **which artifacts exist at all is decided by
fact arrival order**, and each survivor's `path` and `matched_value` feed the id
digest.

The order comes from `listEvidenceFactsByGenerationSQL`
(`storage/postgres/relationship_schema.go`), which is
`ORDER BY observed_at ASC, evidence_id ASC`. `observed_at` is a single
`time.Now().UTC()` captured once per `UpsertEvidenceFacts` call
(`relationship_store.go:202`) and shared by every row in that call, so facts
written by different insert calls interleave by which call ran first — wall
clock, not content. Within one call the `evidence_id` tie-break is
deterministic.

**Proven, not inferred.** `relationships/resolver_evidence_order_independence_test.go`
feeds one candidate seven facts in two orders. Deleting its `t.Skip` and running
it fails:

```text
--- FAIL: TestAggregateCandidateEvidencePreviewIsOrderIndependent (0.00s)
    evidence_preview depends on input order, so two indexing runs of the same
    facts produce different candidate details and a different graph.
    forward:  [TERRAFORM_MODULE_SOURCE TERRAFORM_GITHUB_REPOSITORY
               HELM_CHART_DEPENDENCY PACKAGE_MANIFEST GITHUB_WORKFLOW_USES]
    reversed: [SUBMODULE_PIN DOCKERFILE_FROM GITHUB_WORKFLOW_USES
               PACKAGE_MANIFEST HELM_CHART_DEPENDENCY]
```

That is the same defect behind the section 3 movement (13 → 19 across passes)
and behind this section's unstable pre-wipe reference. One cause, two symptoms.

The fix is to order a candidate's facts by a content key — confidence
descending, then evidence kind, path, matched value — before the five-item cap.
It changes projected graph truth and therefore the golden snapshot, so it is
deliberately not part of #4594.

One thing this does **not** explain: `Module` (228 vs 231) and `Environment`
(2 vs 0) also move between indexing runs, and `Module` is not built from the
evidence preview. Those have not been traced. Do not assume the resolver fix
covers them.

Exact identity equality against a snapshot taken from one indexing run cannot
pass reliably until the resolver ordering is fixed. The assertion was left
alone: it is the right assertion, and the reference under it is what is not yet
stable.

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
