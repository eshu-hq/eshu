// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"
)

// TestReadIncidentContextTimelineTruncatedWhenVisibleRowDrops is the
// incident-context sibling of the #4733 work-item pagination bug. The typed-decode
// conversion added a decode-drop path to readIncidentTimeline; combined with the
// handler deriving Truncated from len(decoded), a timeline row that fails typed
// decode INSIDE the requested-limit window shrinks the decoded count so a
// genuinely truncated page reports Truncated=false and the lookahead row becomes
// undiscoverable. Truncated must instead come from the RAW fetched-row count.
//
// The handler passes filter.Limit = requestedLimit+1 (the lookahead probe). Here
// requestedLimit=2, so filter.Limit=3. The lifecycle_event fetch returns 3 rows;
// evt-2 (inside the visible window of 2) is malformed and drops. Truncated must
// be true (3 fetched > 2 requested), and the visible window must exclude the
// lookahead evt-3.
func TestReadIncidentContextTimelineTruncatedWhenVisibleRowDrops(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	db, _ := openIncidentContextStoreTestDB(t, []incidentContextStoreQueryResult{
		{
			match:   "fact.fact_kind = 'incident.record'",
			columns: incidentContextFactColumns(),
			rows: [][]driver.Value{{
				"incident-fact", "pagerduty:acct:prod", "gen-1", "reported",
				"https://pd/incidents/PABC", "PABC", observedAt, "1.0.0",
				[]byte(`{"provider":"pagerduty","provider_incident_id":"PABC"}`),
			}},
		},
		{
			match:   "fact.fact_kind = 'incident.lifecycle_event'",
			columns: incidentContextFactColumns(),
			rows: [][]driver.Value{
				{
					"evt-1", "pagerduty:acct:prod", "gen-1", "reported", "", "erec-1", observedAt, "1.0.0",
					[]byte(`{"provider":"pagerduty","provider_event_id":"E1","provider_incident_id":"PABC","created_at":"2026-06-01T10:00:00Z"}`),
				},
				// MALFORMED: no provider_event_id and no source_record_id fallback -> drops.
				{
					"evt-2", "pagerduty:acct:prod", "gen-1", "reported", "", "", observedAt, "1.0.0",
					[]byte(`{"provider":"pagerduty","provider_incident_id":"PABC","created_at":"2026-06-01T11:00:00Z"}`),
				},
				// Lookahead (3rd fetched) is valid; it must NOT appear in the visible window.
				{
					"evt-3", "pagerduty:acct:prod", "gen-1", "reported", "", "erec-3", observedAt, "1.0.0",
					[]byte(`{"provider":"pagerduty","provider_event_id":"E3","provider_incident_id":"PABC","created_at":"2026-06-01T12:00:00Z"}`),
				},
			},
		},
	})

	store := NewPostgresIncidentContextStore(db)
	snapshot, err := store.ReadIncidentContext(context.Background(), IncidentContextFilter{
		Provider:           "pagerduty",
		ProviderIncidentID: "PABC",
		Limit:              3, // requested limit 2 + 1 lookahead
	})
	if err != nil {
		t.Fatalf("ReadIncidentContext() error = %v, want nil", err)
	}
	if !snapshot.Truncated {
		t.Fatal("Truncated = false, want true: a malformed timeline row inside the window must not hide the lookahead row")
	}
	// Visible window is the first 2 fetched rows (evt-1, evt-2); evt-2 drops, so
	// exactly evt-1 survives, and the lookahead evt-3 is excluded.
	if got := len(snapshot.Timeline); got != 1 {
		t.Fatalf("len(Timeline) = %d, want 1 (evt-1 only; evt-2 dropped, evt-3 is the lookahead): %+v", got, snapshot.Timeline)
	}
	for _, ev := range snapshot.Timeline {
		if ev.EventID == "E3" {
			t.Fatalf("lookahead event E3 leaked into the visible window: %+v", ev)
		}
	}
}

// TestReadIncidentContextRelatedChangesTruncatedWhenVisibleRowDrops is the same
// #4733-shape regression for readIncidentChangeCandidates. The incident carries a
// service id so the related-changes read fires; the routing reads are staged
// empty. The change.record fetch returns 3 rows with the 2nd malformed inside the
// visible window of 2; Truncated must be true and the lookahead excluded.
func TestReadIncidentContextRelatedChangesTruncatedWhenVisibleRowDrops(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	db, _ := openIncidentContextStoreTestDB(t, []incidentContextStoreQueryResult{
		{
			match:   "fact.fact_kind = 'incident.record'",
			columns: incidentContextFactColumns(),
			rows: [][]driver.Value{{
				"incident-fact", "pagerduty:acct:prod", "gen-1", "reported",
				"https://pd/incidents/PABC", "PABC", observedAt, "1.0.0",
				[]byte(`{"provider":"pagerduty","provider_incident_id":"PABC","service":{"id":"PSVC1"}}`),
			}},
		},
		{
			// Timeline: empty, so it does not contribute to truncation.
			match:   "fact.fact_kind = 'incident.lifecycle_event'",
			columns: incidentContextFactColumns(),
		},
		{
			match:   "fact.fact_kind = 'change.record'",
			columns: incidentContextFactColumns(),
			rows: [][]driver.Value{
				{
					"chg-1", "pagerduty:acct:prod", "gen-1", "reported", "", "crec-1", observedAt, "1.0.0",
					[]byte(`{"provider":"pagerduty","provider_change_id":"C1","timestamp":"2026-06-01T10:00:00Z"}`),
				},
				// MALFORMED: no provider_change_id and no source_record_id fallback -> drops.
				{
					"chg-2", "pagerduty:acct:prod", "gen-1", "reported", "", "", observedAt, "1.0.0",
					[]byte(`{"provider":"pagerduty","timestamp":"2026-06-01T11:00:00Z"}`),
				},
				// Lookahead (3rd fetched) is valid; it must NOT appear in the visible window.
				{
					"chg-3", "pagerduty:acct:prod", "gen-1", "reported", "", "crec-3", observedAt, "1.0.0",
					[]byte(`{"provider":"pagerduty","provider_change_id":"C3","timestamp":"2026-06-01T12:00:00Z"}`),
				},
			},
		},
		{match: "fact.fact_kind = 'incident_routing.applied_pagerduty_resource'", columns: incidentContextFactColumns()},
		{match: "fact.fact_kind = 'incident_routing.observed_pagerduty_service'", columns: incidentContextFactColumns()},
		{match: "fact.fact_kind = 'incident_routing.coverage_warning'", columns: incidentContextFactColumns()},
	})

	store := NewPostgresIncidentContextStore(db)
	snapshot, err := store.ReadIncidentContext(context.Background(), IncidentContextFilter{
		Provider:           "pagerduty",
		ProviderIncidentID: "PABC",
		Limit:              3, // requested limit 2 + 1 lookahead
	})
	if err != nil {
		t.Fatalf("ReadIncidentContext() error = %v, want nil", err)
	}
	if !snapshot.Truncated {
		t.Fatal("Truncated = false, want true: a malformed related-change row inside the window must not hide the lookahead row")
	}
	if got := len(snapshot.RelatedChanges); got != 1 {
		t.Fatalf("len(RelatedChanges) = %d, want 1 (C1 only; chg-2 dropped, chg-3 is the lookahead): %+v", got, snapshot.RelatedChanges)
	}
	for _, ch := range snapshot.RelatedChanges {
		if ch.ChangeID == "C3" {
			t.Fatalf("lookahead change C3 leaked into the visible window: %+v", ch)
		}
	}
}

// TestReadIncidentContextTimelineNotTruncatedWhenWithinLimit proves the fix does
// not over-report truncation: when the timeline fetch returns fewer rows than the
// lookahead bound, Truncated stays false and every decoded event is kept.
func TestReadIncidentContextTimelineNotTruncatedWhenWithinLimit(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	db, _ := openIncidentContextStoreTestDB(t, []incidentContextStoreQueryResult{
		{
			match:   "fact.fact_kind = 'incident.record'",
			columns: incidentContextFactColumns(),
			rows: [][]driver.Value{{
				"incident-fact", "pagerduty:acct:prod", "gen-1", "reported",
				"https://pd/incidents/PABC", "PABC", observedAt, "1.0.0",
				[]byte(`{"provider":"pagerduty","provider_incident_id":"PABC"}`),
			}},
		},
		{
			match:   "fact.fact_kind = 'incident.lifecycle_event'",
			columns: incidentContextFactColumns(),
			rows: [][]driver.Value{
				{
					"evt-1", "pagerduty:acct:prod", "gen-1", "reported", "", "erec-1", observedAt, "1.0.0",
					[]byte(`{"provider":"pagerduty","provider_event_id":"E1","provider_incident_id":"PABC","created_at":"2026-06-01T10:00:00Z"}`),
				},
			},
		},
	})

	store := NewPostgresIncidentContextStore(db)
	snapshot, err := store.ReadIncidentContext(context.Background(), IncidentContextFilter{
		Provider:           "pagerduty",
		ProviderIncidentID: "PABC",
		Limit:              3, // requested limit 2 + 1; only 1 event exists
	})
	if err != nil {
		t.Fatalf("ReadIncidentContext() error = %v, want nil", err)
	}
	if snapshot.Truncated {
		t.Fatal("Truncated = true, want false: only one timeline row was fetched")
	}
	if got := len(snapshot.Timeline); got != 1 {
		t.Fatalf("len(Timeline) = %d, want 1", got)
	}
}
