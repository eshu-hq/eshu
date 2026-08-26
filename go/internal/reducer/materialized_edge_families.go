// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "sort"

// MaterializedEdgeFamilies returns the drift-proof enumeration of reducer
// domains that materialize graph edges directly from parser/collector facts,
// for the Ifá `materialized_edges:<domain>` exhaustiveness gate (#5351). The
// gate binds an Odù expectation to each returned family so a reducer
// materialization silently ceasing to produce an edge family is caught,
// mirroring the P2/P4 graph-determinism and fault-injection proof this
// package already backs (go/internal/ifa/graphdump, scripts/verify-ifa-
// determinism.sh, scripts/verify-ifa-fault-injection.sh).
//
// The result is exactly allProjectionDomains (shared_projection.go), sorted:
// the 14 reducer-owned shared/edge projection domains that write graph edges
// through the ordering-safe shared-projection intent path (repo_dependency,
// workload_dependency, code_calls, sql_relationships, shell_exec,
// inheritance_edges, documentation_edges, rationale_edges,
// deployable_unit_edges, handles_route, runs_in, invokes_cloud_action,
// codeowners_ownership_edges, submodule_pin_edges).
// The full set contains 14 allProjectionDomains families.
// TestMaterializedEdgeFamiliesLocksToAllProjectionDomains locks the two in
// lockstep so a domain added to or removed from allProjectionDomains moves
// this enumeration in the same change, never a second hand-edit.
//
// This is a narrower set than the reducer's full materialized-edge surface.
// The reducer also writes edges DIRECTLY, one family per port straight to a
// go/internal/storage/cypher writer with no intent row in between, and those
// families are enumerated by DirectMaterializedEdgeFamilies below rather than
// here. #5351 landed the gate plus first coverage for sql_relationships,
// leaving the rest waived to per-family child issues. #5991 later added the
// live code_calls baseline/fault proof, #5994 added documentation_edges, #5998
// added rationale_edges, #5993/#6158 wired deployable_unit_edges' vacuity
// guard into the resolver dispatch and its Odu into the catalog -- both
// existed with their own tests but neither was reachable from a production
// coverage run -- and #6002 added the submodule_pin_edges Odu, cassette, and
// expected-edge-set fixture. #5992/#6160 added codeowners_ownership_edges,
// #5999 added repo_dependency, and #5996/#6001 added inheritance_edges and
// shell_exec. #6003 added workload_dependency. #5995/#6000/#5997 added
// handles_route/runs_in/invokes_cloud_action, the last three families under
// this umbrella.
// Those changes removed their families' waivers. No allProjectionDomains
// family carries a waiver as of this change: #5543's decomposition into
// per-domain child issues (#5991-#6003) is complete, and #5543 is closed.
// The direct-materialization families below carry waivers of their own,
// tracked by #6228.
func MaterializedEdgeFamilies() []string {
	out := make([]string, 0, len(allProjectionDomains))
	for _, domain := range allProjectionDomains {
		out = append(out, string(domain))
	}
	sort.Strings(out)
	return out
}

// Direct-materialization edge families for the Ifá `materialized_edges:<family>`
// exhaustiveness gate (#6181, coverage tracked by #6228).
//
// MaterializedEdgeFamilies (materialized_edge_families.go) inventories the
// SHARED half of the reducer's materialized-edge surface: the domains that
// reach the graph through the ordering-safe shared-projection intent path, one
// bare WriteEdges port carrying the family as a runtime `domain` argument. The
// enumeration below inventories the DIRECT half: the ports the reducer declares to write one
// specific edge family straight to a go/internal/storage/cypher writer, with no
// intent row in between.
//
// The two halves are kept in separate enumerations on purpose.
// MaterializedEdgeFamilies is locked to allProjectionDomains by
// TestMaterializedEdgeFamiliesLocksToAllProjectionDomains, and three further
// guards are asserted total over its result in both directions (the cypher
// identity map, the Ifá trigger-stem map, and the ledger's waiver-surface
// check). Folding the direct families into it would loosen the lockstep and
// force twenty-eight hand-fed entries into each of those totality maps, to buy
// nothing the coverage gate cannot get by reading both enumerations.
//
// # Why the port name is not the source of truth
//
// A previous attempt at this enumeration derived the family set by matching
// reducer interface methods against `^Write(.+)Edges$`, on the stated belief
// that the name shape was structural — that a port writing exactly one family
// always bakes that family into its name. It is a convention, not a structure,
// and six ports break it. WriteKubernetesNamespaceNodes is named for nodes and
// MERGEs TARGETS_ENVIRONMENT; WriteCodeInterprocEvidence and
// WriteCodeTaintEvidence are named for evidence and MERGE TAINT_FLOWS_TO and
// HAS_TAINT_EVIDENCE; WriteSemanticEntities is named for entities and MERGEs
// CONTAINS. A name-derived enumeration reports green while those families stay
// invisible, which is the exact false green the exhaustiveness gate exists to
// prevent, reproduced inside the fix for it.
//
// So the tables below are keyed by port and classified against the Cypher each
// port actually executes.
// TestDirectMaterializedEdgePortsMatchTheExecutedCypher
// (go/internal/ifa/materializededges) re-derives that classification from the
// go/internal/storage/cypher source and holds these tables to it, in both
// directions and over every reducer graph-write port — so a new port must be
// classified here on the commit that adds it, whatever it is named.

// directMaterializedEdgeFamilyByPort maps each reducer edge-write port to the
// materialized-edge family it writes.
//
// The family name is the ledger's `materialized_edges:<family>` surface key. It
// is close to the port name for most entries but is NOT derived from it: where
// the port name would mislead, the family is named for the relationship the
// port materializes instead. kubernetes_namespace_environment is the clearest
// case — deriving it mechanically would produce "kubernetes_namespace_nodes",
// an edge family named for nodes, and every reader of the ledger would then
// carry the same wrong belief that produced the name-derived enumeration this
// table replaces.
var directMaterializedEdgeFamilyByPort = map[string]string{
	// Secrets/IAM projection (secrets_iam_graph_projection.go ->
	// cypher/secrets_iam_graph_writer.go). Five edge ports beside four
	// node-only ports on one writer; the node ports are excluded below.
	"WriteUsesServiceAccountEdges":     "uses_service_account",
	"WriteAssumesIAMRoleEdges":         "assumes_iam_role",
	"WriteAuthenticatesVaultRoleEdges": "authenticates_vault_role",
	"WriteUsesVaultPolicyEdges":        "uses_vault_policy",
	"WriteGrantsSecretReadEdges":       "grants_secret_read",

	// IAM materialization. iam_can_perform / iam_can_assume are two of the
	// five families #6181 named by inspection.
	"WriteIAMCanPerformEdges":          "iam_can_perform",
	"WriteIAMCanAssumeEdges":           "iam_can_assume",
	"WriteIAMEscalationEdges":          "iam_escalation",
	"WriteIAMInstanceProfileRoleEdges": "iam_instance_profile_role",

	// AWS resource / relationship materialization.
	"WriteCloudResourceEdges":               "cloud_resource",
	"WriteCloudResourceContainerImageEdges": "cloud_resource_container_image",
	"WriteEC2UsesProfileEdges":              "ec2_uses_profile",
	"WriteS3LogsToEdges":                    "s3_logs_to",
	"WriteWorkloadCloudRelationshipEdges":   "workload_cloud_relationship",

	// Security-group reachability. Two ports, two families: the SG->rule edge
	// and the rule->endpoint edge are separately materialized and can regress
	// independently, so they are not collapsed into one row.
	"WriteSecurityGroupSGRuleEdges":       "security_group_sg_rule",
	"WriteSecurityGroupRuleEndpointEdges": "security_group_rule_endpoint",

	// Container-image and package provenance.
	"WriteBuiltFromEdges":   "built_from",
	"WriteDerivedFromEdges": "derived_from",
	"WritePublishesEdges":   "publishes",

	// Kubernetes and Crossplane correlation.
	"WriteKubernetesCorrelationEdges": "kubernetes_correlation",
	"WriteCrossplaneSatisfiedByEdges": "crossplane_satisfied_by",

	// Observability coverage.
	"WriteObservabilityCoverageEdges": "observability_coverage",

	// The six ports whose names do not carry their family. Each is a real
	// production write site, not a dead port; each MERGEs the relationship
	// named beside it.
	"WriteCodeInterprocEvidence":     "code_interproc_evidence",          // TAINT_FLOWS_TO
	"WriteCodeTaintEvidence":         "code_taint_evidence",              // HAS_TAINT_EVIDENCE
	"WriteIncidentRoutingEvidence":   "incident_routing_evidence",        // incident -> routing
	"WriteS3ExternalPrincipalGrants": "s3_external_principal_grant",      // bucket -> external principal
	"WriteKubernetesNamespaceNodes":  "kubernetes_namespace_environment", // TARGETS_ENVIRONMENT
	"WriteSemanticEntities":          "semantic_entity_containment",      // CONTAINS
}

// directMaterializedEdgeNodeOnlyPorts are the reducer graph-write ports that
// upsert NODES only, mapped to the label each owns.
//
// They are listed rather than left out because leaving a port out is
// indistinguishable from never having looked at it — the same absence-is-not-a
// -waiver argument #6181 makes about the ledger itself, one level down. A port
// here is a reviewed decision that it materializes no edge.
//
// The drift guard is total in the EDGE direction only, and the distinction
// matters. It fails a port that MERGEs a relationship without being declared, a
// port declared node-only that MERGEs one anyway, and a port declared an edge
// family that writes none. A port in neither table that writes no edge falls
// through silently — which is deliberate, not an oversight. Of the 87 ports the
// scan classifies, 44 are in neither table. One of those is WriteEdges, the
// shared-projection port, which DOES write edges and is exempted by its own
// explicit branch rather than by writing none — sharedProjectionEdgeWritePort
// below calls it the one graph-write port belonging to neither table. Of the
// remaining 43, all but one are retract, sweep, execute or read ports. The
// exception is FailureClass, which is not a graph-write port at all: it is
// declared on reducerClassifiedFailure in service_heartbeat.go, an
// error-taxonomy interface, and reaches this scan only because
// scanReducerInterfacePorts harvests every method on every reducer interface
// and classifyCypherPorts matches by bare name.
//
// Failing that set would fail the build on 43 ports that were never meant to be
// declared. An earlier revision of this comment said 43 and called every one of
// them a retract, sweep, execute or read port; both halves were wrong, and this
// is the file that exists to stop a comment asserting what the code does not.
//
// So a NEW node-only write port forgotten from directMaterializedEdgeNodeOnlyPorts
// lands with nothing red. That gap is real and bounded: the moment such a port
// MERGEs a relationship, the first case above catches it, which is the direction
// that can hide a materialized edge family from the ledger.
//
// Four of these sit in cypher/secrets_iam_graph_writer.go beside five edge
// ports, and WriteSecurityGroupRuleNodes sits in
// cypher/security_group_reachability_edge_writer.go beside two. A file-level
// scan would call all seven edge writers. They are node-only because the
// Cypher each one actually executes MERGEs a labelled node and no
// relationship.
var directMaterializedEdgeNodeOnlyPorts = map[string]string{
	"WriteServiceAccountNodes":           "SecretsIAMServiceAccount",
	"WriteVaultAuthRoleNodes":            "SecretsIAMVaultAuthRole",
	"WriteVaultPolicyNodes":              "SecretsIAMVaultPolicy",
	"WriteSecretMetadataPathNodes":       "SecretsIAMSecretMetadataPath",
	"WriteSecurityGroupRuleNodes":        "SecurityGroupRule",
	"WriteCidrBlockNodes":                "CidrBlock",
	"WritePrefixListNodes":               "PrefixList",
	"WriteCloudResourceNodes":            "CloudResource",
	"WriteEC2InstanceNodes":              "EC2Instance",
	"WriteEC2InstanceIdentityNodes":      "EC2InstanceIdentity",
	"WriteEC2InternetExposureNodes":      "EC2InternetExposure",
	"WriteEC2BlockDeviceKMSPostureNodes": "EC2BlockDeviceKMSPosture",
	"WriteRDSPostureNodes":               "RDSPosture",
	"WriteS3InternetExposureNodes":       "S3InternetExposure",
	"WriteKubernetesWorkloadNodes":       "KubernetesWorkload",
}

// sharedProjectionEdgeWritePort is the one graph-write port that belongs to
// neither table: it writes edges, but the family travels as a runtime `domain`
// argument rather than being fixed by the port, and its possible values are
// exactly MaterializedEdgeFamilies(). Classifying it as a direct family would
// double-count all fourteen shared families; classifying it as node-only would
// be false. It is named here so the drift guard's totality has somewhere to put
// it instead of an unexplained exemption.
const sharedProjectionEdgeWritePort = "WriteEdges"

// SharedProjectionEdgeWritePort returns the name of the shared-projection
// edge-write port, whose families are enumerated by MaterializedEdgeFamilies()
// rather than by DirectMaterializedEdgeFamilies().
func SharedProjectionEdgeWritePort() string {
	return sharedProjectionEdgeWritePort
}

// DirectMaterializedEdgeFamilies returns the sorted, deduplicated set of
// materialized-edge families the reducer writes directly, bypassing the
// shared-projection intent path MaterializedEdgeFamilies() inventories.
//
// Every returned family needs a row in the DIRECT half of the ledger,
// specs/ifa-materialized-edge-coverage-direct.v1.yaml — a coverage row, or a
// waiver naming its tracked issue, but present either way. A family absent
// from that file is not leniently treated, it is invisible: the exhaustiveness
// gate emits no row, no finding and no output for it, and reports green.
//
// The shared half, specs/ifa-materialized-edge-coverage.v1.yaml, belongs to
// MaterializedEdgeFamilies() above and is the wrong file for a direct family.
// A row written there is reconciled against the other enumeration, which is
// the cross-half misplacement the split exists to prevent — reached, this
// time, by a maintainer doing what the comment said.
func DirectMaterializedEdgeFamilies() []string {
	seen := make(map[string]struct{}, len(directMaterializedEdgeFamilyByPort))
	out := make([]string, 0, len(directMaterializedEdgeFamilyByPort))
	for _, family := range directMaterializedEdgeFamilyByPort {
		if _, dup := seen[family]; dup {
			continue
		}
		seen[family] = struct{}{}
		out = append(out, family)
	}
	sort.Strings(out)
	return out
}

// DirectMaterializedEdgeWritePorts returns the sorted names of the reducer
// interface ports that write a direct materialized-edge family, so the drift
// guard can compare this classification against the Cypher those ports execute
// without importing the table itself.
func DirectMaterializedEdgeWritePorts() []string {
	out := make([]string, 0, len(directMaterializedEdgeFamilyByPort))
	for port := range directMaterializedEdgeFamilyByPort {
		out = append(out, port)
	}
	sort.Strings(out)
	return out
}

// DirectMaterializedEdgeNodeOnlyWritePorts returns the sorted names of the
// reducer graph-write ports reviewed as materializing no edge, so the drift
// guard's classification is total over every graph-write port rather than
// silently ignoring the ones it does not recognise.
func DirectMaterializedEdgeNodeOnlyWritePorts() []string {
	out := make([]string, 0, len(directMaterializedEdgeNodeOnlyPorts))
	for port := range directMaterializedEdgeNodeOnlyPorts {
		out = append(out, port)
	}
	sort.Strings(out)
	return out
}

// DirectMaterializedEdgeFamilyForPort returns the family the named port writes
// and whether the port is a classified direct edge-write port.
//
// The second return is false for an unclassified port, and callers MUST fail
// closed on it rather than treating the family as absent: an unrecognised port
// is a registration bug, never a valid steady state.
func DirectMaterializedEdgeFamilyForPort(port string) (string, bool) {
	family, ok := directMaterializedEdgeFamilyByPort[port]
	return family, ok
}
