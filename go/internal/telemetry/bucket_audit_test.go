// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// bucketAuditEntry describes one _seconds histogram for the bucket audit.
type bucketAuditEntry struct {
	MetricName string
	Buckets    []float64
	Note       string
}

// bucketAuditTable enumerates every _seconds histogram registered in
// instruments.go with its explicit bucket boundaries. Histograms that
// use the default OTEL bucket set (no WithExplicitBucketBoundaries call)
// are listed with nil buckets.
func bucketAuditTable() []bucketAuditEntry {
	collectorBuckets := []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	workflowClaimWaitBuckets := []float64{0, 0.1, 0.5, 1, 5, 10, 30, 60, 300, 900, 1800, 3600}
	reducerWaitBuckets := []float64{0.001, 0.01, 0.1, 1, 5, 10, 30, 60, 300, 900, 1800, 3600, 21600}
	scannerWorkerWaitBuckets := []float64{0.001, 0.01, 0.1, 1, 5, 10, 30, 60, 300, 900, 1800, 3600, 21600}
	sharedProjectionProcessingBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	neo4jQueryBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	backfillBuckets := []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}

	return []bucketAuditEntry{
		// ---- collector and workflow histograms ----
		{MetricName: "eshu_dp_collector_observe_duration_seconds", Buckets: collectorBuckets},
		{MetricName: "eshu_dp_workflow_claim_wait_seconds", Buckets: workflowClaimWaitBuckets},
		{MetricName: "eshu_dp_tfstate_claim_wait_seconds", Buckets: workflowClaimWaitBuckets},
		{MetricName: "eshu_dp_workflow_claim_lease_age_seconds", Buckets: workflowClaimWaitBuckets},
		{MetricName: "eshu_dp_graph_write_backpressure_wait_seconds", Buckets: workflowClaimWaitBuckets},
		{MetricName: "eshu_dp_tfstate_parse_duration_seconds", Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}},
		{MetricName: "eshu_dp_webhook_request_duration_seconds", Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}},
		{MetricName: "eshu_dp_webhook_store_duration_seconds", Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}},

		// ---- collector fetch histograms ----
		{MetricName: "eshu_dp_oci_registry_scan_duration_seconds", Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120}},
		{MetricName: "eshu_dp_kubernetes_list_duration_seconds", Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120}},
		{MetricName: "eshu_dp_dependency_list_duration_seconds", Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}},
		{MetricName: "eshu_dp_package_registry_observe_duration_seconds", Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}},
		{MetricName: "eshu_dp_package_registry_generation_lag_seconds", Buckets: collectorBuckets},
		{MetricName: "eshu_dp_vulnerability_intelligence_fetch_duration_seconds", Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}},
		{MetricName: "eshu_dp_security_alert_fetch_duration_seconds", Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}},
		{MetricName: "eshu_dp_ci_cd_run_fetch_duration_seconds", Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}},
		{MetricName: "eshu_dp_pagerduty_fetch_duration_seconds", Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}},
		{MetricName: "eshu_dp_pagerduty_generation_lag_seconds", Buckets: collectorBuckets},
		{MetricName: "eshu_dp_jira_fetch_duration_seconds", Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}},
		{MetricName: "eshu_dp_grafana_fetch_duration_seconds", Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}},
		{MetricName: "eshu_dp_prometheus_mimir_fetch_duration_seconds", Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}},
		{MetricName: "eshu_dp_loki_fetch_duration_seconds", Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}},
		{MetricName: "eshu_dp_tempo_fetch_duration_seconds", Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}},
		{MetricName: "eshu_dp_confluence_fetch_duration_seconds", Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}},
		{MetricName: "eshu_dp_aws_scan_duration_seconds", Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}},

		// ---- scanner worker histograms ----
		{MetricName: "eshu_dp_scanner_worker_queue_wait_seconds", Buckets: scannerWorkerWaitBuckets},
		{MetricName: "eshu_dp_scanner_worker_scan_duration_seconds", Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1200}},
		{MetricName: "eshu_dp_scanner_worker_cpu_seconds", Buckets: []float64{0.01, 0.1, 1, 10, 30, 60, 120, 300, 600, 1800}},

		// ---- reducer / projector histograms ----
		{MetricName: "eshu_dp_reducer_run_duration_seconds", Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 900}},
		{MetricName: "eshu_dp_reducer_queue_wait_seconds", Buckets: reducerWaitBuckets},
		{MetricName: "eshu_dp_search_index_write_duration_seconds", Buckets: reducerWaitBuckets},
		{MetricName: "eshu_dp_gcp_materialization_duration_seconds", Buckets: reducerWaitBuckets},
		{MetricName: "eshu_dp_projector_run_duration_seconds", Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120}},
		{MetricName: "eshu_dp_projector_stage_duration_seconds", Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120}},

		// ---- scope / fact / queue ----
		{MetricName: "eshu_dp_scope_assign_duration_seconds", Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}},
		{MetricName: "eshu_dp_fact_emit_duration_seconds", Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}},
		{MetricName: "eshu_dp_queue_claim_duration_seconds", Buckets: nil}, // sub-second ops; default OTEL buckets are adequate

		// ---- generation retention ----
		{MetricName: "eshu_dp_generation_retention_duration_seconds", Buckets: []float64{0.001, 0.01, 0.1, 1, 5, 10, 30, 60, 300, 900}},
		{MetricName: "eshu_dp_generation_retention_oldest_eligible_age_seconds", Buckets: []float64{3600, 21600, 43200, 86400, 259200, 604800, 1209600, 2592000, 7776000}},

		// ---- canonical write / retract / phase ----
		{MetricName: "eshu_dp_canonical_write_duration_seconds", Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}},
		{MetricName: "eshu_dp_canonical_retract_duration_seconds", Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}},
		{MetricName: "eshu_dp_canonical_phase_duration_seconds", Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}},
		{MetricName: "eshu_dp_canonical_projection_duration_seconds", Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}},

		// ---- query / API histograms ----
		{MetricName: "eshu_dp_postgres_query_duration_seconds", Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}},
		{MetricName: "eshu_dp_neo4j_query_duration_seconds", Buckets: neo4jQueryBuckets},
		{MetricName: "eshu_dp_iac_resource_list_duration_seconds", Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}},
		{MetricName: "eshu_dp_cloud_resource_list_duration_seconds", Buckets: neo4jQueryBuckets},
		{MetricName: "eshu_dp_api_request_duration_seconds", Buckets: neo4jQueryBuckets},

		// ---- shared acceptance / projection ----
		{MetricName: "eshu_dp_shared_acceptance_upsert_duration_seconds", Buckets: nil}, // default OTEL
		{MetricName: "eshu_dp_shared_acceptance_lookup_duration_seconds", Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}},
		{MetricName: "eshu_dp_shared_projection_intent_wait_seconds", Buckets: sharedProjectionWaitBuckets()},
		{MetricName: "eshu_dp_shared_projection_processing_seconds", Buckets: sharedProjectionProcessingBuckets},
		{MetricName: "eshu_dp_shared_projection_step_seconds", Buckets: sharedProjectionProcessingBuckets},
		{MetricName: "eshu_dp_shared_projection_partition_processing_seconds", Buckets: sharedProjectionProcessingBuckets},
		{MetricName: "eshu_dp_documentation_drift_generation_duration_seconds", Buckets: nil}, // default OTEL

		// ---- repo snapshot / collector stage / file parse ----
		{MetricName: "eshu_dp_repo_snapshot_duration_seconds", Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}},
		{MetricName: "eshu_dp_collector_snapshot_stage_duration_seconds", Buckets: []float64{0.005, 0.025, 0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120}},
		{MetricName: "eshu_dp_file_parse_duration_seconds", Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}},

		// ---- SCIP / large repo / edge write / code call ----
		{MetricName: "eshu_dp_scip_process_wait_seconds", Buckets: []float64{0, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60}},
		{MetricName: "eshu_dp_large_repo_semaphore_wait_seconds", Buckets: []float64{0, 0.1, 0.5, 1, 5, 10, 30, 60, 120, 300}},
		{MetricName: "eshu_dp_shared_edge_write_group_duration_seconds", Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}},
		{MetricName: "eshu_dp_code_call_edge_batch_duration_seconds", Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}},

		// ---- deferred backfill / IAC reachability / cross-repo ----
		{MetricName: "eshu_dp_deferred_backfill_duration_seconds", Buckets: backfillBuckets},
		{MetricName: "eshu_dp_deferred_backfill_batch_duration_seconds", Buckets: backfillBuckets},
		{MetricName: "eshu_dp_deferred_backfill_partition_load_duration_seconds", Buckets: backfillBuckets},
		{MetricName: "eshu_dp_iac_reachability_materialization_duration_seconds", Buckets: backfillBuckets},
		{MetricName: "eshu_dp_cross_repo_resolution_duration_seconds", Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}},

		// ---- bootstrap / workflow claim run / pipeline overlap ----
		{MetricName: "eshu_dp_bootstrap_pipeline_phase_seconds", Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 600, 1200, 1800, 3600}},
		{MetricName: "eshu_dp_workflow_claim_run_duration_seconds", Buckets: []float64{0.1, 0.5, 1, 5, 15, 30, 60, 120, 300, 600, 1200, 1800}},
		{MetricName: "eshu_dp_pipeline_overlap_seconds", Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600, 1800}},
	}
}

func sharedProjectionWaitBuckets() []float64 {
	return []float64{0.001, 0.01, 0.1, 1, 5, 10, 30, 60, 300, 900, 1800, 3600, 21600}
}

// bucketVerdict records the result of one histogram's bucket boundary audit.
type bucketVerdict struct {
	MetricName string
	Verdict    string
	Reason     string
}

// TestBucketAudit verifies every _seconds histogram bucket array meets
// basic mathematical validity properties. This test is the Q-3 bucket
// audit from Epic Q #3743.
func TestBucketAudit(t *testing.T) {
	table := bucketAuditTable()
	var verdicts []bucketVerdict

	for _, entry := range table {
		v := auditEntry(entry)
		verdicts = append(verdicts, v)
		if v.Verdict == "fails" {
			t.Errorf("%s: FAILS — %s", v.MetricName, v.Reason)
		}
	}

	t.Logf("\n=== BUCKET AUDIT SUMMARY (%d histograms) ===", len(table))
	t.Logf("%-60s %-8s %s", "METRIC", "VERDICT", "NOTE")
	t.Logf("%-60s %-8s %s", strings.Repeat("-", 60), strings.Repeat("-", 8), strings.Repeat("-", 40))

	passing, failing, usingDefault := 0, 0, 0
	for _, v := range verdicts {
		note := v.Reason
		if len(note) > 50 {
			note = note[:47] + "..."
		}
		t.Logf("%-60s %-8s %s", v.MetricName, v.Verdict, note)
		switch v.Verdict {
		case "passes":
			passing++
		case "fails":
			failing++
		case "default":
			usingDefault++
		}
	}
	t.Logf("\nPass: %d  Fail: %d  Default-OTEL-buckets: %d  Total: %d",
		passing, failing, usingDefault, len(table))
}

func auditEntry(entry bucketAuditEntry) bucketVerdict {
	if entry.Buckets == nil {
		return bucketVerdict{
			MetricName: entry.MetricName,
			Verdict:    "default",
			Reason:     "uses default OTEL histogram buckets (no explicit boundaries); evaluate in follow-up",
		}
	}
	return auditBuckets(entry.MetricName, entry.Buckets)
}

func auditBuckets(name string, buckets []float64) bucketVerdict {
	problems := make([]string, 0)

	if len(buckets) == 0 {
		problems = append(problems, "bucket array is empty")
		return bucketVerdict{MetricName: name, Verdict: "fails", Reason: strings.Join(problems, "; ")}
	}

	// 1. Monotonicity: buckets must be strictly increasing.
	for i := 1; i < len(buckets); i++ {
		if buckets[i] <= buckets[i-1] {
			problems = append(problems,
				fmt.Sprintf("non-monotonic at index %d: %.6g <= %.6g", i, buckets[i], buckets[i-1]))
		}
	}

	// 2. No negative buckets (zero is allowed for wait/lag histograms).
	for i, b := range buckets {
		if b < 0 {
			problems = append(problems, fmt.Sprintf("negative bucket at index %d: %.6g", i, b))
		}
	}

	// 3. No NaN or Inf buckets.
	for i, b := range buckets {
		if math.IsNaN(b) {
			problems = append(problems, fmt.Sprintf("NaN bucket at index %d", i))
		}
		if math.IsInf(b, 0) {
			problems = append(problems, fmt.Sprintf("Inf bucket at index %d", i))
		}
	}

	// 4. Gap check: no pathological gaps where the ratio between adjacent
	//    buckets exceeds 100x (indicating a missing intermediate bucket
	//    where real measurements would land with zero resolution).
	//    Exempt the first pair when the first bucket is 0 (wait/lag).
	startIdx := 0
	if buckets[0] == 0 && len(buckets) > 1 {
		startIdx = 1
	}
	for i := startIdx + 1; i < len(buckets); i++ {
		if buckets[i-1] > 0 && buckets[i]/buckets[i-1] > 100 {
			problems = append(problems,
				fmt.Sprintf("large gap between bucket %d and %d: %.6g → %.6g (%.1fx jump)",
					i-1, i, buckets[i-1], buckets[i], buckets[i]/buckets[i-1]))
		}
	}

	if len(problems) > 0 {
		return bucketVerdict{MetricName: name, Verdict: "fails", Reason: strings.Join(problems, "; ")}
	}
	return bucketVerdict{MetricName: name, Verdict: "passes", Reason: fmt.Sprintf("%d buckets, range [%g, %g]", len(buckets), buckets[0], buckets[len(buckets)-1])}
}
