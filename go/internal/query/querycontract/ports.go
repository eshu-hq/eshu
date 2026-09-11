// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "context"

// GraphQuery is the read-only graph traversal surface used by query handlers.
// Implementations must permit concurrent Run calls. Neo4jReader satisfies that
// contract with the driver's concurrent-safe connection pool and opens a
// separate read session for every attempt; callers must never share a session.
type GraphQuery interface {
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// ContentStore is the relational content-query surface used by read handlers.
type ContentStore interface {
	GetFileContent(ctx context.Context, repoID, relativePath string) (*FileContent, error)
	GetFileLines(ctx context.Context, repoID, relativePath string, startLine, endLine int) (*FileContent, error)
	GetEntityContent(ctx context.Context, entityID string) (*EntityContent, error)
	SearchFileContent(ctx context.Context, repoID, pattern string, limit int) ([]FileContent, error)
	SearchFileContentAnyRepo(ctx context.Context, pattern string, limit int) ([]FileContent, error)
	SearchFileContentAnyRepoExactCase(ctx context.Context, pattern string, limit int) ([]FileContent, error)
	SearchEntityContent(ctx context.Context, repoID, pattern string, limit int) ([]EntityContent, error)
	SearchEntityContentAnyRepo(ctx context.Context, pattern string, limit int) ([]EntityContent, error)
	SearchEntitiesByName(ctx context.Context, repoID, entityType, name string, limit int) ([]EntityContent, error)
	SearchEntitiesByNameAnyRepo(ctx context.Context, entityType, name string, limit int) ([]EntityContent, error)
	// SearchEntitiesByExactName and SearchEntitiesByExactNameAnyRepo are the
	// name-search twins that ask `entity_name = $n` where the two above ask
	// `entity_name ILIKE '%' || $n || '%'`.
	//
	// A caller that wants the one symbol it named cannot get it from the
	// substring read. That read fills its LIMIT page, in relative_path order,
	// with whatever CONTAINS the name, so a repository holding more than
	// `limit` near-misses -- PaymentGatewayFactory, PaymentGatewayBuilder,
	// PaymentGatewayAdapter -- hands back a full page with the exact
	// PaymentGateway nowhere in it. A caller that then keeps only the exact
	// names is left with nothing and reports a symbol that exists as missing
	// (#6555, the single-repository remainder of #6553's cross-repository
	// fix). Reading one repository at a time does not help: there is only one
	// repository, and a larger LIMIT only moves the row count at which the
	// same thing happens.
	//
	// These are additions rather than a change to the substring pair, which
	// several routes want exactly as it is -- a code search for "Handler"
	// must keep returning HandlerFactory. The two shapes are not
	// interchangeable; pick the one whose question the caller is asking.
	//
	// They are also additions rather than a reuse of EntityNameSearcher, the
	// narrow port whose SearchEntityNames already matches exactly and already
	// pushes a repository grant into one statement. Two things rule it out for
	// a caller that only wants a name resolved. Its statement is gated on
	// eshu_require_content_substring_indexes_ready(), which RAISES while a
	// deployment's deferred trigram bootstrap is unfinished (migration 057), so
	// routing exact resolution through it would make a read that needs no
	// trigram index fail whenever those indexes are not ready. And it joins
	// ingestion_scopes to carry a repo_name this caller never reads. Reusing it
	// would mean changing a statement the global entity-name routes share, to
	// serve a caller that wants strictly less than it offers.
	SearchEntitiesByExactName(ctx context.Context, repoID, entityType, name string, limit int) ([]EntityContent, error)
	SearchEntitiesByExactNameAnyRepo(ctx context.Context, entityType, name string, limit int) ([]EntityContent, error)
	SearchEntitiesReferencingComponent(ctx context.Context, repoID, componentName string, limit int) ([]EntityContent, error)
	ListRepoFiles(ctx context.Context, repoID string, limit int) ([]FileContent, error)
	ListRepoEntities(ctx context.Context, repoID string, limit int) ([]EntityContent, error)
	ListRepoEntitiesByType(ctx context.Context, repoID, entityType string, limit int) ([]EntityContent, error)
	// ListRepoEntitiesByTypes returns entities filtered to a SET of
	// entity_type values in one query (`entity_type = ANY($types)`), so a
	// caller that cares about several types together -- such as the
	// repository infrastructure panel spanning K8sResource, TerraformResource,
	// ArgoCDApplication, and the rest of isRepositoryInfrastructureType's list
	// -- can bound its LIMIT on that combined type-filtered set instead of
	// either the repo's total entity count (ListRepoEntities, which can push
	// the caller's actual type family past LIMIT while irrelevant types fill
	// the page) or issuing one ListRepoEntitiesByType call per type (#5764 P1
	// review follow-up).
	ListRepoEntitiesByTypes(ctx context.Context, repoID string, entityTypes []string, limit int) ([]EntityContent, error)
	ListRepoEntitiesByPaths(ctx context.Context, repoID string, relativePaths []string, limit int) ([]EntityContent, error)
	// ListRepoEntitiesByIDs hydrates the wide EntityContent rows for a bounded
	// entity-ID set (the impact-trace directed SELECTS scan re-fetches only the
	// Services that actually selector-match the traced Deployment; #5363).
	ListRepoEntitiesByIDs(ctx context.Context, repoID string, entityIDs []string, limit int) ([]EntityContent, error)
	// ListRepoK8sSelectCandidates returns the narrow matcher projection consumed
	// by K8s service-selector matching instead of hydrating EntityContent for the
	// candidate scan (#5363); it never carries the wide metadata JSONB.
	ListRepoK8sSelectCandidates(ctx context.Context, repoID string, limit int) ([]K8sSelectCandidate, error)
	SearchEntitiesByLanguageAndType(ctx context.Context, repoID, language, entityType, query string, limit int) ([]EntityContent, error)
	ListFrameworkRoutes(ctx context.Context, repoID string) ([]FrameworkRouteEvidence, error)
	RepositoryCoverage(ctx context.Context, repoID string) (RepositoryContentCoverage, error)
	// CountRepositoriesByLanguage, ListRepositoriesByLanguage, and
	// RepositoryLanguageInventory all aggregate over content_files, which is
	// keyed by repo_id but carries no scope grant of its own (#5167 Group B).
	// allScopes selects the admin/all-scopes path (no row filtering, byte-
	// identical to the pre-#5167 query). When allScopes is false, rows MUST be
	// restricted to allowedRepositoryIDs/allowedScopeIDs so a scoped caller
	// never observes another tenant's repository or language coverage; the
	// query handler (repository/language_inventory.go) short-circuits to an
	// empty page before calling these methods at all when a scoped caller
	// holds no grants, matching the #5137 LiveActivityStore precedent.
	CountRepositoriesByLanguage(
		ctx context.Context,
		languages []string,
		allScopes bool,
		allowedRepositoryIDs []string,
		allowedScopeIDs []string,
	) (RepositoryLanguageAggregate, error)
	ListRepositoriesByLanguage(
		ctx context.Context,
		languages []string,
		limit int,
		offset int,
		allScopes bool,
		allowedRepositoryIDs []string,
		allowedScopeIDs []string,
	) ([]RepositoryLanguageRepository, error)
	RepositoryLanguageInventory(
		ctx context.Context,
		limit int,
		offset int,
		allScopes bool,
		allowedRepositoryIDs []string,
		allowedScopeIDs []string,
	) ([]RepositoryLanguageInventoryRow, error)
	ListRepositories(ctx context.Context) ([]RepositoryCatalogEntry, error)
	MatchRepositories(ctx context.Context, selector string) ([]RepositoryCatalogEntry, error)
	ResolveRepository(ctx context.Context, selector string) (*RepositoryCatalogEntry, error)
}
