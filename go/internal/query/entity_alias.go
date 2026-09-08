// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6060: type aliases and thin forwarders for the moved entity family must live in package query so handler wiring, cmd constructors, and staying callers compile unchanged.

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/entity"
	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// entity_alias.go is the root alias shim for the entity handler family
// (#6060, lane B B5). EntityHandler and its method files moved to entity/,
// including the absorbed B4 EntityHandler service seam (story envelope,
// supply-chain enrichment, investigation, workload resolution) and the
// workload topology/provisioned-platform reads. The *ContentReader
// target-support seam stays in package query (service_story_target_support.go,
// service_story_target_support_source_only.go): Go requires methods to live
// with their receiver type, and ContentReader is a later lane's family.
// Names the rest of the program still spells `query.X` (handler wiring, cmd
// routers, staying root callers and tests) alias here so the move touches
// no caller outside the family.
//
// One home per symbol: nothing here implements behavior, it only aliases or
// forwards to the canonical home. New code must import entity directly.

// EntityHandler is the entity-handler family type. Its home is entity/;
// this alias keeps the APIRouter wiring and the cmd/api and cmd/mcp-server
// constructors spelling query.EntityHandler unchanged. See #6060.
type EntityHandler = entity.EntityHandler

// attachSemanticSummary attaches the entity semantic summary to a result
// row. Its home is entitysemantics; this wrapper keeps the staying
// language-query callers spelling the package-local name unchanged.
func attachSemanticSummary(result map[string]any) {
	entitysemantics.AttachSemanticSummary(result)
}

// safeStr extracts a string from a map while filtering empty and nil
// values. Its home is querycontract; this wrapper keeps the staying
// visualization caller spelling the package-local name unchanged.
func safeStr(m map[string]any, key string) string {
	return querycontract.SafeStr(m, key)
}

// resolveEntityRequest is the request body for entity resolution. Its home
// is entity/; this alias keeps the staying queryplan production-binding
// tests spelling the package-local name unchanged. See #6060.
type resolveEntityRequest = entity.ResolveEntityRequest

// buildResolveEntityGraphQuery renders the repository-anchored entity
// resolution Cypher. Its home is entity/; this forwarder keeps the staying
// queryplan production-binding tests calling the package-local name.
func buildResolveEntityGraphQuery(
	req resolveEntityRequest,
	limit int,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	return entity.BuildResolveEntityGraphQuery(req, limit, access)
}

// buildResolveWorkloadQueries renders the property and relationship
// workload resolution Cypher. Its home is entity/; this forwarder keeps
// the staying queryplan production-binding tests calling the package-local
// name.
func buildResolveWorkloadQueries(
	name string,
	repoID string,
	limit int,
	access querycontract.RepositoryAccessFilter,
) (string, string, map[string]any) {
	return entity.BuildResolveWorkloadQueries(name, repoID, limit, access)
}

// buildWorkloadStory creates a narrative summary of a workload's
// deployment. The implementation lives in querycontract; this wrapper keeps
// the staying evidence-boundary, endpoint, and workload tests calling the
// package-local name.
func buildWorkloadStory(ctx map[string]any) string {
	return querycontract.BuildWorkloadStory(ctx)
}

// contentEntityTypeForResolve maps a resolve_entity entity_type filter
// value to its content-entity label. The implementation lives in
// querycontract; this wrapper keeps the staying content-relationship tests
// calling the package-local name.
func contentEntityTypeForResolve(typeName string) string {
	return querycontract.ContentEntityTypeForResolve(typeName)
}

// resolveExactGraphEntityCandidates lists exact-name entity candidates.
// The implementation lives in querycontract; this wrapper keeps the staying
// authorization and resolution tests calling the package-local name.
func resolveExactGraphEntityCandidates(
	ctx context.Context,
	reader querycontract.ContentStore,
	repoID string,
	name string,
) ([]querycontract.EntityContent, error) {
	return querycontract.ResolveExactGraphEntityCandidates(ctx, reader, repoID, name)
}

// resolveExactGraphEntityCandidate resolves one exact-name entity
// candidate. The implementation lives in querycontract; this wrapper keeps
// the staying resolution tests calling the package-local name.
func resolveExactGraphEntityCandidate(
	ctx context.Context,
	reader querycontract.ContentStore,
	repoID string,
	name string,
) (*querycontract.EntityContent, error) {
	return querycontract.ResolveExactGraphEntityCandidate(ctx, reader, repoID, name)
}

// provisionedPlatformTopologyEdges shapes a provisioned-platform row into
// topology edges. Its home is entity/; this forwarder keeps the staying
// OpenAPI deployment-identity test calling the package-local name.
func provisionedPlatformTopologyEdges(row map[string]any) []map[string]any {
	return entity.ProvisionedPlatformTopologyEdges(row)
}

// workloadPlatformEdgeLimit caps attached-platform edge selection per
// workload read. Its home is entity/; this alias keeps the staying live
// determinism test spelling the package-local name unchanged. See #6060.
const workloadPlatformEdgeLimit = entity.WorkloadPlatformEdgeLimit

// GlobalContentEntityFilter is the content-index name filter for one global
// entity type. Its home is entity/; this alias keeps the staying
// name-search test reading filter fields unchanged. See #6060.
type GlobalContentEntityFilter = entity.GlobalContentEntityFilter

// globalContentEntityNameFilter maps a global entity type name to its
// content-index filter. Its home is entity/; this forwarder keeps the
// staying name-search test calling the package-local name. See #6060.
func globalContentEntityNameFilter(typeName string) (GlobalContentEntityFilter, bool) {
	return entity.GlobalContentEntityNameFilter(typeName)
}

// githubActionsSourceCacheTruncationReason is the stable partial-truth
// reason for workflow-source truncation. Its home is entity/; this alias
// keeps the staying OpenAPI entity-context test spelling the package-local
// name unchanged. See #6060.
const githubActionsSourceCacheTruncationReason = entity.GithubActionsSourceCacheTruncationReason

// normalizeResolveEntityLimit clamps a resolve limit to the sane range.
// Its home is entity/; this forwarder keeps the staying queryplan execution
// test calling the package-local name. See #6060.
func normalizeResolveEntityLimit(limit int) int {
	return entity.NormalizeResolveEntityLimit(limit)
}

// serviceWorkloadCandidateLimit bounds service-workload candidate selection
// per read. Its home is entity/; this alias keeps the staying legacy
// queryplan test spelling the package-local name unchanged. See #6060.
const serviceWorkloadCandidateLimit = entity.ServiceWorkloadCandidateLimit

// buildHydrateResolvedWorkloadRepoNamesQuery builds the repository-name
// hydration query. Its home is entity/; this forwarder keeps the staying
// queryplan execution test calling the package-local name. See #6060.
func buildHydrateResolvedWorkloadRepoNamesQuery(repoIDs []string, access querycontract.RepositoryAccessFilter) (string, map[string]any) {
	return entity.BuildHydrateResolvedWorkloadRepoNamesQuery(repoIDs, access)
}
