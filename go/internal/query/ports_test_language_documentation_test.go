// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

// This file holds fakePortContentStore's language-inventory and
// documentation-read-model methods, split out of ports_test.go to keep that
// file under the repository's 500-line cap (#5764 round-6 review follow-up
// added ListRepoEntitiesByTypes, which pushed the combined file over the
// limit).

func (f fakePortContentStore) CountRepositoriesByLanguage(
	_ context.Context,
	languages []string,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (RepositoryLanguageAggregate, error) {
	if f.languageCounts == nil {
		return RepositoryLanguageAggregate{}, nil
	}
	if !allScopes {
		filtered := fakeFilterLanguageRepos(f.languageRepos, allowedRepositoryIDs, allowedScopeIDs)
		var aggregate RepositoryLanguageAggregate
		for _, repo := range filtered {
			aggregate.RepositoryCount++
			aggregate.FileCount += repo.FileCount
			if repo.IndexedAt.After(aggregate.LastIndexedAt) {
				aggregate.LastIndexedAt = repo.IndexedAt
			}
		}
		return aggregate, nil
	}
	return f.languageCounts[strings.Join(languages, ",")], nil
}

func (f fakePortContentStore) ListRepositoriesByLanguage(
	_ context.Context,
	_ []string,
	limit int,
	offset int,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) ([]RepositoryLanguageRepository, error) {
	all := f.languageRepos
	if !allScopes {
		all = fakeFilterLanguageRepos(all, allowedRepositoryIDs, allowedScopeIDs)
	}
	if offset >= len(all) {
		return nil, nil
	}
	rows := all[offset:]
	if limit > 0 && limit < len(rows) {
		rows = rows[:limit]
	}
	return append([]RepositoryLanguageRepository(nil), rows...), nil
}

// fakeFilterLanguageRepos restricts languageRepos to those whose repository ID
// is in the merged allowed set, mirroring the real ContentReader's
// repo_id = ANY(allowed_repository_ids) OR repo_id = ANY(allowed_scope_ids)
// predicate for #5167 scoped-token test coverage.
func fakeFilterLanguageRepos(
	repos []RepositoryLanguageRepository,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) []RepositoryLanguageRepository {
	allowed := make(map[string]struct{}, len(allowedRepositoryIDs)+len(allowedScopeIDs))
	for _, id := range allowedRepositoryIDs {
		allowed[id] = struct{}{}
	}
	for _, id := range allowedScopeIDs {
		allowed[id] = struct{}{}
	}
	filtered := make([]RepositoryLanguageRepository, 0, len(repos))
	for _, repo := range repos {
		if _, ok := allowed[repo.Repository.ID]; ok {
			filtered = append(filtered, repo)
		}
	}
	return filtered
}

func (f fakePortContentStore) RepositoryLanguageInventory(
	_ context.Context,
	limit int,
	offset int,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) ([]RepositoryLanguageInventoryRow, error) {
	all := f.languageInventory
	if !allScopes {
		// The fake has no per-language repo_id linkage to intersect against a
		// grant, so a scoped caller with any grant sees no synthesized
		// inventory rows in tests that do not set up scoped fixtures directly.
		all = nil
	}
	if offset >= len(all) {
		return nil, nil
	}
	rows := all[offset:]
	if limit > 0 && limit < len(rows) {
		rows = rows[:limit]
	}
	return append([]RepositoryLanguageInventoryRow(nil), rows...), nil
}

func (f fakePortContentStore) RepositoryReadModelSummary(context.Context, string) (RepositoryReadModelSummary, error) {
	return f.summary, nil
}

func (f fakePortContentStore) RepositoryRelationshipReadModel(context.Context, string) (RepositoryRelationshipReadModel, error) {
	return f.relationshipReadModel, nil
}

func (f fakePortContentStore) RepositoryEntryPoints(context.Context, string) (repositoryEntryPointReadModel, error) {
	return f.entryPoints, nil
}

func (f fakePortContentStore) RepositoryDeploymentEvidence(context.Context, string) (repositoryDeploymentEvidenceReadModel, error) {
	if f.deploymentEvidenceErr != nil {
		return repositoryDeploymentEvidenceReadModel{}, f.deploymentEvidenceErr
	}
	return f.deploymentEvidence, nil
}

func (f fakePortContentStore) RelationshipEvidenceByResolvedID(context.Context, string) (relationshipEvidenceReadModel, error) {
	return f.relationshipEvidence, nil
}

func (f fakePortContentStore) DocumentationFindings(_ context.Context, filter documentationFindingFilter) (documentationFindingListReadModel, error) {
	if f.documentationFindingsFilter != nil {
		*f.documentationFindingsFilter = filter
	}
	if f.documentationFindingsErr != nil {
		return documentationFindingListReadModel{}, f.documentationFindingsErr
	}
	return f.documentationFindingsModel, nil
}

func (f fakePortContentStore) DocumentationFacts(_ context.Context, filter documentationFactFilter) (documentationFactListReadModel, error) {
	if f.documentationFactsFilter != nil {
		*f.documentationFactsFilter = filter
	}
	if f.documentationFactsErr != nil {
		return documentationFactListReadModel{}, f.documentationFactsErr
	}
	return f.documentationFactsModel, nil
}

func (f fakePortContentStore) DocumentationEvidencePacket(context.Context, string) (documentationEvidencePacketReadModel, error) {
	if f.documentationPacketErr != nil {
		return documentationEvidencePacketReadModel{}, f.documentationPacketErr
	}
	return f.documentationPacketModel, nil
}

func (f fakePortContentStore) DocumentationEvidencePacketWithFilter(
	_ context.Context,
	filter documentationEvidencePacketFilter,
) (documentationEvidencePacketReadModel, error) {
	if f.documentationPacketFilter != nil {
		*f.documentationPacketFilter = filter
	}
	if f.documentationPacketErr != nil {
		return documentationEvidencePacketReadModel{}, f.documentationPacketErr
	}
	return f.documentationPacketModel, nil
}

func (f fakePortContentStore) DocumentationEvidencePacketFreshness(
	context.Context,
	string,
	string,
) (documentationEvidencePacketFreshnessReadModel, error) {
	if f.documentationFreshnessErr != nil {
		return documentationEvidencePacketFreshnessReadModel{}, f.documentationFreshnessErr
	}
	return f.documentationFreshnessModel, nil
}

func (f fakePortContentStore) DocumentationEvidencePacketFreshnessWithFilter(
	_ context.Context,
	filter documentationEvidencePacketFreshnessFilter,
) (documentationEvidencePacketFreshnessReadModel, error) {
	if f.documentationFreshnessFilter != nil {
		*f.documentationFreshnessFilter = filter
	}
	if f.documentationFreshnessErr != nil {
		return documentationEvidencePacketFreshnessReadModel{}, f.documentationFreshnessErr
	}
	return f.documentationFreshnessModel, nil
}
