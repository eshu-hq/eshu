// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialized

import (
	edgewriter "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/ec2"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/iam"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/relationship"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/s3"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/security/group"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/semantic/entity"
)

// The single-type registry table lives here rather than in families.go so
// neither file approaches the 500-line cap: this table is where every new
// one-relationship-type family registration lands, while families.go keeps
// the materializedEdgeFamily type, the identity declarations, and the
// accessors. materializedEdgeFamily is defined in families.go (same package).

// singleTypeMaterializedEdgeFamilies indexes the one-relationship-type families
// by their reducer domain constant value.
//
// Each type below is corroborated by three independent sources: the write
// template's MERGE, the retract alternation, and the domain's own producer.
// codeowners_ownership_edges and submodule_pin_edges are the two whose MERGE is
// assembled at runtime rather than written as a literal in the template file, so
// for those the retract is the load-bearing confirmation.
var singleTypeMaterializedEdgeFamilies = map[string]materializedEdgeFamily{
	"shell_exec": {
		EdgeTypes:      map[string]string{"EXECUTES_SHELL": "shell invocation edge (BatchCanonicalShellExecUpsertCypher)"},
		RetractCypher:  edgewriter.RetractShellExecEdgesCypher,
		IdentityCypher: edgewriter.BatchCanonicalShellExecUpsertCypher,
	},
	"workload_dependency": {
		EdgeTypes:          map[string]string{"DEPENDS_ON": "workload-to-workload dependency (BatchCanonicalWorkloadDependencyUpsertCypher)"},
		RetractCypher:      sourcecypher.RetractWorkloadDependencyEdgesCypher,
		IdentityCypher:     sourcecypher.BatchCanonicalWorkloadDependencyUpsertCypher,
		IdentityProperties: []string{"identity_key"},
	},
	"deployable_unit_edges": {
		EdgeTypes:      map[string]string{"CORRELATES_DEPLOYABLE_UNIT": "deployable-unit correlation (BatchCanonicalDeployableUnitCorrelationUpsertCypher)"},
		RetractCypher:  sourcecypher.RetractDeployableUnitCorrelationEdgesCypher,
		IdentityCypher: sourcecypher.BatchCanonicalDeployableUnitCorrelationUpsertCypher,
	},
	"handles_route": {
		EdgeTypes:      map[string]string{"HANDLES_ROUTE": "handler-to-route binding (BatchCanonicalHandlesRouteEdgeUpsertCypher)"},
		RetractCypher:  sourcecypher.RetractHandlesRouteEdgesCypher,
		IdentityCypher: sourcecypher.BatchCanonicalHandlesRouteEdgeUpsertCypher,
	},
	"runs_in": {
		EdgeTypes:      map[string]string{"RUNS_IN": "entity-runs-in-workload edge (BatchCanonicalRunsInEdgeUpsertCypher)"},
		RetractCypher:  sourcecypher.RetractRunsInEdgesCypher,
		IdentityCypher: sourcecypher.BatchCanonicalRunsInEdgeUpsertCypher,
	},
	"invokes_cloud_action": {
		EdgeTypes:      map[string]string{"INVOKES_CLOUD_ACTION": "code-to-cloud-action invocation (BatchCanonicalInvokesCloudActionUpsertCypher)"},
		RetractCypher:  sourcecypher.RetractInvokesCloudActionEdgesCypher,
		IdentityCypher: sourcecypher.BatchCanonicalInvokesCloudActionUpsertCypher,
	},
	"codeowners_ownership_edges": {
		EdgeTypes:          map[string]string{"DECLARES_CODEOWNER": "CODEOWNERS ownership declaration (BatchCanonicalCodeownersOwnershipEdgeCypher)"},
		RetractCypher:      sourcecypher.RetractCodeownersOwnershipEdgesCypher,
		IdentityCypher:     sourcecypher.BatchCanonicalCodeownersOwnershipEdgeCypher,
		IdentityProperties: []string{"pattern", "source_path"},
	},
	"submodule_pin_edges": {
		EdgeTypes:          map[string]string{"PINS_SUBMODULE": "submodule commit pin (BatchCanonicalSubmodulePinEdgeCypher)"},
		RetractCypher:      sourcecypher.RetractSubmodulePinEdgesCypher,
		IdentityCypher:     sourcecypher.BatchCanonicalSubmodulePinEdgeCypher,
		IdentityProperties: []string{"path"},
	},
	"documentation_edges": {
		EdgeTypes:      map[string]string{"DOCUMENTS": "doc-section-to-entity edge (BatchCanonicalDocumentationEntityEdgeCypher)"},
		RetractCypher:  sourcecypher.RetractDocumentationEdgesCypher,
		IdentityCypher: sourcecypher.BatchCanonicalDocumentationEntityEdgeCypher,
	},
	"rationale_edges": {
		EdgeTypes:      map[string]string{"EXPLAINS": "rationale-to-entity edge (BatchCanonicalRationaleExplainsEdgeCypher)"},
		RetractCypher:  sourcecypher.RetractRationaleEdgesCypher,
		IdentityCypher: sourcecypher.BatchCanonicalRationaleExplainsEdgeCypher,
	},
	// The fourteen DIRECT-materialization families registered here (#6228;
	// #8359 for semantic_entity_containment).
	// Unlike every entry above them, these reach the graph straight from their
	// own reducer port with no shared-projection intent row in between, so
	// reducer.DirectMaterializedEdgeFamilies() enumerates them rather than
	// reducer.MaterializedEdgeFamilies().
	//
	// All three types below are read off the Cypher the port executes, never off
	// the port's name and never off its statement-metadata label. That is the
	// #6181 rule, and all three families are cases where the shortcut would have
	// produced the wrong answer:
	//
	//   - kubernetes_namespace_environment's port is named
	//     WriteKubernetesNamespaceNodes and MERGEs a relationship.
	//   - iam_instance_profile_role's statement-metadata label is
	//     iamInstanceProfileRoleEdgeLabel ("IAM_INSTANCE_PROFILE_HAS_ROLE"),
	//     which is NOT a graph relationship type; the type its template MERGEs
	//     is HAS_ROLE.
	//   - workload_cloud_relationship's statement-metadata label is
	//     workloadCloudRelationshipEdgeLabel ("WORKLOAD_USES_CLOUD_RESOURCE"),
	//     which is NOT a graph relationship type; the type its template MERGEs
	//     is USES.
	//
	// Since #6309 six of the fourteen carry coverage rows:
	// kubernetes_namespace_environment, iam_instance_profile_role,
	// iam_can_assume, iam_can_perform, ec2_uses_profile, and s3_logs_to.
	// Registering a family here makes `eshu-ifa assert-edges -domain <family>`
	// addressable and lets its vacuity guard resolve; it does not assert that
	// any live matrix drives it. workload_cloud_relationship, iam_escalation,
	// kubernetes_correlation, code_taint_evidence, semantic_entity_containment,
	// security_group_rule_endpoint, s3_external_principal_grant, and
	// uses_service_account still carry their waiver rows in
	// specs/ifa-materialized-edge-coverage-direct.v1.yaml for that reason.
	//
	// kubernetes_namespace_environment's write template MERGEs the Environment
	// node with a property map and the relationship without one, so the
	// identity scan (which only matches a property map on a relationship
	// MERGE, `-[rel:TYPE {...}]`) correctly reports no identity properties:
	// this edge keys on its two endpoint nodes alone.
	"kubernetes_namespace_environment": {
		EdgeTypes:      map[string]string{"TARGETS_ENVIRONMENT": "namespace-to-environment binding (CanonicalKubernetesNamespaceWithEnvironmentUpsertCypher)"},
		RetractCypher:  sourcecypher.RetractKubernetesNamespaceStaleTargetsEnvironmentCypher,
		IdentityCypher: sourcecypher.CanonicalKubernetesNamespaceWithEnvironmentUpsertCypher,
	},
	// iam_instance_profile_role's IdentityCypher holds the %s FORMAT const
	// itself, unformatted. The token substituted into it comes from
	// iamInstanceProfileRoleRelationshipVocabulary, a closed single-member set
	// screened per row by validateIAMInstanceProfileRoleRelationshipType, so
	// HAS_ROLE is the only type this writer can emit. The template MERGEs on
	// its two endpoint nodes alone, so the identity scan yields nothing
	// whether or not the %s has been substituted.
	"iam_instance_profile_role": {
		EdgeTypes:      map[string]string{"HAS_ROLE": "IAM instance-profile to role attachment (CanonicalIAMInstanceProfileRoleEdgeUpsertCypherFormat)"},
		RetractCypher:  iam.RetractIAMInstanceProfileRoleEdgesCypher,
		IdentityCypher: iam.CanonicalIAMInstanceProfileRoleEdgeUpsertCypherFormat,
	},
	// workload_cloud_relationship follows the same single-vocabulary shape:
	// IdentityCypher holds the %s FORMAT const unformatted, and the token
	// substituted into it comes from workloadCloudRelationshipVocabulary, a
	// closed single-member set screened per row by
	// validateWorkloadCloudRelationshipType, so USES is the only type this
	// writer can emit. The template MERGEs on its two endpoint nodes alone
	// (the WorkloadInstance is matched, not merged), so the identity scan
	// yields nothing whether or not the %s has been substituted.
	// workloadCloudRelationshipEdgeLabel ("WORKLOAD_USES_CLOUD_RESOURCE") is
	// statement metadata carried beside the query, not a graph relationship
	// type -- the same #6181-shaped trap iam_instance_profile_role documents
	// one level below the port name.
	"workload_cloud_relationship": {
		EdgeTypes:      map[string]string{"USES": "workload-instance to cloud-resource attachment (WorkloadCloudRelationshipUpsertCypherFormat)"},
		RetractCypher:  relationship.RetractWorkloadCloudRelationshipEdgesCypher,
		IdentityCypher: relationship.WorkloadCloudRelationshipUpsertCypherFormat,
	},
	// iam_can_assume follows the same single-vocabulary shape as the two
	// entries above it: IdentityCypher holds the %s FORMAT const unformatted,
	// and the token substituted into it comes from
	// iamCanAssumeRelationshipVocabulary, a closed single-member set screened
	// per row by validateIAMCanAssumeRelationshipType, so CAN_ASSUME is the
	// only type this writer can emit. The template MERGEs on its two endpoint
	// nodes alone (both are MATCHed, never merged), so the identity scan
	// yields nothing whether or not the %s has been substituted.
	// iamCanAssumeEdgeLabel ("IAM_CAN_ASSUME") is statement metadata carried
	// beside the query, not a graph relationship type -- the same #6181-shaped
	// trap both entries above document at one level below the port name.
	"iam_can_assume": {
		EdgeTypes:      map[string]string{"CAN_ASSUME": "assuming-principal to role trust attachment (CanonicalIAMCanAssumeEdgeUpsertCypherFormat)"},
		RetractCypher:  iam.RetractIAMCanAssumeEdgesCypher,
		IdentityCypher: iam.CanonicalIAMCanAssumeEdgeUpsertCypherFormat,
	},
	// iam_can_perform follows the same single-vocabulary shape as the three
	// entries above it, minus the %s: CanonicalIAMCanPerformEdgeUpsertCypher
	// bakes the static CAN_PERFORM token directly into the MERGE (the granted
	// action set lives in rel.actions, never in the relationship type), so
	// CAN_PERFORM is still the only type this writer can emit. The template
	// MERGEs on its two endpoint nodes alone (both are MATCHed, never
	// merged), so IdentityProperties stays nil — the identity scan yields
	// nothing. iamCanPerformEdgeLabel IS "CAN_PERFORM" here (the const
	// doubles as the relationship type and the statement-metadata tag);
	// "IAM_CAN_PERFORM" appears nowhere in code, so the type is read off the
	// template, never derived from the port or family name.
	"iam_can_perform": {
		EdgeTypes:      map[string]string{"CAN_PERFORM": "principal to resource effective-permission attachment (CanonicalIAMCanPerformEdgeUpsertCypher)"},
		RetractCypher:  iam.RetractIAMCanPerformEdgesCypher,
		IdentityCypher: iam.CanonicalIAMCanPerformEdgeUpsertCypher,
	},
	// iam_escalation follows the same static-token shape as iam_can_perform:
	// CanonicalIAMEscalationEdgeUpsertCypher bakes the static
	// CAN_ESCALATE_TO token directly into the MERGE (the merged primitive
	// set lives in rel.primitives, never in the relationship type), so
	// CAN_ESCALATE_TO is still the only type this writer can emit. The
	// template MERGEs on its two endpoint nodes alone (both are MATCHed,
	// never merged, on the uid index), so IdentityProperties stays nil —
	// the identity scan yields nothing. iamEscalationEdgeLabel IS
	// "CAN_ESCALATE_TO" here (the const doubles as the relationship type
	// and the statement-metadata tag); "IAM_ESCALATION" appears nowhere in
	// code, so the type is read off the template, never derived from the
	// port or family name.
	"iam_escalation": {
		EdgeTypes:      map[string]string{"CAN_ESCALATE_TO": "principal to target privilege-escalation attachment (CanonicalIAMEscalationEdgeUpsertCypher)"},
		RetractCypher:  iam.RetractIAMEscalationEdgesCypher,
		IdentityCypher: iam.CanonicalIAMEscalationEdgeUpsertCypher,
	},
	// ec2_uses_profile follows the same single-vocabulary shape as
	// iam_instance_profile_role: IdentityCypher holds the %s FORMAT const
	// unformatted, and the token substituted into it comes from
	// ec2UsesProfileRelationshipVocabulary, a closed single-member set
	// screened per row by validateEC2UsesProfileRelationshipType, so
	// USES_PROFILE is the only type this writer can emit. The template
	// MERGEs on its two endpoint nodes alone (both are MATCHed, never
	// merged), so the identity scan yields nothing whether or not the %s
	// has been substituted.
	// ec2UsesProfileEdgeLabel ("EC2_USES_PROFILE") is statement metadata
	// carried beside the query, not a graph relationship type -- the same
	// #6181-shaped trap iam_instance_profile_role documents one level
	// below the port name.
	"ec2_uses_profile": {
		EdgeTypes:      map[string]string{"USES_PROFILE": "EC2-instance to IAM instance-profile attachment (CanonicalEC2UsesProfileEdgeUpsertCypherFormat)"},
		RetractCypher:  ec2.RetractEC2UsesProfileEdgesCypher,
		IdentityCypher: ec2.CanonicalEC2UsesProfileEdgeUpsertCypherFormat,
	},
	// s3_logs_to follows the same single-vocabulary shape as
	// iam_instance_profile_role: IdentityCypher holds the %s FORMAT const
	// unformatted, and the token substituted into it comes from
	// s3LogsToRelationshipVocabulary, a closed single-member set screened
	// per row by validateS3LogsToRelationshipType, so LOGS_TO is the only
	// type this writer can emit. The template MERGEs on its two endpoint
	// nodes alone (both are MATCHed, never merged), so the identity scan
	// yields nothing whether or not the %s has been substituted.
	// s3LogsToEdgeLabel ("S3_LOGS_TO") is statement metadata carried beside
	// the query, not a graph relationship type -- the same #6181-shaped
	// trap iam_instance_profile_role documents one level below the port
	// name.
	"s3_logs_to": {
		EdgeTypes:      map[string]string{"LOGS_TO": "S3-bucket to log-bucket delivery attachment (CanonicalS3LogsToEdgeUpsertCypherFormat)"},
		RetractCypher:  s3.RetractS3LogsToEdgesCypher,
		IdentityCypher: s3.CanonicalS3LogsToEdgeUpsertCypherFormat,
	},
	// kubernetes_correlation follows the same single-vocabulary shape as
	// s3_logs_to with one fewer substitution: IdentityCypher holds the %s
	// FORMAT const unformatted, and the one hole fills per row from the
	// closed OCI source vocabulary (screened per row by
	// kubernetesCorrelationEdgeSourceLabel, so only OciImageManifest,
	// OciImageIndex, and OciImageDescriptor can reach the node-label
	// position). RUNS_IMAGE needs no vocabulary: it is a static token in
	// the template, kept out of the MERGE property map, so no row content
	// can change the type this writer emits. The template MERGEs on its
	// two endpoint nodes alone (both are MATCHed, never merged), so the
	// identity scan yields nothing whether or not the %s has been
	// substituted.
	// kubernetesCorrelationEdgeLabel ("RUNS_IMAGE") is statement metadata
	// carried beside the query that happens to spell the graph
	// relationship type — the same #6181-shaped trap
	// iam_instance_profile_role documents one level below the port name,
	// kept honest here by reading the type off the template instead.
	"kubernetes_correlation": {
		EdgeTypes:      map[string]string{"RUNS_IMAGE": "Kubernetes-workload to digest-addressed OCI source image attachment (CanonicalKubernetesCorrelationEdgeUpsertCypherFormat)"},
		RetractCypher:  sourcecypher.RetractKubernetesCorrelationEdgesCypher,
		IdentityCypher: sourcecypher.CanonicalKubernetesCorrelationEdgeUpsertCypherFormat,
	},
	// code_taint_evidence is the first evidenceNodeDetach family (#6228 split
	// verdict, kind A). Its edges hang off private CodeTaintEvidence nodes no
	// other production writer MERGEs (pinned by the sole-creator census in
	// go/internal/storage/cypher), so the retract reaps the nodes with
	// DETACH DELETE and the edges cascade. The edge type is still declared
	// here so the resolver scopes assert-edges to it; the drift guard derives
	// it from the held write template rather than trusting this literal.
	"code_taint_evidence": {
		EdgeTypes:     map[string]string{"HAS_TAINT_EVIDENCE": "taint finding edge (CodeTaintEvidenceWriter in code_taint_evidence_writer.go)"},
		RetractKind:   retractKindEvidenceNodeDetach,
		RetractCypher: sourcecypher.RetractCodeTaintEvidenceCypher,
		RetractVariants: []string{
			sourcecypher.RetractStaleCodeTaintEvidenceCypher,
			sourcecypher.RetractCodeTaintEvidenceByUIDsCypher,
			sourcecypher.RetractStaleCodeTaintEvidenceByUIDsCypher,
		},
		IdentityCypher: sourcecypher.CodeTaintEvidenceUpsertCypher,
	},
	// semantic_entity_containment shares its CONTAINS type with the canonical
	// node writer, so it registers only since #8359 gave it a dedicated
	// edge retract (the #6228 verdict excluded its old shared-node DETACH
	// DELETE). RetractCypher holds the stamped-edge retract; the node-scoped
	// and delta variants join RetractVariants — the drift guard pins every
	// held variant's alternation against CONTAINS. Edgeless-node reaps stay
	// unheld (they delete nodes, never edges). Endpoint labels plus the rel
	// stamp fence the canonical population out — see endpoints.go.
	"semantic_entity_containment": {
		EdgeTypes:     map[string]string{"CONTAINS": "file/impl-block to semantic entity containment (semantic entity writer templates in semantic/entity/statements.go)"},
		RetractCypher: entity.RetractSemanticEntityContainmentEdgesCypher,
		RetractVariants: []string{
			entity.RetractSemanticEntityContainmentNodeScopedEdgesCypher,
			entity.RetractSemanticEntityContainmentDeltaEdgesCypher,
			entity.RetractSemanticEntityContainmentDeltaNodeScopedEdgesCypher,
		},
		IdentityCypher: entity.SemanticEntityContainmentUpsertCypher,
	},
	// security_group_rule_endpoint is the single-type TO sibling of the
	// multi-type security_group_sg_rule family (#6228): the rule -> endpoint
	// edge MERGEs the static TO token while the target-node LABEL varies per
	// row over the closed endpoint vocabulary screened by
	// validateSecurityGroupEndpointLabel. The label fills a MATCH node-label
	// position, never the relationship-type position, so TO is still the only
	// type this writer can emit. IdentityCypher holds the FORMAT const
	// unformatted; the template MERGEs on its two endpoint nodes alone, so
	// IdentityProperties stays nil. The ledger-anchored ByUIDs variant joins
	// RetractVariants so the drift guard pins both.
	"security_group_rule_endpoint": {
		EdgeTypes:     map[string]string{"TO": "rule-to-endpoint attachment (CanonicalSecurityGroupToEdgeUpsertCypherFormat)"},
		RetractCypher: group.RetractSecurityGroupToEdgesCypher,
		RetractVariants: []string{
			group.RetractSecurityGroupToEdgesByUIDsCypher,
		},
		IdentityCypher: group.CanonicalSecurityGroupToEdgeUpsertCypherFormat,
	},
	// s3_external_principal_grant follows the same single-vocabulary shape as
	// iam_instance_profile_role: IdentityCypher holds the %s FORMAT const
	// unformatted, and the token substituted into it comes from
	// s3ExternalPrincipalGrantRelationshipVocabulary, a closed single-member
	// set screened per row by
	// validateS3ExternalPrincipalGrantRelationshipType, so GRANTS_ACCESS_TO
	// is the only type this writer can emit. The template MERGEs the
	// relationship on its two endpoint nodes alone, so IdentityProperties
	// stays nil.
	"s3_external_principal_grant": {
		EdgeTypes:      map[string]string{"GRANTS_ACCESS_TO": "bucket-to-external-principal grant (CanonicalS3ExternalPrincipalGrantUpsertCypherFormat)"},
		RetractCypher:  s3.RetractS3ExternalPrincipalGrantEdgesCypher,
		IdentityCypher: s3.CanonicalS3ExternalPrincipalGrantUpsertCypherFormat,
	},
	// uses_service_account follows the same static-token shape as
	// iam_can_perform:
	// CanonicalSecretsIAMUsesServiceAccountEdgeUpsertCypher bakes the static
	// SECRETS_IAM_USES_SERVICE_ACCOUNT token directly into the MERGE, so it
	// is still the only type this writer can emit. The template MERGEs on
	// its two endpoint nodes alone (both MATCHed by uid, never merged), so
	// IdentityProperties stays nil. The held retract is the stale-edge
	// safety pass for rows whose START node is the retained
	// KubernetesWorkload endpoint (normal rows leave with the ServiceAccount
	// DETACH DELETE); it scopes per single scope_id rather than an IN-list,
	// and the single-type drift guard pins the type alternation rather than
	// the scope predicate shape, so the per-scope form holds.
	"uses_service_account": {
		EdgeTypes:      map[string]string{"SECRETS_IAM_USES_SERVICE_ACCOUNT": "workload-to-service-account attachment (CanonicalSecretsIAMUsesServiceAccountEdgeUpsertCypher)"},
		RetractCypher:  iam.RetractSecretsIAMUsesServiceAccountEdgeCypher,
		IdentityCypher: iam.CanonicalSecretsIAMUsesServiceAccountEdgeUpsertCypher,
	},
}
