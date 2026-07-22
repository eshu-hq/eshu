// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// NativeRepositorySelector owns Go-native repository selection and sync behavior.
type NativeRepositorySelector struct {
	Config            RepoSyncConfig
	Now               func() time.Time
	DiscoverSelection func(context.Context, RepoSyncConfig, string) (RepositorySelection, error)
	SyncFilesystem    func(context.Context, RepoSyncConfig, []string) ([]string, bool, error)
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
		s.Logger.InfoContext(
			ctx, "collector repository shard selected",
			log.CollectorKind("git"),
			slog.Int("repo_shard_count", s.Config.RepoShardCount),
			slog.Int("repo_shard_index", s.Config.RepoShardIndex),
			slog.Int("repository_count", len(repositoryIDs)),
			slog.Int("discovered_repository_count", len(selection.RepositoryIDs)),
		)
	}

	switch s.Config.SourceMode {
	case "filesystem":
		syncFilesystemFn := s.SyncFilesystem
		if syncFilesystemFn == nil {
			syncFilesystemFn = syncFilesystemRepositories
		}
		repoPaths, corpusChanged, err := syncFilesystemFn(ctx, s.Config, repositoryIDs)
		if err != nil {
			return SelectionBatch{}, err
		}
		// Emit the basename-collision diagnostic on a real corpus change only.
		// Three independent correctness properties drive the gate:
		//
		// 1. Completeness — the report inspects the full pre-shard
		//    selection.RepositoryIDs, NOT the post-shard repositoryIDs subset.
		//    Basename collisions are a property of the DISCOVERED set: with
		//    sharding active a colliding pair (e.g. "worker" and "repos/worker")
		//    may hash into shard buckets that no single shard's post-shard subset
		//    holds together, so a per-shard check is permanently silent even
		//    though the corpus is inflated (issue #3700, regression on #3688).
		//
		// 2. Single-emit — only the index-0 shard reports. Every shard instance
		//    inspects the same global pre-shard set, so letting all N shards report
		//    would multiply one real collision into N duplicate WARN lines and an
		//    N× metric reading, breaking alert thresholds tuned to the true surplus.
		//    Shard index 0 exists for any shard count >= 1, and at the unsharded
		//    default (count <= 1) the index is 0, so single-instance behaviour is
		//    unchanged.
		//
		// 3. Changed-batch anti-spam, decoupled from ownership — the report fires
		//    on corpusChanged, the FULL-corpus changed signal (FilesystemRoot
		//    fingerprint vs stored manifest), NOT on len(repoPaths). repoPaths is
		//    the index-0 shard's OWN materialized subset, which is empty whenever
		//    the colliding repos all hash to other shards. Gating the emitter on
		//    its own subset would silence the diagnostic exactly in the inflated-
		//    corpus case it targets (issue #3700 P2). corpusChanged is identical on
		//    every shard because the fingerprint covers the whole root, so the
		//    designated emitter fires regardless of which repos it owns, and stays
		//    silent on an unchanged re-poll (corpusChanged == false). At the
		//    unsharded default this is equivalent to the old len(repoPaths) > 0
		//    gate, since shard 0 then owns the full set.
		if s.Config.RepoShardIndex == 0 && corpusChanged {
			reportRepositoryBasenameCollisions(ctx, selection.RepositoryIDs, s.Logger, s.Instruments)
		}
		repositories := buildSelectedRepositories(s.Config, repoPaths, nil, nil, nil, collectLocalRefs(ctx, s.Logger, s.Config, selection.RepositoryIDs, repoPaths), nil)
		attachFilesystemGitTreePaths(s.Config, selection.RepositoryIDs, repositories)
		return SelectionBatch{ObservedAt: observedAt, Repositories: repositories}, nil
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
				synced.SourceCommitSHAByRepoPath,
				synced.RefsByRepoPath,
				synced.RefWorktreesByRepoPath,
			),
		}, nil
	default:
		return SelectionBatch{}, fmt.Errorf("unsupported ESHU_REPO_SOURCE_MODE=%q", s.Config.SourceMode)
	}
}

// attachFilesystemGitTreePaths preserves the source checkout used for Git
// committed-tree reads. Filesystem mode intentionally strips .git from its
// managed copy, so submodule gitlink resolution must read the original source
// while content discovery continues to read the isolated managed workspace.
func attachFilesystemGitTreePaths(config RepoSyncConfig, repositoryIDs []string, repositories []SelectedRepository) {
	sourceBySelectedPath := make(map[string]string, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		sourcePath, targetPath, err := filesystemRepoPaths(config, repositoryID)
		if err != nil {
			continue
		}
		selectedPath := targetPath
		if config.FilesystemDirect {
			selectedPath = sourcePath
		}
		selectedPath = canonicalLocalPath(selectedPath)
		sourceBySelectedPath[selectedPath] = canonicalLocalPath(sourcePath)
	}
	for index := range repositories {
		if sourcePath := sourceBySelectedPath[canonicalLocalPath(repositories[index].RepoPath)]; sourcePath != "" {
			repositories[index].GitTreePath = sourcePath
		}
	}
}

func canonicalLocalPath(localPath string) string {
	absolutePath, err := filepath.Abs(localPath)
	if err != nil {
		return localPath
	}
	if resolvedPath, resolveErr := filepath.EvalSymlinks(absolutePath); resolveErr == nil {
		return resolvedPath
	}
	return absolutePath
}

func buildSelectedRepositories(
	config RepoSyncConfig,
	repoPaths []string,
	deltaByRepoPath map[string]GitSyncDelta,
	reconcileByRepoPath map[string]bool,
	sourceCommitSHAByRepoPath map[string]string,
	refsByRepoPath map[string][]GitRef,
	refWorktreesByRepoPath map[string][]RefWorktreeEntry,
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
		// In filesystem mode, canonicalize the repository root through symlinks so
		// it shares a prefix with the symlink-resolved file paths content
		// discovery produces (normalizeScanRoot EvalSymlinks the scan root).
		// Without this, on platforms where the corpus lives under a symlinked temp
		// root (macOS /var -> /private/var), filepath.Rel(repoRoot, file) yields a
		// broken ../.. path, the canonical directory chain never roots at the
		// Repository node, and no Directory/File/entity nodes materialize.
		//
		// Scoped to filesystem mode on purpose: git mode derives repo identity
		// from the raw managed ReposDir prefix (repoIDFromManagedPath), so
		// resolving only one side there would break the Rel-based id derivation.
		if config.SourceMode == "filesystem" {
			if resolved, resolveErr := filepath.EvalSymlinks(absolutePath); resolveErr == nil {
				absolutePath = resolved
			}
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
		if sha, ok := sourceCommitSHAByRepoPath[repoPath]; ok {
			repository.SourceCommitSHA = sha
		} else if sha, ok := sourceCommitSHAByRepoPath[absolutePath]; ok {
			repository.SourceCommitSHA = sha
		}
		if refs, ok := refsByRepoPath[repoPath]; ok {
			repository.GitRefs = cloneGitRefs(refs)
		} else if refs, ok := refsByRepoPath[absolutePath]; ok {
			repository.GitRefs = cloneGitRefs(refs)
		}
		if config.SourceMode != "filesystem" {
			repoID := repoIDFromManagedPath(config.ReposDir, absolutePath)
			repository.RemoteURL = repoRemoteURL(config, repoID)
		} else if strings.TrimSpace(config.GithubOrg) != "" {
			// Filesystem repos carry no real git remote. When an operator explicitly
			// declares an org (ESHU_GITHUB_ORG), synthesize a deterministic remote
			// from the repo directory name so URL-keyed cross-repo correlations
			// (package-registry source hints, etc.) can resolve the owning repo.
			// Without an explicit org we leave RemoteURL empty rather than fabricate
			// a github.com URL for an arbitrary local directory.
			repository.RemoteURL = repoRemoteURL(config, filepath.Base(absolutePath))
		}
		repositories = append(repositories, repository)

		// Append ref-scoped worktree entries as separate SelectedRepository
		// entries with Ref populated, so each pinned ref snapshots as its own
		// isolated scope (epic #5393, enabler #5417). Lookups mirror the
		// repoPath/absolutePath fallback pattern used by the other maps
		// (delta, sourceSHA, gitRefs, reconcile) above.
		entries := refWorktreesByRepoPath[repoPath]
		if len(entries) == 0 {
			entries = refWorktreesByRepoPath[absolutePath]
		}
		for _, entry := range entries {
			repositories = append(repositories, SelectedRepository{
				RepoPath:        entry.WorktreePath,
				RemoteURL:       repository.RemoteURL,
				IsDependency:    repository.IsDependency,
				DisplayName:     repository.DisplayName,
				Language:        repository.Language,
				GitRefs:         repository.GitRefs,
				Reconcile:       repository.Reconcile,
				SourceCommitSHA: entry.HeadSHA,
				Ref:             entry.Ref,
			})
		}
	}
	// N5 fix: process repos that have ref worktree entries but whose default
	// branch did not move — emit ONLY the ref-scoped SelectedRepository entries,
	// no main-line entry. This avoids a full file-tree walk + parse on the
	// default branch when only a pinned ref advanced (fleet CPU waste per the
	// #5393 model).
	handled := make(map[string]struct{}, len(repoPaths))
	for _, rp := range repoPaths {
		abs, err := filepath.Abs(rp)
		if err != nil {
			continue
		}
		handled[abs] = struct{}{}
	}
	for mainPath, entries := range refWorktreesByRepoPath {
		absMain, err := filepath.Abs(mainPath)
		if err != nil {
			continue
		}
		if _, ok := handled[absMain]; ok {
			continue // already processed in the main loop above
		}
		// This repo's default branch did not move — only pinned refs advanced.
		// Emit ref-scoped entries only, no main-line SelectedRepository (N5 fix).
		refRepoID := repoIDFromManagedPath(config.ReposDir, absMain)
		remoteURL := repoRemoteURL(config, refRepoID)
		// Mirror the main-loop refs lookup so ref-only entries carry the same
		// git_refs payload as entries emitted on a default-branch-move cycle
		// (N5a alignment; both forms keyed like the other maps above).
		var gitRefs []GitRef
		if refs, ok := refsByRepoPath[mainPath]; ok {
			gitRefs = cloneGitRefs(refs)
		} else if refs, ok := refsByRepoPath[absMain]; ok {
			gitRefs = cloneGitRefs(refs)
		}
		for _, entry := range entries {
			repositories = append(repositories, SelectedRepository{
				RepoPath:        entry.WorktreePath,
				RemoteURL:       remoteURL,
				IsDependency:    config.DependencyMode,
				DisplayName:     strings.TrimSpace(config.DependencyName),
				Language:        strings.TrimSpace(config.DependencyLanguage),
				GitRefs:         gitRefs,
				SourceCommitSHA: entry.HeadSHA,
				Ref:             entry.Ref,
			})
		}
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
