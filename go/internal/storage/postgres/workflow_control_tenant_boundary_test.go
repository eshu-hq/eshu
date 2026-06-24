// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkflowEligibleTargetsQueryIncludesTenantBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 10, 12, 0, 0, 0, time.UTC)
	item := awsScheduledWorkItem("run-tenant-boundary", "lambda", now)
	item.TenantID = "tenant-a"
	item.WorkspaceID = "workspace-a"
	item.SubjectClass = "collector"
	item.PolicyRevisionHash = "policy-a"

	query, args := workflowEligibleTargetsQuery("run-tenant-boundary", []workflow.WorkItem{item})
	for _, want := range []string{
		"tenant_id",
		"workspace_id",
		"subject_class",
		"policy_revision_hash",
		"item.tenant_id = planned.tenant_id",
		"item.workspace_id = planned.workspace_id",
		"item.subject_class = planned.subject_class",
		"item.policy_revision_hash = planned.policy_revision_hash",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("eligible-target query missing %q:\n%s", want, query)
		}
	}
	for _, want := range []any{"tenant-a", "workspace-a", "collector", "policy-a"} {
		if !containsArg(args, want) {
			t.Fatalf("eligible-target query args missing %v: %#v", want, args)
		}
	}
}

func TestCompleteWorkflowClaimQueryChecksActiveTenantGrant(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"tenant_scope_grants",
		"item.tenant_id",
		"item.workspace_id",
		"item.subject_class",
		"item.policy_revision_hash",
		"tenant_scope_grants.policy_revision_hash = item.policy_revision_hash",
		"tenant_scope_grants.tombstoned_at IS NULL",
		"tenant_scope_grants.effective_at <= $1",
		"(tenant_scope_grants.expires_at IS NULL OR tenant_scope_grants.expires_at > $1)",
		"FOR SHARE OF tenant_scope_grants, tenants, workspaces",
	} {
		if !strings.Contains(completeWorkflowClaimQuery, want) {
			t.Fatalf("complete-claim query missing %q:\n%s", want, completeWorkflowClaimQuery)
		}
	}
}

func TestHeartbeatWorkflowClaimQueryLocksActiveTenantGrant(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"tenant_scope_grants",
		"item.tenant_id",
		"item.workspace_id",
		"item.subject_class",
		"item.policy_revision_hash",
		"tenant_scope_grants.policy_revision_hash = item.policy_revision_hash",
		"tenant_scope_grants.tombstoned_at IS NULL",
		"tenant_scope_grants.effective_at <= $1",
		"(tenant_scope_grants.expires_at IS NULL OR tenant_scope_grants.expires_at > $1)",
		"FOR SHARE OF tenant_scope_grants, tenants, workspaces",
	} {
		if !strings.Contains(heartbeatWorkflowClaimQuery, want) {
			t.Fatalf("heartbeat-claim query missing %q:\n%s", want, heartbeatWorkflowClaimQuery)
		}
	}
}

func TestWorkflowControlStoreHeartbeatClaimRejectsInactiveTenantGrant(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 10, 13, 5, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		mutation workflow.ClaimMutation
	}{
		{
			name:     "revoked grant",
			mutation: tenantGrantMutation(now),
		},
		{
			name: "stale policy revision",
			mutation: func() workflow.ClaimMutation {
				mutation := tenantGrantMutation(now)
				mutation.PolicyRevisionHash = "policy-stale"
				return mutation
			}(),
		},
		{
			name: "deleted workspace",
			mutation: func() workflow.ClaimMutation {
				mutation := tenantGrantMutation(now)
				mutation.WorkspaceID = "workspace-deleted"
				return mutation
			}(),
		},
		{
			name: "expired grant",
			mutation: func() workflow.ClaimMutation {
				mutation := tenantGrantMutation(now)
				mutation.ObservedAt = now.Add(2 * time.Hour)
				return mutation
			}(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := &tenantBoundaryExecQueryer{result: rowsAffectedResult{rowsAffected: 0}}
			store := NewWorkflowControlStore(db)

			err := store.HeartbeatClaim(context.Background(), tc.mutation)
			if !errors.Is(err, ErrWorkflowClaimRejected) {
				t.Fatalf("HeartbeatClaim() error = %v, want ErrWorkflowClaimRejected", err)
			}
			if got, want := len(db.execs), 1; got != want {
				t.Fatalf("exec count = %d, want %d", got, want)
			}
			if !strings.Contains(db.execs[0].query, "FOR SHARE OF tenant_scope_grants") {
				t.Fatalf("heartbeat query did not lock tenant grant:\n%s", db.execs[0].query)
			}
		})
	}
}

type tenantBoundaryExecQueryer struct {
	execs  []fakeExecCall
	result sql.Result
}

func (f *tenantBoundaryExecQueryer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.execs = append(f.execs, fakeExecCall{query: query, args: args})
	return f.result, nil
}

func (f *tenantBoundaryExecQueryer) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, errors.New("unexpected query")
}

func containsArg(args []any, want any) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
