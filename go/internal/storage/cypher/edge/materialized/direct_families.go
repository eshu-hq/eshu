// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialized

import (
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/security/group"
	edgewriter "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"
)

// multiTypeDirectMaterializedEdgeFamilies indexes the direct-materialization
// families that own more than one relationship type (#6228 split verdict).
// Membership requires a held, exactly-scoped retract: either an edge-scoped
// alternation (scope_id plus evidence_source) or a node-detach on a
// sole-creator evidence label (kind A). The drift guard pins predicate shape
// on this table per RetractKind.
// It sits beside singleTypeMaterializedEdgeFamilies rather than inside it so
// TestSingleTypeFamiliesDeclareExactlyOneType keeps its exactly-one meaning:
// a second type here is a multi-type family with its own reasoning, never a
// quiet append to the single-type premise. Each entry carries the same
// retract-contract fields (RetractKind, RetractCypher plus RetractVariants,
// IdentityCypher), and the drift guard ranges over both tables with the same
// RetractKind dispatch. The resolver consults this table only through explicit
// switch arms in ifa.MaterializedEdgeDomainEdgeTypes, following the code_calls
// precedent, so registering here never silently widens the default lookup.
var multiTypeDirectMaterializedEdgeFamilies = map[string]materializedEdgeFamily{
	// incident_routing_evidence is the multi-type evidenceNodeDetach family
	// (#6228 split verdict, kind A): three routing-slot types hanging off
	// private IncidentRoutingEvidence nodes, reaped by DETACH DELETE with the
	// edges cascading. The slot-to-type map the write path screens each row
	// against is the derivation source the drift guard reads.
	"incident_routing_evidence": {
		EdgeTypes: map[string]string{
			"HAS_INTENDED_ROUTING": "intended routing edge (IncidentRoutingEvidenceWriter in edge/writer/incident_routing_evidence.go)",
			"HAS_APPLIED_ROUTING":  "applied routing edge (IncidentRoutingEvidenceWriter in edge/writer/incident_routing_evidence.go)",
			"HAS_LIVE_ROUTING":     "live routing edge (IncidentRoutingEvidenceWriter in edge/writer/incident_routing_evidence.go)",
		},
		RetractKind:    retractKindEvidenceNodeDetach,
		RetractCypher:  edgewriter.RetractIncidentRoutingEvidenceCypher,
		IdentityCypher: edgewriter.CanonicalIncidentRoutingEvidenceUpsertCypherFormat,
	},
	// observability_coverage is a closed-vocabulary relAlternation family
	// (#6228 split verdict, kind C): five AWS_COVERS_<signal> types named in
	// both retract variants. The vocabulary the write path screens each row
	// against is the derivation source the drift guard reads.
	"observability_coverage": {
		EdgeTypes: map[string]string{
			"AWS_COVERS_alarm":           "alarm coverage edge (ObservabilityCoverageEdgeWriter in edge/writer/observability_coverage.go)",
			"AWS_COVERS_composite_alarm": "composite alarm coverage edge (ObservabilityCoverageEdgeWriter in edge/writer/observability_coverage.go)",
			"AWS_COVERS_dashboard":       "dashboard coverage edge (ObservabilityCoverageEdgeWriter in edge/writer/observability_coverage.go)",
			"AWS_COVERS_log_group":       "log group coverage edge (ObservabilityCoverageEdgeWriter in edge/writer/observability_coverage.go)",
			"AWS_COVERS_trace_sampling":  "trace sampling coverage edge (ObservabilityCoverageEdgeWriter in edge/writer/observability_coverage.go)",
		},
		RetractCypher: edgewriter.RetractObservabilityCoverageEdgesCypher,
		RetractVariants: []string{
			edgewriter.RetractObservabilityCoverageEdgesByUIDsCypher,
		},
		IdentityCypher: edgewriter.CanonicalObservabilityCoverageEdgeUpsertCypherFormat,
	},
	// security_group_sg_rule is a closed-vocabulary relAlternation family
	// (#6228 split verdict, kind C): ALLOWS_INGRESS and ALLOWS_EGRESS named in
	// both SG -> rule retract variants (full-scope plus ledger-anchored
	// ByUIDs). The direction vocabulary the write path screens each row
	// against is the derivation source the drift guard reads. The sibling TO
	// family (security_group_rule_endpoint) is a separate family with its own
	// single-type retract and is not covered here.
	"security_group_sg_rule": {
		EdgeTypes: map[string]string{
			"ALLOWS_INGRESS": "ingress direction edge (SecurityGroupReachabilityWriter in cloud/aws/security/group/security_reachability_edge_writer.go)",
			"ALLOWS_EGRESS":  "egress direction edge (SecurityGroupReachabilityWriter in cloud/aws/security/group/security_reachability_edge_writer.go)",
		},
		RetractCypher: group.RetractSecurityGroupSGRuleEdgesCypher,
		RetractVariants: []string{
			group.RetractSecurityGroupSGRuleEdgesByUIDsCypher,
		},
		IdentityCypher: group.CanonicalSecurityGroupSGRuleEdgeUpsertCypherFormat,
	},
}

// MultiTypeDirectMaterializedEdgeFamilyNames returns the registered multi-type
// direct family names, so callers and tests can enumerate coverage without
// importing the table itself.
func MultiTypeDirectMaterializedEdgeFamilyNames() []string {
	out := make([]string, 0, len(multiTypeDirectMaterializedEdgeFamilies))
	for family := range multiTypeDirectMaterializedEdgeFamilies {
		out = append(out, family)
	}
	return out
}

// MultiTypeDirectMaterializedEdgeTypes returns the relationship types the named
// multi-type direct family materializes, keyed by type and mapped to a short
// reason. It fails closed like SingleTypeMaterializedEdgeTypes: an empty set
// would make the live gate assert zero edges and pass any graph vacuously.
func MultiTypeDirectMaterializedEdgeTypes(family string) (map[string]string, bool) {
	entry, ok := multiTypeDirectMaterializedEdgeFamilies[family]
	if !ok {
		return nil, false
	}
	out := make(map[string]string, len(entry.EdgeTypes))
	for edgeType, reason := range entry.EdgeTypes {
		out[edgeType] = reason
	}
	return out, true
}
