// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func queryHasBoundedReadinessRequirement(query, domain, keyspace, phase string) bool {
	return strings.Contains(query, "reducer_claim_readiness_requirements AS") &&
		strings.Contains(query, "('"+domain+"', '"+keyspace+"', '"+phase+"'")
}

func queryHasPayloadReadinessLookup(query, workAlias, requirementAlias, phaseAlias string) bool {
	return strings.Contains(query, "FROM reducer_claim_readiness_requirements AS "+requirementAlias) &&
		strings.Contains(query, phaseAlias+".scope_id = "+workAlias+".scope_id") &&
		strings.Contains(query, phaseAlias+".keyspace = "+requirementAlias+".keyspace") &&
		strings.Contains(query, phaseAlias+".phase = "+requirementAlias+".phase") &&
		strings.Contains(query, "COALESCE(NULLIF("+workAlias+".payload->>'entity_key', ''), "+workAlias+".scope_id)")
}

func queryHasScopePrefixReadinessRequirement(query, domain, keyspace, phase, prefix string) bool {
	return strings.Contains(query, "('"+domain+"', '"+keyspace+"', '"+phase+"', 'scope_prefix', '"+prefix+"')")
}

func TestReducerClaimQueriesUseBoundedReadinessRequirements(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"single": claimReducerWorkQuery,
		"batch":  claimReducerWorkBatchQuery,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, want := range []string{
				"reducer_claim_readiness_requirements AS",
				"FROM reducer_claim_readiness_requirements AS readiness_req",
				"readiness_phase.scope_id",
				"CASE readiness_req.acceptance_unit_source",
			} {
				if !strings.Contains(query, want) {
					t.Fatalf("claim query missing bounded readiness lookup %q:\n%s", want, query)
				}
			}
			for _, forbidden := range []string{
				"graph_projection_phase_state AS aws_nodes",
				"graph_projection_phase_state AS iam_permission_nodes",
				"graph_projection_phase_state AS ec2_uses_profile_instance_node",
				"graph_projection_phase_state AS sg_rule_nodes",
			} {
				if strings.Contains(query, forbidden) {
					t.Fatalf("claim query still has per-domain readiness predicate %q:\n%s", forbidden, query)
				}
			}
		})
	}
}

func TestReducerClaimReadinessRequirementsCoverMultiPhaseDomains(t *testing.T) {
	t.Parallel()

	requirements := reducerClaimReadinessRequirementsSQL()
	for _, want := range []string{
		"aws_relationship_materialization",
		"iam_can_perform_materialization",
		"kubernetes_correlation_materialization",
		"security_group_rule_uid",
		"security_group_endpoint_uid",
		"ec2_instance_node_materialization:",
		"aws_resource_materialization:",
	} {
		if !strings.Contains(requirements, want) {
			t.Fatalf("readiness requirements missing %q:\n%s", want, requirements)
		}
	}
}

func TestReducerStatusBlockagesUseBoundedReadinessRequirements(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"reducer_claim_readiness_requirements AS",
		"JOIN reducer_claim_readiness_requirements AS readiness_req",
		"CASE readiness_req.acceptance_unit_source",
	} {
		if !strings.Contains(reducerConflictBlockageQuery, want) {
			t.Fatalf("status blockage query missing bounded readiness lookup %q:\n%s", want, reducerConflictBlockageQuery)
		}
	}
	for _, forbidden := range []string{
		"security_group_reachability_readiness_blocked AS",
		"ec2_uses_profile_readiness_blocked AS",
		"ec2_block_device_kms_posture_readiness_blocked AS",
		"graph_projection_phase_state AS aws_nodes",
	} {
		if strings.Contains(reducerConflictBlockageQuery, forbidden) {
			t.Fatalf("status blockage query still has per-domain readiness predicate %q:\n%s", forbidden, reducerConflictBlockageQuery)
		}
	}
}
