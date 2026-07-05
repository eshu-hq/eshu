# Evidence: skip the Helm REFERENCES retract on first projection (#4726 / #3624)

## Problem

On a clean-volume cold ingest, the dominant bootstrap canonical-write timeout is
the Helm template-value **legacy REFERENCES retract**
(`retractLegacyHelmReferenceEdgesCypher`):

```cypher
UNWIND $source_uids AS uid
MATCH (u:HelmTemplateValueUsage {uid: uid})-[r:REFERENCES]->(:HelmValueDefinition)
WHERE r.call_kind = 'helm_template_value_reference'
DELETE r
```

It MATCHes on the large shared `REFERENCES` type (`MATCH ()-[r:REFERENCES]->()` =
47,214 edges on the 909-repo corpus). Although the MATCH has an inline
`(u:HelmTemplateValueUsage {uid})` anchor, on NornicDB the planner does not seed
on the indexed uid and full-scans `REFERENCES`, timing out at the 30s
canonical-write budget — even though on a clean install it matches **nothing**
(the dedicated `HELM_VALUE_REFERENCE` type is used from the first projection).

In a full 909-repo clean-volume E2E this shape was **22 of 26** bootstrap
canonical-write timeouts and froze bootstrap progress at ~855/909 scopes.

## Fix

Skip both Helm retracts (the legacy shared-REFERENCES migration and the
dedicated-type retract) on the scope's first generation
(`if !mat.FirstGeneration`), leaving only the MERGE upsert. A first-ever
projection has no prior `HELM_VALUE_REFERENCE` edges and no pre-#4476 legacy
`REFERENCES` edges to remove. A legacy edge requires a prior generation
*attempt* from an old binary, and any generation that ran far enough to write
edges records a prior-generation marker — so `FirstGeneration` is already false
for it. Crucially `FirstGeneration` keys on ANY prior generation of any status
(activated, failed, or superseded), NOT only prior *active* generations (the
#4710 invariant: these edges write on acceptance, before activation), so the
skip cannot miss legacy edges a prior generation wrote before it activated. Both
DELETEs are therefore provable no-ops on a true first generation; the second
generation onward runs the retracts normally. This mirrors the Atlantis MANAGES
edge's first-generation guard (`canonical_atlantis_edges.go`).

## Proof

Performance Evidence: on the 909-repo clean-volume cold ingest (built binary,
NornicDB, budget held fixed), skipping the Helm legacy REFERENCES retract on
first projection drops the Helm-shape 30s canonical-write timeouts from 22 to 0
and canonical projection failures from 13 to 1, unfreezing bootstrap past 855.
No-Regression Evidence: the change only removes provable no-op DELETEs on a
scope's first generation; the MERGE upsert and all second-generation retract
behavior are byte-identical, and the full `internal/storage/cypher` package
(681 tests) plus the B-7 rc-35 golden edge-count floor are unchanged.

**Output-preserving (unit):**
`TestHelmTemplateValueEdgeStatementsSkipsRetractOnFirstGeneration` asserts
`FirstGeneration=true` emits exactly 1 statement (the MERGE), no `[r:REFERENCES]`
retract; `TestHelmTemplateValueEdgeStatementsResolvesUsageToDefinition`
(`FirstGeneration=false`) still emits the two retracts + MERGE. `go test
./internal/storage/cypher -run Helm` green. The MERGE that creates the edges the
B-7 rc-35 golden gate asserts is unchanged, so edge output (and rc-35) is
identical — the skip only removes no-op DELETEs.

**Work (909-repo clean-volume cold-ingest E2E, built binary, same machine/
backend/corpus; both runs also set `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=8` to hold
the concurrency variable fixed so the Helm-shape delta is isolated):**

| metric | before (main) | after (this branch) |
|---|---|---|
| Helm-shape (`HelmTemplateValueUsage…REFERENCES`) 30s timeouts | 22 | **0** |
| all bootstrap 30s canonical-write timeouts | 26 | 2 (both `entities/Annotation`, a separate concurrency-class timeout — not the Helm shape) |
| canonical projection failures | 13 | **1** |
| bootstrap progress | frozen at ~855/909 (retrying the Helm scan) | unfrozen, progressing past 855 (858→861→…) |

The Helm legacy-REFERENCES full-scan is eliminated: zero Helm-shape timeouts and
bootstrap is no longer frozen retrying it. The 2 residual timeouts are the
`entities/Annotation` concurrency class, addressed separately by the graph-write
budget (#4456) and not this shape. Edge output (rc-35) is identical — the MERGE
is unchanged.

## Observability

No-Observability-Change: no new instruments. The skip is visible via the
absence of the Helm legacy retract statement on first-generation
structural_edges phase groups and the drop in `graph_write_timeout`
failure-class events for the Helm shape; existing spans/metrics/logs are
unchanged.
