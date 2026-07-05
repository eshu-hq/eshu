// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestArgoCDBearingSignalIgnoresEmptyParsedStructKeys is the regression guard for
// the #3624 Track 1 / B' carve-out precision bug: parsed_file_data serializes the
// empty struct keys "argocd_applications" and "argocd_applicationsets" into EVERY
// parsed file's payload, so a substring marker over lower(payload::text) — as the
// broad argoCDOverSelectAnchors set does for the query's $1 over-select arm —
// matches every file fact and would force EVERY partition to reload, defeating the
// memo. This test drives loadArgoCDBearingPartitions directly and proves the
// precise signal (non-empty parsed array / argoproj.io / artifact_type=argocd)
// does NOT flag a plain file that merely carries the empty keys, while it DOES
// flag a genuine ArgoCD ApplicationSet.
func TestArgoCDBearingSignalIgnoresEmptyParsedStructKeys(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionDeferredPartitionMemoSchema(t, db)

	base := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	seedScopeGen := func(scopeID, genID string) {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, NULL)", scopeID); err != nil {
			t.Fatalf("seed scope %q: %v", scopeID, err)
		}
		if _, err := db.ExecContext(ctx,
			"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
			genID, scopeID, base); err != nil {
			t.Fatalf("seed generation %q: %v", genID, err)
		}
	}
	seedFile := func(factID, scopeID, genID, payload string) {
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'file', $1, 'git', $1, $4, $4, $5::jsonb)`,
			factID, scopeID, genID, base, payload); err != nil {
			t.Fatalf("seed file fact %q: %v", factID, err)
		}
	}

	// Plain Go file: parsed_file_data carries the empty argocd_applications ([])
	// and argocd_applicationsets (null) keys exactly as the real parser emits them,
	// plus non-ArgoCD source. It must NOT be flagged ArgoCD-bearing.
	seedScopeGen("git:scope-plain", "gen-plain")
	seedFile("file-plain", "git:scope-plain", "gen-plain",
		`{"repo_id":"repo-plain","relative_path":"main.go","parsed_file_data":`+
			`{"lang":"go","imports":[],"argocd_applications":[],"argocd_applicationsets":null,"source":"package main\n"}}`)

	// Genuine ArgoCD ApplicationSet content fact (apiVersion: argoproj.io/...). It
	// MUST be flagged ArgoCD-bearing.
	seedScopeGen("git:scope-argo", "gen-argo")
	seedFile("file-argo", "git:scope-argo", "gen-argo",
		`{"repo_id":"repo-argo","artifact_type":"argocd","relative_path":"appset.yaml",`+
			`"content":"apiVersion: argoproj.io/v1alpha1\nkind: ApplicationSet\n"}`)

	adapter := SQLDB{DB: db}
	plain := scopeGenerationPartition{ScopeID: "git:scope-plain", GenerationID: "gen-plain"}
	argo := scopeGenerationPartition{ScopeID: "git:scope-argo", GenerationID: "gen-argo"}

	bearing, err := loadArgoCDBearingPartitions(ctx, adapter, []scopeGenerationPartition{plain, argo})
	if err != nil {
		t.Fatalf("loadArgoCDBearingPartitions: %v", err)
	}

	if _, flagged := bearing[plain]; flagged {
		t.Fatal("plain file with EMPTY argocd_applications/argocd_applicationsets struct keys must NOT be ArgoCD-bearing (empty-key false positive — the bug this guards against)")
	}
	if _, flagged := bearing[argo]; !flagged {
		t.Fatal("genuine ArgoCD ApplicationSet (argoproj.io) MUST be ArgoCD-bearing")
	}
}
