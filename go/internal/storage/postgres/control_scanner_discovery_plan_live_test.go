// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// De-enrolled from the live-postgres-readiness plan under the drift-gate
// option A (PR #9544, steer 6530394665): bounded pages stay proven at QA
// scale by the enrolled TestScannerDiscoveryScaleShapeLive, and the code
// below stays to re-enroll in the post-refit follow-up.

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestScannerDiscoveryPagePlanLive is the #9299 derivation-query
// performance proof: 5,000 manifest observations plus 100 tag-only rows on
// one published generation, one page read, then EXPLAIN (ANALYZE, BUFFERS)
// over the shipped candidates query. The plan is logged, not gated, except
// for the stable shape invariant: the observations leg must not be a
// sequential scan. Wall time is logged, not gated. The tag-only rows
// straddle the first page span, so the skip-count assertion also pins the
// page scope: only the 50 tag-only rows at or below the page's last
// candidate count, never the 50 beyond it.
func TestScannerDiscoveryPagePlanLive(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	const (
		scannerID  = "plan-scanner"
		producerID = "plan-producer"
		scopeID    = "kubernetes://plan"
		generation = "plan-generation"
		delivery   = "plan-delivery"
		registry   = "registry.example.invalid"
		dbIdentity = "11111111-1111-1111-1111-111111111111"
	)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed discovery plan fixture: %v", err)
		}
	}
	exec(`INSERT INTO eshu_security.role_policy_state
		(singleton,policy_revision,database_oid,database_identity,system_identifier,inventory_version,owner_role_oid,
		 authority_epoch,active)
		VALUES (TRUE,1,16384,$1,'plan-test',1,10,1,TRUE)`, dbIdentity)
	exec(`INSERT INTO collector_instances
		(instance_id,collector_kind,mode,enabled,claims_enabled,configuration,last_observed_at,created_at,updated_at)
		VALUES ($1,'scanner_worker','continuous',TRUE,TRUE,
		'{"analyzer":"image_unpacking","derive_from_running_images":true,"registry_domains":[{"authorization_domain":"plan-domain","registry":"registry.example.invalid"}]}',
		$2,$2,$2)`, scannerID, now)
	exec(`INSERT INTO collector_instances
		(instance_id,collector_kind,mode,enabled,claims_enabled,configuration,last_observed_at,created_at,updated_at)
		VALUES ($1,'kubernetes_live','continuous',TRUE,TRUE,'{}',$2,$2,$2)`, producerID, now)
	var grantEpoch int64
	if err := database.QueryRowContext(ctx, `INSERT INTO scanner_running_image_producer_grants
		(principal_oid,producer_instance_id,source_scope_id,producer_class,observation_class,contract_version)
		VALUES (16400,$1,$2,'kubernetes_pod','running_container',1)
		RETURNING grant_epoch`, producerID, scopeID).Scan(&grantEpoch); err != nil {
		t.Fatalf("seed producer grant: %v", err)
	}
	exec(`INSERT INTO scanner_running_image_generations
		(producer_instance_id,source_scope_id,source_generation_id,principal_oid,collection_fence,producer_grant_epoch,
		 producer_class,observation_class,status,observed_from,observed_through,consistency_class)
		VALUES ($1,$2,$3,16400,1,$4,'kubernetes_pod','running_container','published',$5,$5,'observed_interval')`,
		producerID, scopeID, generation, grantEpoch, now)
	exec(`INSERT INTO scanner_running_image_active_scopes
		(producer_instance_id,source_scope_id,published_generation_id,active,published_at)
		VALUES ($1,$2,$3,TRUE,$4)`, producerID, scopeID, generation, now)
	exec(`INSERT INTO scanner_running_image_delivery_receipts
		(producer_instance_id,source_scope_id,source_generation_id,delivery_id,request_sha256)
		VALUES ($1,$2,$3,$4,'plan-request')`, producerID, scopeID, generation, delivery)
	// Tag-only rows straddle the first page span: 50 land inside it, 50
	// land beyond it, so a window-scoped skip count (100) disagrees with
	// the page-scoped count (50) the page contract promises.
	exec(`INSERT INTO scanner_running_image_observations
		(producer_instance_id,source_scope_id,source_generation_id,delivery_id,object_id,slot_id,
		 producer_class,observation_class,registry,repository,runtime_image_reference,
		 runtime_digest,runtime_digest_kind,payload_sha256)
		SELECT $1,$2,$3,$4,'plan-tag-early-'||g,'container','kubernetes_pod','running_container',
		 $5,'plan/repo','registry.example.invalid/plan/repo:untagged','','unknown','plan-payload'
		FROM generate_series(1,50) AS g`, producerID, scopeID, generation, delivery, registry)
	exec(`INSERT INTO scanner_running_image_observations
		(producer_instance_id,source_scope_id,source_generation_id,delivery_id,object_id,slot_id,
		 producer_class,observation_class,registry,repository,runtime_image_reference,
		 runtime_digest,runtime_digest_kind,payload_sha256)
		SELECT $1,$2,$3,$4,'plan-obj-'||g,'container','kubernetes_pod','running_container',
		 $5,'plan/repo','registry.example.invalid/plan/repo:tag',
		 'sha256:plan-'||g,'manifest','plan-payload'
		FROM generate_series(1,5000) AS g`, producerID, scopeID, generation, delivery, registry)
	exec(`INSERT INTO scanner_running_image_observations
		(producer_instance_id,source_scope_id,source_generation_id,delivery_id,object_id,slot_id,
		 producer_class,observation_class,registry,repository,runtime_image_reference,
		 runtime_digest,runtime_digest_kind,payload_sha256)
		SELECT $1,$2,$3,$4,'plan-tag-late-'||g,'container','kubernetes_pod','running_container',
		 $5,'plan/repo','registry.example.invalid/plan/repo:untagged','','unknown','plan-payload'
		FROM generate_series(1,50) AS g`, producerID, scopeID, generation, delivery, registry)

	request := workflow.RunningImageDiscoveryRequest{
		ScannerCollectorInstanceID: scannerID,
		AuthorizationNamespace:     workflow.ScannerAuthorizationLocal,
		DiscoveryPolicyRevision:    1,
		ExpectedRolePolicyRevision: 1,
		ExpectedDatabaseIdentity:   dbIdentity,
		LocalSourceScopeIDs:        []string{scopeID},
		Limit:                      256,
	}
	page, err := store.ListAuthorizedRunningImageObservationsPage(ctx, request)
	if err != nil {
		t.Fatalf("list discovery page: %v", err)
	}
	if len(page.Candidates) != 256 || page.Exhausted {
		t.Fatalf("candidates=%d exhausted=%v, want 256/false on a 5000-row generation",
			len(page.Candidates), page.Exhausted)
	}
	// Page scope, not window scope: the 50 tag-only rows beyond the
	// page's last candidate must not leak into this page's counts.
	if page.SkippedTagOnly != 50 {
		t.Fatalf("page-scoped skipped tag-only=%d, want 50 (window holds 100)", page.SkippedTagOnly)
	}

	// EXPLAIN over the shipped candidates query with the function's own
	// argument order (see ListAuthorizedRunningImageObservationsPage). The
	// resolved cycle upper bound lives on the next cursor: the expected
	// cursor carries it only once a cycle is already open.
	explainArgs := []any{
		request.ScannerCollectorInstanceID,
		string(request.AuthorizationNamespace), request.TenantID, request.WorkspaceID,
		request.SubjectClass, request.PolicyRevisionHash, request.LocalSourceScopeIDs,
		page.ExpectedCursor.AfterObservationID,
		page.ExpectedCursor.AfterAuthorizationDomain, page.NextCursor.CycleUpperObservationID,
		request.ExpectedRolePolicyRevision, request.ExpectedDatabaseIdentity,
	}
	if len(explainArgs) != 12 {
		t.Fatalf("explain args=%d, want the 12 bound arguments both discovery reads share", len(explainArgs))
	}
	started := time.Now()
	rows, err := database.QueryContext(ctx,
		"EXPLAIN (ANALYZE, BUFFERS) "+scannerDiscoveryCandidatesQuery(request.Limit+1), explainArgs...)
	if err != nil {
		t.Fatalf("explain discovery candidates: %v", err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			_ = rows.Close()
			t.Fatalf("scan plan row: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		t.Fatalf("read plan rows: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close plan rows: %v", err)
	}
	elapsed := time.Since(started)
	t.Logf("discovery candidates EXPLAIN (ANALYZE, BUFFERS) over 5100 observations took %v:\n%s",
		elapsed, plan.String())
	if strings.Contains(plan.String(), "Seq Scan on scanner_running_image_observations") {
		t.Fatalf("derivation plan scans observations sequentially:\n%s", plan.String())
	}

	// EXPLAIN both shipped skip-counts statements: the page-bounded one
	// the read above actually ran (span end = the page's last candidate),
	// and the unbounded one an exhausted page runs. Both must stay
	// index-driven on the observations leg. The statements come from the
	// production constructor, never from a hand-copied string.
	last := page.Candidates[len(page.Candidates)-1]
	pageSkipArgs := append(append([]any{}, explainArgs...), last.ObservationID, last.AuthorizationDomain)
	for _, shape := range []struct {
		name      string
		statement string
		args      []any
	}{
		{"page-bounded", scannerDiscoverySkipCountsStatement(false), pageSkipArgs},
		{"window-remainder", scannerDiscoverySkipCountsStatement(true), explainArgs},
	} {
		skipPlan := explainDiscoveryStatement(t, database, ctx, shape.statement, shape.args)
		t.Logf("discovery skip counts (%s) EXPLAIN (ANALYZE, BUFFERS) over 5100 observations:\n%s",
			shape.name, skipPlan)
		if strings.Contains(skipPlan, "Seq Scan on scanner_running_image_observations") {
			t.Fatalf("skip counts (%s) scan observations sequentially:\n%s", shape.name, skipPlan)
		}
	}
}

// explainDiscoveryStatement runs EXPLAIN (ANALYZE, BUFFERS) over a shipped
// discovery statement and returns its plan text.
func explainDiscoveryStatement(t *testing.T, database *sql.DB, ctx context.Context, statement string, args []any) string {
	t.Helper()
	started := time.Now()
	rows, err := database.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+statement, args...)
	if err != nil {
		t.Fatalf("explain discovery statement: %v", err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			_ = rows.Close()
			t.Fatalf("scan plan row: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		t.Fatalf("read plan rows: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close plan rows: %v", err)
	}
	t.Logf("explain took %v", time.Since(started))
	return plan.String()
}

// TestScannerDiscoveryExhaustedPageSkipSpanLive pins the exhausted-page
// branch of the page-span contract: a single page holding the whole window
// counts every skip in the window remainder, which is its span.
func TestScannerDiscoveryExhaustedPageSkipSpanLive(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	const (
		scannerID  = "exhausted-scanner"
		producerID = "exhausted-producer"
		scopeID    = "kubernetes://exhausted"
		generation = "exhausted-generation"
		delivery   = "exhausted-delivery"
		registry   = "registry.example.invalid"
		dbIdentity = "22222222-2222-2222-2222-222222222222"
	)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed exhausted discovery fixture: %v", err)
		}
	}
	exec(`INSERT INTO eshu_security.role_policy_state
		(singleton,policy_revision,database_oid,database_identity,system_identifier,inventory_version,owner_role_oid,
		 authority_epoch,active)
		VALUES (TRUE,1,16384,$1,'exhausted-test',1,10,1,TRUE)`, dbIdentity)
	exec(`INSERT INTO collector_instances
		(instance_id,collector_kind,mode,enabled,claims_enabled,configuration,last_observed_at,created_at,updated_at)
		VALUES ($1,'scanner_worker','continuous',TRUE,TRUE,
		'{"analyzer":"image_unpacking","derive_from_running_images":true,"registry_domains":[{"authorization_domain":"exhausted-domain","registry":"registry.example.invalid"}]}',
		$2,$2,$2)`, scannerID, now)
	exec(`INSERT INTO collector_instances
		(instance_id,collector_kind,mode,enabled,claims_enabled,configuration,last_observed_at,created_at,updated_at)
		VALUES ($1,'kubernetes_live','continuous',TRUE,TRUE,'{}',$2,$2,$2)`, producerID, now)
	var grantEpoch int64
	if err := database.QueryRowContext(ctx, `INSERT INTO scanner_running_image_producer_grants
		(principal_oid,producer_instance_id,source_scope_id,producer_class,observation_class,contract_version)
		VALUES (16400,$1,$2,'kubernetes_pod','running_container',1)
		RETURNING grant_epoch`, producerID, scopeID).Scan(&grantEpoch); err != nil {
		t.Fatalf("seed producer grant: %v", err)
	}
	exec(`INSERT INTO scanner_running_image_generations
		(producer_instance_id,source_scope_id,source_generation_id,principal_oid,collection_fence,producer_grant_epoch,
		 producer_class,observation_class,status,observed_from,observed_through,consistency_class)
		VALUES ($1,$2,$3,16400,1,$4,'kubernetes_pod','running_container','published',$5,$5,'observed_interval')`,
		producerID, scopeID, generation, grantEpoch, now)
	exec(`INSERT INTO scanner_running_image_active_scopes
		(producer_instance_id,source_scope_id,published_generation_id,active,published_at)
		VALUES ($1,$2,$3,TRUE,$4)`, producerID, scopeID, generation, now)
	exec(`INSERT INTO scanner_running_image_delivery_receipts
		(producer_instance_id,source_scope_id,source_generation_id,delivery_id,request_sha256)
		VALUES ($1,$2,$3,$4,'exhausted-request')`, producerID, scopeID, generation, delivery)
	exec(`INSERT INTO scanner_running_image_observations
		(producer_instance_id,source_scope_id,source_generation_id,delivery_id,object_id,slot_id,
		 producer_class,observation_class,registry,repository,runtime_image_reference,
		 runtime_digest,runtime_digest_kind,payload_sha256)
		SELECT $1,$2,$3,$4,'exhausted-obj-'||g,'container','kubernetes_pod','running_container',
		 $5,'plan/repo','registry.example.invalid/plan/repo:tag',
		 'sha256:exhausted-'||g,'manifest','exhausted-payload'
		FROM generate_series(1,3) AS g`, producerID, scopeID, generation, delivery, registry)
	exec(`INSERT INTO scanner_running_image_observations
		(producer_instance_id,source_scope_id,source_generation_id,delivery_id,object_id,slot_id,
		 producer_class,observation_class,registry,repository,runtime_image_reference,
		 runtime_digest,runtime_digest_kind,payload_sha256)
		SELECT $1,$2,$3,$4,'exhausted-tag-'||g,'container','kubernetes_pod','running_container',
		 $5,'plan/repo','registry.example.invalid/plan/repo:untagged','','unknown','exhausted-payload'
		FROM generate_series(1,2) AS g`, producerID, scopeID, generation, delivery, registry)

	request := workflow.RunningImageDiscoveryRequest{
		ScannerCollectorInstanceID: scannerID,
		AuthorizationNamespace:     workflow.ScannerAuthorizationLocal,
		DiscoveryPolicyRevision:    1,
		ExpectedRolePolicyRevision: 1,
		ExpectedDatabaseIdentity:   dbIdentity,
		LocalSourceScopeIDs:        []string{scopeID},
		Limit:                      256,
	}
	page, err := store.ListAuthorizedRunningImageObservationsPage(ctx, request)
	if err != nil {
		t.Fatalf("list exhausted discovery page: %v", err)
	}
	if len(page.Candidates) != 3 || !page.Exhausted {
		t.Fatalf("candidates=%d exhausted=%v, want 3/true on a 3-candidate window",
			len(page.Candidates), page.Exhausted)
	}
	if page.SkippedTagOnly != 2 {
		t.Fatalf("exhausted page skipped tag-only=%d, want 2", page.SkippedTagOnly)
	}
	if page.SkippedUnresolvedConfig != 0 {
		t.Fatalf("exhausted page skipped unresolved config=%d, want 0", page.SkippedUnresolvedConfig)
	}
}
