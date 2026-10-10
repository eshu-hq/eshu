// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// deploymentArtifactsContextFixture runs the repository context route over a
// graph that answers every read with no rows, so the deployment-artifact stage
// is the only read under test (#7126). The content store lists one Terraform
// file whose content only GetFileContent returns, the read a failing store
// breaks.
func deploymentArtifactsContextFixture(t *testing.T, contentStore querycontract.ContentStore) (map[string]any, string) {
	t.Helper()

	reader := graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				return map[string]any{"id": "repo-da-1", "name": "repo-da-one"}, nil
			}
			return nil, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "RETURN count(") {
				return []map[string]any{{"count": int64(0)}}, nil
			}
			return []map[string]any{}, nil
		},
	}
	var logs bytes.Buffer
	handler := &Handler{Neo4j: reader, Content: contentStore, Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-da-1/context", nil)
	req.SetPathValue("repo_id", "repo-da-1")
	rec := httptest.NewRecorder()
	handler.getRepositoryContext(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the stage is auxiliary); body = %s", rec.Code, rec.Body.String())
	}
	return decodeRepositoryAuthzBody(t, rec), logs.String()
}

func deploymentArtifactsContextFiles() []querycontract.FileContent {
	return []querycontract.FileContent{
		{RepoID: "repo-da-1", RelativePath: "infra/main.tf", Content: "module \"m\" {\n  source = \"./m\"\n}\n"},
	}
}

// contentInfrastructureOverviewLine returns the context stage line that
// carries the deployment-artifact read counts.
func contentInfrastructureOverviewLine(t *testing.T, logs string) string {
	t.Helper()
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "stage=content_infrastructure_overview") && strings.Contains(line, "repository_query.stage_completed") {
			return line
		}
	}
	t.Fatalf("no stage_completed line for stage content_infrastructure_overview in:\n%s", logs)
	return ""
}

func TestRepositoryContextDeploymentArtifactsReadFailureIsNamedNotSwallowed(t *testing.T) {
	t.Parallel()

	store := failingHydrationContent{metadataOnlyContent{FakePortContentStore: content.FakePortContentStore{
		RepoFiles: deploymentArtifactsContextFiles(),
	}}}
	body, logs := deploymentArtifactsContextFixture(t, store)

	reasons := testutil.RequireStringAnySlice(t, body, "partial_reasons")
	if !testutil.AnySliceContains(reasons, deploymentArtifactsReadDegradedReason) {
		t.Fatalf("partial_reasons = %#v, want %q after a failed deployment-artifact load", reasons, deploymentArtifactsReadDegradedReason)
	}
	line := contentInfrastructureOverviewLine(t, logs)
	for _, want := range []string{
		"deployment_artifacts_error=true",
		"failure_class=" + deploymentArtifactsReadDegradedReason,
		"files_listed=1",
		"files_hydrated=",
		"statements=",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("content_infrastructure_overview line = %q, want it to contain %q", line, want)
		}
	}
	degraded := deploymentArtifactsStageLine(t, logs, "repository_query.deployment_artifacts_degraded")
	for _, want := range []string{"level=WARN", "operation=repository_context", "content store unavailable"} {
		if !strings.Contains(degraded, want) {
			t.Errorf("degraded line = %q, want it to contain %q", degraded, want)
		}
	}
}

func TestRepositoryContextDeploymentArtifactsSuccessAddsNoDegradedReason(t *testing.T) {
	t.Parallel()

	store := metadataOnlyContent{FakePortContentStore: content.FakePortContentStore{
		RepoFiles: deploymentArtifactsContextFiles(),
	}}
	body, logs := deploymentArtifactsContextFixture(t, store)

	reasons := testutil.RequireStringAnySlice(t, body, "partial_reasons")
	if testutil.AnySliceContains(reasons, deploymentArtifactsReadDegradedReason) {
		t.Fatalf("partial_reasons = %#v, want no %q on a healthy load", reasons, deploymentArtifactsReadDegradedReason)
	}
	line := contentInfrastructureOverviewLine(t, logs)
	for _, want := range []string{"deployment_artifacts_error=false", "files_listed=1", "statements="} {
		if !strings.Contains(line, want) {
			t.Errorf("content_infrastructure_overview line = %q, want it to contain %q", line, want)
		}
	}
	if strings.Contains(line, "failure_class=") {
		t.Errorf("content_infrastructure_overview line = %q, want no failure_class on success", line)
	}
	if strings.Contains(logs, "repository_query.deployment_artifacts_degraded") {
		t.Errorf("healthy load emitted a degraded WARN; logs = %s", logs)
	}
}
