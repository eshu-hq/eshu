// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

// This file holds fakePortContentStore's language-inventory, catalog, and
// documentation read-model forwarders, split out of ports_test.go to keep that
// file under the repository's 500-line cap. Each one forwards to
// querytestutil.PortContentStore; the behavior lives there so handler families
// outside package query can share the same double (#6060).

func (f fakePortContentStore) CountRepositoriesByLanguage(
	ctx context.Context,
	languages []string,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (RepositoryLanguageAggregate, error) {
	return f.promoted().CountRepositoriesByLanguage(ctx, languages, allScopes, allowedRepositoryIDs, allowedScopeIDs)
}

func (f fakePortContentStore) ListRepositoriesByLanguage(
	ctx context.Context,
	languages []string,
	limit int,
	offset int,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) ([]RepositoryLanguageRepository, error) {
	return f.promoted().ListRepositoriesByLanguage(
		ctx,
		languages,
		limit,
		offset,
		allScopes,
		allowedRepositoryIDs,
		allowedScopeIDs,
	)
}

func (f fakePortContentStore) RepositoryLanguageInventory(
	ctx context.Context,
	limit int,
	offset int,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) ([]RepositoryLanguageInventoryRow, error) {
	return f.promoted().RepositoryLanguageInventory(
		ctx,
		limit,
		offset,
		allScopes,
		allowedRepositoryIDs,
		allowedScopeIDs,
	)
}

func (f fakePortContentStore) ListRepositories(ctx context.Context) ([]RepositoryCatalogEntry, error) {
	return f.promoted().ListRepositories(ctx)
}

func (f fakePortContentStore) ListWorkloadIdentities(
	ctx context.Context,
	limit int,
) ([]CatalogWorkloadIdentityEntry, bool, error) {
	return f.promoted().ListWorkloadIdentities(ctx, limit)
}

func (f fakePortContentStore) MatchRepositories(
	ctx context.Context,
	selector string,
) ([]RepositoryCatalogEntry, error) {
	return f.promoted().MatchRepositories(ctx, selector)
}

func (f fakePortContentStore) ResolveRepository(
	ctx context.Context,
	selector string,
) (*RepositoryCatalogEntry, error) {
	return f.promoted().ResolveRepository(ctx, selector)
}

func (f fakePortContentStore) RepositoryReadModelSummary(
	ctx context.Context,
	repoID string,
) (RepositoryReadModelSummary, error) {
	return f.promoted().RepositoryReadModelSummary(ctx, repoID)
}

func (f fakePortContentStore) RepositoryRelationshipReadModel(
	ctx context.Context,
	repoID string,
) (RepositoryRelationshipReadModel, error) {
	return f.promoted().RepositoryRelationshipReadModel(ctx, repoID)
}

func (f fakePortContentStore) RepositoryEntryPoints(
	ctx context.Context,
	repoID string,
) (repositoryEntryPointReadModel, error) {
	return f.promoted().RepositoryEntryPoints(ctx, repoID)
}

func (f fakePortContentStore) RepositoryDeploymentEvidence(
	ctx context.Context,
	repoID string,
) (repositoryDeploymentEvidenceReadModel, error) {
	return f.promoted().RepositoryDeploymentEvidence(ctx, repoID)
}

func (f fakePortContentStore) RelationshipEvidenceByResolvedID(
	ctx context.Context,
	resolvedID string,
) (relationshipEvidenceReadModel, error) {
	return f.promoted().RelationshipEvidenceByResolvedID(ctx, resolvedID)
}

func (f fakePortContentStore) DocumentationFindings(
	ctx context.Context,
	filter documentationFindingFilter,
) (documentationFindingListReadModel, error) {
	return f.promoted().DocumentationFindings(ctx, filter)
}

func (f fakePortContentStore) DocumentationFacts(
	ctx context.Context,
	filter documentationFactFilter,
) (documentationFactListReadModel, error) {
	return f.promoted().DocumentationFacts(ctx, filter)
}

func (f fakePortContentStore) DocumentationEvidencePacket(
	ctx context.Context,
	findingID string,
) (documentationEvidencePacketReadModel, error) {
	return f.promoted().DocumentationEvidencePacket(ctx, findingID)
}

func (f fakePortContentStore) DocumentationEvidencePacketWithFilter(
	ctx context.Context,
	filter documentationEvidencePacketFilter,
) (documentationEvidencePacketReadModel, error) {
	return f.promoted().DocumentationEvidencePacketWithFilter(ctx, filter)
}

func (f fakePortContentStore) DocumentationEvidencePacketFreshness(
	ctx context.Context,
	packetID string,
	savedPacketVersion string,
) (documentationEvidencePacketFreshnessReadModel, error) {
	return f.promoted().DocumentationEvidencePacketFreshness(ctx, packetID, savedPacketVersion)
}

func (f fakePortContentStore) DocumentationEvidencePacketFreshnessWithFilter(
	ctx context.Context,
	filter documentationEvidencePacketFreshnessFilter,
) (documentationEvidencePacketFreshnessReadModel, error) {
	return f.promoted().DocumentationEvidencePacketFreshnessWithFilter(ctx, filter)
}
