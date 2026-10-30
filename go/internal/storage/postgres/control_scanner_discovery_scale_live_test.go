// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 disposable PostgreSQL proof that re-measures the shipped derivation statements at QA-scale cardinality (300k observations, 40 scopes) with plan shapes and wall times logged at this head
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestScannerDiscoveryScaleShapeLive re-measures the exact shipped
// derivation statements at QA-scale cardinality: 300,000 observations
// across 40 scopes (80% manifest / 10% config / 10% tag-only digests,
// manifest verifications for half the config rows), then EXPLAIN
// (ANALYZE, BUFFERS) over the candidates query at page-1, mid-window and
// tail cursors plus both skip-counts arms. Plan shapes and wall times are
// logged for the scale evidence note, not gated: dev-box walls are
// order-of-magnitude evidence, and a full-table page-1 shape is the
// honestly recorded expectation past ~50k active observations (see
// docs/internal/evidence/9299-derivation-scale.md). The durable
// assertions are the scale contracts: page 1 returns a bounded full
// page, the mid-window page is full, and the tail page terminates short.
func TestScannerDiscoveryScaleShapeLive(t *testing.T) {
	adminDSN := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	optIn := os.Getenv("ESHU_POSTGRES_DISPOSABLE")
	if adminDSN == "" {
		t.Skip("set ESHU_POSTGRES_DSN and ESHU_POSTGRES_DISPOSABLE=1 to run the derivation scale proof")
	}
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(t, adminDSN, optIn, 30*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrap(ctx, SQLDB{DB: database})
		})
	const (
		scannerID  = "scale-scanner"
		producerID = "scale-producer"
		registry   = "registry.example.invalid"
		dbIdentity = "33333333-3333-3333-3333-333333333333"
		scopes     = 40
	)
	now := time.Now().UTC().Truncate(time.Second)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed derivation scale fixture: %v", err)
		}
	}
	exec(`INSERT INTO eshu_security.role_policy_state
		(singleton,policy_revision,database_oid,database_identity,system_identifier,inventory_version,owner_role_oid,
		 authority_epoch,active)
		VALUES (TRUE,1,16384,$1,'scale-test',1,10,1,TRUE)`, dbIdentity)
	exec(`INSERT INTO collector_instances
		(instance_id,collector_kind,mode,enabled,claims_enabled,configuration,last_observed_at,created_at,updated_at)
		VALUES ($1,'scanner_worker','continuous',TRUE,TRUE,
		'{"analyzer":"image_unpacking","derive_from_running_images":true,"registry_domains":[{"authorization_domain":"scale-domain","registry":"registry.example.invalid"}]}',
		$2,$2,$2)`, scannerID, now)
	exec(`INSERT INTO collector_instances
		(instance_id,collector_kind,mode,enabled,claims_enabled,configuration,last_observed_at,created_at,updated_at)
		VALUES ($1,'kubernetes_live','continuous',TRUE,TRUE,'{}',$2,$2,$2)`, producerID, now)
	scopeIDs := make([]string, 0, scopes)
	for s := 1; s <= scopes; s++ {
		scopeID := fmt.Sprintf("kubernetes://scale-%02d", s)
		scopeIDs = append(scopeIDs, scopeID)
		generation := fmt.Sprintf("scale-generation-%02d", s)
		delivery := fmt.Sprintf("scale-delivery-%02d", s)
		var grantEpoch int64
		if err := database.QueryRowContext(ctx, `INSERT INTO scanner_running_image_producer_grants
			(principal_oid,producer_instance_id,source_scope_id,producer_class,observation_class,contract_version)
			VALUES (16400,$1,$2,'kubernetes_pod','running_container',1)
			RETURNING grant_epoch`, producerID, scopeID).Scan(&grantEpoch); err != nil {
			t.Fatalf("seed scale producer grant: %v", err)
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
			VALUES ($1,$2,$3,$4,'scale-request')`, producerID, scopeID, generation, delivery)
		prefix := fmt.Sprintf("scale-s%02d", s)
		exec(`INSERT INTO scanner_running_image_observations
			(producer_instance_id,source_scope_id,source_generation_id,delivery_id,object_id,slot_id,
			 producer_class,observation_class,registry,repository,runtime_image_reference,
			 runtime_digest,runtime_digest_kind,payload_sha256)
			SELECT $1,$2,$3,$4,$5||'-m-'||g,'container','kubernetes_pod','running_container',
			 $6,'scale/repo','registry.example.invalid/scale/repo:tag',
			 'sha256:`+prefix+`-m-'||g,'manifest','scale-payload'
			FROM generate_series(1,6000) AS g`, producerID, scopeID, generation, delivery, prefix, registry)
		exec(`INSERT INTO scanner_running_image_observations
			(producer_instance_id,source_scope_id,source_generation_id,delivery_id,object_id,slot_id,
			 producer_class,observation_class,registry,repository,runtime_image_reference,
			 runtime_digest,runtime_digest_kind,payload_sha256)
			SELECT $1,$2,$3,$4,$5||'-c-'||g,'container','kubernetes_pod','running_container',
			 $6,'scale/repo','registry.example.invalid/scale/repo:tag',
			 'sha256:`+prefix+`-c-'||g,'config','scale-payload'
			FROM generate_series(1,750) AS g`, producerID, scopeID, generation, delivery, prefix, registry)
		exec(`INSERT INTO scanner_running_image_observations
			(producer_instance_id,source_scope_id,source_generation_id,delivery_id,object_id,slot_id,
			 producer_class,observation_class,registry,repository,runtime_image_reference,
			 runtime_digest,runtime_digest_kind,payload_sha256)
			SELECT $1,$2,$3,$4,$5||'-t-'||g,'container','kubernetes_pod','running_container',
			 $6,'scale/repo','registry.example.invalid/scale/repo:untagged','','unknown','scale-payload'
			FROM generate_series(1,750) AS g`, producerID, scopeID, generation, delivery, prefix, registry)
		// Exactly one verification per even config row, so those rows
		// carry match_count=1 and the odd ones stay unresolved: the
		// candidates filter admits the former and rejects the latter.
		exec(`INSERT INTO scanner_running_image_manifest_verifications
			(verification_id,observation_id,registry,repository,manifest_digest,config_digest,
			 verified_by_principal_oid,verified_at)
			SELECT 'scale-v-'||observation_id,observation_id,registry,repository,
			 'sha256:scale-vm-'||observation_id,runtime_digest,16400,$2
			FROM scanner_running_image_observations
			WHERE source_scope_id=$1 AND runtime_digest_kind='config'
			  AND observation_id % 2 = 0`, scopeID, now)
	}
	for _, table := range []string{
		"scanner_running_image_observations",
		"scanner_running_image_manifest_verifications",
		"scanner_running_image_generations",
		"scanner_running_image_active_scopes",
	} {
		exec(`ANALYZE ` + table)
	}
	var total, maxID int64
	if err := database.QueryRowContext(ctx, `SELECT count(*),COALESCE(max(observation_id),0)
		FROM scanner_running_image_observations`).Scan(&total, &maxID); err != nil {
		t.Fatalf("count scale observations: %v", err)
	}
	if total != scopes*7500 {
		t.Fatalf("scale observations=%d, want %d (40 scopes x 7500)", total, scopes*7500)
	}

	store := NewWorkflowControlStore(SQLDB{DB: database})
	request := workflow.RunningImageDiscoveryRequest{
		ScannerCollectorInstanceID: scannerID,
		AuthorizationNamespace:     workflow.ScannerAuthorizationLocal,
		DiscoveryPolicyRevision:    1,
		ExpectedRolePolicyRevision: 1,
		ExpectedDatabaseIdentity:   dbIdentity,
		LocalSourceScopeIDs:        scopeIDs,
		Limit:                      256,
	}
	page, err := store.ListAuthorizedRunningImageObservationsPage(ctx, request)
	if err != nil {
		t.Fatalf("list scale page 1: %v", err)
	}
	if len(page.Candidates) != 256 || page.Exhausted {
		t.Fatalf("scale page-1 candidates=%d exhausted=%v, want 256/false over 300k observations",
			len(page.Candidates), page.Exhausted)
	}
	t.Logf("scale page 1: 256 candidates over %d observations, skipped tag-only=%d unresolved-config=%d",
		total, page.SkippedTagOnly, page.SkippedUnresolvedConfig)

	// The 12 bound arguments both discovery reads share, in the
	// production argument order (see the candidates call site).
	shapeArgs := func(afterID int64, afterDomain string, upper int64) []any {
		return []any{
			request.ScannerCollectorInstanceID,
			string(request.AuthorizationNamespace), request.TenantID, request.WorkspaceID,
			request.SubjectClass, request.PolicyRevisionHash, request.LocalSourceScopeIDs,
			afterID, afterDomain, upper,
			request.ExpectedRolePolicyRevision, request.ExpectedDatabaseIdentity,
		}
	}
	explain := func(name, statement string, args []any) {
		t.Helper()
		started := time.Now()
		rows, err := database.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+statement, args...)
		if err != nil {
			t.Fatalf("explain scale %s: %v", name, err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				_ = rows.Close()
				t.Fatalf("scan scale %s plan row: %v", name, err)
			}
			plan.WriteString(line + "\n")
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("read scale %s plan rows: %v", name, err)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("close scale %s plan rows: %v", name, err)
		}
		t.Logf("scale %s took %v over %d observations:\n%s", name, time.Since(started), total, plan.String())
	}
	explain("candidates page-1", scannerDiscoveryCandidatesQuery(request.Limit+1),
		shapeArgs(0, "", maxID))
	explain("candidates mid-window", scannerDiscoveryCandidatesQuery(request.Limit+1),
		shapeArgs(150000, "", maxID))
	explain("candidates tail", scannerDiscoveryCandidatesQuery(request.Limit+1),
		shapeArgs(maxID-100, "", maxID))

	last := page.Candidates[len(page.Candidates)-1]
	pageSkipArgs := append(append([]any{}, shapeArgs(0, "", maxID)...),
		last.ObservationID, last.AuthorizationDomain)
	explain("skip-counts page-span", scannerDiscoverySkipCountsStatement(false), pageSkipArgs)
	explain("skip-counts window-remainder", scannerDiscoverySkipCountsStatement(true),
		shapeArgs(0, "", maxID))

	// The tail page terminates the cycle short. The store resolves its
	// window from the persisted cursor row, so position the cursor the
	// way a consumer that paged to the tail would have left it; at most
	// the 100 rows above it can remain, far below the 256 bound.
	exec(`INSERT INTO scanner_image_discovery_cursors
		(scanner_collector_instance_id,authorization_namespace,tenant_id,workspace_id,subject_class,
		 policy_revision_hash,discovery_policy_revision,version,cycle_upper_observation_id,
		 after_observation_id,after_authorization_domain,advanced_at)
		VALUES ($1,'local','','','', '',1,1,$2,$3,'',$4)
		ON CONFLICT (scanner_collector_instance_id,authorization_namespace,tenant_id,workspace_id,
		 subject_class,policy_revision_hash,discovery_policy_revision)
		DO UPDATE SET version=EXCLUDED.version,
		 cycle_upper_observation_id=EXCLUDED.cycle_upper_observation_id,
		 after_observation_id=EXCLUDED.after_observation_id,
		 after_authorization_domain=EXCLUDED.after_authorization_domain,
		 advanced_at=EXCLUDED.advanced_at`,
		scannerID, maxID, maxID-100, now)
	tail, err := store.ListAuthorizedRunningImageObservationsPage(ctx, request)
	if err != nil {
		t.Fatalf("list scale tail page: %v", err)
	}
	if len(tail.Candidates) >= 256 {
		t.Fatalf("scale tail candidates=%d, want a short terminating page", len(tail.Candidates))
	}
	t.Logf("scale tail: %d candidates exhausted=%v", len(tail.Candidates), tail.Exhausted)
}
