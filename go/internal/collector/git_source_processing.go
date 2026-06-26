// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// discoverRepositories runs repo selection with telemetry instrumentation.
func (s *GitSource) discoverRepositories(ctx context.Context) (SelectionBatch, error) {
	if s.Tracer != nil {
		ctx, span := s.Tracer.Start(ctx, telemetry.SpanScopeAssign)
		defer span.End()

		start := time.Now()
		batch, err := s.Selector.SelectRepositories(ctx)
		duration := time.Since(start).Seconds()

		if s.Instruments != nil {
			s.Instruments.ScopeAssignDuration.Record(
				ctx, duration,
				metric.WithAttributes(
					telemetry.AttrCollectorKind("git"),
					telemetry.AttrSourceSystem("git"),
				),
			)
		}

		if s.Logger != nil && err == nil {
			logFn := s.Logger.InfoContext
			if len(batch.Repositories) == 0 {
				logFn = s.Logger.DebugContext
			}
			logFn(
				ctx, "collector discovery completed",
				log.CollectorKind("git"),
				slog.Int("repository_count", len(batch.Repositories)),
			)
		}

		return batch, err
	}

	return s.Selector.SelectRepositories(ctx)
}

// resolveRepositories converts selected repositories to absolute paths,
// computes the stable source run ID, and returns the repositories ordered
// largest-first by file count.
//
// Longest-first scheduling overlaps the slow giant-repo parse tail with the
// small-repo bulk instead of letting giants (which cluster at the end in
// discovery order) serialize at the end of collection. The per-repo file count
// is walked once here and reused for the small/large lane classification in
// startStream, so the walk is not repeated.
// The returned counts slice is aligned 1:1 with the returned (sorted)
// repositories, so startStream can classify the small/large lanes without
// walking each tree a second time.
func (s *GitSource) resolveRepositories(batch SelectionBatch) ([]SelectedRepository, []int, string, error) {
	resolved := make([]SelectedRepository, 0, len(batch.Repositories))
	paths := make([]string, 0, len(batch.Repositories))

	for _, repo := range batch.Repositories {
		absPath, err := filepath.Abs(repo.RepoPath)
		if err != nil {
			return nil, nil, "", fmt.Errorf("resolve selected repo path %q: %w", repo.RepoPath, err)
		}
		resolved = append(resolved, SelectedRepository{
			RepoPath:     absPath,
			RemoteURL:    repo.RemoteURL,
			IsDependency: repo.IsDependency,
			DisplayName:  repo.DisplayName,
			Language:     repo.Language,
			FileTargets:  append([]string(nil), repo.FileTargets...),
		})
		paths = append(paths, absPath)
	}

	// Order largest-first so the heaviest repos start before the small-repo
	// bulk and their long parses overlap rather than serialize at the tail.
	// The count is paired with each repo so the stable sort keeps repo and
	// count aligned; equal-count repos keep their input (discovery) order for
	// deterministic scheduling.
	type repoWithCount struct {
		repo  SelectedRepository
		count int
	}
	pairs := make([]repoWithCount, len(resolved))
	for i := range resolved {
		pairs[i] = repoWithCount{repo: resolved[i], count: countRepositoryFiles(resolved[i].RepoPath)}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		return pairs[i].count > pairs[j].count
	})
	counts := make([]int, len(pairs))
	for i := range pairs {
		resolved[i] = pairs[i].repo
		counts[i] = pairs[i].count
	}

	sourceRunID := facts.StableID(
		"GitCollectorSnapshotRun",
		map[string]any{
			"component":             s.componentName(),
			"observed_at":           batch.ObservedAt.UTC().Format(time.RFC3339Nano),
			"selected_repositories": paths,
		},
	)

	return resolved, counts, sourceRunID, nil
}

// processRepo snapshots a single repository, invokes the afterSnapshot
// callback (used to release the large-repo semaphore between snapshot and
// stream send), and sends the result downstream. The semaphore lifecycle
// is managed by the caller, not by processRepo.
func (s *GitSource) processRepo(
	ctx context.Context,
	repo SelectedRepository,
	afterSnapshot func(),
	sourceRunID string,
	observedAt time.Time,
	workerID int,
	errOnce *sync.Once,
	firstErr *error, //nolint:gocritic // ptrToRefParam: shared error pointer across the snapshotter goroutines is the documented capture pattern.
	cancel context.CancelFunc,
	completed *atomic.Int64,
) {
	gen, err := s.snapshotOneRepository(ctx, repo, sourceRunID, observedAt, workerID)

	// Release semaphore (if held) after snapshot completes but before the
	// potentially-blocking stream send. This lets another worker start a
	// large repo while this worker waits for buffer space.
	if afterSnapshot != nil {
		afterSnapshot()
	}

	if err != nil {
		if s.Instruments != nil {
			s.Instruments.ReposSnapshotted.Add(
				ctx, 1,
				metric.WithAttributes(attribute.String("status", "failed")),
			)
		}
		errOnce.Do(func() {
			*firstErr = err
			cancel()
		})
		return
	}

	completed.Add(1)

	select {
	case s.stream <- gen:
	case <-ctx.Done():
	}
}

// snapshotOneRepository processes a single repository snapshot and returns a
// CollectedGeneration. This method records telemetry and handles all the
// snapshot-to-generation conversion logic.
func (s *GitSource) snapshotOneRepository(
	ctx context.Context,
	repository SelectedRepository,
	sourceRunID string,
	observedAt time.Time,
	workerID int,
) (CollectedGeneration, error) {
	var span trace.Span
	if s.Tracer != nil {
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanFactEmit)
		defer span.End()
	}

	start := time.Now()
	snapshot, err := s.Snapshotter.SnapshotRepository(ctx, repository)
	if err != nil {
		return CollectedGeneration{}, fmt.Errorf("snapshot repository %q: %w", repository.RepoPath, err)
	}

	repoPath := repository.RepoPath
	if snapshot.RepoPath == "" {
		snapshot.RepoPath = repoPath
	}
	if snapshot.RemoteURL == "" {
		snapshot.RemoteURL = repository.RemoteURL
	}
	if len(snapshot.GitRefs) == 0 {
		snapshot.GitRefs = cloneGitRefs(repository.GitRefs)
	}

	repositoryName := repository.DisplayName
	if strings.TrimSpace(repositoryName) == "" {
		repositoryName = filepath.Base(repoPath)
	}

	metadata, err := repositoryidentity.MetadataFor(
		repositoryName,
		repoPath,
		repository.RemoteURL,
	)
	if err != nil {
		return CollectedGeneration{}, fmt.Errorf("build repository metadata for %q: %w", repoPath, err)
	}

	generation := buildStreamingGenerationWithContext(
		ctx,
		repoPath,
		metadata,
		sourceRunID,
		observedAt,
		snapshot,
		repository.IsDependency,
	)
	enrichDiscoveryAdvisoryRun(
		generation.DiscoveryAdvisory,
		s.componentName(),
		metadata.ID,
		sourceRunID,
		generation.Scope.ScopeID,
		generation.Generation.GenerationID,
	)

	duration := time.Since(start).Seconds()
	scopeID := generation.Scope.ScopeID
	factCount := generation.FactCount
	sizeTier := s.repoSizeTier(snapshot.FileCount)

	// Record metrics. The per-repo duration carries the bounded repo_size_tier
	// dimension so an operator can slice the giant-repo cost signal by size
	// without the unbounded cardinality of a raw file_count label; the exact
	// file_count is recorded on the structured completion log below.
	if s.Instruments != nil {
		s.Instruments.RepoSnapshotDuration.Record(
			ctx, duration,
			metric.WithAttributes(
				telemetry.AttrScopeID(scopeID),
				telemetry.AttrRepoSizeTier(sizeTier),
			),
		)
		s.Instruments.ReposSnapshotted.Add(
			ctx, 1,
			metric.WithAttributes(attribute.String("status", "succeeded")),
		)
		s.Instruments.FactEmitDuration.Record(
			ctx, duration,
			metric.WithAttributes(
				telemetry.AttrCollectorKind("git"),
				telemetry.AttrSourceSystem("git"),
				telemetry.AttrScopeID(scopeID),
			),
		)
		s.Instruments.FactsEmitted.Add(
			ctx, int64(factCount),
			metric.WithAttributes(
				telemetry.AttrCollectorKind("git"),
				telemetry.AttrSourceSystem("git"),
				telemetry.AttrScopeID(scopeID),
			),
		)
	}

	// Log completion
	if s.Logger != nil {
		logAttrs := []any{
			log.CollectorKind("git"),
			log.RepoPath(repoPath),
			slog.Int("file_count", snapshot.FileCount),
			slog.Int("fact_count", factCount),
		}
		if workerID > 0 {
			logAttrs = append(logAttrs, log.WorkerID(fmt.Sprintf("%d", workerID)))
		}
		s.Logger.InfoContext(ctx, "collector snapshot completed", logAttrs...)
	}

	return generation, nil
}

func (s *GitSource) componentName() string {
	if s.Component == "" {
		return "collector-git"
	}
	return s.Component
}

// defaultLargeRepoThreshold is the file-count boundary between the small and
// large scheduling lanes when LargeRepoThreshold is unset.
const defaultLargeRepoThreshold = 500

// largeRepoThreshold returns the configured large-repo file-count threshold,
// falling back to the default when unset. Shared by lane classification and the
// repo_size_tier telemetry dimension so both agree on the boundary.
func (s *GitSource) largeRepoThreshold() int {
	if s.LargeRepoThreshold <= 0 {
		return defaultLargeRepoThreshold
	}
	return s.LargeRepoThreshold
}

// repoSizeTier maps a repository file count to the bounded "small"/"large"
// telemetry dimension using the same threshold as lane classification.
func (s *GitSource) repoSizeTier(fileCount int) string {
	if fileCount > s.largeRepoThreshold() {
		return "large"
	}
	return "small"
}

func buildScope(repo repositoryidentity.Metadata) scope.IngestionScope {
	metadata := map[string]string{
		"repo_id":    repo.ID,
		"repo_name":  repo.Name,
		"source_key": repo.ID,
	}
	if repo.RepoSlug != "" {
		metadata["repo_slug"] = repo.RepoSlug
	}
	if repo.RemoteURL != "" {
		metadata["remote_url"] = repo.RemoteURL
	}
	if repo.LocalPath != "" {
		metadata["local_path"] = repo.LocalPath
	}

	return scope.IngestionScope{
		ScopeID:       "git-repository-scope:" + repo.ID,
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  repo.ID,
		Metadata:      metadata,
	}
}

// countRepositoryFiles walks the repository tree once and returns the number of
// regular files that contribute to the real parse. Directories that never
// contribute (.git, node_modules, vendor, etc.) are skipped for speed. The full
// count (no early bail) is used both to order repositories largest-first and to
// classify them into the small/large scheduling lanes.
func countRepositoryFiles(repoPath string) int {
	count := 0
	_ = filepath.WalkDir(repoPath, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".venv", "__pycache__":
				return filepath.SkipDir
			}
			return nil
		}
		count++
		return nil
	})
	return count
}

// isLargeRepository classifies a repository as "large" (above the threshold)
// and returns the file count it walked, so callers that need the exact count
// (largest-first scheduling) do not have to walk the tree a second time.
func isLargeRepository(repoPath string, threshold int) (bool, int) {
	count := countRepositoryFiles(repoPath)
	return count > threshold, count
}

func buildGeneration(
	scopeID string,
	sourceRunID string,
	repoPath string,
	observedAt time.Time,
	freshnessHint string,
	sourceCommitSHA string,
	isDelta bool,
) scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: facts.StableID(
			"GitRepositorySnapshot",
			map[string]any{
				"repo_path":     repoPath,
				"source_run_id": sourceRunID,
			},
		),
		ScopeID:         scopeID,
		ObservedAt:      observedAt,
		IngestedAt:      observedAt,
		Status:          scope.GenerationStatusPending,
		TriggerKind:     scope.TriggerKindSnapshot,
		FreshnessHint:   freshnessHint,
		SourceCommitSHA: sourceCommitSHA,
		IsDelta:         isDelta,
	}
}
