package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
