// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producer

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type fakeQueryer struct {
	query string
	args  []any
	calls int
	rows  [][]any
	err   error
}

func (f *fakeQueryer) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	f.calls++
	f.query, f.args = query, args
	if f.err != nil {
		return nil, f.err
	}
	return &fakeRows{rows: f.rows}, nil
}

type fakeRows struct {
	rows [][]any
	next int
}

func (r *fakeRows) Next() bool   { r.next++; return r.next <= len(r.rows) }
func (r *fakeRows) Err() error   { return nil }
func (r *fakeRows) Close() error { return nil }

func (r *fakeRows) Scan(dest ...any) error {
	row := r.rows[r.next-1]
	for i, value := range row {
		switch target := dest[i].(type) {
		case *string:
			*target = value.(string)
		case *int:
			*target = value.(int)
		default:
			return errors.New("unexpected scan target")
		}
	}
	return nil
}

var workloadProducer = []reducercontract.Domain{reducercontract.DomainWorkloadMaterialization}

// The store satisfies the handler's port; a signature drift fails to compile.
var _ servicecatalog.RuntimeProducerReadiness = Store{}

func TestRuntimeProducerStatesMapsRepositoriesToScopesAndBack(t *testing.T) {
	t.Parallel()

	fake := &fakeQueryer{rows: [][]any{
		{"git-repository-scope:repository:r_a", 1, 0, 2, 0, 4},
	}}
	states, err := Store{DB: fake}.RuntimeProducerStates(context.Background(),
		[]string{"repository:r_a", " repository:r_b ", "repository:r_a", ""}, workloadProducer)
	if err != nil {
		t.Fatalf("RuntimeProducerStates() error = %v", err)
	}
	if got, want := fake.args[0], []string{"git-repository-scope:repository:r_a", "git-repository-scope:repository:r_b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scope ids = %v, want %v (deduped, trimmed, blank dropped)", got, want)
	}
	if got, want := fake.args[1], []string{"workload_materialization"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("domains = %v, want %v", got, want)
	}
	want := map[string]servicecatalog.RuntimeProducerState{
		"repository:r_a": {Pending: 1, Claimed: 2, Total: 4},
	}
	if !reflect.DeepEqual(states, want) {
		t.Fatalf("states = %+v, want %+v (r_b absent: no producer row)", states, want)
	}
	if !strings.Contains(fake.query, "work.generation_id = scope.active_generation_id") {
		t.Fatalf("query must pin the active generation:\n%s", fake.query)
	}
}

func TestRuntimeProducerStatesNoInputIssuesNoQuery(t *testing.T) {
	t.Parallel()

	fake := &fakeQueryer{}
	for _, tc := range []struct {
		repos   []string
		domains []reducercontract.Domain
	}{{nil, workloadProducer}, {[]string{"repository:r_a"}, nil}, {[]string{" "}, workloadProducer}} {
		states, err := Store{DB: fake}.RuntimeProducerStates(context.Background(), tc.repos, tc.domains)
		if err != nil || states != nil {
			t.Fatalf("RuntimeProducerStates(%v, %v) = %v, %v, want nil, nil", tc.repos, tc.domains, states, err)
		}
	}
	if fake.calls != 0 {
		t.Fatalf("query calls = %d, want 0", fake.calls)
	}
}

func TestRuntimeProducerStatesReturnsErrorsNeverEmptyState(t *testing.T) {
	t.Parallel()

	if _, err := (Store{DB: &fakeQueryer{err: errors.New("boom")}}).RuntimeProducerStates(
		context.Background(), []string{"repository:r_a"}, workloadProducer); err == nil {
		t.Fatal("query error swallowed; a store that cannot answer must return an error")
	}
	unrequested := &fakeQueryer{rows: [][]any{{"git-repository-scope:repository:r_z", 0, 0, 0, 0, 1}}}
	if _, err := (Store{DB: unrequested}).RuntimeProducerStates(
		context.Background(), []string{"repository:r_a"}, workloadProducer); err == nil {
		t.Fatal("row for an unrequested scope accepted")
	}
	if _, err := (Store{}).RuntimeProducerStates(context.Background(), []string{"repository:r_a"}, workloadProducer); err == nil {
		t.Fatal("nil database accepted")
	}
}
