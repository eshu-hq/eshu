// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPostgresIncidentContextStoreReadsCollectedPagerDutyIncidentBySourceRecordID(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	db, recorder := openIncidentContextStoreTestDB(t, []incidentContextStoreQueryResult{
		{
			match:   "fact.fact_kind = 'incident.record'",
			columns: incidentContextFactColumns(),
			rows: [][]driver.Value{{
				"incident-fact",
				"pagerduty:account:prod",
				"generation-1",
				"reported",
				"https://example.pagerduty.com/incidents/PABC123",
				"PABC123",
				observedAt,
				"1.0.0",
				[]byte(`{"provider":"pagerduty","status":"triggered","title":"checkout-api elevated errors"}`),
			}},
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
	if got, want := snapshot.Incident.ProviderIncidentID, "PABC123"; got != want {
		t.Fatalf("ProviderIncidentID = %q, want %q", got, want)
	}
	if got, want := snapshot.Incident.EvidenceFactID, "incident-fact"; got != want {
		t.Fatalf("EvidenceFactID = %q, want %q", got, want)
	}
	if got, want := snapshot.Incident.ScopeID, "pagerduty:account:prod"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}

	response := BuildIncidentContextResponse(snapshot)
	assertIncidentEdge(t, response.EvidencePath, IncidentSlotIncident, IncidentTruthExact)
	assertIncidentEdge(t, response.EvidencePath, IncidentSlotService, IncidentTruthMissing)
	assertIncidentEdge(t, response.EvidencePath, IncidentSlotWorkItem, IncidentTruthMissing)
	if len(recorder.queries) != 2 {
		t.Fatalf("query count = %d, want 2", len(recorder.queries))
	}
}

func TestPostgresIncidentContextStoreReturnsAmbiguousSourceRecordMatches(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	db, _ := openIncidentContextStoreTestDB(t, []incidentContextStoreQueryResult{
		{
			match:   "fact.fact_kind = 'incident.record'",
			columns: incidentContextFactColumns(),
			rows: [][]driver.Value{
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
				{
					"incident-fact-stage",
					"pagerduty:account:stage",
					"generation-stage",
					"reported",
					"https://example.pagerduty.com/incidents/PABC123",
					"PABC123",
					observedAt,
					"1.0.0",
					[]byte(`{"provider":"pagerduty","status":"triggered","title":"checkout-stage elevated errors"}`),
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
	if got, want := len(ambiguous.Candidates), 2; got != want {
		t.Fatalf("candidate count = %d, want %d", got, want)
	}
}

type incidentContextStoreQueryResult struct {
	match                string
	columns              []string
	rows                 [][]driver.Value
	err                  error
	requireQueryContains []string
}

type incidentContextStoreRecorder struct {
	queries []string
	args    [][]driver.Value
}

func openIncidentContextStoreTestDB(
	t *testing.T,
	results []incidentContextStoreQueryResult,
) (*sql.DB, *incidentContextStoreRecorder) {
	t.Helper()

	recorder := &incidentContextStoreRecorder{}
	name := fmt.Sprintf("incident-context-store-test-%d", atomic.AddUint64(&incidentContextStoreDriverSeq, 1))
	sql.Register(name, &incidentContextStoreDriver{results: results, recorder: recorder})

	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db, recorder
}

var incidentContextStoreDriverSeq uint64

type incidentContextStoreDriver struct {
	results  []incidentContextStoreQueryResult
	recorder *incidentContextStoreRecorder
}

func (d *incidentContextStoreDriver) Open(string) (driver.Conn, error) {
	return &incidentContextStoreConn{
		results:  append([]incidentContextStoreQueryResult(nil), d.results...),
		recorder: d.recorder,
	}, nil
}

type incidentContextStoreConn struct {
	results  []incidentContextStoreQueryResult
	recorder *incidentContextStoreRecorder
}

func (c *incidentContextStoreConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("Prepare not implemented")
}

func (c *incidentContextStoreConn) Close() error {
	return nil
}

func (c *incidentContextStoreConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("Begin not implemented")
}

func (c *incidentContextStoreConn) QueryContext(
	_ context.Context,
	query string,
	args []driver.NamedValue,
) (driver.Rows, error) {
	c.recorder.queries = append(c.recorder.queries, query)
	c.recorder.args = append(c.recorder.args, incidentContextDriverValues(args))
	if len(c.results) == 0 {
		return nil, fmt.Errorf("unexpected incident context query: %s", query)
	}
	result := c.results[0]
	c.results = c.results[1:]
	if result.match != "" && !strings.Contains(query, result.match) {
		return nil, fmt.Errorf("incident context query missing match %q: %s", result.match, query)
	}
	for _, want := range result.requireQueryContains {
		if !strings.Contains(query, want) {
			return &incidentContextStoreRows{columns: result.columns}, nil
		}
	}
	if result.err != nil {
		return nil, result.err
	}
	return &incidentContextStoreRows{columns: result.columns, rows: result.rows}, nil
}

type incidentContextStoreRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *incidentContextStoreRows) Columns() []string {
	return r.columns
}

func (r *incidentContextStoreRows) Close() error {
	return nil
}

func (r *incidentContextStoreRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}

func incidentContextFactColumns() []string {
	return []string{
		"fact_id",
		"scope_id",
		"generation_id",
		"source_confidence",
		"source_uri",
		"source_record_id",
		"observed_at",
		"schema_version",
		"payload",
	}
}

func incidentContextDriverValues(args []driver.NamedValue) []driver.Value {
	out := make([]driver.Value, 0, len(args))
	for _, arg := range args {
		out = append(out, arg.Value)
	}
	return out
}

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
