// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 disposable PostgreSQL proof that authority epoch bumps never ABA across tenant, workspace, grant, and instance revocation cycles
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"testing"
	"time"
)

func TestScannerAuthorityEpochNoABALive(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, statement := range []string{
		`INSERT INTO ingestion_scopes
          (scope_id,scope_kind,source_system,source_key,collector_kind,partition_key,observed_at,ingested_at,status)
          VALUES ('epoch-source','kubernetes','kubernetes_live','epoch-source','kubernetes_live','epoch-source',$1,$1,'active')`,
		`INSERT INTO tenants (tenant_id,status,policy_revision_hash,created_at,updated_at)
          VALUES ('epoch-tenant','active','same-policy',$1,$1)`,
		`INSERT INTO workspaces (tenant_id,workspace_id,status,policy_revision_hash,created_at,updated_at)
          VALUES ('epoch-tenant','epoch-workspace','active','same-policy',$1,$1)`,
		`INSERT INTO tenant_scope_grants
          (tenant_id,workspace_id,scope_id,subject_class,grant_source,policy_revision_hash,
           effective_at,created_at,updated_at)
          VALUES ('epoch-tenant','epoch-workspace','epoch-source','collector','operator','same-policy',$1,$1,$1)`,
		`INSERT INTO collector_instances
          (instance_id,collector_kind,mode,enabled,claims_enabled,configuration,last_observed_at,created_at,updated_at)
          VALUES ('epoch-scanner','scanner_worker','continuous',TRUE,TRUE,'{}'::jsonb,$1,$1,$1)`,
	} {
		if _, err := database.ExecContext(ctx, statement, now); err != nil {
			t.Fatal(err)
		}
	}
	read := func(table, predicate string) int64 {
		t.Helper()
		var epoch int64
		if err := database.QueryRowContext(ctx, `SELECT authority_epoch FROM `+table+` WHERE `+predicate).Scan(&epoch); err != nil {
			t.Fatal(err)
		}
		if epoch <= 0 {
			t.Fatalf("%s authority epoch = %d, want positive", table, epoch)
		}
		return epoch
	}
	grantWhere := `tenant_id='epoch-tenant' AND workspace_id='epoch-workspace' AND scope_id='epoch-source' AND subject_class='collector'`
	tenantWhere := `tenant_id='epoch-tenant'`
	workspaceWhere := tenantWhere + ` AND workspace_id='epoch-workspace'`
	scannerWhere := `instance_id='epoch-scanner'`
	grantEpoch := read("tenant_scope_grants", grantWhere)
	tenantEpoch := read("tenants", tenantWhere)
	workspaceEpoch := read("workspaces", workspaceWhere)
	scannerEpoch := read("collector_instances", scannerWhere)

	if _, err := database.ExecContext(ctx, `UPDATE tenant_scope_grants SET updated_at=$1 WHERE `+grantWhere, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := read("tenant_scope_grants", grantWhere); got != grantEpoch {
		t.Fatalf("metadata-only grant update changed authority epoch %d -> %d", grantEpoch, got)
	}
	for _, tombstone := range []any{now, nil} {
		if _, err := database.ExecContext(ctx, `UPDATE tenant_scope_grants SET tombstoned_at=$1 WHERE `+grantWhere, tombstone); err != nil {
			t.Fatal(err)
		}
		got := read("tenant_scope_grants", grantWhere)
		if got <= grantEpoch {
			t.Fatalf("grant revoke/regrant reused authority epoch %d -> %d", grantEpoch, got)
		}
		grantEpoch = got
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM tenant_scope_grants WHERE `+grantWhere); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO tenant_scope_grants
      (tenant_id,workspace_id,scope_id,subject_class,grant_source,policy_revision_hash,
       effective_at,created_at,updated_at)
      VALUES ('epoch-tenant','epoch-workspace','epoch-source','collector','operator','same-policy',$1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if got := read("tenant_scope_grants", grantWhere); got <= grantEpoch {
		t.Fatalf("deleted/reinserted grant reused authority epoch %d -> %d", grantEpoch, got)
	}
	for _, state := range []string{"disabled", "active"} {
		if _, err := database.ExecContext(ctx, `UPDATE tenants SET status=$1 WHERE `+tenantWhere, state); err != nil {
			t.Fatal(err)
		}
		got := read("tenants", tenantWhere)
		if got <= tenantEpoch {
			t.Fatalf("tenant %s reused authority epoch %d -> %d", state, tenantEpoch, got)
		}
		tenantEpoch = got
		if _, err := database.ExecContext(ctx, `UPDATE workspaces SET status=$1 WHERE `+workspaceWhere, state); err != nil {
			t.Fatal(err)
		}
		got = read("workspaces", workspaceWhere)
		if got <= workspaceEpoch {
			t.Fatalf("workspace %s reused authority epoch %d -> %d", state, workspaceEpoch, got)
		}
		workspaceEpoch = got
	}
	for _, enabled := range []bool{false, true} {
		if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET enabled=$1,claims_enabled=$1 WHERE `+scannerWhere, enabled); err != nil {
			t.Fatal(err)
		}
		got := read("collector_instances", scannerWhere)
		if got <= scannerEpoch {
			t.Fatalf("collector enabled=%t reused authority epoch %d -> %d", enabled, scannerEpoch, got)
		}
		scannerEpoch = got
	}
}
