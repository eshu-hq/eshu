// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
)

// RepositoryStoryReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the repository story read (GET
// /api/v0/repositories/{repo_id}/story). Canonical reads persist
// through explicit core-owned admission contracts: eshu-api keeps the
// HTTP shape and calls this versioned port, which the core-owned graph,
// content, and reducer-owned stores satisfy jointly, composed by the
// consumer. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const RepositoryStoryReadAdmissionContractVersion = "v0"

// RepositoryStoryReadAdmission is the versioned admission-contract port
// the API handler will depend on instead of the direct store handles.
// It declares exactly the fifteen reads the repository story route
// admits through — the graph reads (RunSingle on the repository base
// row, Run on the story, dependency, deployment-evidence, and artifact
// traversals plus the selector fallback), the eight content reads the
// story closure proves exercised (MatchRepositories on the path
// selector, RepositoryCoverage on the coverage stage, ListRepoEntities
// and ListRepoFiles on the semantic stage, ListRepoEntitiesByTypes on
// the infrastructure panel, GetFileContent on narrative hydration,
// GetFileContents on artifact and workflow hydration, and
// NonBlankRepoFilePaths on the config needed-content check), the CI/CD
// run correlation read (ListCICDRunCorrelations on the ci/cd evidence
// stage), and the four narrow read-model facets the story stages
// assert (RepositoryReadModelSummary on the graph summary, the
// ServiceStoryTargetSupport facet on target support,
// DocumentationFindings on target documentation,
// RepositoryDeploymentEvidence on the deployment-evidence fast path) —
// so the boundary is held honest by the compile-time production mirror
// (root repository_story_read_admission_mirror.go) plus the frozen
// method-set contract test. It will back the eshu-api extraction
// handoff for this family. The seam is read-only on purpose: every
// declared method is a bounded read and the port carries no mutation
// surface, so a dest consumer implementing only this contract can serve
// the story read but cannot acquire a write it did not bargain for
// through it.
//
// W-wave consumer: W5f story single (GET
// /api/v0/repositories/{repo_id}/story ONLY). The impact packet single
// rides its own packet pin and neither route may cite the other's pin.
// Minting a whole-Repositories pin to cover this route would
// retroactively widen D4, which stays forbidden: the D4 transport
// record covers the 11 terminated repository GETs and explicitly
// excludes story. The satellite records this pin in a follow-up; a
// satellite PR consuming an unminted pin is invalid. Arbiter review of
// this pin precedes satellite termination of the story leg.
//
// Narrow by construction, recorded not silently assumed: the fifteen
// methods re-declare the port signatures in this story-scoped shape
// instead of embedding or aliasing querycontract.GraphQuery,
// querycontract.ContentStore,
// querycontract.CICDRunCorrelationStore, or the four read-model facet
// ports. No other route can cite this pin — a blanket or shared-port
// pin is forbidden permanently.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - ResolveRepository and ListRepositories: the story resolves its
//     repository by RunSingle on the base row plus MatchRepositories in
//     the path selector. The ResolveRepository fallback belongs to the
//     stats, tree, freshness, and branches legs; ListRepositories
//     belongs to the list leg. Freeze only what call sites prove.
//   - RepositoryContextCoverage: the story coverage stage calls
//     RepositoryCoverage directly. The narrow-facet assertion lives in
//     the context route's loadRepositoryContentCoverage, not on the
//     story path — decided in the census, recorded here.
//   - GetEntityContent, the Search* family beyond MatchRepositories,
//     ListRepoEntitiesByKeys, ListRepoEntitiesByIDs,
//     ListRepoEntitiesByPaths, ListRepoK8sSelectCandidates,
//     ListFrameworkRoutes, the language aggregates,
//     MatchRepositoriesByIDs, RepositoryFilesLastIndexedAt,
//     SearchEntitiesByLanguageAndType, GetFileLines: no story stage
//     calls them.
//   - Freshness, Selections, Retirements, and ServiceCatalog
//     correlation reads: list and catalog legs, not story.
//   - repositoryartifacts loader internals (candidate budgets, stored
//     rows, shadow loads), the responder and writer seams, and the
//     fixed failure messages are handler behavior, proven at
//     byte-parity time and never frozen here.
//   - No suppression leg exists on this route; the Decision 2
//     suppression-state-filtering parity obligation stays on the
//     supply-chain packet leg this pin does not touch.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type RepositoryStoryReadAdmission interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
	RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error)
	MatchRepositories(ctx context.Context, selector string) ([]querycontract.RepositoryCatalogEntry, error)
	RepositoryCoverage(ctx context.Context, repoID string) (querycontract.RepositoryContentCoverage, error)
	ListRepoEntities(ctx context.Context, repoID string, limit int) ([]querycontract.EntityContent, error)
	ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error)
	ListRepoEntitiesByTypes(ctx context.Context, repoID string, entityTypes []string, limit int) ([]querycontract.EntityContent, error)
	GetFileContent(ctx context.Context, repoID, relativePath string) (*querycontract.FileContent, error)
	GetFileContents(ctx context.Context, repoID string, relativePaths []string) ([]querycontract.FileContent, error)
	NonBlankRepoFilePaths(ctx context.Context, repoID string, relativePaths []string) ([]string, error)
	ListCICDRunCorrelations(ctx context.Context, filter querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error)
	RepositoryReadModelSummary(ctx context.Context, repoID string) (repository.RepositoryReadModelSummary, error)
	ServiceStoryTargetSupportEvidence(ctx context.Context, filter repository.ServiceStoryTargetSupportFilter) (repository.ServiceStoryTargetSupportReadModel, error)
	DocumentationFindings(ctx context.Context, filter querycontract.DocumentationFindingFilter) (querycontract.DocumentationFindingListReadModel, error)
	RepositoryDeploymentEvidence(ctx context.Context, repoID string) (repository.RepositoryDeploymentEvidenceReadModel, error)
}
