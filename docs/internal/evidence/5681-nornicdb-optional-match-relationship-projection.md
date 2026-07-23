# #5681 — NornicDB OPTIONAL MATCH corrupts relationship type/identity projection

## Problem

`POST /api/v0/code/relationships` name/entity lookups silently returned empty
`outgoing`/`incoming` on NornicDB deployments, even when the graph held correct,
present edges. This blocked production-tier proof for
`symbol_graph.inheritance`'s `analyze_code_relationships` route and the
`symbol_graph.imports`, `call_graph.direct_callers`, and
`call_graph.direct_callees` capabilities.

## Root cause (NornicDB executor defect, isolated live)

`nornicDBOneHopRelationshipsCypher` emitted a relationship-bound primary `MATCH`
followed by four trailing `OPTIONAL MATCH` clauses and relied on `type(rel)`,
`coalesce(...)`, and `head(labels(...))` in the `RETURN`. On the pinned
`eshu-nornicdb-pr261:149245885258` image (and confirmed still present on
NornicDB branch HEAD and `main`), that query shape routes to
`executeCompoundMatchOptionalMatch`'s traversal branch, which resolves `RETURN`
items with `resolveReturnExprFromVarMap` (handles only `var.prop` / bare vars)
instead of the real evaluator `evaluateExpressionWithContext`. Every
function-call projection therefore comes back as its **literal source text**
(`type(rel)` → `"type(rel)"`). The corrupt `type` column never equalled the
requested relationship type, so `filterRelationships`
(`go/internal/query/code_relationships.go`) dropped every edge.

Full behavior characterization and the upstream root-cause map are in
`docs/public/reference/nornicdb-pitfalls.md` ("Trailing `OPTIONAL MATCH`
Corrupts Every Function-Call Projection").

### Isolated live characterization (real Bolt driver, DB `nornic`)

| Query shape | `type(rel)` | `coalesce(e.id,e.uid)` | `target.name` (plain) |
| --- | --- | --- | --- |
| relationship `MATCH`, **no** OPTIONAL MATCH | `"INHERITS"` ✓ | `"cls:ServiceDog"` ✓ | `"Dog"` ✓ |
| relationship `MATCH` + trailing OPTIONAL MATCH | `"type(rel)"` ✗ | `"coalesce(e.id, e.uid)"` ✗ | `"Dog"` ✓ |

## Fix

Split the read (established repo pattern for this NornicDB defect class):

- The relationship **core** read (`nornicDBOneHopRelationshipsCypher`) now carries
  no `OPTIONAL MATCH`, so `type(rel)`, `coalesce(...)`, and `head(labels(...))`
  evaluate. It projects two extra `source_entity_uid`/`target_entity_uid`
  columns to key the merge; they are stripped before the response.
- File/repository/language **enrichment** is fetched by `OPTIONAL-MATCH`-free,
  index-anchored path reads
  (`go/internal/query/code_relationships_nornicdb_enrich.go`) and merged onto the
  core rows in Go by endpoint identity. File and repository metadata are read
  **separately** (a File-only read plus a File→Repository read, for both the far
  endpoints and the anchor) so an endpoint with a File but no `REPO_CONTAINS`
  edge — a partially projected graph — still keeps its file path and language,
  matching the pre-split OPTIONAL MATCH behavior; a mandatory File→Repository
  path would have dropped the file metadata with the absent repository (#5730
  review, Codex P2). Endpoints with no `File` never appear (a left-join in Go).
  Language prefers the entity node's own language and falls back to the file
  language, matching the pre-split `coalesce(node.language, file.language)`.
- The reads are **bounded and truncation-disclosing**: each carries a
  deterministic `ORDER BY` + `LIMIT`, over-fetching one row past
  `nornicDBRelationshipRowLimit` (500) so `nornicDBOneHopRelationships` can set
  the response's `outgoing_truncated`/`incoming_truncated` flags when a
  high-degree symbol (e.g. an incoming-CALLS hub) exceeds the ceiling, instead of
  presenting a clipped set under an exact-truth envelope (#5730 review, Codex
  P1). The OpenAPI relationships response schema declares both flags.

## Local proof (failing-then-green, pinned image)

Pinned NornicDB started as:

```
docker run -d --name nornic-rel-proof -e NORNICDB_EMBEDDING_ENABLED=false \
  -e NORNICDB_NO_AUTH=true -p 17687:7687 eshu-nornicdb-pr261:149245885258
```

- **Before (base code, new live test):**
  `--- FAIL: TestLiveNornicDBRelationshipsSurviveOptionalMatchProjection`
  `outgoing INHERITS edges = 0, want 3 (Dog, LogMixin, SerializeMixin); rows=[]` —
  the exact reported symptom.
- **After (fix):**
  `--- PASS: TestLiveNornicDBRelationshipsSurviveOptionalMatchProjection (0.13s)` —
  three `INHERITS` edges with evaluated `type="INHERITS"`, `Dog`/`LogMixin`
  carrying enriched `repo:1` / `svc.py`, and `SerializeMixin` (no `File`)
  correctly carrying evaluated identity with absent file/repo metadata.

Command:

```
cd go && go test ./internal/query -tags live_nornicdb_relationships_proof \
  -run TestLiveNornicDBRelationshipsSurviveOptionalMatchProjection -count=1 -v
```

Default-tag suite (`cd go && go test ./internal/query -count=1`) stays green; the
fake-graph-reader unit tests are unchanged because the split issues no enrichment
read when the core rows carry no endpoint uid (the shape the fakes produce).

## Performance

No-Regression Evidence: relationship reads are bounded one-hop lookups anchored
on the entity's label+uid index (`MATCH (e:Label {uid:$id})...`). The change
replaces one relationship-bound query carrying four trailing `OPTIONAL MATCH`
clauses with one `OPTIONAL-MATCH`-free core read plus up to four additional
`OPTIONAL-MATCH`-free, index-anchored enrichment reads (far/anchor × File/Repo),
issued only when the core read returns rows. Each read keeps the same selective indexed
anchor and bounded one-hop fan-out as before; no full-label scan, no
variable-length traversal, and no unlabelled anchor is introduced (the middle
`enrichNode` is bound by traversal from the indexed anchor, not scanned). The
prior single query returned wrong results, so correctness governs; the enrichment
reads add bounded, indexed work proportional to the edge count already being
returned. Every read now also carries a deterministic `ORDER BY` and
`LIMIT $row_limit` (`nornicDBRelationshipRowLimit = 500`), so a pathological
high-degree symbol can no longer trigger an unbounded graph read on this hot API
path — a strict improvement over the pre-change unbounded read, and the typed
`keyed_support`/`single_key` disposition recorded for both callsites in
`internal/queryplan/testdata/query-source-coverage.yaml`. `ORDER BY`+`LIMIT` on
the `OPTIONAL-MATCH`-free shape was verified live to keep `type(rel)` evaluated
and the row set deterministically bounded. Measured live on the pinned image,
the fixed route resolves the seeded inheritance fixture in 0.13s end to end
including seed-free readback.

Observability Evidence: no metric, span, log field, queue stage, worker knob, or
status field changes. The relationship route keeps its existing query spans and
duration metrics, which continue to expose graph read latency and failures for
this path. The behavior change is a correctness fix (empty → correct edges),
observable through the same request-path telemetry.
