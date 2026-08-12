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

The rebuild's projector rows carry `updated_at` between `15:35:00.8` and
`15:35:15.1`. Grouping every reducer row by domain and asking whether its latest
update falls inside that window splits the seventeen domains cleanly in two:

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

## What this does not establish

The diagnosis stops at "these domains were not re-enqueued". It does not say why
five domains are re-enqueued by a re-projection and twelve are not. The two
candidate mechanisms — the projector's post-commit fan-out covering only some
domains, versus a dedup that treats an existing `succeeded` row as work already
done — were not separated here, and separating them is the first step of a fix.

Nor does this establish that re-enqueueing the other twelve would be safe or
cheap. Forcing every materializer to re-run for every scope on every refinalize
changes both the cost and the concurrency profile of a routine recovery
operation, not only a disaster one. That trade needs an owner and its own
measurement before anyone writes code.

The comparison is count equivalence per label and per relationship type, not the
canonical byte-identity #4594 asks for. It catches a family that disappeared. It
would not catch a property whose value changed while cardinality held.

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
