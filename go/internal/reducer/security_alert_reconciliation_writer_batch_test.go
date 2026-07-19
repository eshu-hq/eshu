// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// TestPostgresSecurityAlertReconciliationWriterPersistsBatchedFacts proves
// WriteSecurityAlertReconciliations upserts decisions through the shared
// reducerBatchInsertFacts bulk-insert path (issue #5317) rather than one
// ExecContext per decision, and that the decoded rows carry byte-identical
// content to what the retired per-row canonicalReducerFactInsertQuery loop
// produced: the row-building helpers (securityAlertReconciliationFactID/
// StableFactKey/Payload/ScopeID/GenerationID) are unchanged, only the
// ExecContext call site moved.
func TestPostgresSecurityAlertReconciliationWriterPersistsBatchedFacts(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresSecurityAlertReconciliationWriter{DB: db}

	write := SecurityAlertReconciliationWrite{
		IntentID:     "intent-security-alert-batch",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-security-alert-batch",
		SourceSystem: "github_dependabot",
		Cause:        "provider alerts observed",
		Decisions: []SecurityAlertReconciliationDecision{
			{
				Provider:             "github_dependabot",
				ProviderAlertID:      "alert-1",
				ProviderAlertNumber:  1,
				ProviderRepositoryID: "repo:team-api",
				RepositoryID:         "repo:team-api",
				PackageID:            "npm:left-pad@1",
				Status:               SecurityAlertReconciliationMatched,
			},
			{
				Provider:                  "github_dependabot",
				ProviderAlertID:           "alert-2",
				ProviderAlertNumber:       2,
				ProviderRepositoryID:      "security-alert:github:acme/api",
				ProviderAlertScopeID:      "security-alert:github:acme/api",
				ProviderAlertGenerationID: "security-alert-generation-2",
				RepositoryID:              "repo:team-api",
				PackageID:                 "npm:left-pad@2",
				Status:                    SecurityAlertReconciliationMatched,
			},
		},
	}

	result, err := writer.WriteSecurityAlertReconciliations(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSecurityAlertReconciliations() error = %v, want nil", err)
	}
	if got, want := result.FactsWritten, 2; got != want {
		t.Fatalf("FactsWritten = %d, want %d", got, want)
	}

	// One ExecContext call for two decisions: proves the batched path
	// replaced the retired per-decision loop.
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("ExecContext calls = %d, want %d (batched insert)", got, want)
	}
	rows := decodeBatchedFactCalls(t, db.execs)
	if got, want := len(rows), 2; got != want {
		t.Fatalf("decoded rows = %d, want %d", got, want)
	}

	for i, decision := range write.Decisions {
		row := rows[i]
		if got, want := row.FactID, securityAlertReconciliationFactID(write, decision); got != want {
			t.Fatalf("row %d FactID = %q, want %q (byte-identical to per-row loop)", i, got, want)
		}
		if got, want := row.StableFactKey, securityAlertReconciliationStableFactKey(write, decision); got != want {
			t.Fatalf("row %d StableFactKey = %q, want %q", i, got, want)
		}
		if got, want := row.ScopeID, securityAlertReconciliationWriteScopeID(write, decision); got != want {
			t.Fatalf("row %d ScopeID = %q, want %q (per-decision scope override must survive batching)", i, got, want)
		}
		if got, want := row.GenerationID, securityAlertReconciliationWriteGenerationID(write, decision); got != want {
			t.Fatalf("row %d GenerationID = %q, want %q (per-decision generation override must survive batching)", i, got, want)
		}
		if got, want := row.FactKind, securityAlertReconciliationFactKind; got != want {
			t.Fatalf("row %d FactKind = %q, want %q", i, got, want)
		}
		wantPayload, err := json.Marshal(securityAlertReconciliationPayload(write, decision))
		if err != nil {
			t.Fatalf("marshal expected payload: %v", err)
		}
		if got, want := string(row.Payload), string(wantPayload); got != want {
			t.Fatalf("row %d Payload = %s, want %s (byte-identical to per-row loop)", i, got, want)
		}
	}
}

// TestWriteSecurityAlertReconciliationsBoundedExecCount guards issue #5317: N
// decisions must be persisted in O(N/batchSize) bulk inserts rather than one
// ExecContext per decision.
func TestWriteSecurityAlertReconciliationsBoundedExecCount(t *testing.T) {
	t.Parallel()

	const decisionCount = 1500
	decisions := make([]SecurityAlertReconciliationDecision, decisionCount)
	for i := range decisions {
		decisions[i] = SecurityAlertReconciliationDecision{
			Provider:             "github_dependabot",
			ProviderAlertID:      fmt.Sprintf("alert-%d", i),
			ProviderAlertNumber:  int64(i),
			ProviderRepositoryID: "repo:team-api",
			RepositoryID:         "repo:team-api",
			PackageID:            fmt.Sprintf("npm:left-pad@%d", i),
			Status:               SecurityAlertReconciliationMatched,
		}
	}

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresSecurityAlertReconciliationWriter{DB: db}

	result, err := writer.WriteSecurityAlertReconciliations(context.Background(), SecurityAlertReconciliationWrite{
		IntentID:     "intent-security-alert-batch",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-batch",
		SourceSystem: "github_dependabot",
		Decisions:    decisions,
	})
	if err != nil {
		t.Fatalf("WriteSecurityAlertReconciliations() error = %v", err)
	}
	if got, want := result.FactsWritten, decisionCount; got != want {
		t.Fatalf("FactsWritten = %d, want %d", got, want)
	}

	wantExecs := expectedBatchedExecCount(decisionCount)
	if got := len(db.execs); got != wantExecs {
		t.Fatalf("ExecContext calls = %d for %d decisions, want %d (bounded batched inserts)", got, decisionCount, wantExecs)
	}
	if rows := decodeBatchedFactCalls(t, db.execs); len(rows) != decisionCount {
		t.Fatalf("decoded rows = %d, want %d", len(rows), decisionCount)
	}
}
