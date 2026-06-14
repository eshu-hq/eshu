# Code-Graph Sub-Scope Partitioning

The code-graph sub-scope partitioning contract defines when reducer work for one
large repository can safely run with more than one concurrent canonical
code-graph writer. It applies before changing `code_graph` conflict keys,
splitting reducer intents, or claiming intra-repo concurrency support.

This page is a design and benchmark gate. It does not change reducer conflict
keys, queue SQL, graph writers, Cypher, leases, worker defaults, batch sizes, or
production runtime knobs.

## Current Boundary

The reducer queue currently fences high-volume code graph domains with
`conflict_domain=code_graph` and the whole repository scope as the conflict key.
That protects NornicDB and Neo4j graph truth because only one reducer worker can
write canonical code graph edges or semantic node labels for a repository at a
time.

The ceiling is monorepo concurrency. Independent packages or directories in one
scope cannot project in parallel even when their write sets are disjoint.

Sub-scope partitioning may improve this only if it preserves the same graph
truth, retry, and dead-letter behavior as the whole-scope fence.

## Partition Key Contract

A partition key is valid only when it is durable, deterministic, and derived
from source facts or reducer-owned accepted units. It must not depend on local
absolute paths, machine-specific roots, timestamps, random IDs, graph readback,
or stale graph neighbors.

Allowed key inputs:

- repository `scope_id`;
- normalized repo-relative package, module, or directory root;
- durable affected-file set from the accepted reducer input;
- language or parser partition only when the source facts prove it;
- a stable hash of the normalized partition identity when the raw value is too
  long or not safe for logs.

The key format must be versioned before implementation. A follow-up
implementation may choose a shape such as:

```text
code_graph.v1:<scope_id>:<partition_kind>:<partition_id>
```

If the partition cannot be derived from durable input, the reducer must use the
existing whole-scope key.

## CALLS Foundation

Issue #2555 adds the first `code_call_materialization` partition-key
foundation without enabling concurrent CALLS graph writes. Refresh intents now
use versioned keys:

```text
code-calls:v1:whole:<repository_id>
code-calls:v1:files:<repository_id>:<sha256>
```

The file-scoped key is used only when the accepted delta unit carries a
non-empty, normalized repo-relative affected-file set. The raw affected paths
are sorted, deduplicated, and hashed with the repository id; no raw path, source
excerpt, commit SHA, IP address, or credential-shaped value appears in the
partition key. Missing, empty, absolute, parent-traversing, malformed, or
otherwise unsafe affected-file input falls back to the whole-scope key. The
acceptance key remains the same
`scope_id` / `acceptance_unit_id` / `source_run_id`, so whole-scope rows and
future file-partitioned rows for the same repository remain in one freshness
contract.

No-Regression Evidence: `go test ./internal/reducer -run
'Test(BuildCodeCallRefreshIntentsUseVersionedDeltaPartitionKey|CodeCallRefreshPartitionKeyFallsBackForUnsafeAffectedFiles|BuildCodeCallDeltaFileScopesRejectsUnsafeAffectedPath|BuildCodeCallRefreshIntentsCarriesDeltaFileScope|BuildCodeCallSharedIntentRowsDeduplicatesIntentIdentity|BuildCodeCallDeltaFilePathsByRepoIDUsesRepositoryDeltaFact)'
-count=1` proves deterministic hashed CALLS file keys, duplicate replay
stability, malformed-input fallback, unchanged delta payload carry, and
acceptance-key compatibility. The change is statement-construction only for
shared intent rows; it does not change queue SQL, partition leases, worker
defaults, graph Cypher, graph write batching, or full-refresh repo-wide
retraction behavior.

No-Observability-Change: operators continue to diagnose this path through the
existing shared-intent rows, shared acceptance rows, code-call projection cycle
logs, `eshu_dp_queue_claim_duration_seconds{queue="code_calls"}`,
shared-acceptance gauges, graph write metrics, and `/admin/status` backlog.
No metric, metric label, span, log field, status field, worker, lease, or
runtime knob is added or changed.

## Partitionable Work

Code graph work is partitionable only when the reducer can name the full write
set before graph mutation.

| Domain | Partitionable when |
| --- | --- |
| `code_call_materialization` | The accepted unit names a bounded affected-file set and retracts or writes only source edges owned by those files. |
| `semantic_entity_materialization` | The semantic node labels and properties belong to entities rooted in the same affected-file or package set. |
| `sql_relationship_materialization` | The SQL relationship endpoints and retract scope are bounded to the same package, module, or affected-file set. |
| `inheritance_materialization` | The inheritance, override, alias, and interface edges have a closed source-owner set and do not require whole-repo ambiguity resolution. |

Cross-partition target reads are allowed only when they do not mutate the target
partition's owned properties or relationships. If the writer can mutate both
source and target ownership, the work is not partitionable unless it is split
into deterministic per-partition intents.

## Fallback To Whole Scope

The reducer must keep the whole-scope `code_graph` fence when any of these are
true:

- first full projection has no durable affected-file set;
- generated code or source maps make ownership ambiguous;
- a parser, SCIP input, or relationship resolver reports ambiguous ownership;
- the accepted unit spans multiple partitions and cannot be split without
  changing ordering;
- stale or missing generation data prevents exact retract scope;
- the write path retracts by repository, language, relationship type, or graph
  neighbor query instead of by durable source-owner keys;
- the backend reports retry storms, uniqueness conflicts, or dead letters for
  the partitioned shape.

Fallback must be explicit and observable. It must not silently claim
partitioned support while running the whole-scope path.

## Concurrency Invariants

Future partitioning changes must preserve these invariants:

| Invariant | Requirement |
| --- | --- |
| Conflict isolation | Two workers may overlap only when their durable partition keys are disjoint. |
| Whole-scope compatibility | Whole-scope rows still block partitioned rows for the same repository, and partitioned rows block later whole-scope rows until they finish. |
| Deterministic ordering | Multi-partition work is split or ordered by a stable partition list before any graph write. |
| Idempotency | Replaying a partitioned row must converge with the same graph truth as a whole-scope replay. |
| Retraction safety | Partitioned retracts must remove only relationships owned by the partition's source-owner keys. |
| Dead-letter visibility | Partition failures keep the partition key, domain, failure class, retry count, and fallback reason visible. |

Reducing worker counts, forcing batch size `1`, or disabling concurrent graph
writers is not a permanent fix for unsafe partitioning. Those are diagnostics
unless a tracked benchmark proves a permanent serial path still satisfies the
large-repo performance contract.

## Benchmark Matrix

Implementation work must prove correctness first, then performance, then
concurrency.

| Dimension | Required evidence |
| --- | --- |
| Repository shape | One large monorepo with multiple independent package or directory partitions, plus one mixed or cross-partition case. |
| Domain coverage | `CALLS`, semantic entity, SQL relationship, and inheritance materialization, or an explicit reason a domain remains whole-scope. |
| Worker shape | Whole-scope baseline, intended partitioned worker count, and at least one contention run with multiple reducer replicas or workers. |
| Queue state | Pending, retrying, expired-claim, active-conflict, and dead-letter rows for partitioned and whole-scope work. |
| Graph backend | Pinned NornicDB binary and Neo4j compatibility proof when the write shape changes. |
| Result proof | Fixture intent, reducer graph truth, API/MCP read truth, retry replay, and dead-letter replay agree. |

The performance claim is valid only when:

- measured intra-repo concurrency is greater than one for independent
  partitions;
- p95 projection-tail time improves on the large-repo proof without increasing
  graph write retries or dead letters;
- whole-scope fallback cases remain no worse than the same-shape baseline by
  more than 10%;
- operator signals identify whether time is spent in claim wait, handler
  duration, graph write, readiness wait, conflict blocking, retry, or dead
  letter handling.

If partitioned writes are correct but slower, classify the result as a rejected
hypothesis or diagnostic win. Do not present it as a throughput win.

## Operator Signals

Future implementation PRs must expose or reuse bounded signals that answer:

- which partition key is hot or falling back to whole scope;
- whether rows are blocked by whole-scope compatibility or same-partition
  conflict;
- how many partitioned rows succeeded, retried, fell back, or dead-lettered;
- whether graph write retries increased after partitioning;
- whether API/MCP reads agree with reducer graph truth after replay.

Metric labels must stay bounded. Raw file paths, local absolute paths, commit
SHAs, and source excerpts belong in logs, traces, or durable facts when safe, not
metric labels.

## Verification Gate

Use the smallest gate that proves the touched boundary.

| Change | Required gate |
| --- | --- |
| Contract or docs only | Strict MkDocs build, performance-evidence guard scripts, `git diff --check`, and sensitive-string scan. |
| Conflict-key implementation | Failing tests first for partition derivation, whole-scope fallback, expired claim reclaim, duplicate replay, and dead-letter replay. |
| Graph writer or retract scope | Fixture truth, reducer graph truth, API/MCP truth, same-shape graph-write benchmark, and performance-evidence guard scripts. |
| Worker or batch behavior | Contention proof with intended workers, retry/idempotency proof, dead-letter proof, and operator signal proof. |

No-Regression Evidence: this contract changes documentation only. It adds no
reducer conflict key, queue SQL, graph write, Cypher, worker, lease, batch,
runtime knob, schema DDL, metric, span, log field, or status field.

No-Observability-Change: this contract names required future signals but does
not change runtime telemetry. Current diagnosis continues through reducer queue
wait, reducer run duration, queue claim duration, graph write metrics,
`/admin/status`, and reducer logs.

## Related Docs

- [Resolution Engine](../services/resolution-engine.md)
- [Local Performance Envelope](local-performance-envelope.md)
- [Profiling And Concurrency](local-testing/profiling-and-concurrency.md)
- [Cypher Performance](cypher-performance.md)
