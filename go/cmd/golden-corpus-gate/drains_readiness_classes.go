// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// readinessDeferredFailureClasses are the failure classes a work item
// self-assigns when a readiness gate defers it: it is waiting for an upstream
// phase to commit, not failing on its own merits. The reducer exempts these from
// the retry budget for that reason (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go).
//
// The map has two roles. For a strict drain it is only a diagnostic label:
// classifyResidualRows reports a retrying row in one of these classes as
// readiness-deferred rather than live, and the drain still waits for it. For
// preMaintenanceQuiescence (-drain-allow-readiness-deferred, used by the Ifá
// pre-maintenance cells) it is a control decision: an enrolled class is
// tolerated until the maintenance pass and the strict drain that follows it,
// while a missing class is counted as live work and can block the gate before
// the maintenance pass that would make the work runnable.
//
// Not every non-counting class belongs here. generation_activation_not_ready
// (#6686) is deliberately excluded: it can sit on any reducer domain, including
// the families pre-maintenance cells assert absent, and resolves without the
// maintenance pass. The exclusions and their reasons live in
// readinessLiveByDesignFailureClasses (drains_readiness_classes_test.go), and
// TestEveryNonCountingFailureClassIsEnrolledOrExcluded fails when a
// non-counting class has neither an entry here nor an exclusion there (#7284).
//
// Listed as string literals rather than imported to keep the gate binary free
// of a runtime reducer dependency; only the lockstep tests import the storage
// package that owns the set.
var readinessDeferredFailureClasses = map[string]bool{
	"aws_cloud_runtime_drift_state_pending":    true,
	"aws_cloud_runtime_drift_write_superseded": true,
	"secrets_iam_endpoint_not_ready":           true,
	"kubernetes_correlation_nodes_not_ready":   true,
	"gcp_relationship_nodes_not_ready":         true,
	"ec2_instance_identity_nodes_not_ready":    true,
	"cross_scope_producer_not_ready":           true,
	// #5717: an ec2_instance_uses_ami edge waiting on the EC2 instance node
	// phase. Without this entry the drain breakdown counts it as live work and
	// reports "the pipeline just needed longer" for a queue that is actually
	// blocked on a precondition.
	"aws_relationship_ec2_instance_nodes_not_ready": true,
	// #6184: fail-closed cross-repo and deployable-unit deferrals. Without
	// these entries, work waiting on backward evidence or canonical repository
	// projection looks live to the pre-maintenance quiescence decision.
	"cross_repo_backward_evidence_not_ready":                true,
	"deployable_unit_correlation_resolution_not_ready":      true,
	"deployable_unit_correlation_canonical_nodes_not_ready": true,
	"workload_materialization_resolution_not_ready":         true,
	// #7258: service catalog correlation deferred on the same relationship
	// corpus fence. Without this entry the deferral looks live to the
	// pre-maintenance quiescence decision.
	"service_catalog_correlation_resolution_not_ready": true,
	// #6759: workload materialization waiting on the deploy Repository node
	// that another scope's repo_dependency write has not committed yet.
	"workload_materialization_deployment_source_target_not_ready": true,
	// #7268: deployable_unit_correlation waiting on an edge-target Repository
	// node another scope's materialization has not committed yet.
	"shared_edge_target_not_ready": true,
	// #6785: USES waiting on its WorkloadInstance endpoint, which itself can
	// wait on the maintenance pass; CAN_PERFORM waiting on sibling-scope
	// target nodes.
	"workload_cloud_relationship_instances_not_ready": true,
	"iam_can_perform_target_not_ready":                true,
	// #5046 / #7284: graph-node readiness classes. Each waits on the
	// canonical-nodes phase for its own scope and generation, none can be
	// enqueued by a pre-maintenance cell's corpus, and none writes a family
	// those cells assert absent. Unenrolled, a retrying row here counted as
	// live and could hold pre-maintenance quiescence open until timeout.
	"aws_cloud_image_nodes_not_ready":              true,
	"aws_relationship_nodes_not_ready":             true,
	"azure_relationship_nodes_not_ready":           true,
	"ec2_block_device_kms_posture_nodes_not_ready": true,
	"ec2_internet_exposure_nodes_not_ready":        true,
	"ec2_uses_profile_nodes_not_ready":             true,
	"iam_can_assume_nodes_not_ready":               true,
	"iam_can_perform_nodes_not_ready":              true,
	"iam_escalation_nodes_not_ready":               true,
	"iam_instance_profile_role_nodes_not_ready":    true,
	"observability_coverage_nodes_not_ready":       true,
	"rds_posture_nodes_not_ready":                  true,
	"s3_external_principal_grant_nodes_not_ready":  true,
	"s3_internet_exposure_nodes_not_ready":         true,
	"s3_logs_to_nodes_not_ready":                   true,
	"security_group_reachability_nodes_not_ready":  true,
	"workload_cloud_relationship_nodes_not_ready":  true,
	// #6887 / #7284: the cloud-resource retract's admission-drain fence. Its
	// blocker is another scope's cloud_inventory_admission item, which the
	// same snapshot already counts as live or dead_letter, so tolerating the
	// waiter hides nothing.
	"cloud_admission_not_ready": true,
	// #6923 / #7284: the value-flow refresh singleton's input-liveness fence.
	// It writes only value-flow evidence, and its unenrolled wait (up to 30
	// minutes) would time pre-maintenance drains out -- the same shape #6785
	// enrolled workload_cloud_relationship_instances_not_ready for.
	"value_flow_inputs_not_ready": true,
}
