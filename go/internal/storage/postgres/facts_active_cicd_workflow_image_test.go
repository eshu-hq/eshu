// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestListActiveCICDWorkflowImageFactsQueryIsOwnerBounded(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{
		workflowImageFactRow("workflow-owner", "repository:owner"),
	}}}}
	loaded, err := NewFactStore(db).ListActiveCICDWorkflowImageFacts(
		context.Background(),
		[]string{" repository:owner ", "repository:owner", "repository:second"},
	)
	if err != nil {
		t.Fatalf("ListActiveCICDWorkflowImageFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("ListActiveCICDWorkflowImageFacts() len = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"scope.partition_key = ANY($1::text[])",
		"scope.scope_kind IN ('repository', 'repository_ref')",
		"scope.source_system = 'git'",
		"scope.collector_kind = 'git'",
		"scope.status = 'active'",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.fact_kind = 'ci.workflow_image_evidence'",
		"fact.source_system = 'git'",
		"fact.collector_kind = 'git'",
		"fact.is_tombstone = FALSE",
		"ORDER BY fact.observed_at ASC, fact.fact_id ASC",
		"LIMIT $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[1], maxActiveCICDWorkflowImageFacts+1; got != want {
		t.Fatalf("query limit = %#v, want cap+1 %#v", got, want)
	}
}

func TestListActiveCICDWorkflowImageFactsRequiresRepositoryOwner(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	loaded, err := NewFactStore(db).ListActiveCICDWorkflowImageFacts(context.Background(), []string{" ", ""})
	if err != nil {
		t.Fatalf("ListActiveCICDWorkflowImageFacts() error = %v, want nil", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("ListActiveCICDWorkflowImageFacts() len = %d, want 0", len(loaded))
	}
	if got := len(db.queries); got != 0 {
		t.Fatalf("query count = %d, want 0 for an empty owner set", got)
	}
}

func TestListActiveCICDWorkflowImageFactsFailsClosedAboveCap(t *testing.T) {
	previousCap := maxActiveCICDWorkflowImageFacts
	maxActiveCICDWorkflowImageFacts = 2
	t.Cleanup(func() { maxActiveCICDWorkflowImageFacts = previousCap })

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{
		workflowImageFactRow("workflow-1", "repository:owner"),
		workflowImageFactRow("workflow-2", "repository:owner"),
		workflowImageFactRow("workflow-3", "repository:owner"),
	}}}}
	_, err := NewFactStore(db).ListActiveCICDWorkflowImageFacts(context.Background(), []string{"repository:owner"})
	if err == nil || !strings.Contains(err.Error(), "result exceeds safety cap 2") {
		t.Fatalf("ListActiveCICDWorkflowImageFacts() error = %v, want safety-cap failure", err)
	}
}

func workflowImageFactRow(factID, repositoryID string) []any {
	return []any{
		factID,
		"git-repository-scope:" + repositoryID,
		"generation-git",
		"ci.workflow_image_evidence",
		"stable-" + factID,
		"1.0.0",
		"git",
		int64(1),
		"observed",
		"git",
		factID,
		"",
		"",
		time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC),
		false,
		[]byte(`{"repository_id":"` + repositoryID + `","workflow_path":".github/workflows/release.yml","command_kind":"docker_build","evidence_class":"workflow_image_ref","image_ref":"registry.example.com/team/api:prod","source_category":"static_workflow"}`),
	}
}
