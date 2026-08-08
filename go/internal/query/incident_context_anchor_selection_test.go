// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// How the incident-context read chooses among the anchor rows that matched:
// which one answers, when the answer is not-found, and when it is ambiguous.
//
// Split out of incident_context_store_test.go when #4830's cases took that file
// past the 500-line cap. Anchor selection is a coherent enough subject to own a
// file — every test here exercises selectIncidentContextAnchor through the
// public read.

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"
)

// Two rows match the incident, but only one is a well-formed incident. The
// answer is that one incident, not a 409 offering a candidate the caller
// cannot use.
//
// The count-first ordering made this unreachable: the store counted raw rows,
// returned ambiguous at two, and only then decoded. That was harmless while the
// anchor decode could not fail, and stopped being harmless when #4794 made it
// typed. Ambiguity is a property of well-formed anchors — a row that decodes to
// nothing is not a rival answer (#4830).
func TestPostgresIncidentContextStoreReadsTheSoleWellFormedAnchor(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	db, _ := openIncidentContextStoreTestDB(t, []incidentContextStoreQueryResult{
		{
			match:   "fact.fact_kind = 'incident.record'",
			columns: incidentContextFactColumns(),
			rows: [][]driver.Value{
				{
					"incident-fact-unreadable",
					"pagerduty:account:stage",
					"generation-stage",
					"reported",
					"https://example.pagerduty.com/incidents/PABC123",
					"PABC123",
					observedAt,
					// A schema major this build does not support: the row is
					// stored, matches the query, and cannot be decoded.
					"9.0.0",
					[]byte(`{"provider":"pagerduty","status":"triggered","title":"unreadable"}`),
				},
				{
					"incident-fact-prod",
					"pagerduty:account:prod",
					"generation-prod",
					"reported",
					"https://example.pagerduty.com/incidents/PABC123",
					"PABC123",
					observedAt,
					"1.0.0",
					[]byte(`{"provider":"pagerduty","status":"triggered","title":"checkout-api elevated errors"}`),
				},
			},
			requireQueryContains: []string{"fact.source_record_id = $2"},
		},
		{
			match:   "fact.fact_kind = 'incident.lifecycle_event'",
			columns: incidentContextFactColumns(),
		},
	})

	store := NewPostgresIncidentContextStore(db)
	snapshot, err := store.ReadIncidentContext(context.Background(), IncidentContextFilter{
		Provider:           "pagerduty",
		ProviderIncidentID: "PABC123",
		Limit:              10,
	})
	if err != nil {
		t.Fatalf("ReadIncidentContext() error = %v, want nil", err)
	}
	// The readable row, not merely the first row: it is second in the result
	// set, so a fix that only reordered decode ahead of the count without
	// choosing among the survivors would still read the wrong one.
	if got, want := snapshot.Incident.EvidenceFactID, "incident-fact-prod"; got != want {
		t.Fatalf("EvidenceFactID = %q, want %q", got, want)
	}
}

// Every matching row fails to decode. There is no well-formed incident to
// answer for, which is indistinguishable from no match — the same answer the
// store already gives when a sole row fails to decode.
func TestPostgresIncidentContextStoreReportsNotFoundWhenNoAnchorDecodes(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	db, _ := openIncidentContextStoreTestDB(t, []incidentContextStoreQueryResult{
		{
			match:   "fact.fact_kind = 'incident.record'",
			columns: incidentContextFactColumns(),
			rows: [][]driver.Value{
				{
					"incident-fact-a", "pagerduty:account:a", "generation-a", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-b", "pagerduty:account:b", "generation-b", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
			},
			requireQueryContains: []string{"fact.source_record_id = $2"},
		},
	})

	store := NewPostgresIncidentContextStore(db)
	_, err := store.ReadIncidentContext(context.Background(), IncidentContextFilter{
		Provider:           "pagerduty",
		ProviderIncidentID: "PABC123",
		Limit:              10,
	})
	if !errors.Is(err, ErrIncidentContextNotFound) {
		t.Fatalf("ReadIncidentContext() error = %T %v, want ErrIncidentContextNotFound", err, err)
	}
}

// Filling the probe means there may be more rows behind it, so one surviving
// decode is not provably the only one. The store says ambiguous rather than
// handing back a single incident it cannot show is unique — the same
// fail-closed posture the malformed-row fix is built on, applied to the one
// case the fix cannot see past.
func TestPostgresIncidentContextStoreIsAmbiguousWhenTheAnchorProbeFills(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if incidentContextAnchorProbeLimit != 8 {
		t.Fatalf("probe limit = %d, but this fixture supplies 8 rows; keep them in step",
			incidentContextAnchorProbeLimit)
	}
	db, _ := openIncidentContextStoreTestDB(t, []incidentContextStoreQueryResult{
		{
			match:   "fact.fact_kind = 'incident.record'",
			columns: incidentContextFactColumns(),
			rows: [][]driver.Value{
				{
					"incident-fact-0", "pagerduty:account:0", "generation-0", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-1", "pagerduty:account:1", "generation-1", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-2", "pagerduty:account:2", "generation-2", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-3", "pagerduty:account:3", "generation-3", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"1.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-4", "pagerduty:account:4", "generation-4", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-5", "pagerduty:account:5", "generation-5", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-6", "pagerduty:account:6", "generation-6", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-7", "pagerduty:account:7", "generation-7", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
			},
			requireQueryContains: []string{"fact.source_record_id = $2"},
		},
	})

	store := NewPostgresIncidentContextStore(db)
	_, err := store.ReadIncidentContext(context.Background(), IncidentContextFilter{
		Provider:           "pagerduty",
		ProviderIncidentID: "PABC123",
		Limit:              10,
	})
	var ambiguous IncidentContextAmbiguousError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("ReadIncidentContext() error = %T %v, want IncidentContextAmbiguousError", err, err)
	}
	// Only the readable row is offered: a caller cannot disambiguate against a
	// candidate it cannot decode.
	if got, want := len(ambiguous.Candidates), 1; got != want {
		t.Fatalf("candidate count = %d, want %d", got, want)
	}
}

// A filled probe where NOTHING decodes is not-found, not ambiguous.
//
// The first version of this change collapsed the two: it asked whether fewer
// than two rows decoded, which is true at zero as well as at one. A caller then
// got 409 "matched multiple active provider scopes" with an empty candidate
// list and no way to pick a scope_id — a message that is simply false, since
// nothing readable matched. Three reviewers flagged it independently, and it
// contradicted the contract this same change wrote into
// docs/public/reference/pagerduty-evidence.md.
//
// One survivor in a filled probe stays ambiguous: that one IS unprovable,
// because another well-formed anchor may sit past the limit.
func TestPostgresIncidentContextStoreReportsNotFoundWhenAFullProbeDecodesNothing(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if incidentContextAnchorProbeLimit != 8 {
		t.Fatalf("probe limit = %d, but this fixture supplies 8 rows; keep them in step",
			incidentContextAnchorProbeLimit)
	}
	db, _ := openIncidentContextStoreTestDB(t, []incidentContextStoreQueryResult{
		{
			match:   "fact.fact_kind = 'incident.record'",
			columns: incidentContextFactColumns(),
			rows: [][]driver.Value{
				{
					"incident-fact-0", "pagerduty:account:0", "generation-0", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-1", "pagerduty:account:1", "generation-1", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-2", "pagerduty:account:2", "generation-2", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-3", "pagerduty:account:3", "generation-3", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-4", "pagerduty:account:4", "generation-4", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-5", "pagerduty:account:5", "generation-5", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-6", "pagerduty:account:6", "generation-6", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
				{
					"incident-fact-7", "pagerduty:account:7", "generation-7", "reported",
					"https://example.pagerduty.com/incidents/PABC123", "PABC123", observedAt,
					"9.0.0", []byte(`{"provider":"pagerduty","status":"triggered"}`),
				},
			},
			requireQueryContains: []string{"fact.source_record_id = $2"},
		},
	})

	store := NewPostgresIncidentContextStore(db)
	_, err := store.ReadIncidentContext(context.Background(), IncidentContextFilter{
		Provider:           "pagerduty",
		ProviderIncidentID: "PABC123",
		Limit:              10,
	})
	if !errors.Is(err, ErrIncidentContextNotFound) {
		t.Fatalf("ReadIncidentContext() error = %T %v, want ErrIncidentContextNotFound", err, err)
	}
}
