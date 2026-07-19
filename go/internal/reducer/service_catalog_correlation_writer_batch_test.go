// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// TestPostgresServiceCatalogCorrelationWriterPersistsBatchedFacts proves
// WriteServiceCatalogCorrelations upserts decisions through the shared
// reducerBatchInsertFacts bulk-insert path (issue #5317) rather than one
// ExecContext per decision, and that the decoded rows carry byte-identical
// content to what the retired per-row canonicalReducerFactInsertQuery loop
// produced: the row-building helpers (serviceCatalogCorrelationFactID/
// StableFactKey/Payload) are unchanged, only the ExecContext call site moved.
func TestPostgresServiceCatalogCorrelationWriterPersistsBatchedFacts(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresServiceCatalogCorrelationWriter{DB: db}

	write := ServiceCatalogCorrelationWrite{
		IntentID:     "intent-service-catalog-batch",
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		GenerationID: "generation-service-catalog-batch",
		SourceSystem: "service_catalog",
		Cause:        "service catalog facts observed",
		Decisions: []ServiceCatalogCorrelationDecision{
			{
				Provider:     "backstage",
				EntityRef:    "component:default/checkout",
				EntityType:   "component",
				DisplayName:  "Checkout",
				RepositoryID: "repo-checkout",
				Outcome:      ServiceCatalogCorrelationExact,
			},
			{
				Provider:     "backstage",
				EntityRef:    "component:default/billing",
				EntityType:   "component",
				DisplayName:  "Billing",
				RepositoryID: "repo-billing",
				Outcome:      ServiceCatalogCorrelationExact,
			},
		},
	}

	result, err := writer.WriteServiceCatalogCorrelations(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteServiceCatalogCorrelations() error = %v, want nil", err)
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
		if got, want := row.FactID, serviceCatalogCorrelationFactID(write, decision); got != want {
			t.Fatalf("row %d FactID = %q, want %q (byte-identical to per-row loop)", i, got, want)
		}
		if got, want := row.StableFactKey, serviceCatalogCorrelationStableFactKey(write, decision); got != want {
			t.Fatalf("row %d StableFactKey = %q, want %q", i, got, want)
		}
		if got, want := row.FactKind, serviceCatalogCorrelationFactKind; got != want {
			t.Fatalf("row %d FactKind = %q, want %q", i, got, want)
		}
		wantPayload, err := json.Marshal(serviceCatalogCorrelationPayload(write, decision))
		if err != nil {
			t.Fatalf("marshal expected payload: %v", err)
		}
		if got, want := string(row.Payload), string(wantPayload); got != want {
			t.Fatalf("row %d Payload = %s, want %s (byte-identical to per-row loop)", i, got, want)
		}
	}
}

// TestWriteServiceCatalogCorrelationsBoundedExecCount guards issue #5317: N
// decisions must be persisted in O(N/batchSize) bulk inserts rather than one
// ExecContext per decision.
func TestWriteServiceCatalogCorrelationsBoundedExecCount(t *testing.T) {
	t.Parallel()

	const decisionCount = 1500
	decisions := make([]ServiceCatalogCorrelationDecision, decisionCount)
	for i := range decisions {
		decisions[i] = ServiceCatalogCorrelationDecision{
			Provider:     "backstage",
			EntityRef:    fmt.Sprintf("component:default/svc-%d", i),
			RepositoryID: fmt.Sprintf("repo-%d", i),
			Outcome:      ServiceCatalogCorrelationExact,
		}
	}

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresServiceCatalogCorrelationWriter{DB: db}

	result, err := writer.WriteServiceCatalogCorrelations(context.Background(), ServiceCatalogCorrelationWrite{
		IntentID:     "intent-service-catalog-batch",
		ScopeID:      "scope-batch",
		GenerationID: "generation-batch",
		SourceSystem: "backstage",
		Decisions:    decisions,
	})
	if err != nil {
		t.Fatalf("WriteServiceCatalogCorrelations() error = %v", err)
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
