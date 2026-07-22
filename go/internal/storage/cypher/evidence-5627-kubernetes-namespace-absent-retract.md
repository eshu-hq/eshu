# Evidence: complete Kubernetes namespace snapshot reconciliation

## Correctness theory

The prior projector emitted `kubernetes_namespace_materialization` only when a
generation contained at least one `kubernetes_live.namespace` fact. Therefore a
successful namespace list that became empty emitted no reducer work. The node
writer also only upserted incoming rows, so a deleted namespace and its
`TARGETS_ENVIRONMENT` edge remained indefinitely.

The corrected path treats a Kubernetes live cluster generation with
`FreshnessHint=complete` as the reconciliation marker. Current rows are stamped
with the generation ID, then a cluster- and evidence-source-scoped sweep removes
only reducer-owned `KubernetesNamespace` nodes whose generation differs. Partial
snapshots never request the sweep. The reducer also suppresses the sweep when
any namespace fact is quarantined and rejects a complete-snapshot cluster
identity mismatch before graph mutation, preventing malformed input from being
misread as authoritative absence.

## Prove-the-theory-first shim

Backend: pinned `eshu-nornicdb-pr261:149245885258` image, NornicDB 1.1.11.
Dataset: 10,000 `KubernetesNamespace` nodes split evenly between
`target-cluster` and `other-cluster`; 10 target-cluster nodes were stamped
`current-generation`, leaving 4,990 stale target nodes.

Candidate query:

```cypher
MATCH (n:KubernetesNamespace {cluster_id: $cluster_id})
WHERE n.evidence_source = $evidence_source
  AND coalesce(n.generation_id, "") <> $generation_id
DETACH DELETE n
```

Observed result on the same seeded graph:

- Before: 4,990 stale target-cluster nodes.
- After: 0 stale target-cluster nodes.
- Preserved: all 10 current target-cluster nodes and all 5,000 other-cluster
  nodes.
- Wall time: 10.26 seconds, 34.2% of the existing 30-second NornicDB canonical
  write timeout.
- Upgrade compatibility: a reducer-owned target node with no `generation_id`
  (the pre-fix shape) was removed by the same `coalesce` predicate, while a
  current stamped node remained.

This is a behavior correction, so the required proof is the intended delta,
not equality with the old false-stale result. The query is marked as a node
drain for NornicDB-aware executors and remains bounded to one cluster and this
reducer's evidence source.

## Finished-path proof

Red tests before implementation:

```text
TestBuildProjectionQueuesKubernetesNamespaceReconciliationIntentForCompleteEmptySnapshot
  no reconciliation intent enqueued
TestKubernetesNamespaceMaterializationCompleteEmptySnapshotRetractsStaleNodes
  retract calls = 0, want 1
```

Focused unit proof:

```bash
cd go && go test ./internal/projector ./internal/reducer ./internal/storage/cypher \
  -run 'KubernetesNamespace' -count=1
```

Live production-writer proof against the pinned backend:

```bash
cd go && ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb \
  ESHU_NEO4J_URI=bolt://127.0.0.1:<ephemeral-port> \
  ESHU_NEO4J_USERNAME=neo4j ESHU_NEO4J_PASSWORD=<local-test-value> \
  ESHU_NEO4J_DATABASE=nornic \
  go test ./internal/replay/offlinetier \
  -run TestReducerKubernetesNamespaceAbsentNodeRetractGraphTruth -count=1 -v
```

Result: PASS. The live test proves both transitions: a complete snapshot keeps
the current node while removing a stale peer, and the next complete empty
snapshot removes the last node.
