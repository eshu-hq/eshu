// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/rest"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/gitlab"
	"github.com/eshu-hq/eshu/go/internal/github/app"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// RepositorySelectionObserver evaluates one selector's listing against the
// owner's known repository scopes and records selection observations (#7625).
// Implementations must not fail the collector cycle: the result reports the
// outcome, including store errors.
type RepositorySelectionObserver interface {
	Observe(ctx context.Context, request membership.Request) membership.Result
}

// observeSelection hands the cycle's full pre-shard selection to the
// configured observer: the githubOrg listing as one request, or the explicit
// configured list as one request per owner. Exactly one request carries
// SweepExpired in a cycle that issues any, so the expired-row sweep runs at
// most once per cycle; filesystem mode, an explicit list with no owners,
// and a cycle whose discovery fails issue none.
// Only shard 0 observes, because
// every shard sees the same selection and N writers would race on the same
// observation rows. Filesystem mode has no remote identity to observe.
func (s NativeRepositorySelector) observeSelection(
	ctx context.Context, discovered RepositorySelection, observedAt time.Time, token string,
) {
	if s.SelectionObserver == nil || s.Config.RepoShardIndex != 0 {
		return
	}
	switch s.Config.SourceMode {
	case "githubOrg":
		observeGitHubOrgSelection(s, ctx, discovered, observedAt, token)
	case "gitlabGroup":
		s.SelectionObserver.Observe(ctx, gitlabGroupSelectionRequest(s.Config, discovered, observedAt))
	case "explicit":
		for _, request := range explicitSelectionRequests(s.Config, discovered.RepositoryIDs, observedAt) {
			s.SelectionObserver.Observe(ctx, request)
		}
	}
}

// explicitSelectionRequests maps the configured explicit repositories to one
// complete, all-selected listing per owner, in owner order. Each repository
// gets the scope id and repo slug a git sync of it would write, and its owner
// is the slug's first segment, the same partition the store's known-scope
// read uses. A repository whose identity or slug cannot be derived is skipped.
// The explicit selector writes rows only for repositories that already have a
// scope, so a configured repository that was never synced gets none.
func explicitSelectionRequests(config RepoSyncConfig, repositoryIDs []string, observedAt time.Time) []membership.Request {
	type ownerSelection struct {
		rules  []membership.Rule
		listed []membership.ListedRepository
	}
	byOwner := make(map[string]*ownerSelection)
	for _, repoID := range repositoryIDs {
		scopeID, slug := gitScopeIdentityForRepositoryID(config, repoID)
		owner, _, found := strings.Cut(slug, "/")
		owner = strings.ToLower(strings.TrimSpace(owner))
		if scopeID == "" || !found || owner == "" {
			continue
		}
		group := byOwner[owner]
		if group == nil {
			group = &ownerSelection{}
			byOwner[owner] = group
		}
		group.rules = append(group.rules, membership.Rule{Kind: "exact", Value: repoID})
		group.listed = append(group.listed, membership.ListedRepository{ScopeID: scopeID, Slug: slug, State: membership.StateSelected})
	}
	principal := selectionPrincipal(config)
	requests := make([]membership.Request, 0, len(byOwner))
	for _, owner := range slices.Sorted(maps.Keys(byOwner)) {
		group := byOwner[owner]
		requests = append(requests, membership.Request{
			Selector:       membership.NewExplicitSelector(config.SourceMode, owner, group.rules, principal),
			SourceMode:     config.SourceMode,
			RepoShardCount: config.RepoShardCount,
			Now:            observedAt,
			LivenessWindow: config.SelectionLivenessWindow,
			Listing:        membership.Listing{Complete: true, Repositories: group.listed},
		})
	}
	if n := len(requests); n > 0 {
		requests[n-1].SweepExpired = true
	}
	return requests
}

// githubOrgSelectionRequest maps a githubOrg discovery result to an
// observation request. Each listed repository gets the scope id a git sync of
// it would write, so listed slugs join stored scopes without a checkout.
func githubOrgSelectionRequest(
	config RepoSyncConfig,
	discovered RepositorySelection,
	observedAt time.Time,
) membership.Request {
	archived := make(map[string]struct{}, len(discovered.ArchivedRepositoryIDs))
	for _, repoID := range discovered.ArchivedRepositoryIDs {
		archived[repoID] = struct{}{}
	}
	ruleExcluded := make(map[string]struct{}, len(discovered.RuleExcludedRepositoryIDs))
	for _, repoID := range discovered.RuleExcludedRepositoryIDs {
		ruleExcluded[repoID] = struct{}{}
	}
	listed := make([]membership.ListedRepository, 0, len(discovered.ListedRepositories))
	for _, record := range discovered.ListedRepositories {
		state := membership.StateSelected
		if _, ok := archived[record.RepoID]; ok {
			state = membership.StateArchivedExcluded
		} else if _, ok := ruleExcluded[record.RepoID]; ok {
			state = membership.StateRuleExcluded
		}
		listed = append(listed, membership.ListedRepository{
			ScopeID:  gitScopeIDForRepositoryID(config, record.RepoID),
			Slug:     record.RepoID,
			GitHubID: record.GitHubID,
			State:    state,
		})
	}
	rules := make([]membership.Rule, 0, len(config.RepositoryRules))
	for _, rule := range config.RepositoryRules {
		rules = append(rules, membership.Rule{Kind: rule.Kind, Value: rule.Value})
	}
	return membership.Request{
		Selector:       membership.NewGitHubOrgSelector(config.SourceMode, config.GithubOrg, rules, config.IncludeArchivedRepos, selectionPrincipal(config)),
		SourceMode:     config.SourceMode,
		RepoShardCount: config.RepoShardCount,
		RepoLimit:      config.RepoLimit,
		Now:            observedAt,
		LivenessWindow: config.SelectionLivenessWindow,
		Listing: membership.Listing{
			Complete:     discovered.ListingComplete,
			Repositories: listed,
		},
		SweepExpired: true,
	}
}

// gitlabGroupSelectionRequest maps a gitlabGroup discovery result to an
// observation request. Each listed repository gets the scope id and stored
// slug a git sync of it would write, so listed records join stored scopes
// without a checkout. The selector carries the listing host and the
// resolved group full path.
func gitlabGroupSelectionRequest(
	config RepoSyncConfig,
	discovered RepositorySelection,
	observedAt time.Time,
) membership.Request {
	archived := make(map[string]struct{}, len(discovered.ArchivedRepositoryIDs))
	for _, repoID := range discovered.ArchivedRepositoryIDs {
		archived[repoID] = struct{}{}
	}
	ruleExcluded := make(map[string]struct{}, len(discovered.RuleExcludedRepositoryIDs))
	for _, repoID := range discovered.RuleExcludedRepositoryIDs {
		ruleExcluded[repoID] = struct{}{}
	}
	listed := make([]membership.ListedRepository, 0, len(discovered.ListedRepositories))
	for _, record := range discovered.ListedRepositories {
		state := membership.StateSelected
		if _, ok := archived[record.RepoID]; ok {
			state = membership.StateArchivedExcluded
		} else if _, ok := ruleExcluded[record.RepoID]; ok {
			state = membership.StateRuleExcluded
		}
		scopeID, slug := gitScopeIdentityForRepositoryID(config, record.RepoID)
		if slug == "" {
			slug = record.RepoID
		}
		listed = append(listed, membership.ListedRepository{
			ScopeID:  scopeID,
			Slug:     slug,
			GitHubID: record.GitHubID,
			State:    state,
		})
	}
	rules := make([]membership.Rule, 0, len(config.RepositoryRules))
	for _, rule := range config.RepositoryRules {
		rules = append(rules, membership.Rule{Kind: rule.Kind, Value: rule.Value})
	}
	return membership.Request{
		Selector:       membership.NewGitLabGroupSelector(config.SourceMode, repoProviderHost(config, "gitlab"), discovered.GroupFullPath, rules, config.IncludeArchivedRepos, selectionPrincipal(config)),
		SourceMode:     config.SourceMode,
		RepoShardCount: config.RepoShardCount,
		RepoLimit:      config.RepoLimit,
		Now:            observedAt,
		LivenessWindow: config.SelectionLivenessWindow,
		Listing: membership.Listing{
			Complete:     discovered.ListingComplete,
			Repositories: listed,
		},
		SweepExpired: true,
	}
}

// selectionPrincipal names the credential the collector selected with,
// following resolveGitToken's choice: the GitHub App installation for
// githubApp auth, otherwise the salted token hash of the configured token, or
// blank (anonymous) when there is none. The token itself never leaves
// membership.TokenPrincipal.
func selectionPrincipal(config RepoSyncConfig) string {
	if strings.EqualFold(strings.TrimSpace(config.GitAuthMethod), "githubapp") {
		return membership.GitHubAppPrincipal(config.GitHubAppID, config.GitHubAppInstallation)
	}
	if token := strings.TrimSpace(config.GitToken); token != "" {
		return membership.TokenPrincipal(token)
	}
	return ""
}

func discoverSelection(
	ctx context.Context,
	config RepoSyncConfig,
	token string,
	newGitLabClient func(apiBase, token string) (*gitlab.Client, error),
) (RepositorySelection, error) {
	switch strings.TrimSpace(config.SourceMode) {
	case "filesystem":
		if strings.TrimSpace(config.FilesystemRoot) == "" {
			return RepositorySelection{}, fmt.Errorf("filesystem source mode requires ESHU_FILESYSTEM_ROOT")
		}
		if len(config.Repositories) > 0 {
			return RepositorySelection{
				RepositoryIDs: sortUniqueStrings(config.Repositories),
			}, nil
		}
		repositoryIDs, err := discoverFilesystemRepositoryIDs(config.FilesystemRoot)
		if err != nil {
			return RepositorySelection{}, err
		}
		return RepositorySelection{RepositoryIDs: repositoryIDs}, nil
	case "explicit":
		return RepositorySelection{RepositoryIDs: sortUniqueStrings(config.Repositories)}, nil
	case "githubOrg":
		if strings.TrimSpace(config.GithubOrg) == "" {
			return RepositorySelection{}, fmt.Errorf("githubOrg source mode requires ESHU_GITHUB_ORG")
		}
		if strings.TrimSpace(token) == "" {
			return RepositorySelection{}, fmt.Errorf("githubOrg source mode requires GitHub token or App auth")
		}
		repositories, complete, err := listGitHubOrgRepositories(ctx, config, token)
		if err != nil {
			return RepositorySelection{}, err
		}
		selection := selectGitHubRepositoryIDs(repositories, config.RepositoryRules, config.IncludeArchivedRepos)
		selection.ListingComplete = complete
		return selection, nil
	case "gitlabGroup":
		if strings.TrimSpace(config.GitlabGroup) == "" {
			return RepositorySelection{}, fmt.Errorf("gitlabGroup source mode requires ESHU_GITLAB_GROUP")
		}
		if strings.TrimSpace(token) == "" {
			return RepositorySelection{}, fmt.Errorf("gitlabGroup source mode requires ESHU_GIT_TOKEN")
		}
		if newGitLabClient == nil {
			return RepositorySelection{}, fmt.Errorf("gitlabGroup source mode requires a GitLab client factory")
		}
		apiBase := "https://" + repoProviderHost(config, "gitlab") + "/api/v4"
		client, err := newGitLabClient(apiBase, token)
		if err != nil {
			return RepositorySelection{}, err
		}
		group, err := client.ResolveGroup(ctx, config.GitlabGroup)
		if err != nil {
			return RepositorySelection{}, err
		}
		if err := gitlab.CheckExactRules(group.FullPath, exactRuleIDs(config.RepositoryRules)); err != nil {
			return RepositorySelection{}, err
		}
		listing, err := client.ListGroupProjects(ctx, group.FullPath, config.RepoLimit)
		if err != nil {
			return RepositorySelection{}, err
		}
		selection := selectGitLabRepositoryIDs(listing.Projects, config.RepositoryRules, config.IncludeArchivedRepos)
		selection.ListingComplete = listing.Complete
		selection.GroupFullPath = group.FullPath
		return selection, nil
	default:
		return RepositorySelection{}, fmt.Errorf("unsupported ESHU_REPO_SOURCE_MODE=%q", config.SourceMode)
	}
}

// defaultDiscoverSelection returns the production discovery function, with
// gitlabGroup listing through a live client.
func defaultDiscoverSelection(inst *telemetry.Instruments, logger *slog.Logger) func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
	live := func(apiBase, token string) (*gitlab.Client, error) {
		return gitlab.NewClient(nil, apiBase, token, inst, logger)
	}
	return func(ctx context.Context, config RepoSyncConfig, token string) (RepositorySelection, error) {
		return discoverSelection(ctx, config, token, live)
	}
}

func resolveGitToken(ctx context.Context, config RepoSyncConfig) (string, error) {
	switch strings.ToLower(strings.TrimSpace(config.GitAuthMethod)) {
	case "", "none", "ssh":
		return strings.TrimSpace(config.GitToken), nil
	case "token":
		token := strings.TrimSpace(config.GitToken)
		if token == "" {
			return "", fmt.Errorf("ESHU_GIT_TOKEN or GITHUB_TOKEN is required when ESHU_GIT_AUTH_METHOD=token")
		}
		return token, nil
	case "githubapp":
		return mintGitHubAppToken(ctx, config)
	default:
		return "", fmt.Errorf("unsupported ESHU_GIT_AUTH_METHOD=%q", config.GitAuthMethod)
	}
}

// listGitHubOrgRepositories lists the org's repositories from the GitHub API,
// up to config.RepoLimit, against config's API base and retry budget. See
// listGitHubOrgRepositoriesFrom for the completeness flag.
func listGitHubOrgRepositories(
	ctx context.Context,
	config RepoSyncConfig,
	token string,
) ([]GitHubRepositoryRecord, bool, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	baseURL, retry := config.GitHubAPI.Resolve()
	return listGitHubOrgRepositoriesFromWithRetry(ctx, client, baseURL, config.GithubOrg, config.RepoLimit, token, retry)
}

// githubListingPageSize is the per_page of every org listing request. GitHub
// pages by offset (page N at per_page P covers items (N-1)*P+1..N*P), so the
// page size must never change mid-listing: a smaller last page would re-read
// earlier repositories instead of reaching the next ones.
const githubListingPageSize = 100

// listGitHubOrgRepositoriesFrom pages through baseURL's org repository
// listing at githubListingPageSize and trims the result to repoLimit. Only an
// empty page or reaching repoLimit ends the listing; a short page mid-listing
// does not, so the repositories after it are still synced. The bool reports a
// complete listing: true only when an empty page ended it while the listed
// count was below repoLimit and that page's Link header had no rel="next". A
// listing that reaches repoLimit is incomplete, even when the org holds
// exactly repoLimit repositories, so a cut listing is never mistaken for the
// whole org. A missing Link header never ends the listing and never blocks
// completeness, because proxies may strip it.
func listGitHubOrgRepositoriesFrom(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	org string,
	repoLimit int,
	token string,
) ([]GitHubRepositoryRecord, bool, error) {
	return listGitHubOrgRepositoriesFromWithRetry(ctx, client, baseURL, org, repoLimit, token, rest.DefaultConfig())
}

// listGitHubOrgRepositoriesFromWithRetry is listGitHubOrgRepositoriesFrom
// with an explicit page-retry budget. Production callers use
// listGitHubOrgRepositoriesFrom; tests inject a clock through retry so the
// backoff schedule is asserted without waiting it out.
func listGitHubOrgRepositoriesFromWithRetry(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	org string,
	repoLimit int,
	token string,
	retry rest.Config,
) ([]GitHubRepositoryRecord, bool, error) {
	repositories := make([]GitHubRepositoryRecord, 0)
	complete := false
	for page := 1; len(repositories) < repoLimit; page++ {
		fetched, err := fetchGitHubListingPage(ctx, client, baseURL, org, page, token, retry)
		if err != nil {
			return nil, false, err
		}
		payload := fetched.items
		if len(payload) == 0 {
			complete = !gitlab.HasNextLink(fetched.linkNext)
			break
		}
		for _, item := range payload {
			repoID := normalizeRepositoryID(item.FullName)
			if repoID == "" {
				continue
			}
			repositories = append(repositories, GitHubRepositoryRecord{
				RepoID:   repoID,
				GitHubID: item.ID,
				Archived: item.Archived,
			})
		}
	}
	if len(repositories) >= repoLimit {
		complete = false
	}
	if len(repositories) > repoLimit {
		repositories = repositories[:repoLimit]
	}
	return repositories, complete, nil
}

// mintGitHubAppToken exchanges the configured GitHub App credentials for an
// installation token through the shared github/app minter. It keeps the org
// listing's retry budget and fault classes (#8084): a transient fault retries
// and then returns a *rest.RetryableError, while 401, 403 and 404 are final.
func mintGitHubAppToken(ctx context.Context, config RepoSyncConfig) (string, error) {
	baseURL, retry := config.GitHubAPI.Resolve()
	token, err := app.MintInstallationToken(ctx, app.Config{
		BaseURL:        baseURL,
		AppID:          config.GitHubAppID,
		InstallationID: config.GitHubAppInstallation,
		PrivateKey:     config.GitHubAppPrivateKey,
		MaxAttempts:    retry.MaxAttempts,
		BaseBackoff:    retry.BaseBackoff,
		Jitter:         retry.Jitter,
		Sleep:          retry.Sleep,
		Now:            retry.Now,
		Observe:        restMintObserver{ctx: ctx},
	})
	if err != nil {
		return "", mapMintError(err)
	}
	return token, nil
}

// mapMintError restores the discovery error shapes callers count on: a
// transient that outlasted the budget becomes the listing's
// *rest.RetryableError, while final faults stay plain errors.
func mapMintError(err error) error {
	var retryable *app.RetryableError
	if errors.As(err, &retryable) {
		return rest.NewRetryableError(app.OperationMint, retryable.Class, retryable.Attempts, retryable.RetryAfter, err)
	}
	return err
}

// restMintObserver forwards shared-minter callbacks to the discovery
// operation signals so the mint keeps counting under operation "mint".
type restMintObserver struct {
	ctx context.Context
}

// MintAttempt implements app.Observer.
func (o restMintObserver) MintAttempt(operation, class, outcome string) {
	rest.RecordAttempt(o.ctx, operation, class, outcome)
}

// MintRetry implements app.Observer.
func (o restMintObserver) MintRetry(operation, class string, attempt, status int, wait time.Duration) {
	rest.RecordRetry(o.ctx, operation, class, attempt, status, wait)
}
