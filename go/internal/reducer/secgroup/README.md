# secgroup

## Purpose

Projects `aws_security_group_rule` facts into the Option D network-reachability
graph: CidrBlock/PrefixList endpoint nodes, port-precise `:SecurityGroupRule`
nodes, the `SecurityGroup -[:ALLOWS_INGRESS|ALLOWS_EGRESS]-> rule` edges, and
the `rule -[:TO]-> endpoint` edges. This is issue #1135. This package moved out
of the flat `internal/reducer` root under issue #6061.

## Ownership boundary

This package owns the security-group endpoint/rule-node/reachability-edge
extraction (`ExtractSecurityGroupEndpointRows`, `ExtractSecurityGroupReachability`),
the three additive domain definitions and their handlers, and the rule/CIDR/
prefix-list uid derivations.

It does **not** own the `aws_security_group_rule` decoder (`schemadecode`), the
CloudResource join index or node uid (`cloudjoin`), quarantine
(`factdecode`), the scoped fact read (`factload`), or readiness phases
(`gpphase`). Registration stays in the reducer root
(`defaults_security_group_endpoint.go`), and the Cypher writers live in
`internal/storage/cypher/security_group_cidr_node_writer.go` and
`internal/storage/cypher/security_group_reachability_edge_writer.go`.

## Exported surface

| symbol | consumer |
|---|---|
| `CidrMaterializationDomainDefinition` | the root additive-domain registry (`defaults_security_group_endpoint.go`) |
| `SecurityGroupCidrMaterializationHandler` | the root additive-domain registry |
| `SecurityGroupEndpointNodeWriter` | `cmd/reducer` (implemented by `sourcecypher.SecurityGroupEndpointNodeWriter`); `defaults.go` declares `DefaultHandlers.SecurityGroupEndpointNodeWriter` with this type directly -- no root compat file |
| `ExtractSecurityGroupEndpointRows` | no cross-package caller today; kept exported for symmetry with the sibling extraction seams (`ExtractSecurityGroupReachability`, `iamescalation.ExtractIAMEscalationEdges`) |
| `RuleMaterializationDomainDefinition` | the root additive-domain registry |
| `SecurityGroupRuleMaterializationHandler` | the root additive-domain registry |
| `SecurityGroupRuleNodeWriter` | `cmd/reducer`; `defaults.go` declares the `DefaultHandlers` field with this type directly |
| `ReachabilityMaterializationDomainDefinition` | the root additive-domain registry |
| `SecurityGroupReachabilityMaterializationHandler` | the root additive-domain registry |
| `SecurityGroupReachabilityWriter` | `cmd/reducer`; `defaults.go` declares the `DefaultHandlers` field with this type directly |
| `SecurityGroupReachabilityResult` | no cross-package caller today; the bounded output type `ExtractSecurityGroupReachability` returns |
| `ExtractSecurityGroupReachability` | no cross-package caller today; also the shared extractor `SecurityGroupRuleMaterializationHandler` calls for rule-node projection |
| `SecurityGroupReachabilityEvidenceSource` | `cmd/reducer` (wires `ProjectedSourceEdgeBackfiller`) and `internal/reducer`'s own backfill-reader test call it directly -- no root compat forwarder |
| `SecurityGroupReachabilityNodesNotReadyFailureClass` | `internal/storage/postgres`' readiness claim gate -- a storage contract literal, not just a Go identifier -- called directly, no root compat alias |

See `doc.go` for the godoc-rendered contract.

## Dependencies

`reducer/contract`, `reducer/cloudjoin`, `reducer/factdecode`,
`reducer/factload`, `reducer/gpphase`, `reducer/payloadcore`,
`reducer/schemadecode`, `internal/facts`, `internal/graph/edgetype`,
`internal/telemetry`, `internal/truth`, `pkg/log`,
`sdk/go/factschema/aws/v1`. Never `internal/reducer`.

## Telemetry

| signal | dimensions |
|---|---|
| `eshu_dp_security_group_endpoint_nodes_total` | `endpoint_kind` (`cidr_block`/`prefix_list`) |
| `eshu_dp_security_group_reachability_rule_nodes_total` | -- |
| `eshu_dp_security_group_reachability_edges_total` | `edge_type` (`sg_rule`/`rule_endpoint`) |
| `eshu_dp_security_group_reachability_skipped_total` | `skip_reason` (`unresolved_anchor`/`unresolved_endpoint`/`unknown_source`) |

Every `skip_reason` and `endpoint_kind` is recorded even at zero so the series
exists and a rising skip rate charts from zero. Span
`reducer.security_group_reachability_materialization`; each handler's
completion log carries per-stage `load` / `extract` / `retract` /
`graph_write` / `phase_publish` / `total_duration_seconds`.

No-Regression Evidence: #6061 relocates this family's production logic
without changing it. Every hunk in the moved production files is a package
clause, an import requalification, or an identifier requalification: symbols
the reducer root supplied as one-line forwarders (`loadFactsForKinds`,
`partitionDecodeFailures`, `recordQuarantinedFacts`,
`buildCloudResourceJoinIndex`, `cloudResourceUID`, `anyToString`,
`sourceUIDsFromRowsByKey`, `publishIntentGraphPhase`,
`graphProjectionPhaseStateForIntent`, `decodeAWSSecurityGroupRule`) are now
called directly against the leaf package that already owned them
(`factload`, `factdecode`, `cloudjoin`, `payloadcore`, `gpphase`,
`schemadecode`). Measured on this branch: `go build ./...` exits 0, `go vet
./...` exits 0, `go test ./internal/reducer/secgroup -count=1` passes, and
`go test ./internal/reducer/... -count=1` passes.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph, or
storage contract. The four counters above and the span/log are the same
before and after the move.

## Gotchas / invariants

- Option D keys the rule node on `(sg_uid, direction, ip_protocol, from_port,
  to_port, source_kind, source_value)` -- port and protocol live in the NODE
  key so two ports key two distinct `:SecurityGroupRule` nodes, rather than in
  a relationship-property MERGE that times out on NornicDB.
- A referenced security group already has a `:CloudResource` node and is
  never re-materialized as a `CidrBlock`/`PrefixList` endpoint.
- An unscanned SG anchor or endpoint degrades gracefully (counted in the skip
  tally) -- it never dangles an edge against a node that does not exist.
- The reachability edge domain gates on all three canonical-nodes phases
  (rule, endpoint, cloud-resource); a miss on any one is retryable via
  `SecurityGroupReachabilityNodesNotReadyFailureClass`, enrolled in
  `nonCountingReducerRetryFailureClasses` so it never erodes the retry budget.
- The projected-source ledger (issue #4858, #4881) records the UNION of the
  SG->rule edge source uids (keyed `sg_uid`) and the rule->endpoint edge
  source uids (keyed `rule_uid`) BEFORE the edge writes, so a crash mid-write
  never orphans a graph edge the ledger cannot anchor a later retract on.
- Test helpers (`stubFactLoader`, `resourceEnvelope`,
  `recordingGraphProjectionPhasePublisher`, `fakeProjectedSourceLedger`,
  `statefulProjectedSourceLedger`, `metricHasAttrs`, `cloudResourceUID`,
  `anyToString`) are duplicated from the reducer root by design; do not
  export a root test helper to reach it.

## Related docs

- `go/internal/reducer/README.md`
- `go/internal/reducer/cloudjoin/README.md`
- `go/internal/reducer/gpphase/README.md`
- `go/internal/reducer/schemadecode/README.md`
- `docs/public/observability/telemetry-coverage.md`
