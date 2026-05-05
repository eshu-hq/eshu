package collector

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// NativeRepositorySelector owns Go-native repository selection and sync behavior.
type NativeRepositorySelector struct {
	Config            RepoSyncConfig
	Now               func() time.Time
	DiscoverSelection func(context.Context, RepoSyncConfig, string) (RepositorySelection, error)
	SyncFilesystem    func(context.Context, RepoSyncConfig, []string) ([]string, error)
	SyncGit           func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error)
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

	switch s.Config.SourceMode {
	case "filesystem":
		syncFilesystemFn := s.SyncFilesystem
		if syncFilesystemFn == nil {
			syncFilesystemFn = syncFilesystemRepositories
		}
		repoPaths, err := syncFilesystemFn(ctx, s.Config, selection.RepositoryIDs)
		if err != nil {
			return SelectionBatch{}, err
		}
		return SelectionBatch{
			ObservedAt:   observedAt,
			Repositories: buildSelectedRepositories(s.Config, repoPaths),
		}, nil
	case "explicit", "githubOrg":
		syncGitFn := s.SyncGit
		if syncGitFn == nil {
			syncGitFn = syncGitRepositories
		}
		synced, err := syncGitFn(ctx, s.Config, selection.RepositoryIDs)
		if err != nil {
			return SelectionBatch{}, err
		}
		return SelectionBatch{
			ObservedAt:   observedAt,
			Repositories: buildSelectedRepositories(s.Config, synced.SelectedRepoPaths),
		}, nil
	default:
		return SelectionBatch{}, fmt.Errorf("unsupported ESHU_REPO_SOURCE_MODE=%q", s.Config.SourceMode)
	}
}

func buildSelectedRepositories(
	config RepoSyncConfig,
	repoPaths []string,
) []SelectedRepository {
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
