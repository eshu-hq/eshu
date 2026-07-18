# #5287 — resource-investigation impact reads NornicDB-safe

`go/internal/query/impact_resource_investigation.go`. Two multi-clause reads
corrupt on the pinned NornicDB backend (`eshu-nornicdb-pr261`, v1.1.11 base).
Neither the file nor the handler branches on backend, so the unsafe shapes run
directly on NornicDB.

Classification: **Correctness win** (behavior fix — old returned corrupt/empty
graph truth). No hot-path performance regression (see below).

## Before (live, Bolt driver, DB nornic)

Seed: `(:WorkloadInstance)-[:USES]->(:CloudResource)`,
`(:WorkloadInstance)-[:INSTANCE_OF]->(:Workload)`,
`(:CloudResource)-[:BELONGS_TO]->(:Repository)`.

| read | old shape | old result |
| --- | --- | --- |
| `resourceInvestigationWorkloads` | `MATCH (resource) WHERE … MATCH (instance)-[rel:USES]->(resource) OPTIONAL MATCH (instance)-[:INSTANCE_OF]->(workload) WITH … RETURN <computed>` | every column null |
| `resourceInvestigationRepoPaths` | `MATCH (resource) WHERE … MATCH path=… RETURN … length(path) AS depth, [rel IN relationships(path) | {…}] AS hops` | `repo_id` null, `depth` 0, `hops` null |

Two independent NornicDB corruptors were isolated live:

1. **Multi-clause read → null projection** (documented pitfall): the
   `MATCH+MATCH+OPTIONAL MATCH+WITH → computed RETURN` chain collapses all columns
   to null; the map-valued `[rel IN relationships(path) | {…}]` comprehension
   mangles `hops`, `depth`, and `repo_id`.
2. **Empty-guard OR-arm** (newly characterised): the resource anchor
   `coalesce(...) = $resource_id OR ($resource_arn <> '' AND resource.arn = $resource_arn)`
   returns **zero rows** when `$resource_arn = ''` — the `'' <> ''` guard disjunct
   mis-evaluates and the whole predicate collapses. Isolated:
   `coalesce(...) = $id` alone → 1 row; adding `OR ($arn <> '' AND resource.arn=$arn)`
   with `$arn=''` → 0 rows; `coalesce(...) = $id OR resource.arn=$arn` (no guard,
   non-matching arn) → 1 row.

## Fix

- **Shared anchor** `resourceInvestigationResourceAnchor(alias, hasArn)`:
  `coalesce(alias.id, alias.uid, alias.resource_id, alias.name) = $resource_id`,
  appending ` OR alias.arn = $resource_arn` **only** when the resolved candidate
  carries an arn (the `<> ''` guard is dropped). `resourceInvestigationAnchorParams`
  binds `$resource_arn` in lockstep.
- **`resourceInvestigationWorkloads`**: split into two single-clause reads joined
  in Go — (1) the USES instances with rel provenance + environment, ordered by
  instance_id; (2) `MATCH (instance:WorkloadInstance)-[:INSTANCE_OF]->(workload:Workload) WHERE instance.id IN $instance_ids`
  for the surviving instances. `workload_id`/`workload_name` are coalesced in Go
  exactly as before; the result is re-sorted by (workload_name, workload_id,
  instance_id) to preserve display order.
- **`resourceInvestigationRepoPaths`**: single-clause path read projecting raw
  `relationships(path) AS rels`; the `{type, confidence, reason}` hop maps are
  rebuilt in Go by `resourceInvestigationHopList` (driver-aware seam in neo4j.go,
  handling both `neo4j.Relationship` and NornicDB's `map[string]any`). Per-hop
  provenance is fully preserved — the raw edge properties survive where the
  comprehension does not. `direction` moved from a `%q AS direction` column to a
  Go constant.

## After (live)

`resourceInvestigationWorkloads` → 1 workload `{workload_id: ri5287:wl,
workload_name: orders, instance_id: ri5287:inst, environment: prod,
relationship_type: USES, relationship_reason: runtime-use, confidence: 0.91}`.
`resourceInvestigationRepoPaths(outgoing)` → 1 path `{repo_id: ri5287:repo,
repo_name: orders-api, depth: 1, hops: [{type: BELONGS_TO, confidence: 0.77,
reason: provisioned-by}]}`.

## Verification

- `go test ./internal/query -run 'ResourceInvestigation|Investigate' -count=1` — green.
- Unit guards: `TestResourceInvestigationResourceAnchorOmitsEmptyArnGuard`,
  `TestResourceInvestigationAnchorParamsBindArnOnlyWhenPresent`,
  `TestResourceInvestigationHopListDecodesBothBackends`,
  `TestResourceInvestigationHopReasonFallsBackToEvidenceType`.
- Backend-required live: `TestLiveResourceInvestigationReadsAreNornicDBSafe`
  (`ESHU_INFRA_AGG_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687`) drives the
  shipped handler methods; reverting either rewrite reproduces the null/mangled
  columns.

No-Regression Evidence: The reads change shape but not asymptotic cost. The prior
workloads read was already anchored on an unlabeled `MATCH (resource)` + a
`-[:USES]->` hop; the new form anchors the same resource identity inside the
`(instance:WorkloadInstance)-[:USES]->(resource)` pattern (label-anchored on the
instance) plus one bounded `IN $instance_ids` lookup over the already-limited
instance set — no new whole-graph scan. The repo-paths read keeps the same
variable-length `(resource)-[rels*1..N]->(repo:Repository)` pattern and swaps a
map-valued projection for a raw `relationships(path)` projection unwound in Go
(equal result cardinality). Same pinned backend, same anchors, correctness-only
delta; the reads are per-request bounded (limit + depth capped by the
resource-investigation request normalizer).

No-Observability-Change: No metrics, spans, logs, or status fields are added,
removed, or renamed; only the Cypher shapes and their Go decoding changed.
