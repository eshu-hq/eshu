package postgres

import (
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
	} {
		if !strings.Contains(completeWorkflowClaimQuery, want) {
			t.Fatalf("complete-claim query missing %q:\n%s", want, completeWorkflowClaimQuery)
		}
	}
}

func containsArg(args []any, want any) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
