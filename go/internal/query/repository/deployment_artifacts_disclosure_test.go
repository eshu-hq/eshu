// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// wideRelatedContent serves the anchor repository from the embedded fake and
// related repositories of n plain YAML candidates each (the blank-checked
// class), without a quadratic scan.
type wideRelatedContent struct {
	metadataOnlyContent
	related map[string]int
}

func widePath(i int) string { return fmt.Sprintf("cfg/f%05d.yaml", i) }

func (s wideRelatedContent) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	n, ok := s.related[repoID]
	if !ok {
		return s.metadataOnlyContent.ListRepoFiles(ctx, repoID, limit)
	}
	if limit > 0 {
		n = min(n, limit)
	}
	files := make([]querycontract.FileContent, n)
	for i := range files {
		files[i] = querycontract.FileContent{RepoID: repoID, RelativePath: widePath(i)}
	}
	return files, nil
}

func (s wideRelatedContent) GetFileContents(ctx context.Context, repoID string, paths []string) ([]querycontract.FileContent, error) {
	if _, ok := s.related[repoID]; !ok {
		return s.metadataOnlyContent.GetFileContents(ctx, repoID, paths)
	}
	files := make([]querycontract.FileContent, len(paths))
	for i, path := range paths {
		files[i] = querycontract.FileContent{RepoID: repoID, RelativePath: path, Content: "k: v\n"}
	}
	return files, nil
}

func (s wideRelatedContent) NonBlankRepoFilePaths(ctx context.Context, repoID string, paths []string) ([]string, error) {
	if _, ok := s.related[repoID]; !ok {
		return s.metadataOnlyContent.NonBlankRepoFilePaths(ctx, repoID, paths)
	}
	return append([]string(nil), paths...), nil
}

// biteRelated makes both bounds bite under the production floor and budget:
// three related repositories fill the 5,000-file window and a fourth returns
// the sentinel row.
func biteRelated() map[string]int {
	return map[string]int{"repo-a": 5000, "repo-b": 5000, "repo-c": 5000, "repo-d": 5001}
}

// relatedGraphReader answers the repository lookup, the related-repository
// read with ids in a fixed shuffled order, count reads with zero, and every
// other read with no rows.
func relatedGraphReader(related map[string]int) graph.FakeGraphReader {
	ids := make([]string, 0, len(related))
	for id := range related {
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	return graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				return map[string]any{"id": "repo-da-1", "name": "repo-da-one"}, nil
			}
			return nil, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "related:Repository") {
				rows := make([]map[string]any, len(ids))
				for i, id := range ids {
					rows[i] = map[string]any{"repo_id": id, "repo_name": "name-" + id}
				}
				return rows, nil
			}
			if strings.Contains(cypher, "RETURN count(") {
				return []map[string]any{{"count": int64(0)}}, nil
			}
			return []map[string]any{}, nil
		},
	}
}

type disclosureRun struct {
	body     map[string]any
	logs     string
	counters map[string]int64
}

// runDisclosureRoute serves route ("story" or "context") over the anchor's one
// Terraform file and the given related repositories, with a manual metric
// reader on the handler's instruments.
func runDisclosureRoute(t *testing.T, route string, related map[string]int) disclosureRun {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("disclosure-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	store := wideRelatedContent{
		metadataOnlyContent: metadataOnlyContent{FakePortContentStore: content.FakePortContentStore{RepoFiles: deploymentArtifactsContextFiles()}},
		related:             related,
	}
	var logs bytes.Buffer
	handler := &Handler{
		Neo4j:       relatedGraphReader(related),
		Content:     store,
		Logger:      slog.New(slog.NewTextHandler(&logs, nil)),
		Instruments: instruments,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-da-1/"+route, nil)
	req.SetPathValue("repo_id", "repo-da-1")
	rec := httptest.NewRecorder()
	if route == "story" {
		handler.getRepositoryStory(rec, req)
	} else {
		handler.getRepositoryContext(rec, req)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d; body = %s", route, rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data, ok := body["data"].(map[string]any); ok {
		body = data
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	counters := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_config_artifact_candidates_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want an int64 sum", m.Name, m.Data)
			}
			for _, point := range sum.DataPoints {
				outcome, _ := point.Attributes.Value(telemetry.MetricDimensionOutcome)
				counters[outcome.AsString()] += point.Value
			}
		}
	}
	return disclosureRun{body: body, logs: logs.String(), counters: counters}
}

// biteStageFields are the exact counts of biteRelated: the anchor's one
// candidate, then repo-a and repo-b take 5,000 each (4,950 of the budget
// each), repo-c takes the floor plus the last 2,100, and repo-d keeps its
// floor of 50 inside a cut list. Blank checks: 10 + 10 + 5 + 1 chunks.
var biteStageFields = []string{
	"candidate_count=20001", "candidates_hydrated=12201", "candidates_unhydrated=7800",
	"repositories_budget_capped=2", "repositories_list_truncated=1",
	"hydration_budget=12000", "hydration_floor=50", "related_repository_count=4",
	"blank_check_statements=26",
}

func requireLineFields(t *testing.T, line string, fields []string) {
	t.Helper()
	for _, want := range fields {
		if !strings.Contains(line, want+" ") && !strings.HasSuffix(line, want) {
			t.Errorf("stage line = %q, want it to contain %q", line, want)
		}
	}
}

func TestRepositoryStoryDisclosesTheCandidateBounds(t *testing.T) {
	t.Parallel()
	run := runDisclosureRoute(t, "story", biteRelated())

	limitations, _ := run.body["limitations"].([]any)
	for _, reason := range []string{artifacts.CandidatesBudgetReachedReason, artifacts.RelatedFileListTruncatedReason} {
		if !testutil.AnySliceContains(limitations, reason) {
			t.Fatalf("limitations = %#v, want %q", limitations, reason)
		}
	}
	if truncated, _ := run.body["truncated"].(bool); !truncated {
		t.Fatalf("truncated = %#v, want true when a candidate bound bit", run.body["truncated"])
	}
	metadata, _ := run.body["answer_metadata"].(map[string]any)
	if truncated, _ := metadata["truncated"].(bool); !truncated {
		t.Fatalf("answer_metadata.truncated = %#v, want true", metadata["truncated"])
	}
	requireLineFields(t, deploymentArtifactsStageLine(t, run.logs, "repository_query.stage_completed"), biteStageFields)
	want := map[string]int64{"hydrated": 12201, "unhydrated": 7800, "list_truncated": 1}
	if fmt.Sprint(run.counters) != fmt.Sprint(want) {
		t.Fatalf("eshu_dp_config_artifact_candidates_total = %v, want %v", run.counters, want)
	}
}

func TestRepositoryContextDisclosesTheCandidateBounds(t *testing.T) {
	t.Parallel()
	run := runDisclosureRoute(t, "context", biteRelated())

	reasons := testutil.RequireStringAnySlice(t, run.body, "partial_reasons")
	for _, reason := range []string{artifacts.CandidatesBudgetReachedReason, artifacts.RelatedFileListTruncatedReason} {
		if !testutil.AnySliceContains(reasons, reason) {
			t.Fatalf("partial_reasons = %#v, want %q", reasons, reason)
		}
	}
	requireLineFields(t, contentInfrastructureOverviewLine(t, run.logs), biteStageFields)
	want := map[string]int64{"hydrated": 12201, "unhydrated": 7800, "list_truncated": 1}
	if fmt.Sprint(run.counters) != fmt.Sprint(want) {
		t.Fatalf("eshu_dp_config_artifact_candidates_total = %v, want %v", run.counters, want)
	}
}

// TestCandidateBoundsAreAbsentWhenNothingBites runs both routes over a related
// repository of 600 candidates, past the old cap of 50 and under both bounds:
// no reason, no truncation, zero unhydrated on the stage line and no
// unhydrated or list_truncated counter series.
func TestCandidateBoundsAreAbsentWhenNothingBites(t *testing.T) {
	t.Parallel()
	related := map[string]int{"repo-a": 600}
	quiet := []string{
		"candidate_count=601", "candidates_hydrated=601", "candidates_unhydrated=0",
		"repositories_budget_capped=0", "repositories_list_truncated=0",
		"hydration_budget=12000", "hydration_floor=50",
	}
	for _, route := range []string{"story", "context"} {
		run := runDisclosureRoute(t, route, related)
		field := "limitations"
		line := ""
		if route == "story" {
			line = deploymentArtifactsStageLine(t, run.logs, "repository_query.stage_completed")
			if truncated, _ := run.body["truncated"].(bool); truncated {
				t.Fatalf("story truncated = true with no bound biting")
			}
		} else {
			field = "partial_reasons"
			line = contentInfrastructureOverviewLine(t, run.logs)
		}
		reasons, _ := run.body[field].([]any)
		for _, reason := range []string{artifacts.CandidatesBudgetReachedReason, artifacts.RelatedFileListTruncatedReason} {
			if testutil.AnySliceContains(reasons, reason) {
				t.Fatalf("%s %s = %#v, want no %q", route, field, reasons, reason)
			}
		}
		requireLineFields(t, line, quiet)
		if want := map[string]int64{"hydrated": 601}; fmt.Sprint(run.counters) != fmt.Sprint(want) {
			t.Fatalf("%s counter = %v, want %v", route, run.counters, want)
		}
	}
}
