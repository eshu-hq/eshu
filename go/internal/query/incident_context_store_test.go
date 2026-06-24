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
