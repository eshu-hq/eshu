// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"strings"
	"testing"

	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// collectionStateRows is a db.Rows double that scans fixed values in order.
type collectionStateRows struct {
	rows [][]any
	idx  int
}

func (r *collectionStateRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *collectionStateRows) Scan(dest ...any) error {
	row := r.rows[r.idx-1]
	if len(dest) != len(row) {
		return errors.New("column count mismatch")
	}
	for i, value := range row {
		switch d := dest[i].(type) {
		case *string:
			*d = value.(string)
		case *bool:
			*d = value.(bool)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

func (r *collectionStateRows) Err() error   { return nil }
func (r *collectionStateRows) Close() error { return nil }

type recordingCollectionStateQueryer struct {
	queries []string
	args    [][]any
	rows    [][]any
	err     error
}

func (q *recordingCollectionStateQueryer) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	q.queries = append(q.queries, query)
	q.args = append(q.args, args)
	if q.err != nil {
		return nil, q.err
	}
	return &collectionStateRows{rows: q.rows}, nil
}

// TestPostgresSecurityAlertCollectionStateConfigurationsQueryShape pins the
// configuration read to enabled, non-deactivated security_alert instances, so a
// deactivated org target never contributes membership, and bounds it.
func TestPostgresSecurityAlertCollectionStateConfigurationsQueryShape(t *testing.T) {
	t.Parallel()

	queryer := &recordingCollectionStateQueryer{rows: [][]any{{`{"targets":[]}`}}}
	store := NewPostgresSecurityAlertCollectionStateStoreWithReadStore(queryer)
	configs, err := store.SecurityAlertCollectorConfigurations(context.Background())
	if err != nil {
		t.Fatalf("SecurityAlertCollectorConfigurations() error = %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("configs = %v, want one", configs)
	}
	query := queryer.queries[0]
	for _, want := range []string{"FROM collector_instances", "collector_kind = 'security_alert'", "enabled = TRUE", "deactivated_at IS NULL", "LIMIT $1"} {
		if !strings.Contains(query, want) {
			t.Fatalf("configuration query missing %q:\n%s", want, query)
		}
	}
	if got := queryer.args[0][0]; got != securityAlertCollectionStateMaxInstances+1 {
		t.Fatalf("limit arg = %v, want %d", got, securityAlertCollectionStateMaxInstances+1)
	}
}

// TestPostgresSecurityAlertCollectionStateConfigurationsBound fails closed past
// the instance bound instead of dropping targets.
func TestPostgresSecurityAlertCollectionStateConfigurationsBound(t *testing.T) {
	t.Parallel()

	rows := make([][]any, 0, securityAlertCollectionStateMaxInstances+1)
	for i := 0; i <= securityAlertCollectionStateMaxInstances; i++ {
		rows = append(rows, []any{`{}`})
	}
	store := NewPostgresSecurityAlertCollectionStateStoreWithReadStore(&recordingCollectionStateQueryer{rows: rows})
	_, err := store.SecurityAlertCollectorConfigurations(context.Background())
	if !errors.Is(err, supplychain.ErrSecurityAlertCollectionStateBound) {
		t.Fatalf("SecurityAlertCollectorConfigurations() error = %v past the instance bound, want ErrSecurityAlertCollectionStateBound", err)
	}
}

// TestPostgresSecurityAlertCollectionStateTargetStates pins the target-state
// read: it keys work items by collector kind and scope id, takes the latest by
// updated_at, counts only committed generation statuses, never selects the
// failure message, and scans each row into a target state.
func TestPostgresSecurityAlertCollectionStateTargetStates(t *testing.T) {
	t.Parallel()

	queryer := &recordingCollectionStateQueryer{rows: [][]any{
		{"sa-target-a", "failed_terminal", "auth_denied", true, false},
		{"sa-target-b", "completed", "", false, true},
	}}
	store := NewPostgresSecurityAlertCollectionStateStoreWithReadStore(queryer)
	states, err := store.SecurityAlertTargetStates(context.Background(), []string{"sa-target-a", "sa-target-b"})
	if err != nil {
		t.Fatalf("SecurityAlertTargetStates() error = %v", err)
	}
	want := []supplychain.SecurityAlertTargetState{
		{ScopeID: "sa-target-a", LatestWorkItemStatus: "failed_terminal", LatestFailureClass: "auth_denied", LatestFailureTerminal: true},
		{ScopeID: "sa-target-b", LatestWorkItemStatus: "completed", CommittedGeneration: true},
	}
	if len(states) != len(want) {
		t.Fatalf("states = %+v, want %+v", states, want)
	}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("states[%d] = %+v, want %+v", i, states[i], want[i])
		}
	}
	query := queryer.queries[0]
	for _, want := range []string{
		"l.collector_kind = 'security_alert'",
		"l.scope_id = t.scope_id",
		"ORDER BY l.updated_at DESC, l.work_item_id ASC",
		"g.status IN ('active', 'superseded', 'completed')",
		"unnest($1::text[])",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("target state query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "failure_message") {
		t.Fatalf("target state query reads the failure message:\n%s", query)
	}
}

// TestPostgresSecurityAlertCollectionStateTargetStatesBounds covers the empty
// input short-circuit, the target bound, a nil reader, and error wrapping.
func TestPostgresSecurityAlertCollectionStateTargetStatesBounds(t *testing.T) {
	t.Parallel()

	queryer := &recordingCollectionStateQueryer{}
	store := NewPostgresSecurityAlertCollectionStateStoreWithReadStore(queryer)
	if states, err := store.SecurityAlertTargetStates(context.Background(), nil); err != nil || states != nil || len(queryer.queries) != 0 {
		t.Fatalf("empty input: states=%v err=%v queries=%d, want no read", states, err, len(queryer.queries))
	}
	tooMany := make([]string, securityAlertCollectionStateMaxTargets+1)
	if _, err := store.SecurityAlertTargetStates(context.Background(), tooMany); !errors.Is(err, supplychain.ErrSecurityAlertCollectionStateBound) {
		t.Fatalf("SecurityAlertTargetStates() error = %v past the target bound, want ErrSecurityAlertCollectionStateBound", err)
	}
	if _, err := (PostgresSecurityAlertCollectionStateStore{}).SecurityAlertCollectorConfigurations(context.Background()); err == nil {
		t.Fatal("nil reader: error = nil")
	}
	failing := NewPostgresSecurityAlertCollectionStateStoreWithReadStore(&recordingCollectionStateQueryer{err: errors.New("boom")})
	if _, err := failing.SecurityAlertTargetStates(context.Background(), []string{"sa-target-a"}); err == nil || errors.Is(err, supplychain.ErrSecurityAlertCollectionStateBound) {
		t.Fatalf("query error = %v, want a non-bound error", err)
	}
}
