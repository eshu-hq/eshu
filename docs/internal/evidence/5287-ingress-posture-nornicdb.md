# #5287 (part 2) — service ingress-posture NornicDB-safe reads: before/after

`loadServiceIngressPosture` (WAF/TLS protection tile for a service's
internet-facing edge resources) ran one multi-clause aggregation that the pinned
NornicDB build mis-executes. Rewritten to three single-clause set queries merged
in Go.

Backend: NornicDB `eshu-nornicdb-pr261:149245885258`
(commit `1492458852588c884c32f70d27ea2ee07086769c`), isolated Compose project
`eshu-5287b`. Live over Bolt-HTTP on seeded `CloudResource` WAF/ACM edges.
Same-machine relative before/after.

## Accuracy Evidence (behavior fix — corrected delta)

Ground truth (seed): edge-1 is WAF-protected and ACM-terminated; edge-2 has
neither.

| | before (multi-clause) | after (3 single-clause sets) |
|---|---|---|
| query | `MATCH edge OPTIONAL MATCH waf WITH edge, count(*)>0 OPTIONAL MATCH acm RETURN …, count(*)>0` | base set + waf set + tls set, membership merged in Go |
| edge-1 | `edge_id=null`, waf=true, tls=true | `edge-1` present, waf=**true**, tls=**true** |
| edge-2 | `edge_id=null`, waf=**true**, tls=**true** (WRONG — unprotected) | `edge-2` present, waf=**false**, tls=**false** (observed-negative) |

The multi-clause `count(*)>0`-over-`OPTIONAL MATCH` between two clauses returns a
null `edge_id` and reports both flags true for every edge, so an unprotected edge
was falsely shown as WAF/TLS protected. An `EXISTS { … }` subquery is equally
broken — it does not correlate with the outer `edge` (returns true for the
unprotected edge too). The three single-clause set queries (base = which edges
exist / collectorPresent, waf = WAF-protected set, tls = ACM-terminated set)
compute each edge's flags by membership, preserving the collectorPresent (base
row) vs observed-negative (present, flag false) vs missing (absent) distinction
the tile relies on.

## Performance Evidence

This is a **correctness fix**; the honest cost picture (no index seek is
possible, and the safe single-clause shape cannot anchor the protection
traversal on the bounded edge set):

- `ingressPostureBaseCypher`: `MATCH (edge:CloudResource) WHERE coalesce(edge.id,
  edge.uid, edge.resource_id, edge.arn, edge.name) IN $edge_ids`. The 5-property
  `coalesce` identity **cannot use an index seek**, so this is a full
  `CloudResource` **label scan** filtered by the `IN` list. This is the SAME scan
  the prior query's base `MATCH` already performed (the old form anchored the
  same way) — **no change** to the base cost.
- `ingressPostureWafCypher` / `ingressPostureTLSCypher`: single-clause
  `MATCH (:CloudResource)-[:AWS_wafv2_web_acl_protects_resource]->(edge:CloudResource)
  WHERE <identity> IN $edge_ids`. NornicDB scans the WAF / ACM **relationship-type
  population** and filters `edge`. The prior form expanded these as
  `OPTIONAL MATCH` from the already-bounded `edge`, so this rewrite **widens** the
  WAF/TLS lookups from a per-edge expansion to a relationship-type scan. The safe
  single-clause contract requires this — anchoring the traversal on the bounded
  edge first is a 2-clause shape, which was probed live and returns a null
  `edge_id` (the same multi-clause projection defect), so it cannot be used.

No-Regression Evidence: the added cost is bounded by the **WAF web ACL /
ACM-certificate protection-edge population**, which is small and slow-growing per
AWS account (these edges exist only for WAF-protected / TLS-terminated resources —
typically tens to low hundreds), not by total graph size. The base scan is
unchanged from the prior query. So on a realistic estate the net change is two
extra scans over a small relationship population, in exchange for a **correct**
result (the prior query reported every edge as WAF/TLS-protected, including
unprotected ones). Absolute timing was taken only on a 4-node seed and is not a
representative-partition measurement; `EXPLAIN`/`PROFILE` returns no plan on the
pinned build, so no db-hit evidence is available. If the WAF/ACM edge population
is ever large enough to matter, the durable fix is a NornicDB semantic fix that
restores the bounded edge-anchored `OPTIONAL MATCH` form (tracked with the other
multi-clause reads on #5287), not a further Eshu workaround.

## Observability Evidence

No-Observability-Change: no new metric, span, or log field. The posture read
keeps its existing `GraphQuery.Run` spans (now three per call instead of one);
the tile's honesty semantics (collectorPresent / unproven) are unchanged.

## Verification

- Live Bolt-HTTP before/after on the seeded `eshu-5287b` stack: old query
  (null edge_id, both flags true for the unprotected edge) vs the three-set
  merge (edge-1 true/true, edge-2 false/false).
- `go test ./internal/query -run 'IngressPosture'` — the query-shape guard
  (single-clause, no OPTIONAL/WITH/aggregate) and the handler tests updated for
  the three-query design.
