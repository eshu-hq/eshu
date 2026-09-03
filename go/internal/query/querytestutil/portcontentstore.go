// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// PortContentStore is the shared content-read double for handler tests. It
// satisfies querycontract.ContentStore plus the narrow optional ports package
// query type-asserts a store against, answering from fixture slices a caller
// installs instead of reaching Postgres.
//
// It lives here rather than in a package query test file for the reason that
// shapes all of epic #6053 (#6060): a symbol declared in a _test.go file is
// not part of the importable package, so a handler family that moves out of
// package query cannot reach it. The alternative is each family re-declaring
// its own copy, and a re-declared double that drifts from the real port keeps
// passing while guarding nothing.
//
// The fields are exported because an unexported field is unreachable from
// another package -- a caller could name the type but never fill it in.
// Package query keeps an unexported adapter with the field names its ~126
// existing test files already use, and that adapter delegates here rather
// than reimplementing anything.
//
// The zero value is usable. Many callers construct it empty just to satisfy a
// port, so every unset fixture answers with no rows rather than panicking.
//
// Several methods deliberately mirror the production SQL's predicate order --
// filter first, then LIMIT. A double that limited first would hand a test rows
// the real query would not return, which is the failure mode a double exists
// to avoid.
type PortContentStore struct {
	// Coverage answers RepositoryCoverage.
	Coverage querycontract.RepositoryContentCoverage
	// Summary answers RepositoryReadModelSummary.
	Summary querycontract.RepositoryReadModelSummary
	// RelationshipReadModel answers RepositoryRelationshipReadModel.
	RelationshipReadModel querycontract.RepositoryRelationshipReadModel
	// EntryPoints answers RepositoryEntryPoints.
	EntryPoints querycontract.RepositoryEntryPointReadModel
	// DeploymentEvidence and DeploymentEvidenceErr answer
	// RepositoryDeploymentEvidence. The error wins when both are set, so a
	// test can cover the failure path without clearing the fixture.
	DeploymentEvidence    querycontract.RepositoryDeploymentEvidenceReadModel
	DeploymentEvidenceErr error
	// RelationshipEvidence answers RelationshipEvidenceByResolvedID.
	RelationshipEvidence querycontract.RelationshipEvidenceReadModel

	// DocumentationFindingsModel, DocumentationFindingsErr, and
	// DocumentationFindingsFilter answer DocumentationFindings. The filter
	// pointer is a capture slot: when non-nil the double writes the filter it
	// was called with through it, which is how scope-grant tests assert on the
	// authorization fields a handler built rather than only on the rows.
	DocumentationFindingsModel  querycontract.DocumentationFindingListReadModel
	DocumentationFindingsErr    error
	DocumentationFindingsFilter *querycontract.DocumentationFindingFilter
	// The documentation-fact, packet, and freshness triples work the same way.
	DocumentationFactsModel      querycontract.DocumentationFactListReadModel
	DocumentationFactsErr        error
	DocumentationFactsFilter     *querycontract.DocumentationFactFilter
	DocumentationPacketModel     querycontract.DocumentationEvidencePacketReadModel
	DocumentationPacketErr       error
	DocumentationPacketFilter    *querycontract.DocumentationEvidencePacketFilter
	DocumentationFreshnessModel  querycontract.DocumentationEvidencePacketFreshnessReadModel
	DocumentationFreshnessErr    error
	DocumentationFreshnessFilter *querycontract.DocumentationEvidencePacketFreshnessFilter
	// TargetSupportModel and TargetSupportErr answer
	// ServiceStoryTargetSupportEvidence; both are returned as given.
	TargetSupportModel querycontract.ServiceStoryTargetSupportReadModel
	TargetSupportErr   error

	// Entities backs every entity read: the by-type, by-types, by-paths, and
	// by-ID fetches and the K8s candidate scan all filter this one slice, so a
	// single fixture set drives them consistently.
	Entities []querycontract.EntityContent
	// RepoFiles backs GetFileContent and ListRepoFiles.
	RepoFiles []querycontract.FileContent
	// RepositoryRefs answers ListRepositoryRefs.
	RepositoryRefs []querycontract.RepositoryRef
	// Repositories backs ListRepositories, MatchRepositories, and
	// ResolveRepository.
	Repositories []querycontract.RepositoryCatalogEntry
	// LanguageRepos, LanguageCounts, and LanguageInventory back the language
	// inventory reads.
	LanguageRepos     []querycontract.RepositoryLanguageRepository
	LanguageCounts    map[string]querycontract.RepositoryLanguageAggregate
	LanguageInventory []querycontract.RepositoryLanguageInventoryRow
	// WorkloadIdentities answers ListWorkloadIdentities.
	WorkloadIdentities []querycontract.CatalogWorkloadIdentityEntry
}

var _ querycontract.ContentStore = (*PortContentStore)(nil)

// GetFileContent returns the first fixture file at relativePath, repo-filtered.
// A fixture with an empty RepoID matches any repository, so a test that does
// not care about repo scoping can leave it unset.
func (f PortContentStore) GetFileContent(
	_ context.Context,
	repoID, relativePath string,
) (*querycontract.FileContent, error) {
	for i := range f.RepoFiles {
		file := f.RepoFiles[i]
		if file.RepoID != "" && repoID != "" && file.RepoID != repoID {
			continue
		}
		if file.RelativePath == relativePath {
			return &file, nil
		}
	}
	return nil, nil
}

// GetFileLines reports no content. Callers that need line ranges assert
// against a real reader.
func (f PortContentStore) GetFileLines(
	context.Context,
	string,
	string,
	int,
	int,
) (*querycontract.FileContent, error) {
	return nil, nil
}

// GetEntityContent reports no entity.
func (f PortContentStore) GetEntityContent(context.Context, string) (*querycontract.EntityContent, error) {
	return nil, nil
}

// SearchFileContent reports no matches.
func (f PortContentStore) SearchFileContent(
	context.Context,
	string,
	string,
	int,
) ([]querycontract.FileContent, error) {
	return nil, nil
}

// SearchFileContentAnyRepo reports no matches.
func (f PortContentStore) SearchFileContentAnyRepo(
	context.Context,
	string,
	int,
) ([]querycontract.FileContent, error) {
	return nil, nil
}

// SearchFileContentAnyRepoExactCase reports no matches.
func (f PortContentStore) SearchFileContentAnyRepoExactCase(
	context.Context,
	string,
	int,
) ([]querycontract.FileContent, error) {
	return nil, nil
}

// SearchEntityContent reports no matches.
func (f PortContentStore) SearchEntityContent(
	context.Context,
	string,
	string,
	int,
) ([]querycontract.EntityContent, error) {
	return nil, nil
}

// SearchEntityContentAnyRepo reports no matches.
func (f PortContentStore) SearchEntityContentAnyRepo(
	context.Context,
	string,
	int,
) ([]querycontract.EntityContent, error) {
	return nil, nil
}

// SearchEntitiesByName reports no matches.
func (f PortContentStore) SearchEntitiesByName(
	context.Context,
	string,
	string,
	string,
	int,
) ([]querycontract.EntityContent, error) {
	return nil, nil
}

// SearchEntitiesByNameAnyRepo reports no matches.
func (f PortContentStore) SearchEntitiesByNameAnyRepo(
	context.Context,
	string,
	string,
	int,
) ([]querycontract.EntityContent, error) {
	return nil, nil
}

// SearchEntitiesReferencingComponent reports no matches.
func (f PortContentStore) SearchEntitiesReferencingComponent(
	context.Context,
	string,
	string,
	int,
) ([]querycontract.EntityContent, error) {
	return nil, nil
}

// SearchEntitiesByLanguageAndType reports no matches.
func (f PortContentStore) SearchEntitiesByLanguageAndType(
	context.Context,
	string,
	string,
	string,
	string,
	int,
) ([]querycontract.EntityContent, error) {
	return nil, nil
}

// ListFrameworkRoutes reports no routes.
func (f PortContentStore) ListFrameworkRoutes(
	context.Context,
	string,
) ([]querycontract.FrameworkRouteEvidence, error) {
	return nil, nil
}

// ListRepoFiles returns the repo's fixture files up to limit.
func (f PortContentStore) ListRepoFiles(
	_ context.Context,
	repoID string,
	limit int,
) ([]querycontract.FileContent, error) {
	files := make([]querycontract.FileContent, 0, len(f.RepoFiles))
	for _, file := range f.RepoFiles {
		if file.RepoID != "" && repoID != "" && file.RepoID != repoID {
			continue
		}
		files = append(files, file)
		if limit > 0 && len(files) >= limit {
			break
		}
	}
	return files, nil
}

// ListRepositoryRefs returns a copy of the fixture refs.
func (f PortContentStore) ListRepositoryRefs(
	context.Context,
	string,
) ([]querycontract.RepositoryRef, error) {
	return append([]querycontract.RepositoryRef(nil), f.RepositoryRefs...), nil
}

// ListRepoEntities returns the fixture entities up to limit, unfiltered by
// repository.
func (f PortContentStore) ListRepoEntities(
	_ context.Context,
	_ string,
	limit int,
) ([]querycontract.EntityContent, error) {
	if limit > 0 && limit < len(f.Entities) {
		return append([]querycontract.EntityContent(nil), f.Entities[:limit]...), nil
	}
	return append([]querycontract.EntityContent(nil), f.Entities...), nil
}

// ListRepoEntitiesByType filters Entities by entity_type before applying
// limit, mirroring the production ContentReader.ListRepoEntitiesByType
// predicate order (type filter first, then limit) so callers exercising the
// double still see the truncation-avoidance behavior the real query provides.
func (f PortContentStore) ListRepoEntitiesByType(
	_ context.Context,
	repoID, entityType string,
	limit int,
) ([]querycontract.EntityContent, error) {
	filtered := make([]querycontract.EntityContent, 0, len(f.Entities))
	for _, entity := range f.Entities {
		if repoID != "" && entity.RepoID != "" && entity.RepoID != repoID {
			continue
		}
		if entity.EntityType != entityType {
			continue
		}
		filtered = append(filtered, entity)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

// ListRepoEntitiesByTypes filters Entities by entity_type SET membership
// before applying limit, mirroring the production
// ContentReader.ListRepoEntitiesByTypes predicate order (`entity_type =
// ANY($types)` then `LIMIT`) so callers exercising the double see the same
// type-filtered-bound behavior the real query provides (#5764 P1 review
// follow-up).
func (f PortContentStore) ListRepoEntitiesByTypes(
	_ context.Context,
	repoID string,
	entityTypes []string,
	limit int,
) ([]querycontract.EntityContent, error) {
	allowed := make(map[string]struct{}, len(entityTypes))
	for _, entityType := range entityTypes {
		allowed[entityType] = struct{}{}
	}
	filtered := make([]querycontract.EntityContent, 0, len(f.Entities))
	for _, entity := range f.Entities {
		if repoID != "" && entity.RepoID != "" && entity.RepoID != repoID {
			continue
		}
		if _, ok := allowed[entity.EntityType]; !ok {
			continue
		}
		filtered = append(filtered, entity)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

// ListRepoEntitiesByPaths returns the repo's entities whose relative_path is in
// the requested set, up to limit. Unlike the other entity reads this one
// requires an exact repo match: a path set is only meaningful within one
// repository.
func (f PortContentStore) ListRepoEntitiesByPaths(
	_ context.Context,
	repoID string,
	relativePaths []string,
	limit int,
) ([]querycontract.EntityContent, error) {
	pathSet := map[string]struct{}{}
	for _, path := range relativePaths {
		pathSet[path] = struct{}{}
	}
	results := make([]querycontract.EntityContent, 0)
	for _, entity := range f.Entities {
		if entity.RepoID != repoID {
			continue
		}
		if _, ok := pathSet[entity.RelativePath]; !ok {
			continue
		}
		results = append(results, entity)
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results, nil
}

// ListRepoEntitiesByIDs returns Entities whose entity_id is in the requested
// set, repo-filtered, ordered deterministically by relative_path/start_line/
// entity_id to mirror the production ContentReader.ListRepoEntitiesByIDs.
func (f PortContentStore) ListRepoEntitiesByIDs(
	_ context.Context,
	repoID string,
	entityIDs []string,
	limit int,
) ([]querycontract.EntityContent, error) {
	idSet := make(map[string]struct{}, len(entityIDs))
	for _, id := range entityIDs {
		idSet[id] = struct{}{}
	}
	filtered := make([]querycontract.EntityContent, 0, len(entityIDs))
	for _, entity := range f.Entities {
		if repoID != "" && entity.RepoID != "" && entity.RepoID != repoID {
			continue
		}
		if _, ok := idSet[entity.EntityID]; !ok {
			continue
		}
		filtered = append(filtered, entity)
	}
	SortEntityContentByLocation(filtered)
	if limit > 0 && limit < len(filtered) {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

// ListRepoK8sSelectCandidates projects Entities' K8sResource rows into the
// narrow K8sSelectCandidate shape through the same helper the production
// narrow SQL mirrors (querycontract.K8sSelectCandidateFromEntity), preserving
// the comma-ok tri-state and the relative_path/start_line/entity_id ordering.
func (f PortContentStore) ListRepoK8sSelectCandidates(
	_ context.Context,
	repoID string,
	limit int,
) ([]querycontract.K8sSelectCandidate, error) {
	filtered := make([]querycontract.EntityContent, 0, len(f.Entities))
	for _, entity := range f.Entities {
		if repoID != "" && entity.RepoID != "" && entity.RepoID != repoID {
			continue
		}
		if entity.EntityType != "K8sResource" {
			continue
		}
		filtered = append(filtered, entity)
	}
	SortEntityContentByLocation(filtered)
	candidates := make([]querycontract.K8sSelectCandidate, 0, len(filtered))
	for _, entity := range filtered {
		candidates = append(candidates, querycontract.K8sSelectCandidateFromEntity(entity))
		if limit > 0 && len(candidates) >= limit {
			break
		}
	}
	return candidates, nil
}

// RepositoryCoverage returns the fixture coverage.
func (f PortContentStore) RepositoryCoverage(
	context.Context,
	string,
) (querycontract.RepositoryContentCoverage, error) {
	return f.Coverage, nil
}

// SortEntityContentByLocation orders rows by relative_path, start_line,
// entity_id, matching the production ORDER BY so a double's truncation drop
// set and candidate order are deterministic. It sorts in place.
func SortEntityContentByLocation(entities []querycontract.EntityContent) {
	sort.SliceStable(entities, func(i, j int) bool {
		if entities[i].RelativePath != entities[j].RelativePath {
			return entities[i].RelativePath < entities[j].RelativePath
		}
		if entities[i].StartLine != entities[j].StartLine {
			return entities[i].StartLine < entities[j].StartLine
		}
		return entities[i].EntityID < entities[j].EntityID
	})
}
