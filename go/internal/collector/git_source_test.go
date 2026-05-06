package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestGitSourceNextBuildsCollectedGenerationFromSelectionAndPerRepoSnapshots(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC)
	firstRepoPath := t.TempDir()
	secondRepoPath := t.TempDir()
	selector := &stubRepositorySelector{
		batches: []SelectionBatch{{
			ObservedAt: observedAt,
			Repositories: []SelectedRepository{
				{
					RepoPath:  firstRepoPath,
					RemoteURL: "https://github.com/example/service-one",
				},
				{
					RepoPath:  secondRepoPath,
					RemoteURL: "https://github.com/example/service-two",
				},
			},
		}},
	}
	snapshotter := &stubRepositorySnapshotter{
		snapshots: map[string]RepositorySnapshot{
			firstRepoPath: {
				RepoPath:  firstRepoPath,
				RemoteURL: "https://github.com/example/service",
				FileCount: 1,
				ImportsMap: map[string][]string{
					"Worker": {firstRepoPath + "/app.py"},
				},
				FileData: []map[string]any{{
					"path": firstRepoPath + "/app.py",
					"lang": "python",
					"functions": []any{
						map[string]any{
							"name":        "handler",
							"line_number": 1,
							"uid":         "content-entity:e_fn123456789",
						},
					},
				}},
				ContentFiles: []ContentFileSnapshot{{
					RelativePath: "app.py",
					Body:         "def handler():\n    return 1\n",
					Digest:       "digest-1",
					Language:     "python",
				}},
				ContentEntities: []ContentEntitySnapshot{{
					EntityID:     "content-entity:e_fn123456789",
					RelativePath: "app.py",
					EntityType:   "Function",
					EntityName:   "handler",
					StartLine:    1,
					EndLine:      2,
					Language:     "python",
					SourceCache:  "def handler():\n    return 1\n",
					IndexedAt:    observedAt,
					Metadata: map[string]any{
						"docstring":  "Handles requests.",
						"decorators": []string{"@cached"},
					},
				}},
			},
			secondRepoPath: {
				RepoPath:  secondRepoPath,
				RemoteURL: "https://github.com/example/service-two",
				FileCount: 0,
			},
		},
	}

	source := &GitSource{
		Component:   "collector-git",
		Selector:    selector,
		Snapshotter: snapshotter,
	}

	// Collect both generations — order is non-deterministic with concurrent workers.
	collected1, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next(1) error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next(1) ok = false, want true")
	}
	collected2, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next(2) error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next(2) ok = false, want true")
	}

	facts1 := drainFactChannel(collected1.Facts)
	facts2 := drainFactChannel(collected2.Facts)

	// Identify which result is the full repo (6 facts) vs empty repo (3 facts).
	var fullCollected CollectedGeneration
	var fullFacts []facts.Envelope
	var emptyFacts []facts.Envelope
	switch {
	case len(facts1) == 11 && len(facts2) == 8:
		fullCollected = collected1
		fullFacts = facts1
		emptyFacts = facts2
	case len(facts1) == 8 && len(facts2) == 11:
		fullCollected = collected2
		fullFacts = facts2
		emptyFacts = facts1
	default:
		t.Fatalf("unexpected fact counts: %d and %d, want 11 and 8", len(facts1), len(facts2))
	}

	// Validate common scope/generation fields on the full repo.
	if got, want := fullCollected.Scope.SourceSystem, "git"; got != want {
		t.Fatalf("Scope.SourceSystem = %q, want %q", got, want)
	}
	if got, want := string(fullCollected.Scope.ScopeKind), "repository"; got != want {
		t.Fatalf("Scope.ScopeKind = %q, want %q", got, want)
	}
	if got, want := string(fullCollected.Generation.TriggerKind), "snapshot"; got != want {
		t.Fatalf("Generation.TriggerKind = %q, want %q", got, want)
	}

	wantKinds := []string{
		"repository",
		"file",
		"content",
		"content_entity",
		"shared_followup",
		"shared_followup",
		"shared_followup",
		"shared_followup",
		"shared_followup",
		"shared_followup",
		"shared_followup",
	}
	for i, want := range wantKinds {
		if got := fullFacts[i].FactKind; got != want {
			t.Fatalf("Facts[%d].FactKind = %q, want %q", i, got, want)
		}
	}

	repositoryFact := fullFacts[0]
	if _, ok := repositoryFact.Payload["source_run_id"].(string); !ok {
		t.Fatalf("repository fact source_run_id = %#v, want non-empty string", repositoryFact.Payload["source_run_id"])
	}
	importsMap, ok := repositoryFact.Payload["imports_map"].(map[string][]string)
	if !ok {
		t.Fatalf("repository fact imports_map = %#v, want map[string][]string", repositoryFact.Payload["imports_map"])
	}
	if got, want := importsMap["Worker"][0], firstRepoPath+"/app.py"; got != want {
		t.Fatalf("repository fact imports_map Worker path = %q, want %q", got, want)
	}

	fileFact := fullFacts[1]
	if got, want := fileFact.Payload["relative_path"], "app.py"; got != want {
		t.Fatalf("file fact relative_path = %#v, want %#v", got, want)
	}
	if _, ok := fileFact.Payload["parsed_file_data"]; !ok {
		t.Fatal("file fact parsed_file_data missing, want present")
	}

	entityFact := fullFacts[3]
	if got, want := entityFact.Payload["entity_id"], "content-entity:e_fn123456789"; got != want {
		t.Fatalf("entity fact entity_id = %#v, want %#v", got, want)
	}
	if got, want := entityFact.Payload["entity_type"], "Function"; got != want {
		t.Fatalf("entity fact entity_type = %#v, want %#v", got, want)
	}
	if got, want := entityFact.Payload["source_cache"], "def handler():\n    return 1\n"; got != want {
		t.Fatalf("entity fact source_cache = %#v, want %#v", got, want)
	}
	entityMetadata, ok := entityFact.Payload["entity_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("entity fact entity_metadata = %T, want map[string]any", entityFact.Payload["entity_metadata"])
	}
	if got, want := entityMetadata["docstring"], "Handles requests."; got != want {
		t.Fatalf("entity fact entity_metadata[docstring] = %#v, want %#v", got, want)
	}

	for _, fact := range emptyFacts[1:] {
		if fact.FactKind != "shared_followup" {
			t.Fatalf("empty repo followup kind = %q, want shared_followup", fact.FactKind)
		}
	}
	followupDomains := make(map[string]struct{}, len(emptyFacts)-1)
	for _, fact := range emptyFacts[1:] {
		domain, _ := fact.Payload["reducer_domain"].(string)
		if domain != "" {
			followupDomains[domain] = struct{}{}
		}
	}
	for _, wantDomain := range []string{
		"workload_identity",
		"deployable_unit_correlation",
		"workload_materialization",
		"code_call_materialization",
		"deployment_mapping",
		"sql_relationship_materialization",
		"inheritance_materialization",
	} {
		if _, ok := followupDomains[wantDomain]; !ok {
			t.Fatalf("empty repo followups missing %s domain: %#v", wantDomain, emptyFacts[1:])
		}
	}

	// Validate empty repo has repo + 7 followups.
	if got, want := len(emptyFacts), 8; got != want {
		t.Fatalf("len(empty facts) = %d, want %d", got, want)
	}

	if got, want := selector.calls, 1; got != want {
		t.Fatalf("selector calls = %d, want %d", got, want)
	}
	gotCalls := make([]string, len(snapshotter.calls))
	copy(gotCalls, snapshotter.calls)
	sort.Strings(gotCalls)
	wantCalls := []string{firstRepoPath, secondRepoPath}
	sort.Strings(wantCalls)
	if !equalStrings(gotCalls, wantCalls) {
		t.Fatalf("snapshotter calls = %v, want %v (order-independent)", snapshotter.calls, wantCalls)
	}
}

func TestGitSourceNextReturnsEmptyWhenSelectionBatchIsEmpty(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	source := &GitSource{
		Component: "collector-git",
		Logger:    slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt:   time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC),
				Repositories: nil,
			}},
		},
		Snapshotter: &stubRepositorySnapshotter{snapshots: map[string]RepositorySnapshot{}},
	}

	_, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if ok {
		t.Fatal("Next() ok = true, want false")
	}

	logOutput := logs.String()
	if strings.Contains(logOutput, `"level":"INFO"`) {
		t.Fatalf("empty-selection logs contain INFO entries: %s", logOutput)
	}
	if !strings.Contains(logOutput, `"msg":"collector stream: no repositories discovered"`) {
		t.Fatalf("empty-selection logs = %q, want no-repositories entry", logOutput)
	}
}

func TestGitSourceNextEmitsDependencyOwnershipForDependencyRepositories(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC)
	repoPath := t.TempDir()
	source := &GitSource{
		Component: "bootstrap-index",
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt: observedAt,
				Repositories: []SelectedRepository{
					{
						RepoPath:     repoPath,
						IsDependency: true,
						DisplayName:  "@scope/service-lib",
						Language:     "typescript",
					},
				},
			}},
		},
		Snapshotter: &stubRepositorySnapshotter{
			snapshots: map[string]RepositorySnapshot{
				repoPath: {
					RepoPath:  repoPath,
					FileCount: 1,
					FileData: []map[string]any{{
						"path":          repoPath + "/index.ts",
						"lang":          "typescript",
						"is_dependency": true,
						"functions":     []any{},
						"classes":       []any{},
						"variables":     []any{},
						"imports":       []any{},
					}},
				},
			},
		},
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	if got, want := collected.Scope.Metadata["repo_name"], "@scope/service-lib"; got != want {
		t.Fatalf("scope repo_name = %q, want %q", got, want)
	}

	facts := drainFactChannel(collected.Facts)

	if got, want := facts[0].Payload["is_dependency"], true; got != want {
		t.Fatalf("repository fact is_dependency = %#v, want %#v", got, want)
	}
	if got, want := facts[1].Payload["is_dependency"], true; got != want {
		t.Fatalf("file fact is_dependency = %#v, want %#v", got, want)
	}
}

func TestGitSourceNextDoesNotBufferPartialResultsWhenSnapshottingFails(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC)
	firstRepoPath := t.TempDir()
	secondRepoPath := t.TempDir()
	source := &GitSource{
		Component: "collector-git",
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt: observedAt,
				Repositories: []SelectedRepository{
					{RepoPath: firstRepoPath, RemoteURL: "https://github.com/example/one"},
					{RepoPath: secondRepoPath, RemoteURL: "https://github.com/example/two"},
				},
			}},
		},
		Snapshotter: &stubRepositorySnapshotter{
			snapshots: map[string]RepositorySnapshot{
				firstRepoPath: {RepoPath: firstRepoPath, FileCount: 1},
			},
			errForRepoPath: map[string]error{
				secondRepoPath: errors.New("boom"),
			},
		},
	}

	// With streaming, the first repo may succeed before the second fails.
	// Drain all results and verify at least one error surfaces.
	var gotErr error
	for {
		_, ok, err := source.Next(context.Background())
		if err != nil {
			gotErr = err
			break
		}
		if !ok {
			break
		}
	}
	if gotErr == nil {
		t.Fatal("expected error from failing snapshot, got nil")
	}
}

type stubRepositorySelector struct {
	batches []SelectionBatch
	calls   int
}

func (s *stubRepositorySelector) SelectRepositories(context.Context) (SelectionBatch, error) {
	if s.calls >= len(s.batches) {
		s.calls++
		return SelectionBatch{}, nil
	}

	batch := s.batches[s.calls]
	s.calls++
	return batch, nil
}

type stubRepositorySnapshotter struct {
	snapshots      map[string]RepositorySnapshot
	errForRepoPath map[string]error
	calls          []string
	mu             sync.Mutex
}

func (s *stubRepositorySnapshotter) SnapshotRepository(
	_ context.Context,
	repository SelectedRepository,
) (RepositorySnapshot, error) {
	s.mu.Lock()
	s.calls = append(s.calls, repository.RepoPath)
	s.mu.Unlock()

	if err := s.errForRepoPath[repository.RepoPath]; err != nil {
		return RepositorySnapshot{}, err
	}
	snapshot, ok := s.snapshots[repository.RepoPath]
	if !ok {
		return RepositorySnapshot{}, nil
	}
	return snapshot, nil
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestBuildCollectedConcurrent(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC)
	repo1 := t.TempDir()
	repo2 := t.TempDir()
	repo3 := t.TempDir()

	snapshotter := &stubRepositorySnapshotter{
		snapshots: map[string]RepositorySnapshot{
			repo1: {
				RepoPath:  repo1,
				RemoteURL: "https://github.com/example/repo1",
				FileCount: 5,
			},
			repo2: {
				RepoPath:  repo2,
				RemoteURL: "https://github.com/example/repo2",
				FileCount: 3,
			},
			repo3: {
				RepoPath:  repo3,
				RemoteURL: "https://github.com/example/repo3",
				FileCount: 7,
			},
		},
	}

	source := &GitSource{
		Component:       "collector-git",
		SnapshotWorkers: 2,
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt: observedAt,
				Repositories: []SelectedRepository{
					{RepoPath: repo1, RemoteURL: "https://github.com/example/repo1"},
					{RepoPath: repo2, RemoteURL: "https://github.com/example/repo2"},
					{RepoPath: repo3, RemoteURL: "https://github.com/example/repo3"},
				},
			}},
		},
		Snapshotter: snapshotter,
	}

	// Collect all generations
	var collected []CollectedGeneration
	for {
		gen, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v, want nil", err)
		}
		if !ok {
			break
		}
		collected = append(collected, gen)
	}

	if got, want := len(collected), 3; got != want {
		t.Fatalf("collected count = %d, want %d", got, want)
	}

	// Verify all repos were snapshotted
	snapshotter.mu.Lock()
	snapshotCalls := len(snapshotter.calls)
	snapshotter.mu.Unlock()

	if got, want := snapshotCalls, 3; got != want {
		t.Fatalf("snapshot calls = %d, want %d", got, want)
	}

	// Verify each generation has the expected structure
	for _, gen := range collected {
		if gen.Scope.SourceSystem != "git" {
			t.Errorf("Scope.SourceSystem = %q, want %q", gen.Scope.SourceSystem, "git")
		}
		if string(gen.Scope.ScopeKind) != "repository" {
			t.Errorf("Scope.ScopeKind = %q, want %q", gen.Scope.ScopeKind, "repository")
		}
		facts := drainFactChannel(gen.Facts)
		if len(facts) < 2 {
			t.Errorf("Facts count = %d, want >= 2", len(facts))
		}
	}
}

func TestBuildCollectedConcurrentHandlesErrors(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC)
	repo1 := t.TempDir()
	repo2 := t.TempDir()

	snapshotter := &stubRepositorySnapshotter{
		snapshots: map[string]RepositorySnapshot{
			repo1: {RepoPath: repo1, FileCount: 1},
		},
		errForRepoPath: map[string]error{
			repo2: errors.New("snapshot failed"),
		},
	}

	source := &GitSource{
		Component:       "collector-git",
		SnapshotWorkers: 2,
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt: observedAt,
				Repositories: []SelectedRepository{
					{RepoPath: repo1},
					{RepoPath: repo2},
				},
			}},
		},
		Snapshotter: snapshotter,
	}

	// With streaming, the first repo may succeed before the second fails.
	var gotErr error
	for {
		_, ok, err := source.Next(context.Background())
		if err != nil {
			gotErr = err
			break
		}
		if !ok {
			break
		}
	}
	if gotErr == nil {
		t.Fatal("expected error from failing snapshot, got nil")
	}
}

func TestGitSourceStreamingSequentialFallback(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC)
	repo1 := t.TempDir()
	repo2 := t.TempDir()

	source := &GitSource{
		Component:       "collector-git",
		SnapshotWorkers: 0, // sequential fallback
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt: observedAt,
				Repositories: []SelectedRepository{
					{RepoPath: repo1, RemoteURL: "https://github.com/example/repo1"},
					{RepoPath: repo2, RemoteURL: "https://github.com/example/repo2"},
				},
			}},
		},
		Snapshotter: &stubRepositorySnapshotter{
			snapshots: map[string]RepositorySnapshot{
				repo1: {RepoPath: repo1, FileCount: 3},
				repo2: {RepoPath: repo2, FileCount: 5},
			},
		},
	}

	var collected []CollectedGeneration
	for {
		gen, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v, want nil", err)
		}
		if !ok {
			break
		}
		collected = append(collected, gen)
	}

	if got, want := len(collected), 2; got != want {
		t.Fatalf("collected count = %d, want %d", got, want)
	}
}

func TestGitSourceStreamingCancellation(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC)
	repos := make([]SelectedRepository, 20)
	snapshots := make(map[string]RepositorySnapshot)
	for i := range repos {
		dir := t.TempDir()
		repos[i] = SelectedRepository{RepoPath: dir}
		snapshots[dir] = RepositorySnapshot{RepoPath: dir, FileCount: 1}
	}

	source := &GitSource{
		Component:       "collector-git",
		SnapshotWorkers: 4,
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt:   observedAt,
				Repositories: repos,
			}},
		},
		Snapshotter: &stubRepositorySnapshotter{snapshots: snapshots},
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Read one generation, then cancel
	_, ok, err := source.Next(ctx)
	if err != nil {
		t.Fatalf("first Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("first Next() ok = false, want true")
	}

	cancel()

	// Subsequent calls should return context.Canceled or drain cleanly
	for {
		_, ok, err := source.Next(ctx)
		if err != nil {
			break // expected: context cancelled
		}
		if !ok {
			break
		}
	}
}

func TestIsLargeRepositoryReturnsTrueAboveThreshold(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for i := 0; i < 600; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%d.py", i)), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if !isLargeRepository(dir, 500) {
		t.Fatal("isLargeRepository = false, want true for 600 files with threshold 500")
	}
}

func TestIsLargeRepositoryReturnsFalseAtOrBelowThreshold(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for i := 0; i < 100; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%d.py", i)), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if isLargeRepository(dir, 500) {
		t.Fatal("isLargeRepository = true, want false for 100 files with threshold 500")
	}
}

func TestIsLargeRepositorySkipsGitDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// 10 real files
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%d.py", i)), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// .git dir with 1000 files — should be skipped
	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if err := os.WriteFile(filepath.Join(gitDir, fmt.Sprintf("obj_%d", i)), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if isLargeRepository(dir, 500) {
		t.Fatal("isLargeRepository = true, want false — .git directory should be skipped")
	}
}

func TestIsLargeRepositorySkipsNodeModules(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%d.js", i)), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	nmDir := filepath.Join(dir, "node_modules")
	if err := os.Mkdir(nmDir, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2000; i++ {
		if err := os.WriteFile(filepath.Join(nmDir, fmt.Sprintf("dep_%d.js", i)), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if isLargeRepository(dir, 500) {
		t.Fatal("isLargeRepository = true, want false — node_modules should be skipped")
	}
}

func TestLargeRepoSemaphoreLimitsConcurrency(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC)

	// Create 4 repos, each with enough files to be "large" at threshold=1.
	repos := make([]SelectedRepository, 4)
	snapshots := make(map[string]RepositorySnapshot)
	for i := range repos {
		dir := t.TempDir()
		// Write 5 files so each repo exceeds threshold=1.
		for j := 0; j < 5; j++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f_%d.py", j)), []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		repos[i] = SelectedRepository{RepoPath: dir, RemoteURL: "https://github.com/example/repo"}
		snapshots[dir] = RepositorySnapshot{RepoPath: dir, FileCount: 5}
	}

	var maxConcurrent atomic.Int64
	var current atomic.Int64

	trackingSnapshotter := &concurrencyTrackingSnapshotter{
		inner:         &stubRepositorySnapshotter{snapshots: snapshots},
		current:       &current,
		maxConcurrent: &maxConcurrent,
		delay:         50 * time.Millisecond,
	}

	source := &GitSource{
		Component:              "collector-git",
		SnapshotWorkers:        4,
		LargeRepoThreshold:     1, // all repos are "large"
		LargeRepoMaxConcurrent: 2,
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt:   observedAt,
				Repositories: repos,
			}},
		},
		Snapshotter: trackingSnapshotter,
	}

	for {
		gen, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !ok {
			break
		}
		drainFactChannel(gen.Facts)
	}

	if got := maxConcurrent.Load(); got > 2 {
		t.Fatalf("max concurrent large repo snapshots = %d, want <= 2", got)
	}
}

func TestSmallReposBypassSemaphore(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC)

	// Create 4 repos, each with 0 files — all small (below any threshold).
	repos := make([]SelectedRepository, 4)
	snapshots := make(map[string]RepositorySnapshot)
	for i := range repos {
		dir := t.TempDir()
		repos[i] = SelectedRepository{RepoPath: dir, RemoteURL: "https://github.com/example/repo"}
		snapshots[dir] = RepositorySnapshot{RepoPath: dir, FileCount: 0}
	}

	var maxConcurrent atomic.Int64
	var current atomic.Int64

	trackingSnapshotter := &concurrencyTrackingSnapshotter{
		inner:         &stubRepositorySnapshotter{snapshots: snapshots},
		current:       &current,
		maxConcurrent: &maxConcurrent,
		delay:         50 * time.Millisecond,
	}

	source := &GitSource{
		Component:              "collector-git",
		SnapshotWorkers:        4,
		LargeRepoThreshold:     500, // all repos are small
		LargeRepoMaxConcurrent: 1,   // sem=1, but small repos should bypass
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt:   observedAt,
				Repositories: repos,
			}},
		},
		Snapshotter: trackingSnapshotter,
	}

	for {
		gen, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !ok {
			break
		}
		drainFactChannel(gen.Facts)
	}

	// With 4 workers and 50ms delay, all 4 small repos should run concurrently.
	if got := maxConcurrent.Load(); got < 2 {
		t.Fatalf("max concurrent small repo snapshots = %d, want >= 2 (semaphore should not block)", got)
	}
}

// concurrencyTrackingSnapshotter wraps a snapshotter and tracks the maximum
// number of concurrent SnapshotRepository calls.
type concurrencyTrackingSnapshotter struct {
	inner         RepositorySnapshotter
	current       *atomic.Int64
	maxConcurrent *atomic.Int64
	delay         time.Duration
}

func (s *concurrencyTrackingSnapshotter) SnapshotRepository(
	ctx context.Context,
	repository SelectedRepository,
) (RepositorySnapshot, error) {
	cur := s.current.Add(1)
	for {
		old := s.maxConcurrent.Load()
		if cur <= old || s.maxConcurrent.CompareAndSwap(old, cur) {
			break
		}
	}

	time.Sleep(s.delay)

	s.current.Add(-1)
	return s.inner.SnapshotRepository(ctx, repository)
}

func TestTwoLaneSmallReposFlowWhileLargeBlocked(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC)

	// Create 4 large repos (5 files each, threshold=1) interleaved with 4 small
	// repos (0 files). The large snapshotter sleeps 200ms; small sleeps 10ms.
	// With sem=1, only 1 large repo parses at a time (4 * 200ms = 800ms minimum).
	// All 4 small repos should complete in ~10-40ms total — well before large.
	var repos []SelectedRepository
	snapshots := make(map[string]RepositorySnapshot)
	largeDelay := make(map[string]time.Duration)

	for i := 0; i < 4; i++ {
		// Large repo
		largeDir := t.TempDir()
		for j := 0; j < 5; j++ {
			if err := os.WriteFile(filepath.Join(largeDir, fmt.Sprintf("f_%d.py", j)), []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		repos = append(repos, SelectedRepository{RepoPath: largeDir, RemoteURL: "https://github.com/example/repo"})
		snapshots[largeDir] = RepositorySnapshot{RepoPath: largeDir, FileCount: 5}
		largeDelay[largeDir] = 200 * time.Millisecond

		// Small repo (interleaved)
		smallDir := t.TempDir()
		repos = append(repos, SelectedRepository{RepoPath: smallDir, RemoteURL: "https://github.com/example/repo"})
		snapshots[smallDir] = RepositorySnapshot{RepoPath: smallDir, FileCount: 0}
		largeDelay[smallDir] = 10 * time.Millisecond
	}

	var completionOrder sync.Mutex
	var completedPaths []string

	orderSnapshotter := &orderTrackingSnapshotter{
		inner:     &stubRepositorySnapshotter{snapshots: snapshots},
		delays:    largeDelay,
		mu:        &completionOrder,
		completed: &completedPaths,
	}

	source := &GitSource{
		Component:              "collector-git",
		SnapshotWorkers:        4,
		LargeRepoThreshold:     1, // repos with >1 file are "large"
		LargeRepoMaxConcurrent: 1, // only 1 large at a time
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt:   observedAt,
				Repositories: repos,
			}},
		},
		Snapshotter: orderSnapshotter,
	}

	for {
		gen, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !ok {
			break
		}
		drainFactChannel(gen.Facts)
	}

	completionOrder.Lock()
	paths := append([]string(nil), completedPaths...)
	completionOrder.Unlock()

	if len(paths) != 8 {
		t.Fatalf("completed %d repos, want 8", len(paths))
	}

	// All 4 small repos (0 files, 10ms delay) should appear in the first 5
	// completions. If small repos were blocked behind large repos in a single
	// channel, they would interleave much later.
	smallCount := 0
	for _, p := range paths[:5] {
		if snapshots[p].FileCount == 0 {
			smallCount++
		}
	}
	if smallCount < 3 {
		t.Fatalf("only %d small repos in first 5 completions, want >= 3 (small repos should not be blocked by large)", smallCount)
	}
}

func TestTwoLaneDrainsLargeAfterSmallExhausted(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC)

	// 2 small repos + 3 large repos. All must complete.
	var repos []SelectedRepository
	snapshots := make(map[string]RepositorySnapshot)

	for i := 0; i < 2; i++ {
		dir := t.TempDir()
		repos = append(repos, SelectedRepository{RepoPath: dir, RemoteURL: "https://github.com/example/repo"})
		snapshots[dir] = RepositorySnapshot{RepoPath: dir, FileCount: 0}
	}
	for i := 0; i < 3; i++ {
		dir := t.TempDir()
		for j := 0; j < 5; j++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f_%d.py", j)), []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		repos = append(repos, SelectedRepository{RepoPath: dir, RemoteURL: "https://github.com/example/repo"})
		snapshots[dir] = RepositorySnapshot{RepoPath: dir, FileCount: 5}
	}

	source := &GitSource{
		Component:              "collector-git",
		SnapshotWorkers:        2,
		LargeRepoThreshold:     1,
		LargeRepoMaxConcurrent: 1,
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt:   observedAt,
				Repositories: repos,
			}},
		},
		Snapshotter: &stubRepositorySnapshotter{snapshots: snapshots},
	}

	var count int
	for {
		gen, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !ok {
			break
		}
		drainFactChannel(gen.Facts)
		count++
	}

	if count != 5 {
		t.Fatalf("completed %d repos, want 5 (all repos must drain)", count)
	}
}

// orderTrackingSnapshotter applies per-path delays and records completion order.
type orderTrackingSnapshotter struct {
	inner     RepositorySnapshotter
	delays    map[string]time.Duration
	mu        *sync.Mutex
	completed *[]string
}

func (s *orderTrackingSnapshotter) SnapshotRepository(
	ctx context.Context,
	repository SelectedRepository,
) (RepositorySnapshot, error) {
	if d, ok := s.delays[repository.RepoPath]; ok {
		time.Sleep(d)
	}
	snap, err := s.inner.SnapshotRepository(ctx, repository)
	s.mu.Lock()
	*s.completed = append(*s.completed, repository.RepoPath)
	s.mu.Unlock()
	return snap, err
}

func TestStreamBufferPreventsBackPressureStall(t *testing.T) {
	t.Parallel()

	// Scenario: 6 small repos (10ms each) with buffer=6 (matches workers).
	// Consumer deliberately delays reading to simulate slow downstream commit.
	// With buffer=1 this would stall workers; with buffer=workers they all
	// complete and buffer their results without blocking.
	observedAt := time.Date(2026, time.April, 14, 11, 0, 0, 0, time.UTC)

	const repoCount = 6
	var repos []SelectedRepository
	snapshots := make(map[string]RepositorySnapshot)
	delays := make(map[string]time.Duration)

	for i := 0; i < repoCount; i++ {
		dir := t.TempDir()
		repos = append(repos, SelectedRepository{RepoPath: dir, RemoteURL: "https://github.com/example/repo"})
		snapshots[dir] = RepositorySnapshot{RepoPath: dir, FileCount: 0}
		delays[dir] = 10 * time.Millisecond
	}

	var completionMu sync.Mutex
	var completedPaths []string

	source := &GitSource{
		Component:       "collector-git",
		SnapshotWorkers: 4,
		StreamBuffer:    4,
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt:   observedAt,
				Repositories: repos,
			}},
		},
		Snapshotter: &orderTrackingSnapshotter{
			inner:     &stubRepositorySnapshotter{snapshots: snapshots},
			delays:    delays,
			mu:        &completionMu,
			completed: &completedPaths,
		},
	}

	// Delay consuming the first result to simulate slow downstream commit.
	// With buffer=4, the first 4 workers should complete and buffer without
	// blocking, even though we haven't consumed any results yet.
	start := time.Now()
	time.Sleep(100 * time.Millisecond)

	var count int
	for {
		gen, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !ok {
			break
		}
		drainFactChannel(gen.Facts)
		count++
	}

	elapsed := time.Since(start)

	if count != repoCount {
		t.Fatalf("completed %d repos, want %d", count, repoCount)
	}

	// With buffer=4, the 100ms consumer delay should NOT cause total time
	// to exceed 500ms (it would if workers were serially blocked on buffer=1).
	if elapsed > 500*time.Millisecond {
		t.Fatalf("elapsed %v > 500ms — workers likely blocked on stream buffer", elapsed)
	}
}

func TestDefaultStreamBufferMatchesWorkerCount(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 14, 11, 0, 0, 0, time.UTC)
	dir := t.TempDir()

	source := &GitSource{
		Component:       "collector-git",
		SnapshotWorkers: 3,
		StreamBuffer:    0, // should default to worker count (3)
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt:   observedAt,
				Repositories: []SelectedRepository{{RepoPath: dir}},
			}},
		},
		Snapshotter: &stubRepositorySnapshotter{
			snapshots: map[string]RepositorySnapshot{
				dir: {RepoPath: dir, FileCount: 0},
			},
		},
	}

	gen, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatal("Next() returned ok=false, want generation")
	}
	drainFactChannel(gen.Facts)

	// Verify the stream channel has the expected capacity.
	// After consuming the one generation, the stream should have cap=3.
	if cap(source.stream) != 3 {
		t.Fatalf("stream capacity = %d, want 3 (should match worker count)", cap(source.stream))
	}
}

func TestGitSourceStreamResetsBetweenBatches(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC)
	repo1 := t.TempDir()
	repo2 := t.TempDir()

	source := &GitSource{
		Component:       "collector-git",
		SnapshotWorkers: 1,
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{
				{
					ObservedAt:   observedAt,
					Repositories: []SelectedRepository{{RepoPath: repo1}},
				},
				{
					ObservedAt:   observedAt.Add(time.Minute),
					Repositories: []SelectedRepository{{RepoPath: repo2}},
				},
			},
		},
		Snapshotter: &stubRepositorySnapshotter{
			snapshots: map[string]RepositorySnapshot{
				repo1: {RepoPath: repo1, FileCount: 1},
				repo2: {RepoPath: repo2, FileCount: 2},
			},
		},
	}

	// First batch: one repo
	gen1, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("batch1 Next() error = %v", err)
	}
	if !ok {
		t.Fatal("batch1 Next() ok = false, want true")
	}

	// Drain first batch
	_, ok, err = source.Next(context.Background())
	if err != nil {
		t.Fatalf("batch1 drain error = %v", err)
	}
	if ok {
		t.Fatal("batch1 should be exhausted")
	}

	// Second batch: should trigger fresh discovery
	gen2, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("batch2 Next() error = %v", err)
	}
	if !ok {
		t.Fatal("batch2 Next() ok = false, want true")
	}

	if gen1.Scope.ScopeID == gen2.Scope.ScopeID {
		t.Fatal("expected different scopes across batches")
	}
}
