// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The gate answers two different questions about a retrying work item, and
// each has its own set (#7308).
//
// Label: is the row waiting on a readiness precondition rather than failing
// on its own merits? readinessDeferred answers it from the reducer queue's
// non-counting retry set, so every class the reducer exempts from its retry
// budget is labeled readiness-deferred the moment it is registered. The label
// never changes a verdict.
//
// Control: may pre-maintenance quiescence (-drain-allow-readiness-deferred,
// used by the Ifá pre-maintenance cells) stop waiting while the row is still
// retrying? preMaintenanceToleratedFailureClasses answers it. It is an
// allow-list: a class that is not listed blocks quiescence, so a class added
// to the reducer set later is labeled correctly at once and stays on the safe
// side of the control decision until someone decides.
// TestEveryNonCountingFailureClassHasPreMaintenanceDecision forces that
// decision.
//
// The pre-maintenance cells assert, right after their pre drain, that the
// cross-repo resolver logged "gated" and wrote no resolver/cross-repo edges,
// that no CORRELATES_DEPLOYABLE_UNIT edge exists, and that no workload-owned
// DEPENDS_ON edge exists. Each reason below says why a retrying row in the
// class cannot make one of those absences pass for the wrong reason.

// Reasons a class is tolerated.
const (
	toleratedOwnFence = "returned by the owning handler's own fail-closed readiness fence before it " +
		"reads or writes its family's output, so a retrying row has already evaluated that fence and found it closed (#6184, #7258)"
	toleratedResolvedTarget = "returned only after the handler has resolved deployment relationships to write " +
		"(admitted deployable-unit rows, or DEPLOYS_FROM-derived deployment sources); cross-repo resolution " +
		"produces none until the maintenance pass publishes backward evidence, so the class does not occur " +
		"before that pass (#6759, #7268)"
	toleratedCloudFamilyGate = "returned by a cloud, Kubernetes, or secrets-IAM family's own readiness gate " +
		"while it waits on upstream graph nodes or endpoints; those families write none of the edges the " +
		"pre-maintenance cells assert absent"
	toleratedRuntimeDrift = "returned by aws_cloud_runtime_drift, which writes only AWS runtime-drift findings, " +
		"a family no pre-maintenance cell asserts absent (#5848)"
	toleratedOwnScopeNodes = "returned by a cloud-family handler waiting on the canonical-nodes phase " +
		"of its own scope and generation; no pre-maintenance cell corpus enqueues it and it writes " +
		"no family those cells assert absent (#5046, #7284)"
	toleratedCrossScopeFloor = "returned only by the cross-scope readiness floor, which fronts " +
		"ci_cd_run_correlation and supply_chain_impact; no pre-maintenance cell asserts either absent (#5709)"
	toleratedAdmissionFence = "its blocker is another scope's cloud_inventory_admission item, " +
		"which the same snapshot already counts as live or dead_letter (#6887, #7284)"
	toleratedValueFlowFence = "a singleton that writes only value-flow evidence, which no " +
		"pre-maintenance cell asserts absent (#6923, #7284)"
	toleratedCloudRelationshipEndpoint = "returned by the USES and CAN_PERFORM cloud relationship writers, " +
		"which no pre-maintenance cell asserts absent; USES waits on its WorkloadInstance endpoint, which can " +
		"itself wait on the maintenance pass, and CAN_PERFORM on sibling-scope target nodes (#6785)"
)

// preMaintenanceToleratedFailureClasses maps each failure class that
// pre-maintenance quiescence tolerates on a retrying row to the reason it is
// safe. The post-maintenance strict drain re-checks every one of them.
//
// A class missing here blocks quiescence. Classes left out on purpose, with
// their reasons, are in preMaintenanceBlockingFailureClasses
// (drains_readiness_classes_test.go).
var preMaintenanceToleratedFailureClasses = map[string]string{
	// #5848.
	"aws_cloud_runtime_drift_state_pending":    toleratedRuntimeDrift,
	"aws_cloud_runtime_drift_write_superseded": toleratedRuntimeDrift,
	"secrets_iam_endpoint_not_ready":           toleratedCloudFamilyGate,
	"kubernetes_correlation_nodes_not_ready":   toleratedCloudFamilyGate,
	"gcp_relationship_nodes_not_ready":         toleratedCloudFamilyGate,
	"ec2_instance_identity_nodes_not_ready":    toleratedCloudFamilyGate,
	"cross_scope_producer_not_ready":           toleratedCrossScopeFloor,
	// #5717: an ec2_instance_uses_ami edge waiting on the EC2 instance node phase.
	"aws_relationship_ec2_instance_nodes_not_ready": toleratedCloudFamilyGate,
	// #6184: fail-closed cross-repo and deployable-unit deferrals.
	"cross_repo_backward_evidence_not_ready":                toleratedOwnFence,
	"deployable_unit_correlation_resolution_not_ready":      toleratedOwnFence,
	"deployable_unit_correlation_canonical_nodes_not_ready": toleratedOwnFence,
	"workload_materialization_resolution_not_ready":         toleratedOwnFence,
	// #7258: service catalog correlation deferred on the relationship corpus fence.
	"service_catalog_correlation_resolution_not_ready": toleratedOwnFence,
	// #6759 and #7268: waiting on a graph target of an already-resolved write.
	"workload_materialization_deployment_source_target_not_ready": toleratedResolvedTarget,
	"shared_edge_target_not_ready":                                toleratedResolvedTarget,
	// #6785.
	"workload_cloud_relationship_instances_not_ready": toleratedCloudRelationshipEndpoint,
	"iam_can_perform_target_not_ready":                toleratedCloudRelationshipEndpoint,
	// #5046 / #7284: graph-node readiness classes.
	"aws_cloud_image_nodes_not_ready":              toleratedOwnScopeNodes,
	"aws_relationship_nodes_not_ready":             toleratedOwnScopeNodes,
	"azure_relationship_nodes_not_ready":           toleratedOwnScopeNodes,
	"ec2_block_device_kms_posture_nodes_not_ready": toleratedOwnScopeNodes,
	"ec2_internet_exposure_nodes_not_ready":        toleratedOwnScopeNodes,
	"ec2_uses_profile_nodes_not_ready":             toleratedOwnScopeNodes,
	"iam_can_assume_nodes_not_ready":               toleratedOwnScopeNodes,
	"iam_can_perform_nodes_not_ready":              toleratedOwnScopeNodes,
	"iam_escalation_nodes_not_ready":               toleratedOwnScopeNodes,
	"iam_instance_profile_role_nodes_not_ready":    toleratedOwnScopeNodes,
	"observability_coverage_nodes_not_ready":       toleratedOwnScopeNodes,
	"rds_posture_nodes_not_ready":                  toleratedOwnScopeNodes,
	"s3_external_principal_grant_nodes_not_ready":  toleratedOwnScopeNodes,
	"s3_internet_exposure_nodes_not_ready":         toleratedOwnScopeNodes,
	"s3_logs_to_nodes_not_ready":                   toleratedOwnScopeNodes,
	"security_group_reachability_nodes_not_ready":  toleratedOwnScopeNodes,
	"workload_cloud_relationship_nodes_not_ready":  toleratedOwnScopeNodes,
	"cloud_admission_not_ready":                    toleratedAdmissionFence,
	"value_flow_inputs_not_ready":                  toleratedValueFlowFence,
}

// readinessDeferred reports whether failureClass is one the reducer queue
// exempts from its retry budget: a readiness wait, not a failure on the work
// item's own merits. It reads the set storage/postgres owns, so the label
// cannot drift from the reducer.
func readinessDeferred(failureClass string) bool {
	return storagepostgres.IsNonCountingReducerRetryFailureClass(failureClass)
}

// residualCounts is the residual split by what each row is doing.
type residualCounts struct {
	// live is work still running or queued, including a claimed or running
	// row that still carries a readiness failure from an earlier attempt.
	live int64
	// readinessDeferred is retrying rows in any non-counting class.
	readinessDeferred int64
	// preMaintenanceBlocking is the part of readinessDeferred whose class
	// pre-maintenance quiescence does not tolerate.
	preMaintenanceBlocking int64
	// deadLetter and failed are terminal rows.
	deadLetter int64
	failed     int64
}

// classifyResidualRows splits residual groups into live work, readiness
// deferrals, and terminal rows. formatResidualBreakdown and
// preMaintenanceQuiescence both read this one result, so the message and the
// verdict cannot disagree about which row is which. The label is checked
// before the tolerated set: a class the reducer no longer exempts counts as
// live even if it is still listed there.
func classifyResidualRows(rows []residualRow) residualCounts {
	var counts residualCounts
	for _, row := range rows {
		switch {
		case row.Status == "dead_letter":
			counts.deadLetter += row.Count
		// `failed` is terminal, same as dead_letter. Postgres already draws this
		// line: the outstanding-work count in generation_lifecycle_sql.go is
		// `status IN ('pending','claimed','running','retrying')`, which excludes
		// it. Counting a failed row as live would report a stuck pipeline as a
		// busy one and send the reader looking for progress that is not coming.
		case row.Status == "failed":
			counts.failed += row.Count
		// Claim transitions retain prior failure metadata. Only retrying rows are
		// waiting on readiness; claimed/running rows are live even when they still
		// carry a readiness failure from an earlier attempt.
		case row.Status == "retrying" && readinessDeferred(row.FailureClass):
			counts.readinessDeferred += row.Count
			if _, tolerated := preMaintenanceToleratedFailureClasses[row.FailureClass]; !tolerated {
				counts.preMaintenanceBlocking += row.Count
			}
		default:
			counts.live += row.Count
		}
	}
	return counts
}
