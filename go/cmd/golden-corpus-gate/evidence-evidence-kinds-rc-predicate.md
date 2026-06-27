# Evidence: evidence_kinds filter for required correlations (+ rc-29 kustomize)

Scope: the golden-corpus gate's correlation counter
(`go/cmd/golden-corpus-gate/graph.go`) gains an evidence-filtered count, and the
B-7 snapshot gains rc-29 asserting the Kustomize `DEPLOYS_FROM` verb. This file
records the performance and observability evidence the CI hot-path gate requires
because `graph.go` contains Cypher (`MATCH`).

## What changed

`CountCorrelationWithEvidence(from, rel, to, evidenceKinds)` counts only the
(From)-[Rel]->(To) edges whose `evidence_kinds` relationship property contains
every listed kind. This isolates a single verb on a shared, tool-agnostic edge
type (e.g. Kustomize vs ArgoCD, both emit `DEPLOYS_FROM`) without fragmenting the
edge into per-tool relationship types.

## NornicDB WHERE-on-relationship-count finding (why the filter runs in Go)

The first implementation filtered in Cypher
(`MATCH (:L)-[r:T]->(:L) WHERE $k IN r.evidence_kinds RETURN count(r)`). A probe
against the pinned NornicDB binary in the live gate stack proved that shape is a
**false green**:

```
MATCH (:Repository)-[r:DEPLOYS_FROM]->(:Repository) WHERE false RETURN count(r)        -> 2
MATCH (:Repository)-[r:DEPLOYS_FROM]->(:Repository) WHERE 'NOPE' IN r.evidence_kinds    -> 2
MATCH (:Repository)-[r:DEPLOYS_FROM]->(:Repository) WHERE $k IN r.evidence_kinds        -> 2
MATCH (:Repository)-[r:DEPLOYS_FROM]->(:Repository) WHERE ANY(x IN r.evidence_kinds ...) -> 0
```

NornicDB does not apply a `WHERE` clause to this anonymous-anchor
relationship-count shape (`WHERE false` still returns the full count), and its
`ANY()` list predicate returns empty. So neither a membership `WHERE` nor `ANY()`
can filter here. The counter therefore returns each edge's `evidence_kinds` with
a plain `MATCH (:From)-[r:Rel]->(:To) RETURN r.evidence_kinds` (no WHERE — that
shape works and round-trips the list property correctly) and counts the matches
in Go (`edgeEvidenceContainsAll`). This is backend-neutral and correct on both
NornicDB and Neo4j. The Go filter is unit-tested (`TestEdgeEvidenceContainsAll`)
and proven on the live backend: bogus-kind=0, KUSTOMIZE_RESOURCE_REFERENCE=1,
ARGOCD_APPLICATION_SOURCE=1 over the two real DEPLOYS_FROM edges.

## Performance

- Query shape: a single anchored `MATCH (:From)-[r:Rel]->(:To)` returning the
  `evidence_kinds` list of each edge of one relationship type between two labels
  — tens of edges at most in the minimal B-7 corpus. The Go-side match is an
  O(edges × kinds) set check over that tiny result.
- This path runs only in the `golden-corpus-gate` proof binary (CI and
  local/remote validation), never in the serving API/MCP/reducer runtime. It is
  not a production hot path; it executes a handful of times per gate run.

No-Regression Evidence: full local gate green in 35s (budget ceiling 1800s),
42 pass / 0 required-fail, with rc-29 asserting the Kustomize-filtered
DEPLOYS_FROM at count=1 (provably the single Kustomize edge, not the ArgoCD one).
The unfiltered `CountCorrelation` path is unchanged. Backend: NornicDB (default),
shared Bolt counter; the same shape is portable to Neo4j.

## Observability

No-Observability-Change: the gate emits its findings as a structured report to
stdout (see `report.go`); it has no runtime metrics, spans, or logs surface. The
evidence-filtered finding's detail string includes `evidence_kinds⊇[...]` so a
failing rc is self-diagnosing from the gate output alone. No serving-runtime
telemetry is added or altered.
