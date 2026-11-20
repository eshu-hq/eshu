// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	contractrepository "github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
	"github.com/eshu-hq/eshu/go/internal/scope/selection"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// RepositoryReadsReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the repository list, inventory, and
// detail reads (the catalog, repository list, by-language, and
// language-inventory reads plus the per-repository branches, content,
// context, coverage, freshness, stats, and tree reads). Canonical reads
// persist through explicit core-owned admission contracts: eshu-api keeps
// the HTTP shape and calls this versioned port, which the core-owned
// graph reader, content reader, freshness reader, selection and
// retirement readers, and service-catalog correlation store satisfy
// jointly, composed by the consumer. A breaking change to the read shape
// fails the frozen method-set test until that list is deliberately
// updated; bump this version alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers can
// pin the shape.
const RepositoryReadsReadAdmissionContractVersion = "v0"

// RepositoryReadsReadAdmission is the versioned admission-contract
// port the API handler will depend on instead of the shared handles.
// It declares exactly the seventeen reads the repository list and
// detail routes admit through — the bounded graph reads (Run,
// RunSingle, mirroring querycontract.GraphQuery as exercised through
// the list, context, and stats legs), the content reads
// (GetFileContent, ListRepoFiles, the language and path list facets,
// the path-context facet, RepositoryCoverage, ResolveRepository, the
// by-language aggregates, and the language inventory, mirroring
// querycontract.ContentStore plus the tree and catalog narrow facets
// as exercised through the content, tree, context, branches, stats,
// by-language, and language-inventory legs), the catalog reads
// (ListWorkloadIdentities, mirroring the catalog facet, plus
// ListServiceCatalogCorrelations, mirroring
// querycontract.ServiceCatalogCorrelationStore, as exercised through
// the catalog leg), the freshness read (ReadRepositoryFreshness,
// mirroring FreshnessReader as exercised through the freshness leg),
// and the list annotations (ReadPageSelectionsBySlug, mirroring
// PageSelectionReader, plus ReadPageRetirements, mirroring
// PageRetirementReader, as exercised through the list leg) — so the
// boundary is held honest by the compile-time mirror assertion below
// plus the frozen method-set contract test. It will back the eshu-api
// extraction handoff for these reads. The seam is read-only on
// purpose: every declared method is a bounded read and the port
// carries no mutation surface, so a dest consumer implementing only
// this contract can serve the repository reads but cannot acquire a
// write it did not bargain for through it.
//
// W-wave consumer: W5 repositories list/detail (11 oracle Repositories
// rows; the story route rides the sibling RepositoryStoryReadAdmission
// pin, never this one).
//
// Narrow by construction, recorded not silently assumed: the methods
// re-declare the store signatures in this repository-reads-scoped shape
// instead of embedding the stores. This pin therefore grants nothing to
// the other families that read through shared ports — every other store
// consumer keeps its own per-route relay reason until deliberate pins
// cover them.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - The repository story read (GET
//     /api/v0/repositories/{repo_id}/story) rides the sibling
//     RepositoryStoryReadAdmission pin in this package, never this
//     one: minting a whole-Repositories story here would retroactively
//     widen D4, which stays forbidden.
//   - The CI/CD run-correlation read rides the sibling CICD pin; the
//     sibling story pin already cites it and this pin does not
//     re-adopt it.
//   - The manifest-dependency section of the context response rides
//     the ManifestDependencies pin; a nil store omits the section.
//     That family stands on its own re-declaration.
type RepositoryReadsReadAdmission interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
	RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error)
	GetFileContent(ctx context.Context, repoID, relativePath string) (*querycontract.FileContent, error)
	ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error)
	ListRepoFilesByLanguage(ctx context.Context, repoID string, languages []string, pathPrefix string, limit int) ([]querycontract.FileContent, error)
	ListRepoFilesByPath(ctx context.Context, repoID, pathPrefix string, limit int) ([]querycontract.FileContent, error)
	RepoFilePathContext(ctx context.Context, repoID, requestPath string) (bool, string, error)
	RepositoryCoverage(ctx context.Context, repoID string) (querycontract.RepositoryContentCoverage, error)
	ResolveRepository(ctx context.Context, selector string) (*querycontract.RepositoryCatalogEntry, error)
	ReadRepositoriesByLanguage(
		ctx context.Context,
		languages []string,
		limit int,
		offset int,
		allScopes bool,
		allowedRepositoryIDs []string,
		allowedScopeIDs []string,
	) (querycontract.RepositoryLanguageAggregate, []querycontract.RepositoryLanguageRepository, error)
	CountRepositoriesByLanguage(
		ctx context.Context,
		languages []string,
		allScopes bool,
		allowedRepositoryIDs []string,
		allowedScopeIDs []string,
	) (querycontract.RepositoryLanguageAggregate, error)
	RepositoryLanguageInventory(
		ctx context.Context,
		limit int,
		offset int,
		allScopes bool,
		allowedRepositoryIDs []string,
		allowedScopeIDs []string,
	) ([]querycontract.RepositoryLanguageInventoryRow, error)
	ListWorkloadIdentities(ctx context.Context, limit int) ([]contractrepository.CatalogWorkloadIdentityEntry, bool, error)
	ListServiceCatalogCorrelations(context.Context, querycontract.ServiceCatalogCorrelationFilter) ([]querycontract.ServiceCatalogCorrelationRow, error)
	ReadRepositoryFreshness(ctx context.Context, repoID string) (status.RepositoryFreshnessSnapshot, error)
	ReadPageSelectionsBySlug(ctx context.Context, slugs []string) (selection.PageSelections, error)
	ReadPageRetirements(ctx context.Context, repoIDs []string) (map[string]contractrepository.PageRetirement, error)
}

// The mirror assertion keeps the repository-reads-scoped
// re-declaration honest, and it works in one direction only: removal,
// rename, or re-signing of a mirrored method on any staying
// handler-facing port fails compilation here. A new method on those
// ports does not widen this pin — adoption takes deliberate
// re-declaration plus a frozen-list update. It pins no shared port —
// the assertion names the read surface only as the shapes this pin
// mirrors, and no other family may cite this pin as coverage.
var _ RepositoryReadsReadAdmission = interface {
	querycontract.GraphQuery
	querycontract.ContentStore
	repoFileLanguageLister
	repoFilePathLister
	catalogWorkloadIdentityStore
	FreshnessReader
	PageSelectionReader
	PageRetirementReader
	querycontract.ServiceCatalogCorrelationStore
}(nil)
