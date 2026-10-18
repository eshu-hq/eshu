// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/gitlab"
)

// GitHubRepositoryRecord is one GitHub discovery candidate for repository
// selection. GitHubID is the provider's numeric repository id, stable across
// renames and transfers; zero when the listing did not carry one.
type GitHubRepositoryRecord struct {
	RepoID   string
	GitHubID int64
	Archived bool
}

// RepositorySelection holds the selected repository identifiers for one sync cycle.
//
// The githubOrg source mode also fills the listing fields the #7625 selection
// observer consumes: ListingComplete is true only when the org listing ended
// before the repository limit, ListedRepositories is every listed record in
// listing order, ArchivedRepositoryIDs the archived repositories the
// archive policy excluded, and RuleExcludedRepositoryIDs the selectable
// repositories no configured rule matched. Other modes leave them empty.
type RepositorySelection struct {
	RepositoryIDs             []string
	ArchivedRepositoryIDs     []string
	RuleExcludedRepositoryIDs []string
	ListedRepositories        []GitHubRepositoryRecord
	ListingComplete           bool
	// GroupFullPath is the resolved GitLab group full path in the server's
	// case. Only the gitlabGroup source mode sets it (#7765).
	GroupFullPath string
}

// GitSyncSelection captures the repo paths selected after one Git-backed sync pass.
type GitSyncSelection struct {
	SelectedRepoPaths []string
	// DeltaByRepoPath holds incremental deltas, plus path-less entries that
	// only carry GitSyncDelta.DefaultBranchChanged for a full snapshot.
	DeltaByRepoPath map[string]GitSyncDelta
	RefsByRepoPath  map[string][]GitRef
	// ReconcileByRepoPath marks repo paths the sweep forced to a full
	// reconciliation snapshot this cycle so their generation bypasses the
	// freshness-hint skip and always re-projects to retract drift.
	ReconcileByRepoPath map[string]bool
	// ReconcileReasonByRepoPath carries the bounded reconcile decision reason
	// (for example interval_elapsed or graph_dirty) for each repo path in
	// ReconcileByRepoPath. The generation records it as reconcile_reason (#7978).
	ReconcileReasonByRepoPath map[string]string
	// SourceCommitSHAByRepoPath carries the selector-proven commit SHA for each
	// repo path updated this cycle. Git sync provides its resolved remote HEAD;
	// filesystem managed-copy provides the clean source commit only when every
	// copied regular-file byte matches its immutable blob. Other selectors leave
	// it empty. Snapshot code uses it instead of rereading a mutable checkout.
	SourceCommitSHAByRepoPath map[string]string
	// PreparseIdentityByRepoPath carries the pre-parse identity of each full,
	// clean snapshot (#7978 follow-up); the generation stores it so a later
	// reconcile with an equal identity can skip the parse.
	PreparseIdentityByRepoPath map[string]string
	// RefWorktreeByRepoPath maps the main repo path to a list of pinned-ref
	// worktree metadata. Each entry carries a worktree path and the ref name.
	// Populated only when ESHU_PINNED_REFS_JSON is configured. Enabler for
	// epic #5393 / issue #5417.
	RefWorktreesByRepoPath map[string][]RefWorktreeEntry
	// NextReconcileStartOffset is the walk offset the next cycle passes as
	// ReconcileStartOffset (#7987).
	NextReconcileStartOffset int
	// YieldedToWebhook reports that the sweep stopped at a repository
	// boundary because an accepted webhook trigger was queued (#8123).
	// The selection holds only the repositories visited before the
	// yield; the next cycle resumes at NextReconcileStartOffset with a
	// walk limited to SweepRemainingRepositories.
	YieldedToWebhook bool
	// SweepRemainingRepositories is how many rotating-suffix
	// repositories the logical sweep still owes after a yield (#8123);
	// zero on a completed walk.
	SweepRemainingRepositories int
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
	// BaselineCommitSHA is the commit the diff was taken from (#7319). The
	// projector refuses the delta unless it is still the active commit.
	BaselineCommitSHA string
	// DefaultBranchChanged marks a full snapshot taken because the remote
	// default branch changed (#7678). The sync keeps such an entry in
	// DeltaByRepoPath even though it carries no paths, and the selection
	// carries it to SelectedRepository.DefaultBranchChanged.
	DefaultBranchChanged bool
	// ReconcileHeld marks a forced reconcile that the sync held after the
	// fetch, because the remote head equals a pending full with live work
	// (ruling a9-126). Such a sync produces no snapshot.
	ReconcileHeld bool
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
	listed := append([]GitHubRepositoryRecord(nil), repositories...)
	if len(repositoryRules) == 0 {
		return RepositorySelection{
			RepositoryIDs:         selectable,
			ArchivedRepositoryIDs: archived,
			ListedRepositories:    listed,
		}
	}

	selected := make([]string, 0, len(selectable))
	ruleExcluded := make([]string, 0)
	for _, repoID := range selectable {
		matched := false
		for _, rule := range repositoryRules {
			if rule.Matches(repoID) {
				matched = true
				break
			}
		}
		if matched {
			selected = append(selected, repoID)
		} else {
			ruleExcluded = append(ruleExcluded, repoID)
		}
	}
	return RepositorySelection{
		RepositoryIDs:             selected,
		ArchivedRepositoryIDs:     archived,
		RuleExcludedRepositoryIDs: ruleExcluded,
		ListedRepositories:        listed,
	}
}

// selectGitLabRepositoryIDs maps listed GitLab projects to a repository
// selection: gitlab/<path> IDs classified by the shared archive and rule
// policy. The provider numeric ID rides along for the observation rows.
func selectGitLabRepositoryIDs(projects []gitlab.Project, rules []RepoSyncRepositoryRule, includeArchived bool) RepositorySelection {
	records := make([]GitHubRepositoryRecord, 0, len(projects))
	for _, project := range projects {
		records = append(records, GitHubRepositoryRecord{
			RepoID:   "gitlab/" + project.PathWithNamespace,
			GitHubID: project.ID,
			Archived: project.Archived,
		})
	}
	return selectGitHubRepositoryIDs(records, rules, includeArchived)
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
	// Reject names that would squat on the reserved ref-worktree namespace
	// (N3 fix: a repo named .eshu-ref-worktrees would occupy
	// ReposDir/.eshu-ref-worktrees, trapping other repos' ref worktrees
	// inside its canonical checkout where they'd be walked as its files).
	if strings.HasPrefix(normalized, ".eshu-") {
		return "", fmt.Errorf("repository identifier %q conflicts with reserved .eshu- prefix; rename the repository", repoID)
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
		return "git@" + repoProviderHost(config, provider) + ":" + slug + ".git"
	}
	return "https://" + repoProviderHost(config, provider) + "/" + slug + ".git"
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

// defaultGitlabHost is the gitlab provider host when ESHU_GITLAB_HOST is unset (#7764).
const defaultGitlabHost = "gitlab.com"

// gitlabHostLabelPattern matches one hostname label; underscores allowed, never edge hyphens.
var gitlabHostLabelPattern = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?$`)

// normalizeGitlabHost resolves ESHU_GITLAB_HOST, failing closed on anything but a bare hostname.
func normalizeGitlabHost(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(raw))
	if host == "" {
		return defaultGitlabHost, nil
	}
	host = strings.TrimSuffix(host, ".")
	invalid := func(reason string) (string, error) {
		return "", fmt.Errorf("invalid ESHU_GITLAB_HOST %q: %s", raw, reason)
	}
	if strings.ContainsAny(host, "/:@ \t\n\r") {
		return invalid("set a bare hostname such as gitlab.example.com")
	}
	if len(host) > 253 {
		return invalid("hostname exceeds 253 characters")
	}
	for _, label := range strings.Split(host, ".") {
		if !gitlabHostLabelPattern.MatchString(label) {
			return invalid("hostname labels hold 1-63 letters, digits, hyphens, or underscores and must not start or end with a hyphen")
		}
	}
	return host, nil
}

// effectiveGitlabHost returns the gitlab host, defaulting for pre-knob configs.
func effectiveGitlabHost(config RepoSyncConfig) string {
	if strings.TrimSpace(config.GitlabHost) == "" {
		return defaultGitlabHost
	}
	return config.GitlabHost
}

// repoProviderHost returns the remote host for provider: the configured
// ESHU_GITLAB_HOST host for gitlab, the fixed hosts for the other providers.
// It is the single host source for clone URLs (repoRemoteURL) and the token
// auth header scope (gitCommandEnv), so the two stay in lockstep (#7764).
func repoProviderHost(config RepoSyncConfig, provider string) string {
	switch provider {
	case "gitlab":
		return effectiveGitlabHost(config)
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
