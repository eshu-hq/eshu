// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// FakePortContentStore's repository-catalog and language-inventory reads live
// here, split from portcontentstore.go to keep both files under the repo's
// 500-line cap.

// ListRepositories returns a copy of the fixture catalog.
func (f FakePortContentStore) ListRepositories(context.Context) ([]querycontract.RepositoryCatalogEntry, error) {
	return append([]querycontract.RepositoryCatalogEntry(nil), f.Repositories...), nil
}

// ListWorkloadIdentities returns a copy of the fixture workload handles and
// never reports truncation: the double holds whatever the test installed, so
// there is nothing beyond it that a limit could cut.
func (f FakePortContentStore) ListWorkloadIdentities(
	context.Context,
	int,
) ([]querycontract.CatalogWorkloadIdentityEntry, bool, error) {
	return append([]querycontract.CatalogWorkloadIdentityEntry(nil), f.WorkloadIdentities...), false, nil
}

// MatchRepositories returns every fixture repository the selector names by any
// of its identifying fields, mirroring the production selector match.
func (f FakePortContentStore) MatchRepositories(
	_ context.Context,
	selector string,
) ([]querycontract.RepositoryCatalogEntry, error) {
	matches := make([]querycontract.RepositoryCatalogEntry, 0, 1)
	for _, repo := range f.Repositories {
		switch selector {
		case repo.ID, repo.Name, repo.Path, repo.LocalPath, repo.RemoteURL, repo.RepoSlug:
			matches = append(matches, repo)
		}
	}
	return matches, nil
}

// ResolveRepository returns the first fixture repository regardless of
// selector. Callers that care which repository resolves assert through
// MatchRepositories instead.
func (f FakePortContentStore) ResolveRepository(
	context.Context,
	string,
) (*querycontract.RepositoryCatalogEntry, error) {
	if len(f.Repositories) == 0 {
		return nil, nil
	}
	repo := f.Repositories[0]
	return &repo, nil
}

// CountRepositoriesByLanguage aggregates the language repositories a caller is
// entitled to see.
//
// The allScopes branch is the admin path and reads the pre-aggregated
// LanguageCounts fixture straight through. A scoped caller instead gets a
// count recomputed over the filtered rows, because an aggregate computed
// before filtering would report repositories the caller cannot see -- the
// cross-tenant leak the real query's row restriction exists to prevent
// (#5167).
func (f FakePortContentStore) CountRepositoriesByLanguage(
	_ context.Context,
	languages []string,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (querycontract.RepositoryLanguageAggregate, error) {
	if f.LanguageCounts == nil {
		return querycontract.RepositoryLanguageAggregate{}, nil
	}
	if !allScopes {
		filtered := FilterLanguageRepos(f.LanguageRepos, allowedRepositoryIDs, allowedScopeIDs)
		var aggregate querycontract.RepositoryLanguageAggregate
		for _, repo := range filtered {
			aggregate.RepositoryCount++
			aggregate.FileCount += repo.FileCount
			if repo.IndexedAt.After(aggregate.LastIndexedAt) {
				aggregate.LastIndexedAt = repo.IndexedAt
			}
		}
		return aggregate, nil
	}
	return f.LanguageCounts[strings.Join(languages, ",")], nil
}

// ListRepositoriesByLanguage pages the language repositories a caller is
// entitled to see, filtering before offset and limit for the same reason
// CountRepositoriesByLanguage does.
func (f FakePortContentStore) ListRepositoriesByLanguage(
	_ context.Context,
	_ []string,
	limit int,
	offset int,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) ([]querycontract.RepositoryLanguageRepository, error) {
	all := f.LanguageRepos
	if !allScopes {
		all = FilterLanguageRepos(all, allowedRepositoryIDs, allowedScopeIDs)
	}
	if offset >= len(all) {
		return nil, nil
	}
	rows := all[offset:]
	if limit > 0 && limit < len(rows) {
		rows = rows[:limit]
	}
	return append([]querycontract.RepositoryLanguageRepository(nil), rows...), nil
}

// RepositoryLanguageInventory pages the synthesized inventory rows.
//
// A scoped caller sees none of them. The fixture carries no per-language
// repo_id to intersect a grant against, so there is no honest way to filter
// these rows; reporting them all would leak, and reporting them under a scope
// they were never checked against would be worse. Tests that need scoped
// inventory build the scoped fixture directly.
func (f FakePortContentStore) RepositoryLanguageInventory(
	_ context.Context,
	limit int,
	offset int,
	allScopes bool,
	_ []string,
	_ []string,
) ([]querycontract.RepositoryLanguageInventoryRow, error) {
	all := f.LanguageInventory
	if !allScopes {
		all = nil
	}
	if offset >= len(all) {
		return nil, nil
	}
	rows := all[offset:]
	if limit > 0 && limit < len(rows) {
		rows = rows[:limit]
	}
	return append([]querycontract.RepositoryLanguageInventoryRow(nil), rows...), nil
}

// FilterLanguageRepos restricts repos to those whose repository ID is in the
// merged allowed set, mirroring the real ContentReader's
// repo_id = ANY(allowed_repository_ids) OR repo_id = ANY(allowed_scope_ids)
// predicate for #5167 scoped-token test coverage.
func FilterLanguageRepos(
	repos []querycontract.RepositoryLanguageRepository,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) []querycontract.RepositoryLanguageRepository {
	allowed := make(map[string]struct{}, len(allowedRepositoryIDs)+len(allowedScopeIDs))
	for _, id := range allowedRepositoryIDs {
		allowed[id] = struct{}{}
	}
	for _, id := range allowedScopeIDs {
		allowed[id] = struct{}{}
	}
	filtered := make([]querycontract.RepositoryLanguageRepository, 0, len(repos))
	for _, repo := range repos {
		if _, ok := allowed[repo.Repository.ID]; ok {
			filtered = append(filtered, repo)
		}
	}
	return filtered
}
