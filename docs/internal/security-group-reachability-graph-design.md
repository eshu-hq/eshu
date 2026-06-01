# SecurityGroup Network-Reachability Graph (Option D) — #1135 PR2b

> Status: gated graph-write (`risk:schema`). Needs principal review before merge.

This slice projects `aws_security_group_rule` facts into a queryable
network-reachability graph using the principal-approved **Option D**
(reachability-node model).

## Model

Each live security-group rule becomes one `:SecurityGroupRule` node, with
closed-vocabulary edges:

```
(:SecurityGroup CloudResource)-[:ALLOWS_INGRESS|ALLOWS_EGRESS]->(:SecurityGroupRule)-[:TO]->(:CidrBlock|:SecurityGroup CloudResource|:PrefixList)
```

Port and protocol are **node properties keyed in the rule uid**, not relationship
properties. This is the core of Option D: a port-precise key (two ports → two
nodes) that keeps the relationship MERGE on a static token, so NornicDB uses its
relationship hot path. A property-keyed relationship MERGE timed out at 20s vs
0–1ms for the static-token shape (#805 §5.3), which is why the encoding fork was
resolved in favor of Option D over rule-as-edge-property.

### Rule node uid

`securityGroupRuleUID = StableID("SecurityGroupRule", {sg_uid, direction,
ip_protocol, from_port, to_port, source_kind, source_value})`, where `sg_uid` is
the resolved `cloudResourceUID(account, region, "aws_ec2_security_group",
group_id)`. Ports are normalized to a type-stable decimal token first, because
they arrive as `int32` in-memory but `float64` after a Postgres JSON roundtrip;
without normalization the same rule would key two different nodes between local
tests and production.

### Edge keying

- SG→rule: idempotent on `(sg_uid, ALLOWS_INGRESS|ALLOWS_EGRESS, rule_uid)`;
  relationship type from direction (closed 2-member vocab, allowlist + char-class
  validated before interpolation).
- rule→endpoint: idempotent on `(rule_uid, TO, target_uid)`; heterogeneous target
  label ∈ {CidrBlock, CloudResource, PrefixList} validated against a closed
  vocabulary before interpolation.

## Resolution and readiness

Resolution mirrors the AWS relationship edge join (#805 §5.1): a per-generation
in-memory CloudResource join index resolves the SG anchor and a referenced-group
endpoint; CidrBlock/PrefixList endpoint uids are recomputed with the same uid
funcs the #1135 PR2a endpoint materialization used. An unresolved SG anchor,
unresolved endpoint, or `unknown` source skips the rule and is counted — no
dangle, no fabrication.

The slice is two domains in one PR: `security_group_rule_materialization`
(publishes the `security_group_rule_uid` canonical-nodes phase) and
`security_group_reachability_materialization` (the edges). The edge domain gates
on **three** `canonical_nodes_committed` phases — `security_group_rule_uid`,
`security_group_endpoint_uid` (#1135 PR2a), and `cloud_resource_uid` (#805) — via
the durable Postgres claim/blockage gate plus the in-handler per-keyspace
`ReadinessLookup`. The not-ready error is retryable; the prior-generation retract
is edge-scope-filtered by `evidence_source` + `scope_id`.

PR2a (#1163) shipped the endpoint node handler/schema/phase but no projector
trigger, so this PR also wires the endpoint, rule-node, and edge intents (all
anchored on the shared `aws_resource_materialization:<scope>` acceptance unit so
the triple-gate join resolves).

## Deferred

Transitive `resource -[:REACHABLE_FROM]-> resource` across NACLs and route tables
is a follow-up, not this slice.

## Evidence

No-Regression Evidence: focused Eshu-owned write-path benchmark on Apple M4 Pro,
Go test no-op group executor (isolates Eshu statement-construction/batching from
graph round trips). `BenchmarkSecurityGroupReachabilityWriter` writes all three
surfaces (5000 rule nodes + 5000 SG→rule edges + 5000 rule→endpoint TO edges =
15000 rows) at 4.26ms/op, 7.76MB/op, 75367 allocs/op — ~283ns/row, in the same
shape class as the proven baselines on the same machine
(`BenchmarkObservabilityCoverageEdgeWriter` 1.72ms/op for 5000 rows ≈ 344ns/row;
`BenchmarkKubernetesCorrelationEdgeWriter` 1.32ms/op for 5000 rows ≈ 264ns/row).
The reachability writer reuses the identical batched-MERGE row shape (UNWIND +
MATCH-MATCH-MERGE on uid anchors, static-token / validated-label grouping), so it
inherits the COVERS/RUNS_IMAGE no-N+1 write profile; the higher absolute op time
is the three write surfaces, not a per-row regression. Rule and endpoint MATCHes
anchor on uid (backed by the new `SecurityGroupRule` uid constraint + NornicDB uid
index, and the existing CidrBlock/PrefixList/CloudResource uid indexes), so no
MATCH falls back to a label scan.

Observability Evidence: new `eshu_dp_security_group_reachability_rule_nodes_total`
(rule nodes committed), `eshu_dp_security_group_reachability_edges_total`
(edge_type=sg_rule|rule_endpoint), and
`eshu_dp_security_group_reachability_skipped_total`
(skip_reason=unresolved_anchor|unresolved_endpoint|unknown_source) counters, the
`reducer.security_group_reachability_materialization` span, and a structured
completion log carrying per-stage durations (load/extract/retract/write) and the
skip tally let an operator answer "are reachability edges landing, and if a
generation produced nodes but zero TO edges, which node family was unscanned?" at
3 AM. The durable status-blockage query surfaces which of the three gate
keyspaces is the one still uncommitted.
