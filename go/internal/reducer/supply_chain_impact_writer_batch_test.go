// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestPostgresSupplyChainImpactWriterPersistsBatchedFacts proves
// WriteSupplyChainImpactFindings upserts findings through the shared
// reducerBatchInsertVersionedFacts bulk-insert path (issue #5317) rather than
// one ExecContext per finding, and that the decoded rows carry byte-identical
// content — including the governed schema_version — to what the retired
// per-row canonicalVersionedReducerFactInsertQuery loop produced: the
// row-building helpers (supplyChainImpactFactID/StableFactKey/
// TypedPayload) are unchanged, only the ExecContext call site moved.
func TestPostgresSupplyChainImpactWriterPersistsBatchedFacts(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresSupplyChainImpactWriter{DB: db}

	write := SupplyChainImpactWrite{
		IntentID:     "intent-impact-batch",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-impact-batch",
		SourceSystem: "vulnerability_intelligence",
		Cause:        "vulnerability evidence observed",
		Findings: []SupplyChainImpactFinding{
			{
				CVEID:           "CVE-2026-1001",
				PackageID:       testImpactPackageID,
				PURL:            testImpactPURL,
				ObservedVersion: "1.2.3",
				Status:          SupplyChainImpactAffectedExact,
				RepositoryID:    testImpactRepositoryID,
				CanonicalWrites: 1,
			},
			{
				CVEID:           "CVE-2026-1002",
				PackageID:       testImpactPackageID,
				PURL:            testImpactPURL,
				ObservedVersion: "1.2.4",
				Status:          SupplyChainImpactAffectedExact,
				RepositoryID:    testImpactRepositoryID,
				CanonicalWrites: 1,
			},
		},
	}

	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v, want nil", err)
	}
	if got, want := result.FactsWritten, 2; got != want {
		t.Fatalf("FactsWritten = %d, want %d", got, want)
	}
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	// One ExecContext call for two findings: proves the batched path replaced
	// the retired per-finding loop.
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("ExecContext calls = %d, want %d (batched insert)", got, want)
	}
	rows := decodeBatchedVersionedFactCalls(t, db.execs)
	if got, want := len(rows), 2; got != want {
		t.Fatalf("decoded rows = %d, want %d", got, want)
	}

	for i, finding := range write.Findings {
		row := rows[i]
		if got, want := row.FactID, supplyChainImpactFactID(write, finding); got != want {
			t.Fatalf("row %d FactID = %q, want %q (byte-identical to per-row loop)", i, got, want)
		}
		if got, want := row.StableFactKey, supplyChainImpactStableFactKey(write, finding); got != want {
			t.Fatalf("row %d StableFactKey = %q, want %q", i, got, want)
		}
		if got, want := row.FactKind, supplyChainImpactFactKind; got != want {
			t.Fatalf("row %d FactKind = %q, want %q", i, got, want)
		}
		if got, want := row.SchemaVersion, facts.ReducerDerivedSchemaVersionV1; got != want {
			t.Fatalf("row %d SchemaVersion = %q, want %q (governed reducer-derived fact must keep its schema version)", i, got, want)
		}
		wantPayload, err := json.Marshal(supplyChainImpactPayload(write, finding))
		if err != nil {
			t.Fatalf("marshal expected payload: %v", err)
		}
		if got, want := string(row.Payload), string(wantPayload); got != want {
			t.Fatalf("row %d Payload = %s, want %s (byte-identical to per-row loop)", i, got, want)
		}
	}
}

// TestWriteSupplyChainImpactFindingsBoundedExecCount guards issue #5317: N
// findings must be persisted in O(N/batchSize) bulk inserts rather than one
// ExecContext per finding.
func TestWriteSupplyChainImpactFindingsBoundedExecCount(t *testing.T) {
	t.Parallel()

	const findingCount = 1500
	findings := make([]SupplyChainImpactFinding, findingCount)
	for i := range findings {
		findings[i] = SupplyChainImpactFinding{
			CVEID:           fmt.Sprintf("CVE-2026-%d", i),
			PackageID:       testImpactPackageID,
			PURL:            testImpactPURL,
			ObservedVersion: "1.2.3",
			Status:          SupplyChainImpactAffectedExact,
			RepositoryID:    testImpactRepositoryID,
			CanonicalWrites: 1,
		}
	}

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresSupplyChainImpactWriter{DB: db}

	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), SupplyChainImpactWrite{
		IntentID:     "intent-impact-batch",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-batch",
		SourceSystem: "vulnerability_intelligence",
		Findings:     findings,
	})
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if got, want := result.FactsWritten, findingCount; got != want {
		t.Fatalf("FactsWritten = %d, want %d", got, want)
	}

	wantExecs := expectedBatchedExecCount(findingCount)
	if got := len(db.execs); got != wantExecs {
		t.Fatalf("ExecContext calls = %d for %d findings, want %d (bounded batched inserts)", got, findingCount, wantExecs)
	}
	if rows := decodeBatchedVersionedFactCalls(t, db.execs); len(rows) != findingCount {
		t.Fatalf("decoded rows = %d, want %d", len(rows), findingCount)
	}
}
