// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/answer"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const serviceAnchorRepo = "repository:orders"

// wideServiceContent serves repositories of n plain YAML candidates each (the
// blank-checked class) by id, and records every path a content read or blank
// check named.
type wideServiceContent struct {
	content.FakePortContentStore
	repos   map[string]int
	mu      *sync.Mutex
	touched map[string]bool
}

func newWideServiceContent(repos map[string]int) wideServiceContent {
	return wideServiceContent{repos: repos, mu: &sync.Mutex{}, touched: map[string]bool{}}
}

func wideServicePath(i int) string { return fmt.Sprintf("cfg/f%05d.yaml", i) }

func (s wideServiceContent) touch(repoID string, paths ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, path := range paths {
		s.touched[repoID+"|"+path] = true
	}
}

func (s wideServiceContent) wasTouched(repoID, path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.touched[repoID+"|"+path]
}

func (s wideServiceContent) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	n, ok := s.repos[repoID]
	if !ok {
		return s.FakePortContentStore.ListRepoFiles(ctx, repoID, limit)
	}
	if limit > 0 {
		n = min(n, limit)
	}
	files := make([]querycontract.FileContent, n)
	for i := range files {
		files[i] = querycontract.FileContent{RepoID: repoID, RelativePath: wideServicePath(i)}
	}
	return files, nil
}

func (s wideServiceContent) GetFileContent(ctx context.Context, repoID, path string) (*querycontract.FileContent, error) {
	if _, ok := s.repos[repoID]; !ok {
		return s.FakePortContentStore.GetFileContent(ctx, repoID, path)
	}
	s.touch(repoID, path)
	return &querycontract.FileContent{RepoID: repoID, RelativePath: path, Content: "k: v\n"}, nil
}

func (s wideServiceContent) GetFileContents(ctx context.Context, repoID string, paths []string) ([]querycontract.FileContent, error) {
	if _, ok := s.repos[repoID]; !ok {
		return s.FakePortContentStore.GetFileContents(ctx, repoID, paths)
	}
	s.touch(repoID, paths...)
	files := make([]querycontract.FileContent, len(paths))
	for i, path := range paths {
		files[i] = querycontract.FileContent{RepoID: repoID, RelativePath: path, Content: "k: v\n"}
	}
	return files, nil
}

func (s wideServiceContent) NonBlankRepoFilePaths(ctx context.Context, repoID string, paths []string) ([]string, error) {
	if _, ok := s.repos[repoID]; !ok {
		return s.FakePortContentStore.NonBlankRepoFilePaths(ctx, repoID, paths)
	}
	s.touch(repoID, paths...)
	return append([]string(nil), paths...), nil
}

type serviceDisclosureRun struct {
	workload map[string]any
	story    map[string]any
	line     string
	counters map[string]int64
	store    wideServiceContent
}

// runServiceDisclosure enriches the orders-api workload (whose repository has
// anchorFiles candidates) over a graph that names the related repositories,
// then builds the service story.
func runServiceDisclosure(t *testing.T, anchorFiles int, related map[string]int) serviceDisclosureRun {
	t.Helper()
	repos := map[string]int{serviceAnchorRepo: anchorFiles}
	ids := make([]string, 0, len(related))
	for id, n := range related {
		repos[id] = n
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	store := newWideServiceContent(repos)
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("service-disclosure"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	var logs bytes.Buffer
	workload := provisioningTruncationWorkload()
	g := graph.FakeWorkloadGraphReader{
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "related:Repository") {
				rows := make([]map[string]any, len(ids))
				for i, id := range ids {
					rows[i] = map[string]any{"repo_id": id, "repo_name": "name-" + id}
				}
				return rows, nil
			}
			return nil, nil
		},
	}
	if err := EnrichServiceQueryContextWithOptions(context.Background(), g, store, workload, QueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    slog.New(slog.NewTextHandler(&logs, nil)),
		Operation:                 "service_story",
		Instruments:               instruments,
	}); err != nil {
		t.Fatalf("enrich: %v", err)
	}
	line := ""
	for _, candidate := range strings.Split(logs.String(), "\n") {
		if strings.Contains(candidate, "stage=deployment_evidence") && strings.Contains(candidate, "stage_completed") {
			line = candidate
		}
	}
	if line == "" {
		t.Fatalf("no deployment_evidence stage line in:\n%s", logs.String())
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	counters := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && m.Name == "eshu_dp_config_artifact_candidates_total" {
				for _, point := range sum.DataPoints {
					outcome, _ := point.Attributes.Value(telemetry.MetricDimensionOutcome)
					counters[outcome.AsString()] += point.Value
				}
			}
		}
	}
	return serviceDisclosureRun{
		workload: workload,
		story:    BuildServiceStoryResponse("orders-api", workload),
		line:     line,
		counters: counters,
		store:    store,
	}
}

func (r serviceDisclosureRun) limitations() []string {
	return querycontract.StringSliceVal(r.workload, "limitations")
}

func (r serviceDisclosureRun) truncated(t *testing.T) (resultLimits, metadata bool) {
	t.Helper()
	limits := querycontract.MapValue(r.story, "result_limits")
	meta, ok := answer.AnswerMetadataFromData(r.story)
	if !ok {
		t.Fatal("service story has no answer_metadata")
	}
	return querycontract.BoolVal(limits, "truncated"), meta.Truncated
}

func requireServiceLine(t *testing.T, line string, fields ...string) {
	t.Helper()
	for _, want := range fields {
		if !strings.Contains(line, want+" ") && !strings.HasSuffix(line, want) {
			t.Errorf("deployment_evidence line = %q, want it to contain %q", line, want)
		}
	}
}

// TestServiceStoryDisclosesACutAnchorList is the L6 sentinel: the service
// repository's own list is read with 5,001 rows; at 5,001 files the list is
// clipped, file 5,001 is never read, and the anchor reason is disclosed; at
// exactly 5,000 files nothing is disclosed.
func TestServiceStoryDisclosesACutAnchorList(t *testing.T) {
	t.Parallel()

	cut := runServiceDisclosure(t, 5001, nil)
	if !querycontract.ContainsString(cut.limitations(), artifacts.AnchorFileListTruncatedReason) {
		t.Fatalf("limitations = %v, want %q", cut.limitations(), artifacts.AnchorFileListTruncatedReason)
	}
	if cut.store.wasTouched(serviceAnchorRepo, wideServicePath(5000)) {
		t.Fatal("read file 5,001, which sits past the anchor window")
	}
	if limits, meta := cut.truncated(t); !limits || !meta {
		t.Fatalf("result_limits.truncated = %v, answer_metadata.truncated = %v, want both true", limits, meta)
	}
	requireServiceLine(t, cut.line, "anchor_list_truncated=1", "repositories_list_truncated=0")

	whole := runServiceDisclosure(t, 5000, nil)
	if querycontract.ContainsString(whole.limitations(), artifacts.AnchorFileListTruncatedReason) {
		t.Fatalf("limitations = %v, want no anchor reason at exactly 5,000 files", whole.limitations())
	}
	requireServiceLine(t, whole.line, "anchor_list_truncated=0")
}

// TestServiceStoryDisclosesTheCandidateBounds runs the related-repository
// bounds through the service route: the budget and related-list reasons land
// in the workload limitations, fold into result_limits.truncated and
// answer_metadata.truncated, and the stage line and counter carry the exact
// counts. The anchor's 10 candidates come first.
func TestServiceStoryDisclosesTheCandidateBounds(t *testing.T) {
	t.Parallel()

	run := runServiceDisclosure(t, 10, map[string]int{"repo-a": 5000, "repo-b": 5000, "repo-c": 5000, "repo-d": 5001})
	for _, reason := range []string{artifacts.CandidatesBudgetReachedReason, artifacts.RelatedFileListTruncatedReason} {
		if !querycontract.ContainsString(run.limitations(), reason) {
			t.Fatalf("limitations = %v, want %q", run.limitations(), reason)
		}
	}
	if querycontract.ContainsString(run.limitations(), artifacts.AnchorFileListTruncatedReason) {
		t.Fatalf("limitations = %v, want no anchor reason for a 10-file anchor", run.limitations())
	}
	if limits, meta := run.truncated(t); !limits || !meta {
		t.Fatalf("result_limits.truncated = %v, answer_metadata.truncated = %v, want both true", limits, meta)
	}
	// Anchor 10, repo-a and repo-b 5,000 each (4,950 of the budget each),
	// repo-c the floor plus the last 2,100, repo-d its floor in a cut list.
	requireServiceLine(t, run.line,
		"candidate_count=20010", "candidates_hydrated=12210", "candidates_unhydrated=7800",
		"repositories_budget_capped=2", "repositories_list_truncated=1", "anchor_list_truncated=0",
		"hydration_budget=12000", "hydration_floor=50")
	want := map[string]int64{"hydrated": 12210, "list_truncated": 1, "unhydrated": 7800}
	if fmt.Sprint(run.counters) != fmt.Sprint(want) {
		t.Fatalf("counter = %v, want %v", run.counters, want)
	}

	quiet := runServiceDisclosure(t, 10, map[string]int{"repo-a": 600})
	for _, reason := range []string{artifacts.CandidatesBudgetReachedReason, artifacts.RelatedFileListTruncatedReason, artifacts.AnchorFileListTruncatedReason} {
		if querycontract.ContainsString(quiet.limitations(), reason) {
			t.Fatalf("limitations = %v, want no %q when nothing bites", quiet.limitations(), reason)
		}
	}
	if limits, _ := quiet.truncated(t); limits {
		t.Fatal("result_limits.truncated = true with no bound biting")
	}
	requireServiceLine(t, quiet.line, "candidate_count=610", "candidates_unhydrated=0", "anchor_list_truncated=0")
}
