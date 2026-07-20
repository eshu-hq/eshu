// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// GitHubRepositoryRecord is one GitHub discovery candidate for repository selection.
type GitHubRepositoryRecord struct {
	RepoID   string
	Archived bool
}

// RepositorySelection holds the selected repository identifiers for one sync cycle.
type RepositorySelection struct {
	RepositoryIDs         []string
	ArchivedRepositoryIDs []string
}

// GitSyncSelection captures the repo paths selected after one Git-backed sync pass.
type GitSyncSelection struct {
	SelectedRepoPaths []string
	DeltaByRepoPath   map[string]GitSyncDelta
	RefsByRepoPath    map[string][]GitRef
	// ReconcileByRepoPath marks repo paths the sweep forced to a full
	// reconciliation snapshot this cycle so their generation bypasses the
	// freshness-hint skip and always re-projects to retract drift.
	ReconcileByRepoPath map[string]bool
	// SourceCommitSHAByRepoPath carries the sync-resolved remote HEAD SHA for
	// each repo path that was updated this cycle (both delta and full-observe
	// sub-paths). Populated only by the git-sync path; empty for non-sync
	// selectors. The snapshot code uses this to skip a redundant git rev-parse
	// HEAD subprocess.
	SourceCommitSHAByRepoPath map[string]string
	// RefWorktreeByRepoPath maps the main repo path to a list of pinned-ref
	// worktree metadata. Each entry carries a worktree path and the ref name.
	// Populated only when ESHU_PINNED_REFS_JSON is configured. Enabler for
	// epic #5393 / issue #5417.
	RefWorktreesByRepoPath map[string][]RefWorktreeEntry
}

// RefWorktreeEntry describes one pinned-ref git worktree checkout created
// alongside the default-branch checkout during a sync cycle.
type RefWorktreeEntry struct {
	WorktreePath string
	Ref          string
	RefKind      string // "branch" or "tag"
	HeadSHA      string
}

// GitSyncDelta carries the file-scoped change set for an updated Git checkout.
type GitSyncDelta struct {
	ChangedFileTargets   []string
	DeletedRelativePaths []string
}

func selectGitHubRepositoryIDs(
	repositories []GitHubRepositoryRecord,
	repositoryRules []RepoSyncRepositoryRule,
	includeArchivedRepos bool,
) RepositorySelection {
	exactRules := make(map[string]struct{})
	for _, rule := range repositoryRules {
		if strings.ToLower(strings.TrimSpace(rule.Kind)) != "exact" {
			continue
		}
		exactRules[normalizeRepositoryID(rule.Value)] = struct{}{}
	}

	selectable := make([]string, 0, len(repositories))
	archived := make([]string, 0)
	seenSelectable := make(map[string]struct{})
	seenArchived := make(map[string]struct{})
	for _, repository := range repositories {
		repoID := normalizeRepositoryID(repository.RepoID)
		if repoID == "" {
			continue
		}
		_, explicitlyAllowedArchived := exactRules[repoID]
		if repository.Archived && !includeArchivedRepos && !explicitlyAllowedArchived {
			if _, ok := seenArchived[repoID]; !ok {
				seenArchived[repoID] = struct{}{}
				archived = append(archived, repoID)
			}
			continue
		}
		if _, ok := seenSelectable[repoID]; ok {
			continue
		}
		seenSelectable[repoID] = struct{}{}
		selectable = append(selectable, repoID)
	}
	if len(repositoryRules) == 0 {
		return RepositorySelection{
			RepositoryIDs:         selectable,
			ArchivedRepositoryIDs: archived,
		}
	}

	selected := make([]string, 0, len(selectable))
	for _, repoID := range selectable {
		for _, rule := range repositoryRules {
			if rule.Matches(repoID) {
				selected = append(selected, repoID)
				break
			}
		}
	}
	return RepositorySelection{
		RepositoryIDs:         selected,
		ArchivedRepositoryIDs: archived,
	}
}

// DiscoverFilesystemRepositoryIDs returns repository IDs discovered under a
// filesystem source root using the same rules as the filesystem collector.
func DiscoverFilesystemRepositoryIDs(filesystemRoot string) ([]string, error) {
	root, err := filepath.Abs(strings.TrimSpace(filesystemRoot))
	if err != nil {
		return nil, fmt.Errorf("resolve filesystem root %q: %w", filesystemRoot, err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat filesystem root %q: %w", filesystemRoot, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("filesystem root %q is not a directory", filesystemRoot)
	}

	repoRoots, err := discoverRepoRoots(root)
	if err != nil {
		return nil, err
	}
	if len(repoRoots) == 1 && repoRoots[0] == root {
		return []string{"."}, nil
	}
	repoIDs := make([]string, 0, len(repoRoots))
	for _, repoRoot := range repoRoots {
		if repoRoot == root {
			continue
		}
		rel, err := filepath.Rel(root, repoRoot)
		if err != nil {
			return nil, fmt.Errorf("relative filesystem repo path %q: %w", repoRoot, err)
		}
		repoIDs = append(repoIDs, filepath.ToSlash(filepath.Clean(rel)))
	}
	sort.Strings(repoIDs)
	return repoIDs, nil
}

func discoverFilesystemRepositoryIDs(filesystemRoot string) ([]string, error) {
	return DiscoverFilesystemRepositoryIDs(filesystemRoot)
}

func discoverRepoRoots(root string) ([]string, error) {
	repoRoots, _, err := discoverRepoRootsWithGitPriority(root)
	return repoRoots, err
}

func discoverRepoRootsWithGitPriority(root string) ([]string, bool, error) {
	if hasGitMarker(root) {
		return []string{root}, true, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, false, fmt.Errorf("read filesystem root %q: %w", root, err)
	}
	repoRoots := make([]string, 0)
	foundGitBackedChild := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		child := filepath.Join(root, entry.Name())
		discovered, childGitBacked, err := discoverRepoRootsWithGitPriority(child)
		if err != nil {
			return nil, false, err
		}
		foundGitBackedChild = foundGitBackedChild || childGitBacked
		repoRoots = append(repoRoots, discovered...)
	}
	if foundGitBackedChild {
		return repoRoots, true, nil
	}
	if repositoryRootLikeFromEntries(entries) {
		return []string{root}, false, nil
	}
	return repoRoots, false, nil
}

func repositoryRootLike(path string) bool {
	if hasGitMarker(path) {
		return true
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return repositoryRootLikeFromEntries(entries)
}

func repositoryRootLikeFromEntries(entries []os.DirEntry) bool {
	childDirectories := 0
	for _, entry := range entries {
		if entry.IsDir() {
			childDirectories++
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		return true
	}
	return childDirectories == 0
}

func hasGitMarker(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0
}

func repoCheckoutName(repoID string) (string, error) {
	normalized := normalizeRepositoryID(repoID)
	if normalized == "" {
		return "", fmt.Errorf("invalid repository identifier %q", repoID)
	}
	return normalized, nil
}

func repoRemoteURL(config RepoSyncConfig, repoID string) string {
	provider, slug := repoProviderAndSlug(repoID)
	if slug == "" {
		return ""
	}
	if provider == "" {
		provider = "github"
	}
	if provider == "github" && !strings.Contains(slug, "/") && strings.TrimSpace(config.GithubOrg) != "" {
		slug = config.GithubOrg + "/" + slug
	}
	if strings.ToLower(strings.TrimSpace(config.GitAuthMethod)) == "ssh" {
		return "git@" + repoProviderHost(provider) + ":" + slug + ".git"
	}
	return "https://" + repoProviderHost(provider) + "/" + slug + ".git"
}

func repoProviderAndSlug(repoID string) (string, string) {
	normalized := normalizeRepositoryID(repoID)
	parts := strings.Split(normalized, "/")
	if len(parts) < 3 {
		return "", normalized
	}
	switch parts[0] {
	case "github", "gitlab", "bitbucket":
		return parts[0], strings.Join(parts[1:], "/")
	default:
		return "", normalized
	}
}

func repoProviderHost(provider string) string {
	switch provider {
	case "gitlab":
		return "gitlab.com"
	case "bitbucket":
		return "bitbucket.org"
	default:
		return "github.com"
	}
}

func repoIDFromManagedPath(reposDir string, repoPath string) string {
	reposDir, err := filepath.Abs(reposDir)
	if err != nil {
		return ""
	}
	repoPath, err = filepath.Abs(repoPath)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(reposDir, repoPath)
	if err != nil {
		return ""
	}
	if rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(rel))
}

// basenameCollisionPathSampleLimit caps the number of colliding paths reported
// in a single warning log entry to keep the log line bounded.
const basenameCollisionPathSampleLimit = 5

// reportRepositoryBasenameCollisions inspects the discovered repository IDs
// from one collector cycle and emits a structured warning log and metric when
// the same repository basename appears at more than one distinct path. This is
// a pure observability call — it does not change which repositories are indexed.
//
// The signal is a HEURISTIC for accidental corpus nesting, not a true
// repository-identity check. Identity here is only the last path segment
// (basename) of each repoID — a cheap, label-free signal computed without
// reading .git/config on every discovered root. Distinct repositories can
// legitimately share a basename (org-a/utils and org-b/utils, or monorepo
// common/ directories), so a collision is a prompt to inspect the logged paths,
// not proof of duplication. The motivating case is accidental nesting or
// recursive copies (e.g. repos/repos/repos/… as in issue #3677): there the same
// basename appears at multiple depths and the counter advances, making the
// 4× inflation visible from metrics and logs rather than requiring post-hoc
// database forensics.
//
// Metric: eshu_dp_repository_basename_collision_total — incremented by the
// number of surplus (non-first) occurrences of each colliding basename. No path
// or basename labels are attached; those details are in the structured log.
//
// Log: "repository basename collision detected (possible accidental corpus
// nesting)" at WARN level, with fields:
//
//	identity          — the colliding basename
//	path_count        — total paths that share this basename
//	surplus_count     — paths beyond the first (matches the metric delta)
//	path_sample       — up to basenameCollisionPathSampleLimit paths (bounded)
//	total_path_count  — full count when the sample is truncated
func reportRepositoryBasenameCollisions(
	ctx context.Context,
	repoIDs []string,
	logger *slog.Logger,
	inst *telemetry.Instruments,
) {
	if len(repoIDs) == 0 {
		return
	}

	// Group repoIDs by their basename.
	byBasename := make(map[string][]string, len(repoIDs))
	for _, id := range repoIDs {
		if id == "" {
			continue
		}
		basename := filepath.Base(id)
		if basename == "" || basename == "." {
			continue
		}
		byBasename[basename] = append(byBasename[basename], id)
	}

	var totalSurplus int64
	for basename, paths := range byBasename {
		if len(paths) <= 1 {
			continue
		}
		// Each path beyond the first is a surplus collision.
		surplus := int64(len(paths) - 1)
		totalSurplus += surplus

		if logger != nil {
			sample := paths
			truncated := false
			if len(sample) > basenameCollisionPathSampleLimit {
				sample = sample[:basenameCollisionPathSampleLimit]
				truncated = true
			}
			attrs := []any{
				slog.String("identity", basename),
				slog.Int("path_count", len(paths)),
				slog.Int("surplus_count", len(paths)-1),
				slog.Any("path_sample", sample),
			}
			if truncated {
				attrs = append(attrs, slog.Int("total_path_count", len(paths)))
			}
			logger.WarnContext(ctx, "repository basename collision detected (possible accidental corpus nesting)", attrs...)
		}
	}

	if totalSurplus > 0 && inst != nil {
		inst.RepositoryBasenameCollision.Add(ctx, totalSurplus)
	}
}
