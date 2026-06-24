// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "strings"

const (
	readinessAcceptancePayloadEntityKey = "payload_entity_key"
	readinessAcceptanceScopePrefix      = "scope_prefix"
)

func reducerClaimReadinessRequirementsCTE() string {
	return "reducer_claim_readiness_requirements(domain, keyspace, phase, acceptance_unit_source, acceptance_unit_prefix) AS (\n" +
		reducerClaimReadinessRequirementsSQL() +
		"\n)"
}

func reducerClaimReadinessRequirementsSQL() string {
	return `    VALUES
        ('aws_relationship_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('azure_relationship_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
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
