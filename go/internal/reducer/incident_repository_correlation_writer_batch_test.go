// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// TestPostgresIncidentRepositoryCorrelationWriterPersistsBatchedFacts proves
// WriteIncidentRepositoryCorrelations upserts decisions through the shared
// reducerBatchInsertFacts bulk-insert path (issue #5317) rather than one
// ExecContext per decision, and that the decoded rows carry byte-identical
// content to what the retired per-row canonicalReducerFactInsertQuery loop
// produced: the row-building helpers (incidentRepositoryCorrelationFactID/
// StableFactKey/Payload) are unchanged, only the ExecContext call site moved.
func TestPostgresIncidentRepositoryCorrelationWriterPersistsBatchedFacts(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresIncidentRepositoryCorrelationWriter{DB: db}

	write := IncidentRepositoryCorrelationWrite{
		IntentID:     "intent-incident-repo-batch",
		ScopeID:      "state_snapshot:s3:team-api",
		GenerationID: "generation-incident-repo-batch",
		SourceSystem: "pagerduty",
		Cause:        "applied PagerDuty routing observed",
		Decisions: []IncidentRepositoryCorrelationDecision{
			{
				Provider:          "pagerduty",
				ProviderServiceID: "service-a",
				BackendKind:       "s3",
				LocatorHash:       "locator-a",
				RepositoryID:      "repo:team-api-a",
				Outcome:           IncidentRepositoryCorrelationExact,
			},
			{
				Provider:          "pagerduty",
				ProviderServiceID: "service-b",
				BackendKind:       "s3",
				LocatorHash:       "locator-b",
				RepositoryID:      "repo:team-api-b",
				Outcome:           IncidentRepositoryCorrelationExact,
			},
		},
	}

	result, err := writer.WriteIncidentRepositoryCorrelations(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteIncidentRepositoryCorrelations() error = %v, want nil", err)
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
		if got, want := row.FactID, incidentRepositoryCorrelationFactID(write, decision); got != want {
			t.Fatalf("row %d FactID = %q, want %q (byte-identical to per-row loop)", i, got, want)
		}
		if got, want := row.StableFactKey, incidentRepositoryCorrelationStableFactKey(write, decision); got != want {
			t.Fatalf("row %d StableFactKey = %q, want %q", i, got, want)
		}
		if got, want := row.FactKind, incidentRepositoryCorrelationFactKind; got != want {
			t.Fatalf("row %d FactKind = %q, want %q", i, got, want)
		}
		wantPayload, err := json.Marshal(incidentRepositoryCorrelationPayload(write, decision))
		if err != nil {
			t.Fatalf("marshal expected payload: %v", err)
		}
		if got, want := string(row.Payload), string(wantPayload); got != want {
			t.Fatalf("row %d Payload = %s, want %s (byte-identical to per-row loop)", i, got, want)
		}
	}
}

// TestWriteIncidentRepositoryCorrelationsBoundedExecCount guards issue #5317:
// N decisions must be persisted in O(N/batchSize) bulk inserts rather than
// one ExecContext per decision.
func TestWriteIncidentRepositoryCorrelationsBoundedExecCount(t *testing.T) {
	t.Parallel()

	const decisionCount = 1500
	decisions := make([]IncidentRepositoryCorrelationDecision, decisionCount)
	for i := range decisions {
		decisions[i] = IncidentRepositoryCorrelationDecision{
			Provider:          "pagerduty",
			ProviderServiceID: fmt.Sprintf("service-%d", i),
			BackendKind:       "s3",
			LocatorHash:       fmt.Sprintf("locator-%d", i),
			RepositoryID:      fmt.Sprintf("repo:team-api-%d", i),
			Outcome:           IncidentRepositoryCorrelationExact,
		}
	}

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresIncidentRepositoryCorrelationWriter{DB: db}

	result, err := writer.WriteIncidentRepositoryCorrelations(context.Background(), IncidentRepositoryCorrelationWrite{
		IntentID:     "intent-incident-repo-batch",
		ScopeID:      "state_snapshot:s3:team-api",
		GenerationID: "generation-batch",
		SourceSystem: "pagerduty",
		Decisions:    decisions,
	})
	if err != nil {
		t.Fatalf("WriteIncidentRepositoryCorrelations() error = %v", err)
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
