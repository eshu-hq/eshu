package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerQueueClaimGatesIAMPermissionMaterializationOnCanonicalCloudResourceReadiness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 6, 10, 15, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		domain reducer.Domain
	}{
		{name: "iam escalation", domain: reducer.DomainIAMEscalationMaterialization},
		{name: "iam can perform", domain: reducer.DomainIAMCanPerformMaterialization},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := &iamPermissionReadinessQueueDB{
				now:    now,
				domain: tc.domain,
			}
			queue := ReducerQueue{
				db:            db,
				LeaseOwner:    "test-owner",
				LeaseDuration: time.Minute,
				Now:           func() time.Time { return now },
			}

			intent, claimed, err := queue.Claim(context.Background())
			if err != nil {
				t.Fatalf("Claim() error = %v", err)
			}
			if claimed {
				t.Fatalf("Claim() claimed %q before canonical readiness, want unclaimed waiting work", intent.IntentID)
			}

			db.phaseReady = true
			intent, claimed, err = queue.Claim(context.Background())
			if err != nil {
				t.Fatalf("Claim() after readiness error = %v", err)
			}
			if !claimed {
				t.Fatal("Claim() after readiness claimed = false, want true")
			}
			if got := intent.Domain; got != tc.domain {
				t.Fatalf("claimed domain = %q, want %q", got, tc.domain)
			}
		})
	}
}

func TestReducerQueueClaimQueriesIncludeIAMPermissionReadinessDomains(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: nil}},
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return time.Date(2026, time.June, 6, 10, 15, 0, 0, time.UTC) },
	}
	if _, _, err := queue.Claim(context.Background()); err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(db.queries))
	}
	query := db.queries[0].query
	for _, want := range []string{
		"iam_escalation_materialization",
		"iam_can_perform_materialization",
		"graph_projection_phase_state AS iam_permission_nodes",
		"iam_permission_nodes.phase = 'canonical_nodes_committed'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("Claim() query missing %q:\n%s", want, query)
		}
	}
}

func TestReducerQueueClaimBatchQueriesIncludeIAMPermissionReadinessDomains(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: nil}},
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return time.Date(2026, time.June, 6, 10, 15, 0, 0, time.UTC) },
	}
	if _, err := queue.ClaimBatch(context.Background(), 10); err != nil {
		t.Fatalf("ClaimBatch() error = %v, want nil", err)
	}
	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(db.queries))
	}
	query := db.queries[0].query
	for _, want := range []string{
		"iam_escalation_materialization",
		"iam_can_perform_materialization",
		"graph_projection_phase_state AS iam_permission_nodes",
		"iam_permission_nodes.phase = 'canonical_nodes_committed'",
		"graph_projection_phase_state AS same_iam_permission_nodes",
		"same_iam_permission_nodes.phase = 'canonical_nodes_committed'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("ClaimBatch() query missing %q:\n%s", want, query)
		}
	}
}

type iamPermissionReadinessQueueDB struct {
	now        time.Time
	domain     reducer.Domain
	phaseReady bool
}

func (db *iamPermissionReadinessQueueDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *iamPermissionReadinessQueueDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	hasReadinessGate := strings.Contains(query, "'"+string(db.domain)+"'") &&
		strings.Contains(query, "graph_projection_phase_state AS iam_permission_nodes") &&
		strings.Contains(query, "iam_permission_nodes.phase = 'canonical_nodes_committed'")
	if hasReadinessGate && !db.phaseReady {
		return &queueFakeRows{}, nil
	}

	return &queueFakeRows{rows: [][]any{{
		"reducer-iam-permission-1",
		"aws:123456789012:aws-global:iam",
		"gen-aws-1",
		string(db.domain),
		1,
		db.now.Add(-time.Minute),
		db.now.Add(-time.Minute),
		[]byte(`{"entity_key":"aws_resource_materialization:aws:123456789012:aws-global:iam","reason":"iam permission facts observed","fact_id":"fact-iam-1","source_system":"aws"}`),
	}}}, nil
}
