// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

func TestListRunningScannerImageFactsExcludesDisabledAndPagesLive(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	reader := NewFactStore(SQLDB{DB: database})
	ctx := context.Background()
	now := time.Now().UTC()
	for _, row := range []struct {
		id      string
		enabled bool
	}{{"source-enabled", true}, {"source-disabled", false}} {
		if _, err := database.ExecContext(ctx, `INSERT INTO collector_instances
          (instance_id,collector_kind,mode,enabled,claims_enabled,configuration,last_observed_at,created_at,updated_at)
          VALUES ($1,'kubernetes_live','continuous',$2,TRUE,'{}'::jsonb,$3,$3,$3)`, row.id, row.enabled, now); err != nil {
			t.Fatal(err)
		}
		scopeID := "source-scope-" + row.id
		generationID := "source-gen-" + row.id
		if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes
          (scope_id,scope_kind,source_system,source_key,collector_kind,partition_key,observed_at,ingested_at,status,active_generation_id)
          VALUES ($1,'kubernetes','kubernetes_live',$1,'kubernetes_live',$1,$2,$2,'active',$3)`, scopeID, now, generationID); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations
          (generation_id,scope_id,trigger_kind,observed_at,ingested_at,status)
          VALUES ($1,$2,'snapshot',$3,$3,'active')`, generationID, scopeID, now); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO fact_records
          (fact_id,scope_id,generation_id,fact_kind,stable_fact_key,source_system,source_fact_key,observed_at,ingested_at,payload)
          VALUES ($1,$2,$3,'kubernetes_live.pod_template',$1,'kubernetes_live',$1,$4,$4,$5::jsonb)`,
			"fact-"+row.id, scopeID, generationID, now, `{"collector_instance_id":"`+row.id+`","containers":[{"resolved_image_digest":"registry.example.invalid/team/service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`); err != nil {
			t.Fatal(err)
		}
	}
	page, err := reader.ListRunningScannerImageFactsPage(ctx, "", 1)
	if err != nil || len(page) != 1 || page[0].FactID != "fact-source-enabled" {
		t.Fatalf("first page = %+v, %v", page, err)
	}
	page, err = reader.ListRunningScannerImageFactsPage(ctx, page[0].FactID, 1)
	if err != nil || len(page) != 0 {
		t.Fatalf("second page = %+v, %v; want empty", page, err)
	}
}
