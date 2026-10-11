// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"testing"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// budgetStore is an indexed in-memory ContentStore for the budget tests: it
// serves thousands of files per repository without a quadratic scan, and
// records the list limits, every path read or blank-checked, and the chunks.
type budgetStore struct {
	querycontract.ContentStore

	mu         sync.Mutex
	rows       map[string]map[string]string
	sorted     map[string][]string
	listLimits map[string][]int
	read       map[string]map[string]bool
	blanked    map[string]map[string]bool
	batchCalls int
}

func newBudgetStore() *budgetStore {
	return &budgetStore{
		rows:       map[string]map[string]string{},
		sorted:     map[string][]string{},
		listLimits: map[string][]int{},
		read:       map[string]map[string]bool{},
		blanked:    map[string]map[string]bool{},
	}
}

// add stores files for repoID; content keyed by path.
func (s *budgetStore) add(repoID string, files map[string]string) {
	if s.rows[repoID] == nil {
		s.rows[repoID] = map[string]string{}
	}
	for path, content := range files {
		s.rows[repoID][path] = content
	}
	paths := make([]string, 0, len(s.rows[repoID]))
	for path := range s.rows[repoID] {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	s.sorted[repoID] = paths
}

func (s *budgetStore) ListRepoFiles(_ context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listLimits[repoID] = append(s.listLimits[repoID], limit)
	paths := s.sorted[repoID]
	if limit > 0 && len(paths) > limit {
		paths = paths[:limit]
	}
	out := make([]querycontract.FileContent, len(paths))
	for i, path := range paths {
		out[i] = querycontract.FileContent{RepoID: repoID, RelativePath: path}
	}
	return out, nil
}

func (s *budgetStore) GetFileContents(_ context.Context, repoID string, paths []string) ([]querycontract.FileContent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batchCalls++
	if s.read[repoID] == nil {
		s.read[repoID] = map[string]bool{}
	}
	out := make([]querycontract.FileContent, 0, len(paths))
	for _, path := range paths {
		s.read[repoID][path] = true
		if content, ok := s.rows[repoID][path]; ok {
			out = append(out, querycontract.FileContent{RepoID: repoID, RelativePath: path, Content: content})
		}
	}
	return out, nil
}

func (s *budgetStore) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	return nil, nil
}

func (s *budgetStore) NonBlankRepoFilePaths(_ context.Context, repoID string, paths []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blanked[repoID] == nil {
		s.blanked[repoID] = map[string]bool{}
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		s.blanked[repoID][path] = true
		if strings.TrimSpace(s.rows[repoID][path]) != "" {
			out = append(out, path)
		}
	}
	return out, nil
}

// touched reports whether the load read or blank-checked path of repoID.
func (s *budgetStore) touched(repoID, path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read[repoID][path] || s.blanked[repoID][path]
}

// relatedGraph answers the related-repository read with the given ids in the
// given order, each named after its id; every other read returns no rows.
type relatedGraph struct{ ids []string }

func (g relatedGraph) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	if !strings.Contains(cypher, "related:Repository") {
		return nil, nil
	}
	rows := make([]map[string]any, len(g.ids))
	for i, id := range g.ids {
		rows[i] = map[string]any{"repo_id": id, "repo_name": "name-" + id}
	}
	return rows, nil
}

func (relatedGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// moduleFiles returns n Terraform files whose names sort in creation order;
// each yields one module row naming its index.
func moduleFiles(n int) map[string]string {
	files := make(map[string]string, n)
	for i := 0; i < n; i++ {
		files[modulePath(i)] = fmt.Sprintf("module \"m%d\" {\n  source = \"./mod%05d\"\n}\n", i, i)
	}
	return files
}

func modulePath(i int) string { return fmt.Sprintf("infra/m%05d/main.tf", i) }

// budgetLoad runs the deployment-artifact load with loader for an anchor with
// no files and the related ids in graph order.
func budgetLoad(ctx context.Context, t testing.TB, loader configArtifactLoader, store *budgetStore, ids []string) (map[string]any, ConfigArtifactReadStats) {
	t.Helper()
	overview, stats, err := loader.loadDeploymentArtifactOverviewWithStats(
		ctx, relatedGraph{ids: ids}, store, "repo-anchor", "anchor", []querycontract.FileContent{}, nil, nil,
	)
	if err != nil {
		t.Fatalf("load error = %v", err)
	}
	return overview, stats
}

func configPathRowsOf(overview map[string]any) []map[string]any {
	return querycontract.MapSliceValue(querycontract.MapValue(overview, "deployment_artifacts"), "config_paths")
}

func hasModuleRow(overview map[string]any, repoID string, i int) bool {
	want := fmt.Sprintf("mod%05d", i)
	for _, row := range configPathRowsOf(overview) {
		if querycontract.StringVal(row, "source_repo") == "name-"+repoID && strings.Contains(querycontract.StringVal(row, "path"), want) {
			return true
		}
	}
	return false
}

// plantedStopAtFloorLoad is the seeded violation of #7951 behind the loader
// seam: the per-repository load stops at the 50th candidate whatever the
// allowance, and reports only what it took as the pool, so no count or reason
// discloses the rest.
func plantedStopAtFloorLoad(
	ctx context.Context, reader contentartifacts.Reader, repo contentartifacts.Repository,
	files []contentartifacts.File, hasFileList bool, _ int,
) ([]map[string]any, contentartifacts.ReadStats, error) {
	rows, stats, err := contentartifacts.LoadConfigPaths(ctx, reader, repo, files, hasFileList, contentartifacts.HydrationFloor)
	stats.CandidateCount = stats.CandidatesHydrated
	return rows, stats, err
}

// windowViolations is ruling check 1 for loader: repositories of 51, 500,
// 5,000 and 5,001 candidates are hydrated wholly inside the window, a cut
// list carries the list reason and its file past the window is never read.
func windowViolations(t testing.TB, loader configArtifactLoader) []string {
	t.Helper()
	var violations []string
	for _, n := range []int{51, 500, contentartifacts.ListLimit, contentartifacts.ListLimit + 1} {
		store := newBudgetStore()
		store.add("repo-x", moduleFiles(n))
		overview, stats := budgetLoad(context.Background(), t, loader, store, []string{"repo-x"})
		window := min(n, contentartifacts.ListLimit)
		reasons := stats.BoundReasons()
		if stats.CandidateCount != window || stats.CandidatesHydrated != window || stats.CandidatesUnhydrated != 0 {
			violations = append(violations, fmt.Sprintf("%d: candidates %d hydrated %d unhydrated %d, want %d/%d/0",
				n, stats.CandidateCount, stats.CandidatesHydrated, stats.CandidatesUnhydrated, window, window))
		}
		if !hasModuleRow(overview, "repo-x", window-1) {
			violations = append(violations, fmt.Sprintf("%d: no row from candidate %d", n, window))
		}
		if want := (window + 9) / 10; stats.BatchStatements != want {
			violations = append(violations, fmt.Sprintf("%d: chunk statements %d, want ceil(%d/10) = %d", n, stats.BatchStatements, window, want))
		}
		if got := store.listLimits["repo-x"]; len(got) != 1 || got[0] != contentartifacts.ListLimit+1 {
			violations = append(violations, fmt.Sprintf("%d: list limits %v, want one list of %d", n, got, contentartifacts.ListLimit+1))
		}
		truncated := n > contentartifacts.ListLimit
		if truncated {
			if store.touched("repo-x", modulePath(n-1)) {
				violations = append(violations, fmt.Sprintf("%d: read the file past the window", n))
			}
			if stats.RepositoriesListTruncated != 1 || !containsReason(reasons, RelatedFileListTruncatedReason) {
				violations = append(violations, fmt.Sprintf("%d: list cut but truncated=%d reasons=%v", n, stats.RepositoriesListTruncated, reasons))
			}
		} else if len(reasons) != 0 || stats.RepositoriesListTruncated != 0 {
			violations = append(violations, fmt.Sprintf("%d: reasons %v truncated %d on a complete window", n, reasons, stats.RepositoriesListTruncated))
		}
	}
	return violations
}

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

// budgetFixture holds four related repositories whose candidate counts make
// a budget of 100 bite: repo-a 300, repo-b 120, repo-c 30, repo-d 200.
func budgetFixture() (*budgetStore, map[string]int) {
	counts := map[string]int{"repo-a": 300, "repo-b": 120, "repo-c": 30, "repo-d": 200}
	store := newBudgetStore()
	for id, n := range counts {
		store.add(id, moduleFiles(n))
	}
	return store, counts
}

// budgetViolations is ruling check 2: with floor 50 and an extra budget of
// 100 through the seam, every repository keeps its floor, the extra goes in
// RepoID order (repo-a takes all 100), the unhydrated count is exact, and the
// budget reason is present.
func budgetViolations(t testing.TB, loader configArtifactLoader) []string {
	t.Helper()
	var violations []string
	store, _ := budgetFixture()
	overview, stats := budgetLoad(context.Background(), t, loader, store, []string{"repo-d", "repo-b", "repo-a", "repo-c"})
	// repo-a 150 of 300, repo-b 50 of 120, repo-c 30 of 30, repo-d 50 of 200.
	want := map[string]int{"repo-a": 150, "repo-b": 50, "repo-c": 30, "repo-d": 50}
	for id, hydrated := range want {
		if !hasModuleRow(overview, id, hydrated-1) {
			violations = append(violations, fmt.Sprintf("%s: candidate %d not hydrated", id, hydrated))
		}
		if hasModuleRow(overview, id, hydrated) {
			violations = append(violations, fmt.Sprintf("%s: candidate %d hydrated past its allowance", id, hydrated+1))
		}
	}
	if stats.CandidateCount != 650 || stats.CandidatesHydrated != 280 || stats.CandidatesUnhydrated != 370 {
		violations = append(violations, fmt.Sprintf("counts %d/%d/%d, want 650/280/370", stats.CandidateCount, stats.CandidatesHydrated, stats.CandidatesUnhydrated))
	}
	if stats.RepositoriesBudgetCapped != 3 || stats.HydrationBudget != 100 || stats.HydrationFloor != 50 {
		violations = append(violations, fmt.Sprintf("capped %d budget %d floor %d, want 3/100/50", stats.RepositoriesBudgetCapped, stats.HydrationBudget, stats.HydrationFloor))
	}
	if reasons := stats.BoundReasons(); !containsReason(reasons, CandidatesBudgetReachedReason) || containsReason(reasons, RelatedFileListTruncatedReason) {
		violations = append(violations, fmt.Sprintf("reasons %v, want only %s", reasons, CandidatesBudgetReachedReason))
	}
	return violations
}

func TestRelatedRepositoryWindowsAreHydratedWholly(t *testing.T) {
	t.Parallel()
	if violations := windowViolations(t, productionConfigArtifactLoader); len(violations) > 0 {
		t.Fatalf("%d violations: %v", len(violations), violations)
	}
}

func TestCandidateBudgetBitesInRepoIDOrderAboveTheFloors(t *testing.T) {
	t.Parallel()
	loader := productionConfigArtifactLoader
	loader.budget = 100
	if violations := budgetViolations(t, loader); len(violations) > 0 {
		t.Fatalf("%d violations: %v", len(violations), violations)
	}
}

// TestCandidateBudgetChecksSeededPair is ruling check 7 at the loader: checks
// 1 and 2 pass on the production per-repository load and fail on the planted
// stop-at-50 load that drops the disclosure.
func TestCandidateBudgetChecksSeededPair(t *testing.T) {
	t.Parallel()
	planted := productionConfigArtifactLoader
	planted.load = plantedStopAtFloorLoad
	if violations := windowViolations(t, planted); len(violations) == 0 {
		t.Fatal("RED leg: check 1 passed on the planted stop-at-50 load")
	}
	planted.budget = 100
	if violations := budgetViolations(t, planted); len(violations) == 0 {
		t.Fatal("RED leg: check 2 passed on the planted stop-at-50 load")
	}
}

// statsCounts is the comparable part of the stats: every count, no duration.
func statsCounts(stats ConfigArtifactReadStats) ConfigArtifactReadStats {
	stats.ListDuration, stats.HydrateDuration, stats.ParseDuration = 0, 0, 0
	stats.BatchChunksInFlightMax = 0
	return stats
}

// TestCandidateBudgetBodyIsStableAcrossGraphOrder is ruling check 3: the same
// related set delivered in three shuffled orders gives byte-identical bodies
// and identical counts, with and without a budget bite.
func TestCandidateBudgetBodyIsStableAcrossGraphOrder(t *testing.T) {
	t.Parallel()
	ids := []string{"repo-a", "repo-b", "repo-c", "repo-d"}
	for _, budget := range []int{100, CandidateHydrationBudget} {
		loader := productionConfigArtifactLoader
		loader.budget = budget
		var firstBody string
		var firstStats ConfigArtifactReadStats
		for round := int64(0); round < 3; round++ {
			order := append([]string(nil), ids...)
			rand.New(rand.NewSource(7951+round)).Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
			store, _ := budgetFixture()
			overview, stats := budgetLoad(context.Background(), t, loader, store, order)
			body, err := json.Marshal(overview)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if round == 0 {
				firstBody, firstStats = string(body), statsCounts(stats)
				continue
			}
			if string(body) != firstBody {
				t.Fatalf("budget %d, order %v: body differs from the first order", budget, order)
			}
			if statsCounts(stats) != firstStats {
				t.Fatalf("budget %d, order %v: counts %+v differ from %+v", budget, order, statsCounts(stats), firstStats)
			}
		}
		if bit := firstStats.CandidatesUnhydrated > 0; bit != (budget == 100) {
			t.Fatalf("budget %d: bite = %v, want %v", budget, bit, budget == 100)
		}
	}
}

// TestCandidateCountsExcludeRepositoriesOutsideTheGrant is ruling check 10:
// the counts and reasons are computed after the access filter, so an
// out-of-grant repository with 200 candidates (and a cut list) adds nothing.
func TestCandidateCountsExcludeRepositoriesOutsideTheGrant(t *testing.T) {
	t.Parallel()
	store := newBudgetStore()
	store.add("repo-in", moduleFiles(80))
	store.add("repo-out", moduleFiles(200))
	store.add("repo-out-wide", moduleFiles(contentartifacts.ListLimit+1))
	ctx := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-anchor", "repo-in"},
	})
	loader := productionConfigArtifactLoader
	loader.budget = 10
	_, stats := budgetLoad(ctx, t, loader, store, []string{"repo-out", "repo-in", "repo-out-wide"})
	if stats.RelatedRepositoryCount != 1 {
		t.Fatalf("RelatedRepositoryCount = %d, want 1 inside the grant", stats.RelatedRepositoryCount)
	}
	if stats.CandidateCount != 80 || stats.CandidatesHydrated != 60 || stats.CandidatesUnhydrated != 20 {
		t.Fatalf("counts %d/%d/%d, want 80/60/20 from repo-in only", stats.CandidateCount, stats.CandidatesHydrated, stats.CandidatesUnhydrated)
	}
	if stats.RepositoriesBudgetCapped != 1 || stats.RepositoriesListTruncated != 0 {
		t.Fatalf("capped %d truncated %d, want 1/0", stats.RepositoriesBudgetCapped, stats.RepositoriesListTruncated)
	}
	for _, id := range []string{"repo-out", "repo-out-wide"} {
		if len(store.listLimits[id]) != 0 {
			t.Fatalf("listed %s, which is outside the grant", id)
		}
	}
}

// TestAllowanceWholeWindowIsCallableFromTheLoaderSeam keeps the derive's call
// shape: the per-repository function accepts AllowanceWholeWindow and hydrates
// a 600-candidate repository with no budget.
func TestAllowanceWholeWindowIsCallableFromTheLoaderSeam(t *testing.T) {
	t.Parallel()
	store := newBudgetStore()
	store.add("repo-x", moduleFiles(600))
	_, stats, err := contentartifacts.LoadConfigPaths(context.Background(), contentStoreReader{store: store},
		contentartifacts.Repository{ID: "repo-x", Name: "x"}, nil, false, contentartifacts.AllowanceWholeWindow)
	if err != nil || stats.CandidatesHydrated != 600 {
		t.Fatalf("whole-window load: err %v, hydrated %d, want 600", err, stats.CandidatesHydrated)
	}
}
