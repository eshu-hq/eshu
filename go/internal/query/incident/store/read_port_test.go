// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingIncidentReader struct {
	calls int
	err   error
}

func (r *rejectingIncidentReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.calls++
	return nil, r.err
}

func TestIncidentGuardedReadPortsRefuseBeforeBusinessSQL(t *testing.T) {
	want := errors.New("reader freshness refused")
	guard := &rejectingIncidentReader{err: want}
	store := NewStoreWithReadStore(guard)
	incident := model.IncidentContextIncident{}
	incident.Service.Summary = "checkout"
	cases := []struct {
		name string
		read func() error
	}{
		{"anchor", func() error {
			_, err := store.ReadIncidentContext(t.Context(), model.IncidentContextFilter{ProviderIncidentID: "P1", Limit: 2})
			return err
		}},
		{"declared routing", func() error {
			_, err := store.readIncidentDeclaredPagerDutyRouting(t.Context(), incident)
			return err
		}},
		{"pull requests", func() error {
			_, err := store.readIncidentPullRequestsByCommit(t.Context(), "commit")
			return err
		}},
		{"durable authorization", func() error {
			_, err := NewPostgresIncidentRepositoryAuthorizerWithReadStore(guard).ResolveDurableIncidentRepositories(t.Context(), "pagerduty", "P1", "")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := guard.calls
			if err := tc.read(); !errors.Is(err, want) || guard.calls != before+1 {
				t.Fatalf("error %v, calls %d; want original refusal and one guarded read", err, guard.calls-before)
			}
		})
	}
}

type incidentSQLReadPort struct{ database incidentContextQueryer }

func (r incidentSQLReadPort) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return r.database.QueryContext(ctx, query, args...)
}

func TestIncidentGuardedAuthorizerPreservesRepositoryResults(t *testing.T) {
	database, recorder := openIncidentContextStoreTestDB(t, []incidentContextStoreQueryResult{{
		match: "SELECT DISTINCT correlation.payload", columns: []string{"repository_id"},
		rows: [][]driver.Value{{"repo-b"}, {"repo-a"}, {"repo-b"}, {" "}},
	}})
	reader := NewPostgresIncidentRepositoryAuthorizerWithReadStore(incidentSQLReadPort{database: database})
	got, err := reader.ResolveDurableIncidentRepositories(t.Context(), " PagerDuty ", " P1 ", " scope ")
	if err != nil || !reflect.DeepEqual(got, []string{"repo-a", "repo-b"}) {
		t.Fatalf("repositories %v, error %v", got, err)
	}
	if len(recorder.args) != 1 || !reflect.DeepEqual(recorder.args[0], []driver.Value{"pagerduty", "P1", "scope"}) {
		t.Fatalf("query arguments %v", recorder.args)
	}
	if got, err := reader.ResolveDurableIncidentRepositories(t.Context(), "", "", ""); err != nil || len(got) != 0 || len(recorder.queries) != 1 {
		t.Fatalf("blank input read database: rows %v, error %v, queries %d", got, err, len(recorder.queries))
	}
}

func TestIncidentGuardedAnchorPreservesNotFound(t *testing.T) {
	database, recorder := openIncidentContextStoreTestDB(t, []incidentContextStoreQueryResult{{
		match: "fact.fact_kind = 'incident.record'", columns: incidentContextFactColumns(),
	}})
	store := NewStoreWithReadStore(incidentSQLReadPort{database: database})
	_, err := store.ReadIncidentContext(t.Context(), model.IncidentContextFilter{ProviderIncidentID: "missing", Limit: 2})
	if !errors.Is(err, model.ErrIncidentContextNotFound) || len(recorder.queries) != 1 {
		t.Fatalf("error %v, queries %d; want not found after guarded anchor", err, len(recorder.queries))
	}
}
