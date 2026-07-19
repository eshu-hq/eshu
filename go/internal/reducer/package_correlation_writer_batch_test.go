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

// TestPostgresPackageCorrelationWriterPersistsBatchedFacts proves
// WritePackageCorrelations combines ownership, consumption, and publication
// decisions into ONE reducerBatchInsertVersionedFacts bulk-insert call (issue
// #5317) instead of one ExecContext per decision spread across three
// separate loops, and that the decoded rows carry byte-identical content —
// including the governed schema_version — to what the retired per-row
// canonicalVersionedReducerFactInsertQuery loops produced, in the SAME order
// (ownership, then consumption, then publication) the original three loops
// wrote them: the row-building helpers (packageOwnership/Consumption/
// PublicationFactID/StableFactKey/Payload) are unchanged, only the
// ExecContext call site moved and consolidated.
func TestPostgresPackageCorrelationWriterPersistsBatchedFacts(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresPackageCorrelationWriter{DB: db}

	write := PackageCorrelationWrite{
		IntentID:     "intent-package-batch",
		ScopeID:      "scope-package-batch",
		GenerationID: "generation-package-batch",
		SourceSystem: "package_registry",
		Cause:        "package source hints observed",
		OwnershipDecisions: []PackageSourceCorrelationDecision{
			{
				PackageID: "pkg:npm://registry.example/team-api",
				SourceURL: "https://github.com/acme/team-api",
				Outcome:   PackageSourceCorrelationExact,
			},
		},
		ConsumptionDecisions: []PackageConsumptionDecision{
			{
				PackageID:    "pkg:npm://registry.example/team-api",
				RepositoryID: "repo-web",
				RelativePath: "package.json",
				Outcome:      PackageConsumptionManifestDeclared,
			},
		},
		PublicationDecisions: []PackagePublicationDecision{
			{
				PackageID:    "pkg:npm://registry.example/team-api",
				VersionID:    "pkg:npm://registry.example/team-api@1.2.0",
				SourceURL:    "https://github.com/acme/team-api",
				RepositoryID: "repo-team-api",
				Outcome:      PackageSourceCorrelationExact,
			},
		},
	}

	result, err := writer.WritePackageCorrelations(context.Background(), write)
	if err != nil {
		t.Fatalf("WritePackageCorrelations() error = %v, want nil", err)
	}
	if got, want := result.FactsWritten, 3; got != want {
		t.Fatalf("FactsWritten = %d, want %d", got, want)
	}

	// One ExecContext call for three decisions across ownership, consumption,
	// and publication: proves the batched path replaced the retired
	// three-loop per-decision dispatch.
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("ExecContext calls = %d, want %d (batched insert)", got, want)
	}
	rows := decodeBatchedVersionedFactCalls(t, db.execs)
	if got, want := len(rows), 3; got != want {
		t.Fatalf("decoded rows = %d, want %d", got, want)
	}

	wantOwnershipPayload, err := json.Marshal(packageOwnershipPayload(write, write.OwnershipDecisions[0]))
	if err != nil {
		t.Fatalf("marshal expected ownership payload: %v", err)
	}
	wantConsumptionPayload, err := json.Marshal(packageConsumptionPayload(write, write.ConsumptionDecisions[0]))
	if err != nil {
		t.Fatalf("marshal expected consumption payload: %v", err)
	}
	wantPublicationPayload, err := json.Marshal(packagePublicationPayload(write, write.PublicationDecisions[0]))
	if err != nil {
		t.Fatalf("marshal expected publication payload: %v", err)
	}

	ownershipRow, consumptionRow, publicationRow := rows[0], rows[1], rows[2]

	if got, want := ownershipRow.FactID, packageOwnershipFactID(write, write.OwnershipDecisions[0]); got != want {
		t.Fatalf("ownership row FactID = %q, want %q (byte-identical to per-row loop)", got, want)
	}
	if got, want := ownershipRow.StableFactKey, packageOwnershipStableFactKey(write, write.OwnershipDecisions[0]); got != want {
		t.Fatalf("ownership row StableFactKey = %q, want %q", got, want)
	}
	if got, want := ownershipRow.FactKind, packageOwnershipCorrelationFactKind; got != want {
		t.Fatalf("ownership row FactKind = %q, want %q", got, want)
	}
	if got, want := string(ownershipRow.Payload), string(wantOwnershipPayload); got != want {
		t.Fatalf("ownership row Payload = %s, want %s (byte-identical to per-row loop)", got, want)
	}

	if got, want := consumptionRow.FactID, packageConsumptionFactID(write, write.ConsumptionDecisions[0]); got != want {
		t.Fatalf("consumption row FactID = %q, want %q (byte-identical to per-row loop)", got, want)
	}
	if got, want := consumptionRow.StableFactKey, packageConsumptionStableFactKey(write, write.ConsumptionDecisions[0]); got != want {
		t.Fatalf("consumption row StableFactKey = %q, want %q", got, want)
	}
	if got, want := consumptionRow.FactKind, packageConsumptionCorrelationFactKind; got != want {
		t.Fatalf("consumption row FactKind = %q, want %q", got, want)
	}
	if got, want := string(consumptionRow.Payload), string(wantConsumptionPayload); got != want {
		t.Fatalf("consumption row Payload = %s, want %s (byte-identical to per-row loop)", got, want)
	}

	if got, want := publicationRow.FactID, packagePublicationFactID(write, write.PublicationDecisions[0]); got != want {
		t.Fatalf("publication row FactID = %q, want %q (byte-identical to per-row loop)", got, want)
	}
	if got, want := publicationRow.StableFactKey, packagePublicationStableFactKey(write, write.PublicationDecisions[0]); got != want {
		t.Fatalf("publication row StableFactKey = %q, want %q", got, want)
	}
	if got, want := publicationRow.FactKind, packagePublicationCorrelationFactKind; got != want {
		t.Fatalf("publication row FactKind = %q, want %q", got, want)
	}
	if got, want := string(publicationRow.Payload), string(wantPublicationPayload); got != want {
		t.Fatalf("publication row Payload = %s, want %s (byte-identical to per-row loop)", got, want)
	}

	for i, row := range rows {
		if got, want := row.SchemaVersion, facts.ReducerDerivedSchemaVersionV1; got != want {
			t.Fatalf("row %d SchemaVersion = %q, want %q (governed reducer-derived fact must keep its schema version)", i, got, want)
		}
		if got, want := row.ScopeID, write.ScopeID; got != want {
			t.Fatalf("row %d ScopeID = %q, want %q", i, got, want)
		}
		if got, want := row.GenerationID, write.GenerationID; got != want {
			t.Fatalf("row %d GenerationID = %q, want %q", i, got, want)
		}
	}
}

// TestWritePackageCorrelationsBoundedExecCount guards issue #5317: N
// decisions across the three decision lists combined must be persisted in
// O(N/batchSize) bulk inserts rather than one ExecContext per decision.
func TestWritePackageCorrelationsBoundedExecCount(t *testing.T) {
	t.Parallel()

	const perListCount = 500
	ownership := make([]PackageSourceCorrelationDecision, perListCount)
	consumption := make([]PackageConsumptionDecision, perListCount)
	publication := make([]PackagePublicationDecision, perListCount)
	for i := 0; i < perListCount; i++ {
		ownership[i] = PackageSourceCorrelationDecision{
			PackageID: fmt.Sprintf("pkg:npm://registry.example/svc-%d", i),
			SourceURL: fmt.Sprintf("https://github.com/acme/svc-%d", i),
			Outcome:   PackageSourceCorrelationExact,
		}
		consumption[i] = PackageConsumptionDecision{
			PackageID:    fmt.Sprintf("pkg:npm://registry.example/svc-%d", i),
			RepositoryID: fmt.Sprintf("repo-%d", i),
			RelativePath: "package.json",
			Outcome:      PackageConsumptionManifestDeclared,
		}
		publication[i] = PackagePublicationDecision{
			PackageID:    fmt.Sprintf("pkg:npm://registry.example/svc-%d", i),
			VersionID:    fmt.Sprintf("pkg:npm://registry.example/svc-%d@1.0.0", i),
			SourceURL:    fmt.Sprintf("https://github.com/acme/svc-%d", i),
			RepositoryID: fmt.Sprintf("repo-%d", i),
			Outcome:      PackageSourceCorrelationExact,
		}
	}

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresPackageCorrelationWriter{DB: db}

	result, err := writer.WritePackageCorrelations(context.Background(), PackageCorrelationWrite{
		IntentID:             "intent-package-batch",
		ScopeID:              "scope-package-batch",
		GenerationID:         "generation-batch",
		SourceSystem:         "package_registry",
		OwnershipDecisions:   ownership,
		ConsumptionDecisions: consumption,
		PublicationDecisions: publication,
	})
	if err != nil {
		t.Fatalf("WritePackageCorrelations() error = %v", err)
	}
	const totalDecisions = perListCount * 3
	if got, want := result.FactsWritten, totalDecisions; got != want {
		t.Fatalf("FactsWritten = %d, want %d", got, want)
	}

	wantExecs := expectedBatchedExecCount(totalDecisions)
	if got := len(db.execs); got != wantExecs {
		t.Fatalf("ExecContext calls = %d for %d decisions, want %d (bounded batched inserts)", got, totalDecisions, wantExecs)
	}
	if rows := decodeBatchedVersionedFactCalls(t, db.execs); len(rows) != totalDecisions {
		t.Fatalf("decoded rows = %d, want %d", len(rows), totalDecisions)
	}
}
