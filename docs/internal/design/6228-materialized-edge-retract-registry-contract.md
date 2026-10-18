# #6228 materialized-edge retract-registry contract

Split verdict D on the six direct-materialization families that could not
register under the single-type `[rel:TYPE]` retract contract (arbiter ruling,
issue #6228 comment 2026-10-17). This ADR records the two retract kinds the
registry now accepts, the proof each kind demands, and why the remaining two
families stay out.

## The two retract kinds

`materializedEdgeFamily` in
`go/internal/storage/cypher/edge/materialized/families.go` carries a
`RetractKind`:

- `relAlternation` (default): the retract names every owned relationship type
  in a `[rel:A|B]` alternation while keeping the `evidence_source` and scope
  predicates. Every family registered before this contract keeps this meaning
  without an edit.
- `evidenceNodeDetach`: the retract `MATCH`es the family's private evidence
  nodes and `DETACH DELETE`s them, reaping the edges as a cascade. No type
  alternation is needed — or wanted — because the sole-creator census proves
  no other writer's edges can hang off these nodes.

`RetractCypher` plus `RetractVariants` must hold every production retract
variant (full-scope, stale-generation, ledger-anchored `ByUIDs`). The drift
guard (`TestMaterializedEdgeFamilyRegistryMatchesItsRetract`) checks all held
variants, dispatching on `RetractKind`, so a variant edited without its
alternation or detach shape turns the build red.

Multi-type direct families live in a separate table beside the single-type
one (`multiTypeDirectMaterializedEdgeFamilies`). The exactly-one-type test
still ranges over the single-type table only; the resolver consults the
multi-type table solely through explicit switch arms in
`ifa.MaterializedEdgeDomainEdgeTypes`, following the `code_calls` precedent.

## Disposition of the six families

| Family | Verdict | Registered types |
|---|---|---|
| `code_taint_evidence` | node-scoped (A), single-type table | `{HAS_TAINT_EVIDENCE}`, derived from the held write template's `MERGE` |
| `incident_routing_evidence` | node-scoped (A), multi-type table | 3 routing types, derived from the slot-to-type map cross-checked against the slot vocabulary |
| `observability_coverage` | named alternation (C), multi-type table | 5 `AWS_COVERS_*` types, both retract variants |
| `security_group_sg_rule` | named alternation (C), multi-type table | `{ALLOWS_INGRESS, ALLOWS_EGRESS}` |
| `semantic_entity_containment` | excluded, stays waived | none |
| `cloud_resource` | uncovered by design, stays waived | none |

A correction the ruling forced: only `cloud_resource` has an open vocabulary.
`observability_coverage` (5 closed `AWS_COVERS_*` types, screened per row
against `observabilityCoverageSignalVocabulary`) and `security_group_sg_rule`
(`ALLOWS_INGRESS`/`ALLOWS_EGRESS`, screened per row against
`securityGroupAllowsRelTypeVocabulary`) were always closed. Their waiver
reasons said "open" and were corrected, not their fixtures. The ruling rejects
bare `[rel]` plus `evidence_source` as a coverage basis for any family, and
rejects registering an observed-type list for `cloud_resource`.

## Proof obligations

- Kind A, per family: a sole-creator census test (a second production creator
  of the evidence label turns the build red); seeded live tests with a
  foreign-type edge on the doomed node (documents the cascade) and surviving
  foreign-`evidence_source` / foreign-scope siblings (proves the predicates).
- Kind C, per family: a RED/GREEN foreign-edge regression (a seeded
  same-source foreign-type edge is deleted by the old bare `[rel]` retract
  and survives the named alternation); before/after `PROFILE` on the pinned
  Neo4j; grouped-retract dispatch tests unchanged.
- Both kinds: derivation reads held production consts and vocabulary maps,
  never copied literals; the resolver arm and the table declaration are pinned
  to the same set at both ends of the seam.

## Sole-creator criterion

A family may use `evidenceNodeDetach` only while exactly one production file
`MERGE`s its evidence label. `TestEvidenceNodeSoleCreatorsCensus` scans every
non-test `.go` file under `go/internal/storage` at the AST string-literal
level (comments can never match) and fails on a missing or an extra creator.
Test files are excluded: they seed through fakes and DSN-gated backends, never
through production writers. A writer that moves packages keeps the census
green (basename comparison); a writer that gains a second creator turns it
red, and the kind-A registration must then move back to a named alternation or
revert.

## Why semantic containment fails the criterion

`semantic_entity_containment` retracts with a node-label alternation plus
`DETACH DELETE`:

```cypher
MATCH (n:Annotation|Typedef|...|Function)
WHERE n.repo_id IN $repo_ids
  AND n.evidence_source = $evidence_source
DETACH DELETE n
```

The detached nodes are shared semantic entities, not private evidence: other
writers' edges (`CONTAINS` from other scopes, call edges, documentation
edges) hang off the same nodes, so the cascade deletes foreign edges with
them. No census can make that safe — the fix is a dedicated `[rel:CONTAINS]`
edge retract as separate writer work, tracked by the family's unchanged waiver
rows. Registering the family under either kind today would bless the
over-deletion.

## Why cloud_resource stays uncovered

`WriteCloudResourceEdges` merges a per-row `AWS_<relationship_type>` drawn
from an open per-provider vocabulary: no closed set exists to name in an
alternation, and its bare-`[rel]` retract is rejected as a coverage basis by
the same argument that forced the kind-C refactors. Registering an
observed-type list would be an ever-growing allowlist that goes stale the day
a provider adds a relationship. Two unblock routes, either in a follow-up
issue:

1. Close the vocabulary upstream: screen the relationship type against a
   closed per-provider set at the writer (the observability/SG precedent),
   then name the alternation and register normally.
2. Predicate-scoped `assert-edges` scan: extend the assert path with a mode
   that scopes by `evidence_source` plus scope without naming types, with its
   own proof that no second writer shares the source. That mode does not exist
   today and must not be smuggled in as a bare-`[rel]` registration.

## Performance Evidence

No-Regression Evidence: the kind-C refactors narrow the relationship pattern
from any-type to a 2- and 5-member alternation with predicates unchanged, so
the matched population can only shrink; no new label scan, no new property,
no transaction change. Measured 2026-10-18 on the pinned Neo4j
(`docker-compose.live-backend-neo4j.yml`, coordinator-run at contract head
`c661db858`): 5/5 live foreign-edge tests PASS
(`ESHU_CYPHER_BOLT_DATABASE=neo4j` is required — stock Neo4j has no `nornic`
database, which is the runner default). Seeded 50 SG x 4 rules (200 owned +
200 foreign USES edges): bare-`[rel]` matches 400 rels at 1735 DbHits, the
named alternation matches 200 at 1131 DbHits (same predicates; the 200-count
delta is the spared foreign population), ByUIDs over 2 uids matches 8 at 161
DbHits. Backend torn down and volume removed after the run. Re-run before
merge of the per-family leaves if the retracts change:

```bash
docker compose -f docker-compose.live-backend-neo4j.yml up -d
# coordinate with scripts/lib/live-gate-lock.sh — never alongside another live gate
cd go && ESHU_CYPHER_BOLT_DSN=neo4j://127.0.0.1:7687 \
  go test ./internal/storage/cypher/ \
  -run 'TestLive(SecurityGroupSGRule|ObservabilityCoverage|CodeTaintEvidence|IncidentRoutingEvidence)' -v
docker compose -f docker-compose.live-backend-neo4j.yml down
```

No-Observability-Change: no metric, span, log key, or pipeline stage is added
or renamed; retract statements keep their phase metadata and summary keys.
