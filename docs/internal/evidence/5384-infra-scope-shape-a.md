# 5384 — Infra scope predicate SHAPE-A (workload name-collision authorization)

Issue [#5384](https://github.com/eshu-hq/eshu/issues/5384), epic #5161. Fixes the
shared scoped-token authorization predicate for the graph-backed infra read
routes (`infra/resources/count`, `/inventory`, `/search`,
`/relationships`) and the service workload resolution predicate.

## Root cause

`infraResourceScopePredicate` and `workloadScopePredicate` admitted scoped-token
callers through name-collapsed Workload nodes via `EXISTS` bridges. Two distinct
defects, both proven against the pinned NornicDB image
`eshu-nornicdb-pr261:149245885258` over Bolt (`neo4j-go-driver/v5`) and the HTTP
`tx/commit` endpoint:

- the shipped n-last 4-hop `EXISTS { (scopeRepo)-[:DEFINES]->(:Workload)<-
  [:INSTANCE_OF]-(:WorkloadInstance)-[:USES]->(n) WHERE scopeRepo.id IN $grants }`
  bridge evaluates **unconditionally false** on this backend — a scoped token
  saw ZERO CloudResources (silent under-authorization);
- the tempting rewrite `EXISTS { (n)<-[:USES]-(i) WHERE i.repo_id IN $grants }`
  (n-first backward-anchored EXISTS with a WHERE on the far variable) evaluates
  **unconditionally true** — a whole-graph leak.

The EXISTS shape-matrix and a second NornicDB pitfall (inline-map anchor drop
under an indexed-property WHERE) are recorded in the scratch note carried with
this branch and are proposed for `docs/public/reference/nornicdb-pitfalls.md`.

## Fix — SHAPE-A pattern-predicate disjunction

Replace the mis-evaluated `EXISTS` bridges with a pattern used directly as a
boolean predicate with an inline-map property term
(`(n)<-[:USES]-(:WorkloadInstance {repo_id:$scope_grant_0})`), correct on BOTH
NornicDB and Neo4j. The grant array expands into one inline-map term per grant
(O(grant)); the forward `DEPLOYMENT_SOURCE` admission keeps an `EXISTS` with an
`IN $array` filter (the one EXISTS shape the backend evaluates correctly). The
flat direct-ownership disjuncts are unchanged. Both the collision-defined
Workload (admitted via a granted `DEFINES`-ing repository, missed by the flat
`repo_id`) and the direct-ownership cases are covered.

The O(grant) fan-out is capped at `maxScopeGrantInlineTerms = 128` with
fail-closed degradation: past the cap the flat array disjuncts still admit all
direct-ownership grants, so a pathological >128-grant token loses only
collision/bridge admission for grants beyond the cap (missing rows, never extra
rows).

Performance Evidence:

Backend: NornicDB `eshu-nornicdb-pr261:149245885258` via isolated Compose
(project `infra-predicate-fix-nornic`, Bolt `27687`). Fixture: 500-workload
noise chain anchored at repo-a, the exact name-collision topology
(`workload:api` DEFINED by repo-a AND repo-b, materialized `repo_id=repo-b`,
instance `repo_id=repo-b` USES the tenant-B secret `cloud-resource:c-b`), a
deployment-repo-only-grant instance, and a worst-case CloudResource with 1000
inbound USES edges. Grant array of 10 (`repo-a` + 9 pad ids). Measured as the
production whole-label `count(n)` filter with the EXACT shipped composed
predicate (flat + USES inline-map + forward DEPLOYMENT_SOURCE + DEFINES
inline-map).

| Shape | CloudResource count (grant repo-a) | cold | warm min |
| --- | ---: | ---: | ---: |
| OLD n-last EXISTS bridge | 0 (dead / under-auth) | 45.5ms | 1.4ms |
| NEW SHAPE-A (shipped) | 501 (500 noise + hot; excludes c-b + cross-deploy) | 236.7ms | 1.4ms |

Worst-case single node (`cloud-resource:hot`, 1000 inbound USES, 10-grant):
OLD 3.4ms cold / 1.5ms warm (matched=0, dead); NEW 10.0ms cold / 1.4ms warm
(matched=1). Empty labels (TerraformResource, K8sResource) stay cheap
(2.7–3.7ms cold) — the inline-map/DEPLOYMENT_SOURCE disjuncts are inert for
labels without those inbound edges. The n-first-backward EXISTS anchors on the
bound node (bounded inbound fan) rather than the OLD whole-graph scopeRepo scan,
so NEW is per-node cheaper than the OLD bridge at scale.

Intended behavior delta (not output-preserving — this is a correctness fix):

| Case (grant repo-a) | OLD | NEW | Expected |
| --- | ---: | ---: | ---: |
| legit CloudResource (repo-a USES) | 0 (dead) | 1 | 1 |
| tenant-B secret c-b (LEAK test) | 0 | 0 | 0 |
| collision Workload (repo_id=repo-b, DEFINES repo-a) | 0 (dead) | 1 | 1 |
| deploy-repo-only instance (DEPLOYMENT_SOURCE->repo-a) | 0 (dead) | 1 | 1 |
| WorkloadInstance repo_id=repo-b, deploy repo-b | — | 0 | 0 |

The naive backward-EXISTS rewrite was measured at 2/2 (leak) on the same
fixture and rejected. Reproduced by
`go test -tags live_infra_scope_shape ./internal/query -run
TestLiveInfraScopeShapeShapeADiscriminates` (gated on `ESHU_CYPHER_BOLT_DSN`),
which drives the real production predicate builders as `count(n)` filters
against a live NornicDB and asserts RED (dead bridge = 0, naive backward = 2) →
GREEN (SHAPE-A discriminates). Fast unit coverage:
`go test ./internal/query -run 'ScopeGrant|InfraResourceScopePredicate|WorkloadScopePredicate'`.

No-Observability-Change:

The scope predicate renders inside the existing infra aggregate / search /
relationship and service-workload graph reads. It adds no new span, metric,
runtime knob, queue behavior, or graph write; the handlers keep their existing
`GraphQuery.Run` / `RunSingle` adapters and per-query duration/error telemetry.
The `maxScopeGrantInlineTerms` cap exposes a `capped` bool from
`scopeGrantInlineScalars` for a future operator signal; wiring that signal to a
log or metric (which would touch the telemetry coverage contract) is deliberately
out of scope for this correctness fix and tracked as a follow-up.
