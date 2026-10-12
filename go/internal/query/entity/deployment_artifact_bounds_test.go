// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

const (
	boundsAnchorRepo = "repo-1"
	boundsWorkloadID = "workload-1"
	boundsService    = "svc"
)

// boundsContent serves repositories of n plain YAML candidates each by id, so
// the deployment-artifact load sees a repository of any width without a
// fixture tree.
type boundsContent struct {
	content.FakePortContentStore
	repos map[string]int
	// plain names the repositories whose files are not config candidates, so
	// a list bound can bite without the load producing any artifact row.
	plain map[string]bool
}

func (s boundsContent) path(repoID string, i int) string {
	if s.plain[repoID] {
		return fmt.Sprintf("src/f%05d.go", i)
	}
	return fmt.Sprintf("cfg/f%05d.yaml", i)
}

func (s boundsContent) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	n, ok := s.repos[repoID]
	if !ok {
		return s.FakePortContentStore.ListRepoFiles(ctx, repoID, limit)
	}
	if limit > 0 {
		n = min(n, limit)
	}
	files := make([]querycontract.FileContent, n)
	for i := range files {
		files[i] = querycontract.FileContent{RepoID: repoID, RelativePath: s.path(repoID, i)}
	}
	return files, nil
}

func (s boundsContent) GetFileContent(ctx context.Context, repoID, path string) (*querycontract.FileContent, error) {
	if _, ok := s.repos[repoID]; !ok {
		return s.FakePortContentStore.GetFileContent(ctx, repoID, path)
	}
	return &querycontract.FileContent{RepoID: repoID, RelativePath: path, Content: "k: v\n"}, nil
}

func (s boundsContent) GetFileContents(ctx context.Context, repoID string, paths []string) ([]querycontract.FileContent, error) {
	if _, ok := s.repos[repoID]; !ok {
		return s.FakePortContentStore.GetFileContents(ctx, repoID, paths)
	}
	files := make([]querycontract.FileContent, len(paths))
	for i, path := range paths {
		files[i] = querycontract.FileContent{RepoID: repoID, RelativePath: path, Content: "k: v\n"}
	}
	return files, nil
}

func (s boundsContent) NonBlankRepoFilePaths(ctx context.Context, repoID string, paths []string) ([]string, error) {
	if _, ok := s.repos[repoID]; !ok {
		return s.FakePortContentStore.NonBlankRepoFilePaths(ctx, repoID, paths)
	}
	return append([]string(nil), paths...), nil
}

// boundsHandler builds a Handler whose service workload has no graph
// deployment evidence, so the content fallback runs over anchorFiles files
// and the related repositories the graph names. Repositories in plain hold
// files that are not config candidates.
func boundsHandler(anchorFiles int, related map[string]int, plain ...string) *Handler {
	repos := map[string]int{boundsAnchorRepo: anchorFiles}
	ids := make([]string, 0, len(related))
	for id, n := range related {
		repos[id] = n
		ids = append(ids, id)
	}
	sort.Strings(ids)
	plainSet := map[string]bool{}
	for _, id := range plain {
		plainSet[id] = true
	}
	workload := map[string]any{
		"id":        boundsWorkloadID,
		"name":      boundsService,
		"kind":      "service",
		"repo_id":   boundsAnchorRepo,
		"repo_name": boundsService,
		"instances": []any{},
	}
	return &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "collect(DISTINCT dr.id) as defining"):
					return []map[string]any{workload}, nil
				case strings.Contains(cypher, "related:Repository"):
					rows := make([]map[string]any, len(ids))
					for i, id := range ids {
						rows[i] = map[string]any{"repo_id": id, "repo_name": "name-" + id}
					}
					return rows, nil
				case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
					return []map[string]any{{"repo_id": boundsAnchorRepo, "repo_name": boundsService}}, nil
				default:
					return nil, nil
				}
			},
			RunSingleByMatch: map[string]map[string]any{"MATCH (w:Workload)": workload},
		},
		Content: boundsContent{repos: repos, plain: plainSet},
	}
}

// boundsRoutes are the three routes that share the workload context payload
// the deployment-artifact load appends its bound reasons to. The workload
// story response carries partial_reasons and result_limits but no
// limitations key, so it is asserted on those two.
var boundsRoutes = []struct {
	name, path, param, value string
	hasLimitations           bool
}{
	{"service_context", "/api/v0/services/svc/context", "service_name", boundsService, true},
	{"workload_context", "/api/v0/workloads/workload-1/context", "workload_id", boundsWorkloadID, true},
	{"workload_story", "/api/v0/workloads/workload-1/story", "workload_id", boundsWorkloadID, false},
}

// boundsScenarios each trip one bound so a fold that misses a reason cannot
// hide behind another signal: the two list bounds use files that are not
// config candidates, so no artifact row (and no row cut) accompanies them.
var boundsScenarios = []struct {
	name        string
	anchorFiles int
	related     map[string]int
	plain       []string
	want        string
}{
	{
		name:        "anchor_list_cut",
		anchorFiles: 5001,
		plain:       []string{boundsAnchorRepo},
		want:        querycontract.DeploymentArtifactsAnchorFileListTruncatedReason,
	},
	{
		name:        "related_list_cut",
		anchorFiles: 10,
		related:     map[string]int{"repo-a": 5001},
		plain:       []string{"repo-a"},
		want:        querycontract.DeploymentArtifactsRelatedFileListTruncatedReason,
	},
	{
		name:        "budget_reached",
		anchorFiles: 10,
		related:     map[string]int{"repo-a": 5000, "repo-b": 5000, "repo-c": 5000},
		want:        querycontract.DeploymentArtifactsCandidatesBudgetReachedReason,
	},
}

// TestContextRoutesDiscloseTheDeploymentArtifactBounds drives the service
// context, workload context and workload story handlers over a service that
// trips one candidate bound at a time (#7951). The reason must reach
// partial_reasons (and limitations where the route emits it), and
// result_limits.truncated must be true beside it, so a response never reads
// complete next to a reason that says it is a lower bound. The quiet leg
// proves no reason appears, and truncated stays false, when no bound bites.
func TestContextRoutesDiscloseTheDeploymentArtifactBounds(t *testing.T) {
	t.Parallel()

	for _, route := range boundsRoutes {
		for _, sc := range boundsScenarios {
			t.Run(route.name+"/"+sc.name, func(t *testing.T) {
				t.Parallel()
				data, _ := getOutlierJSON(t, boundsHandler(sc.anchorFiles, sc.related, sc.plain...), route.path, route.param, route.value)

				reasons := stringSlice(t, data["partial_reasons"])
				if !slices.Contains(reasons, sc.want) {
					t.Errorf("partial_reasons = %v, want %q", reasons, sc.want)
				}
				if route.hasLimitations {
					if limitations := stringSlice(t, data["limitations"]); !slices.Contains(limitations, sc.want) {
						t.Errorf("limitations = %v, want %q", limitations, sc.want)
					}
				}
				if limits := querycontract.MapValue(data, "result_limits"); !querycontract.BoolVal(limits, "truncated") {
					t.Errorf("result_limits.truncated = false next to partial_reasons %v, want true", reasons)
				}
			})
		}

		t.Run(route.name+"/quiet", func(t *testing.T) {
			t.Parallel()
			data, _ := getOutlierJSON(t, boundsHandler(10, map[string]int{"repo-a": 600}), route.path, route.param, route.value)

			reasons := stringSlice(t, data["partial_reasons"])
			for _, reason := range querycontract.DeploymentArtifactBoundReasons() {
				if slices.Contains(reasons, reason) {
					t.Errorf("partial_reasons = %v, want no %q when nothing bites", reasons, reason)
				}
			}
			if limits := querycontract.MapValue(data, "result_limits"); querycontract.BoolVal(limits, "truncated") {
				t.Errorf("result_limits.truncated = true with no bound biting (partial_reasons %v)", reasons)
			}
		})
	}
}
