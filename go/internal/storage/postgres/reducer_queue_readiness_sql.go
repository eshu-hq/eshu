// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	readinessAcceptancePayloadEntityKey = "payload_entity_key"
	readinessAcceptanceScopePrefix      = "scope_prefix"
)

// nonCountingReducerRetryFailureClasses lists the durable failure_class values a
// readiness-gate miss self-classifies with. A retrying row in one of these classes
// is deferred until its upstream phase or endpoint commits, not failing on its own
// merits, so it is exempt from the retry budget on BOTH the Go retry decision
// (isNonCountingReducerRetryFailureClass) and the SQL claim attempt-count CASE
// (reducerClaimAttemptCountCaseSQL). Counting it toward maxAttempts would
// dead-letter still-pending work that the succeeded-only reopen path
// (ReopenSucceeded / ReplayDomain) would never reopen. This is the single source
// both the Go and SQL claim paths derive from so the exempt set cannot drift
// between them.
var nonCountingReducerRetryFailureClasses = []string{
	reducer.SecretsIAMEndpointNotReadyFailureClass,
	reducer.KubernetesCorrelationNodesNotReadyFailureClass,
	reducer.GCPRelationshipNodesNotReadyFailureClass,
	reducer.EC2InstanceIdentityNodesNotReadyFailureClass,
}

// IsNonCountingReducerRetryFailureClass reports whether failureClass is exempt
// from the reducer retry budget — a readiness-gate miss that is deferred until
// its upstream phase commits rather than failing on its own merits. It exposes
// the same predicate the internal claim path uses so a caller can assert, in
// lockstep, that a failure class it models as counting (for example
// cypher.GraphWriteTimeoutFailureClass in the Ifá saturation regression) has not
// been accidentally added to the exempt set — a drift that would make the real
// queue retry forever while a counting-assumption model dead-letters.
func IsNonCountingReducerRetryFailureClass(failureClass string) bool {
	return isNonCountingReducerRetryFailureClass(failureClass)
}

// reducerClaimAttemptCountCaseSQL renders the attempt_count assignment for the
// claim UPDATE: a retrying row whose failure_class is a non-counting readiness
// class keeps its attempt_count; every other claim increments it. Both the
// single-claim and batch-claim queries alias the updated table as "work" and call
// this helper so the exempt-class set stays byte-identical across both claim
// paths.
func reducerClaimAttemptCountCaseSQL() string {
	return "CASE\n" +
		"            WHEN work.status = 'retrying' AND " +
		reducerNonCountingFailureClassPredicateSQL("work") +
		" THEN work.attempt_count\n" +
		"            ELSE work.attempt_count + 1\n" +
		"        END"
}

// reducerNonCountingFailureClassPredicateSQL renders the disjunction matching any
// non-counting readiness failure class for the given row alias. The chained-OR
// equality form (rather than IN (...)) keeps each class as a discrete
// "alias.failure_class = '...'" predicate so callers and tests can assert one
// class at a time.
func reducerNonCountingFailureClassPredicateSQL(alias string) string {
	predicates := make([]string, 0, len(nonCountingReducerRetryFailureClasses))
	for _, class := range nonCountingReducerRetryFailureClasses {
		predicates = append(predicates, alias+".failure_class = '"+class+"'")
	}
	return "(" + strings.Join(predicates, " OR ") + ")"
}

func reducerClaimReadinessRequirementsCTE() string {
	return "reducer_claim_readiness_requirements(domain, keyspace, phase, acceptance_unit_source, acceptance_unit_prefix) AS (\n" +
		reducerClaimReadinessRequirementsSQL() +
		"\n)"
}

func reducerClaimReadinessRequirementsSQL() string {
	return `    VALUES
        ('aws_relationship_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('azure_relationship_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('gcp_relationship_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('workload_cloud_relationship_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('observability_coverage_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('iam_can_assume_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('iam_escalation_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('iam_can_perform_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('s3_logs_to_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('s3_external_principal_grant_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('rds_posture_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('iam_instance_profile_role_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('ec2_internet_exposure_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('ec2_instance_identity_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('s3_internet_exposure_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('kubernetes_correlation_materialization', 'kubernetes_workload_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('security_group_reachability_materialization', 'security_group_rule_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('security_group_reachability_materialization', 'security_group_endpoint_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('security_group_reachability_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('ec2_uses_profile_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'scope_prefix', 'ec2_instance_node_materialization:'),
        ('ec2_uses_profile_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'scope_prefix', 'aws_resource_materialization:'),
        ('ec2_block_device_kms_posture_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'scope_prefix', 'ec2_instance_node_materialization:'),
        ('ec2_block_device_kms_posture_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'scope_prefix', 'aws_resource_materialization:')`
}

func reducerClaimReadinessGateSQL(workAlias, requirementAlias, phaseAlias string) string {
	return `NOT EXISTS (
          SELECT 1
          FROM reducer_claim_readiness_requirements AS ` + requirementAlias + `
          WHERE ` + requirementAlias + `.domain = ` + workAlias + `.domain
            AND NOT EXISTS (
                SELECT 1
                FROM graph_projection_phase_state AS ` + phaseAlias + `
                WHERE ` + phaseAlias + `.scope_id = ` + workAlias + `.scope_id
                  AND ` + phaseAlias + `.acceptance_unit_id = ` + reducerClaimReadinessAcceptanceUnitSQL(workAlias, requirementAlias) + `
                  AND ` + phaseAlias + `.source_run_id = ` + workAlias + `.generation_id
                  AND ` + phaseAlias + `.generation_id = ` + workAlias + `.generation_id
                  AND ` + phaseAlias + `.keyspace = ` + requirementAlias + `.keyspace
                  AND ` + phaseAlias + `.phase = ` + requirementAlias + `.phase
            )
      )`
}

func reducerClaimReadinessAcceptanceUnitSQL(workAlias, requirementAlias string) string {
	return strings.Join([]string{
		"CASE " + requirementAlias + ".acceptance_unit_source",
		"WHEN '" + readinessAcceptancePayloadEntityKey + "' THEN COALESCE(NULLIF(" + workAlias + ".payload->>'entity_key', ''), " + workAlias + ".scope_id)",
		"WHEN '" + readinessAcceptanceScopePrefix + "' THEN " + requirementAlias + ".acceptance_unit_prefix || " + workAlias + ".scope_id",
		"ELSE " + workAlias + ".scope_id",
		"END",
	}, "\n                      ")
}
