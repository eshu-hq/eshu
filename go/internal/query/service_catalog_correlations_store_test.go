// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"errors"
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

// TestServiceCatalogCorrelationsOutsideGrantQueryInvertsOnlyTheGrantClause is
// the lockstep pin for the two shipped statements. The outside-grant read
// exists to answer one question -- does anything OUTSIDE the caller's grant
// also correlate this service id -- and it must answer it over exactly the
// same rows the ordinary read considers. Anything else (a dropped tombstone
// arm, a missing active-generation join, a different selector) would make the
// exclusivity check answer about a different population than the admission
// check, which is how a fail-open slips back in.
func TestServiceCatalogCorrelationsOutsideGrantQueryInvertsOnlyTheGrantClause(t *testing.T) {
	t.Parallel()

	const grantClause = `  AND (
    (COALESCE(cardinality($13::text[]), 0) = 0 AND COALESCE(cardinality($14::text[]), 0) = 0)
    OR fact.payload->>'repository_id' = ANY($13::text[])
    OR fact.payload->'candidate_repository_ids' ?| $13::text[]
    OR fact.scope_id = ANY($14::text[])
  )`
	const inverseGrantClause = `  AND NOT (
    COALESCE(fact.payload->>'repository_id' = ANY($13::text[]), FALSE)
    OR COALESCE(fact.payload->'candidate_repository_ids' ?| $13::text[], FALSE)
    OR fact.scope_id = ANY($14::text[])
  )`

	if !strings.Contains(listServiceCatalogCorrelationsQuery, grantClause) {
		t.Fatalf("listServiceCatalogCorrelationsQuery no longer carries the grant clause:\n%s", listServiceCatalogCorrelationsQuery)
	}
	if !strings.Contains(listServiceCatalogCorrelationsOutsideGrantQuery, inverseGrantClause) {
		t.Fatalf("listServiceCatalogCorrelationsOutsideGrantQuery no longer carries the inverted grant clause:\n%s",
			listServiceCatalogCorrelationsOutsideGrantQuery)
	}
	// The inverted clause drops the empty-arrays arm on purpose: an empty
	// grant makes the ordinary clause permissive, and its negation would
	// refuse every service. ListServiceCatalogCorrelations rejects that filter
	// before it reaches SQL instead.
	restored := strings.Replace(listServiceCatalogCorrelationsOutsideGrantQuery, inverseGrantClause, grantClause, 1)
	if restored != listServiceCatalogCorrelationsQuery {
		t.Fatalf("the two statements differ outside the grant clause:\n--- ordinary ---\n%s\n--- outside-grant, grant clause restored ---\n%s",
			listServiceCatalogCorrelationsQuery, restored)
	}
}

// TestPostgresServiceCatalogCorrelationsSelectTheStatementByOutsideGrant pins
// which statement each filter shape sends. The ordinary read must stay
// byte-identical to what it sent before the outside-grant probe existed: every
// other caller of this store shares its plan cache entry.
func TestPostgresServiceCatalogCorrelationsSelectTheStatementByOutsideGrant(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		outsideGrant bool
		wantQuery    string
	}{
		{name: "ordinary read", wantQuery: listServiceCatalogCorrelationsQuery},
		{name: "outside-grant read", outsideGrant: true, wantQuery: listServiceCatalogCorrelationsOutsideGrantQuery},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
				{columns: []string{"fact_id", "payload"}},
			})
			store := NewPostgresServiceCatalogCorrelationStore(db)

			if _, err := store.ListServiceCatalogCorrelations(context.Background(), ServiceCatalogCorrelationFilter{
				ServiceID:            "component:default/api",
				AllowedRepositoryIDs: []string{"repository:r_alpha"},
				OutsideGrant:         tc.outsideGrant,
				Limit:                1,
			}); err != nil {
				t.Fatalf("ListServiceCatalogCorrelations() error = %v, want nil", err)
			}
			if got, want := len(recorder.queries), 1; got != want {
				t.Fatalf("len(queries) = %d, want %d", got, want)
			}
			if recorder.queries[0] != tc.wantQuery {
				t.Fatalf("statement sent:\n%s\nwant:\n%s", recorder.queries[0], tc.wantQuery)
			}
		})
	}
}

// TestPostgresServiceCatalogCorrelationsRejectAnOutsideGrantReadWithNoGrant
// pins the fail-loud guard. Negating the grant clause over two empty arrays
// matches every row, so a caller that lost its grant on the way here would
// refuse every service and read as ordinary tenant isolation. The store
// refuses to run instead, and issues no statement at all.
func TestPostgresServiceCatalogCorrelationsRejectAnOutsideGrantReadWithNoGrant(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, nil)
	store := NewPostgresServiceCatalogCorrelationStore(db)

	_, err := store.ListServiceCatalogCorrelations(context.Background(), ServiceCatalogCorrelationFilter{
		ServiceID:    "component:default/api",
		OutsideGrant: true,
		Limit:        1,
	})
	if !errors.Is(err, errServiceCatalogOutsideGrantNeedsAGrant) {
		t.Fatalf("ListServiceCatalogCorrelations() error = %v, want %v", err, errServiceCatalogOutsideGrantNeedsAGrant)
	}
	if got, want := len(recorder.queries), 0; got != want {
		t.Fatalf("len(queries) = %d, want %d; the guard must refuse before any statement is sent", got, want)
	}
}
