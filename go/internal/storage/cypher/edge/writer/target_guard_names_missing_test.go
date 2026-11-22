// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// absentProbeExecutor is a write-guard fake that answers every presence
// probe present except the ones absent reports as missing, so a test can
// plant exactly one absent target anywhere in a batch.
type absentProbeExecutor struct {
	absent func(stmt sourcecypher.Statement) bool
	writes int
}

func (e *absentProbeExecutor) Execute(context.Context, sourcecypher.Statement) error {
	e.writes++
	return nil
}

func (e *absentProbeExecutor) ExecuteProbe(_ context.Context, stmt sourcecypher.Statement) (bool, error) {
	return !e.absent(stmt), nil
}

// absentRepoProbe reports a statement absent when its repo_id parameter is
// one of the named repositories.
func absentRepoProbe(repos ...string) func(sourcecypher.Statement) bool {
	return func(stmt sourcecypher.Statement) bool {
		id, _ := stmt.Parameters["repo_id"].(string)
		for _, repo := range repos {
			if id == repo {
				return true
			}
		}
		return false
	}
}

func deployableUnitRowFor(intentID, repoID, deploymentRepoID string) sharedintent.Row {
	row := validDeployableUnitRow()
	row.IntentID = intentID
	row.RepositoryID = repoID
	row.Payload["repo_id"] = repoID
	row.Payload["deployment_repo_id"] = deploymentRepoID
	return row
}

func runsInRowFor(intentID, repoID string) sharedintent.Row {
	row := validRunsInRow()
	row.IntentID = intentID
	row.RepositoryID = repoID
	row.Payload["repo_id"] = repoID
	return row
}

func handlesRouteRowFor(intentID, repoID, functionID string) sharedintent.Row {
	row := validHandlesRouteRow()
	row.IntentID = intentID
	row.RepositoryID = repoID
	row.Payload["repo_id"] = repoID
	row.Payload["function_entity_id"] = functionID
	return row
}

// TestTargetGuardNamesTheMissingTargetRepository is the #11242 regression:
// the pre-write guard named the first batch row's repository as the "sample
// repo" in its error text, in SampleTarget() and in the `shared edge batch
// target absent` WARN, even when that repository's node was present and a
// different repository's node was the absent one (the CI failure named the
// source repo while the deployment stub was the missing node). Every batch
// kind that shares the guard must name the repository whose node was missing,
// while the retry class, retryability and intent id stay as they were.
func TestTargetGuardNamesTheMissingTargetRepository(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		domain   string
		evidence string
		rows     []sharedintent.Row
		absent   func(sourcecypher.Statement) bool
		wantRepo string
	}{
		{
			name:     "deployable unit: source present, deployment absent",
			domain:   reducercontract.DomainDeployableUnitEdges,
			evidence: "reducer/deployable-unit-correlation",
			rows:     []sharedintent.Row{deployableUnitRowFor("i-du-1", "repo-app", "repo-deploy")},
			absent:   absentRepoProbe("repo-deploy"),
			wantRepo: "repo-deploy",
		},
		{
			name:     "deployable unit: first row fully present, later row deployment absent",
			domain:   reducercontract.DomainDeployableUnitEdges,
			evidence: "reducer/deployable-unit-correlation",
			rows: []sharedintent.Row{
				deployableUnitRowFor("i-du-1", "repo-app", "repo-deploy"),
				deployableUnitRowFor("i-du-2", "repo-app-2", "repo-deploy-2"),
			},
			absent:   absentRepoProbe("repo-deploy-2"),
			wantRepo: "repo-deploy-2",
		},
		{
			name:     "deployable unit: source absent, deployment present",
			domain:   reducercontract.DomainDeployableUnitEdges,
			evidence: "reducer/deployable-unit-correlation",
			rows: []sharedintent.Row{
				deployableUnitRowFor("i-du-1", "repo-app", "repo-deploy"),
				deployableUnitRowFor("i-du-2", "repo-app-2", "repo-deploy-2"),
			},
			absent:   absentRepoProbe("repo-app-2"),
			wantRepo: "repo-app-2",
		},
		{
			name:     "runs in: later row's repository defines no workload",
			domain:   reducercontract.DomainRunsIn,
			evidence: "reducer/runs-in",
			rows: []sharedintent.Row{
				runsInRowFor("i-run-1", "repo-a"),
				runsInRowFor("i-run-2", "repo-b"),
			},
			absent:   absentRepoProbe("repo-b"),
			wantRepo: "repo-b",
		},
		{
			name:     "handles route: later row's endpoint repository absent",
			domain:   reducercontract.DomainHandlesRoute,
			evidence: "parser/framework-routes",
			rows: []sharedintent.Row{
				handlesRouteRowFor("i-hr-1", "repo-a", "content-entity:a"),
				handlesRouteRowFor("i-hr-2", "repo-b", "content-entity:b"),
			},
			absent:   absentRepoProbe("repo-b"),
			wantRepo: "repo-b",
		},
		{
			name:     "handles route: function absent names the row's repository",
			domain:   reducercontract.DomainHandlesRoute,
			evidence: "parser/framework-routes",
			rows: []sharedintent.Row{
				handlesRouteRowFor("i-hr-1", "repo-a", "content-entity:a"),
				handlesRouteRowFor("i-hr-2", "repo-b", "content-entity:b"),
			},
			absent: func(stmt sourcecypher.Statement) bool {
				return stmt.Parameters["uid"] == "content-entity:b"
			},
			wantRepo: "repo-b",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			exec := &absentProbeExecutor{absent: tc.absent}
			w, _, logs := verifyWriter(t, exec)
			headRepo := tc.rows[0].RepositoryID
			headIntent := tc.rows[0].IntentID
			if tc.wantRepo == headRepo {
				t.Fatalf("test setup: missing repo %q equals the first row's repo, the case cannot tell the two apart", tc.wantRepo)
			}

			_, err := w.WriteEdges(context.Background(), tc.domain, tc.rows, tc.evidence)
			if err == nil {
				t.Fatal("WriteEdges() with an absent target succeeded, want the retryable miss")
			}
			if !reducer.IsRetryable(err) {
				t.Fatalf("WriteEdges() error = %v, want retryable", err)
			}
			if got := verifyClassOf(err); got != reducer.SharedEdgeTargetNotReadyFailureClass {
				t.Fatalf("failure class = %q, want %q", got, reducer.SharedEdgeTargetNotReadyFailureClass)
			}
			if exec.writes != 0 {
				t.Fatalf("executor writes = %d, want 0 when a target is absent", exec.writes)
			}

			wantText := fmt.Sprintf("sample repo %q intent %q", tc.wantRepo, headIntent)
			if !strings.Contains(err.Error(), wantText) {
				t.Fatalf("error text = %q, want it to contain %s (the absent target, not the first row's %q)", err.Error(), wantText, headRepo)
			}

			var sampled interface{ SampleTarget() (string, string) }
			if !errors.As(err, &sampled) {
				t.Fatalf("error %v has no SampleTarget", err)
			}
			repo, intent := sampled.SampleTarget()
			if repo != tc.wantRepo || intent != headIntent {
				t.Fatalf("SampleTarget() = (%q, %q), want (%q, %q)", repo, intent, tc.wantRepo, headIntent)
			}

			entry := targetAbsentWarn(t, logs.Bytes())
			if got := entry["sample_repo_id"]; got != tc.wantRepo {
				t.Fatalf("WARN sample_repo_id = %v, want %q (the absent target, not the first row's %q)", got, tc.wantRepo, headRepo)
			}
			if got := entry["sample_intent_id"]; got != headIntent {
				t.Fatalf("WARN sample_intent_id = %v, want %q", got, headIntent)
			}
		})
	}
}

func targetAbsentWarn(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry["msg"] == "shared edge batch target absent, deferring batch" {
			return entry
		}
	}
	t.Fatalf("no `shared edge batch target absent` WARN in logs:\n%s", raw)
	return nil
}
