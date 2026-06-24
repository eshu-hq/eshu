package collector

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// NativeRepositorySelector owns Go-native repository selection and sync behavior.
type NativeRepositorySelector struct {
	Config            RepoSyncConfig
	Now               func() time.Time
	DiscoverSelection func(context.Context, RepoSyncConfig, string) (RepositorySelection, error)
	SyncFilesystem    func(context.Context, RepoSyncConfig, []string) ([]string, error)
	SyncGit           func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error)
	Logger            *slog.Logger
	// BaselineResolver supplies the last-projected commit per scope so git delta
	// syncs baseline on a durable commit instead of the local HEAD (epic #2340).
	// Nil disables the lookup and every git update takes a safe full snapshot.
	BaselineResolver DeltaBaselineResolver
	// Instruments records the delta-baseline fallback rate. Optional.
	Instruments *telemetry.Instruments
}

// SelectRepositories discovers changed repositories for one collector cycle.
func (s NativeRepositorySelector) SelectRepositories(
	ctx context.Context,
) (SelectionBatch, error) {
	if strings.TrimSpace(s.Config.SourceMode) == "" {
		return SelectionBatch{}, fmt.Errorf("repo sync source mode is required")
	}
	observedAt := s.now()
	token, err := resolveGitToken(ctx, s.Config)
	if err != nil && s.Config.SourceMode == "githubOrg" {
		return SelectionBatch{}, err
	}

	discoverSelectionFn := s.DiscoverSelection
	if discoverSelectionFn == nil {
		discoverSelectionFn = discoverSelection
	}
	selection, err := discoverSelectionFn(ctx, s.Config, token)
	if err != nil {
		return SelectionBatch{}, err
	}
	repositoryIDs := filterRepositoryIDsByShard(selection.RepositoryIDs, s.Config)
	if s.Config.RepoShardCount > 1 && s.Logger != nil {
		s.Logger.InfoContext(ctx, "collector repository shard selected",
			slog.String("collector_kind", "git"),
			slog.Int("repo_shard_count", s.Config.RepoShardCount),
			slog.Int("repo_shard_index", s.Config.RepoShardIndex),
			slog.Int("repository_count", len(repositoryIDs)),
			slog.Int("discovered_repository_count", len(selection.RepositoryIDs)),
		)
	}

	switch s.Config.SourceMode {
	case "filesystem":
		// Emit a basename-collision diagnostic before syncing so likely
		// accidental corpus nesting (e.g. repos/repos/… copies — issue #3677)
		// is visible from metrics and logs on the first run rather than only
		// post-hoc. This is a heuristic, not a true-duplication check.
		reportRepositoryBasenameCollisions(ctx, repositoryIDs, s.Logger, s.Instruments)

		syncFilesystemFn := s.SyncFilesystem
		if syncFilesystemFn == nil {
			syncFilesystemFn = syncFilesystemRepositories
		}
		repoPaths, err := syncFilesystemFn(ctx, s.Config, repositoryIDs)
		if err != nil {
			return SelectionBatch{}, err
		}
		return SelectionBatch{
			ObservedAt:   observedAt,
			Repositories: buildSelectedRepositories(s.Config, repoPaths, nil, nil),
		}, nil
	case "explicit", "githubOrg":
		syncGitFn := s.SyncGit
		if syncGitFn == nil {
			syncGitFn = func(ctx context.Context, config RepoSyncConfig, repositoryIDs []string) (GitSyncSelection, error) {
				return syncGitRepositoriesWithLogger(ctx, config, repositoryIDs, s.Logger, gitDeltaBaseline{
					Resolver:    s.BaselineResolver,
					Instruments: s.Instruments,
					Reconcile:   reconcilePolicyFromConfig(config),
					Now:         s.Now,
				})
			}
		}
		synced, err := syncGitFn(ctx, s.Config, repositoryIDs)
		if err != nil {
			return SelectionBatch{}, err
		}
		return SelectionBatch{
			ObservedAt: observedAt,
			Repositories: buildSelectedRepositories(
				s.Config,
				synced.SelectedRepoPaths,
				synced.DeltaByRepoPath,
				synced.ReconcileByRepoPath,
				synced.RefsByRepoPath,
			),
		}, nil
	default:
		return SelectionBatch{}, fmt.Errorf("unsupported ESHU_REPO_SOURCE_MODE=%q", s.Config.SourceMode)
	}
}

func buildSelectedRepositories(
	config RepoSyncConfig,
	repoPaths []string,
	deltaByRepoPath map[string]GitSyncDelta,
	reconcileByRepoPath map[string]bool,
	refsByRepoPath ...map[string][]GitRef,
) []SelectedRepository {
	var refsByPath map[string][]GitRef
	if len(refsByRepoPath) > 0 {
		refsByPath = refsByRepoPath[0]
	}
	repositories := make([]SelectedRepository, 0, len(repoPaths))
	for _, repoPath := range repoPaths {
		if strings.TrimSpace(repoPath) == "" {
			continue
		}
		absolutePath, err := filepath.Abs(repoPath)
		if err != nil {
			continue
		}
		repository := SelectedRepository{
			RepoPath:     absolutePath,
			IsDependency: config.DependencyMode,
			DisplayName:  strings.TrimSpace(config.DependencyName),
			Language:     strings.TrimSpace(config.DependencyLanguage),
			FileTargets:  fileTargetsForRepository(config, absolutePath),
			Reconcile:    reconcileByRepoPath[repoPath] || reconcileByRepoPath[absolutePath],
		}
		if delta, ok := deltaByRepoPath[repoPath]; ok && !delta.IsEmpty() {
			repository.Delta = true
			repository.FileTargets = sortUniquePathStrings(append(repository.FileTargets, delta.ChangedFileTargets...))
			repository.DeletedRelativePaths = sortUniquePathStrings(delta.DeletedRelativePaths)
		} else if delta, ok := deltaByRepoPath[absolutePath]; ok && !delta.IsEmpty() {
			repository.Delta = true
			repository.FileTargets = sortUniquePathStrings(append(repository.FileTargets, delta.ChangedFileTargets...))
			repository.DeletedRelativePaths = sortUniquePathStrings(delta.DeletedRelativePaths)
		}
		if refs, ok := refsByPath[repoPath]; ok {
			repository.GitRefs = cloneGitRefs(refs)
		} else if refs, ok := refsByPath[absolutePath]; ok {
			repository.GitRefs = cloneGitRefs(refs)
		}
		if config.SourceMode != "filesystem" {
			repoID := repoIDFromManagedPath(config.ReposDir, absolutePath)
			repository.RemoteURL = repoRemoteURL(config, repoID)
		}
		repositories = append(repositories, repository)
	}
	return repositories
}

func fileTargetsForRepository(config RepoSyncConfig, repositoryPath string) []string {
	if len(config.FileTargets) == 0 || strings.TrimSpace(config.FilesystemRoot) == "" {
		return nil
	}

	targets := make([]string, 0, len(config.FileTargets))
	for _, fileTarget := range config.FileTargets {
		relativePath, err := filepath.Rel(config.FilesystemRoot, fileTarget)
		if err != nil {
			continue
		}
		if relativePath == "." || strings.HasPrefix(relativePath, "..") {
			continue
		}
		targets = append(targets, filepath.Join(repositoryPath, filepath.FromSlash(relativePath)))
	}
	sort.Strings(targets)
	return targets
}

func (s NativeRepositorySelector) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
