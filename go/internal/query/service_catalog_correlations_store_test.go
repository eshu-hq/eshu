// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"slices"
	"strings"
	"testing"
)

func TestPostgresServiceCatalogCorrelationsResolveCandidateRepositoryIDs(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"fact_id", "payload"},
			rows: [][]driver.Value{
				{
					"catalog-correlation-ambiguous",
					[]byte(`{
						"entity_ref": "component:default/payments-shared",
						"outcome": "ambiguous",
						"provenance_only": true,
						"candidate_repository_ids": ["repository:r_payments", "repository:r_payments_fork"]
					}`),
				},
			},
		},
	})
	store := NewPostgresServiceCatalogCorrelationStore(db)

	rows, err := store.ListServiceCatalogCorrelations(context.Background(), ServiceCatalogCorrelationFilter{
		RepositoryID: "repository:r_payments",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("ListServiceCatalogCorrelations() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	wantCandidates := []string{"repository:r_payments", "repository:r_payments_fork"}
	if got := rows[0].CandidateRepositoryIDs; !slices.Equal(got, wantCandidates) {
		t.Fatalf("CandidateRepositoryIDs = %#v, want %#v", got, wantCandidates)
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("len(queries) = %d, want %d", got, want)
	}
	if !strings.Contains(recorder.queries[0], "fact.payload->'candidate_repository_ids' ? $5") {
		t.Fatalf("query missing candidate repository predicate:\n%s", recorder.queries[0])
	}
}

func TestServiceCatalogCorrelationQueryUsesActiveFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->>'entity_ref' = $4",
		"fact.payload->>'repository_id' = $5",
		"fact.payload->'candidate_repository_ids' ? $5",
		"fact.payload->>'owner_ref' = $8",
		"fact.payload->>'outcome' = $9",
	} {
		if !strings.Contains(listServiceCatalogCorrelationsQuery, want) {
			t.Fatalf("listServiceCatalogCorrelationsQuery missing %q:\n%s", want, listServiceCatalogCorrelationsQuery)
		}
	}
}

func TestServiceCatalogLocalDescriptorEvidenceQueryUsesActiveRepositoryScope(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.scope_id = $1",
		"fact.fact_kind = ANY($2::text[])",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"ORDER BY COALESCE(fact.source_uri, ''), fact.fact_kind, fact.fact_id",
	} {
		if !strings.Contains(listServiceCatalogLocalDescriptorEvidenceQuery, want) {
			t.Fatalf("listServiceCatalogLocalDescriptorEvidenceQuery missing %q:\n%s", want, listServiceCatalogLocalDescriptorEvidenceQuery)
		}
	}
}
