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

No-Regression Evidence: at representative scale (measured) — seeded a large
protection-edge population and measured the shipped WAF/TLS set query against the
base label scan with a 2-element `$edge_ids`, warm median of 4 runs over
Bolt-HTTP:

| corpus | shipped WAF-led set query | base `CloudResource` label scan |
|---|---:|---:|
| **3,633 WAF protection edges / 7,768 CloudResource nodes** | **1.2 ms** | 1.3 ms |

The widened WAF/TLS lookup is **statistically identical to the base scan** at
3.6k protection edges, because the WAF/ACM protection-edge population is a
**subset** of the `CloudResource` node population (only protected resources carry
the edge) — so the relationship-type scan is bounded by protected-resource count,
which is ≤ the base label scan the prior query already performed. The added cost
is therefore not unbounded in global WAF volume; it is bounded by (a subset of)
the same population as the unchanged base scan. This buys a **correct** result
(the prior query reported every edge as WAF/TLS-protected, including unprotected
ones). `EXPLAIN`/`PROFILE` returns no plan on the pinned build, so this
wall-clock measurement is the available evidence. If a future estate makes even
this bounded scan matter, the durable fix is a NornicDB semantic fix restoring
the edge-anchored `OPTIONAL MATCH` form (the 2-clause edge-anchored variant is
itself broken on this build — probed live, returns a null `edge_id`; tracked with
the other multi-clause reads on #5287), not a further Eshu workaround.

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
