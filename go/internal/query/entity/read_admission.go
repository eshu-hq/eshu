// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	repositorycontract "github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
)

// EntityReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the entity family reads (the 9 W5
// entity routes). Canonical reads persist through explicit core-owned
// admission contracts: eshu-api keeps the HTTP shape and calls this
// versioned port, which the core-owned graph, content, and
// reducer-owned stores satisfy jointly, composed by the consumer. A
// breaking change to the read shape fails the frozen method-set test
// until that list is deliberately updated; bump this version alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
//
// The shape stays v0 through this extension: Mint A never shipped to a
// satellite consumer (relay holds every W5 leg until the owed core
// mints merge and pass fresh mint-diff review), so no pinned dest
// shape exists to break. The frozen list is the enforcement; v0 here
// names the pre-consumption shape, and the first post-consumption
// change bumps it.
const EntityReadAdmissionContractVersion = "v0"

// EntityReadAdmission is the versioned admission-contract port the API
// handler will depend on instead of the direct store handles. It
// declares exactly the twenty-seven reads the 9 W5 entity routes admit
// through — the graph reads (Run, RunSingle, mirroring
// querycontract.GraphQuery, exercised directly, through the selector
// catalog fallback, and through workload-candidate resolution), the
// seventeen content reads the family closure proves exercised
// (SearchEntitiesByName, SearchEntitiesByNameAnyRepo,
// GetEntityContent, ResolveRepository, ListRepoEntitiesByKeys,
// ListRepoEntitiesByIDs, MatchRepositories and ListRepositories via
// selector.ResolveExactForAccess, ListRepoFiles via the CI/CD static
// workflow evidence and the service deployment-evidence stage,
// GetFileContent via LoadServiceQueryEvidence hydration and the OpenAPI
// $ref resolver, ListFrameworkRoutes via the enrichment
// framework-routes stage, SearchEntitiesReferencingComponent and
// ListRepoEntitiesByType via the content-fallback relationship build,
// SearchFileContentAnyRepo and SearchFileContentAnyRepoExactCase via
// the consumer-evidence search, ListRepoEntities via the provisioning
// chains, GetFileContents via artifact hydration, and
// NonBlankRepoFilePaths via the config needed-content check), the
// type-asserted EntityNameSearcher facet on the global resolve path
// (SearchEntityNames), the CI/CD run correlation read
// (ListCICDRunCorrelations via LoadRepositoryScopedCICDEvidence on the
// workload-story path), the service-story supply-chain reads
// (ListContainerImageIdentities and ListSBOMAttestationAttachments),
// and the three narrow read-model facets the story and
// deployment-evidence stages assert (ServiceStoryTargetSupportEvidence
// on the service-story operation, DocumentationFindings on the
// service-story operation, RepositoryDeploymentEvidence on the
// deployment-evidence fast path) — so the boundary is held honest by
// the compile-time mirror assertion below plus the frozen method-set
// contract test. It will back the eshu-api extraction handoff for this
// family. The seam is read-only on purpose: every declared method is a
// bounded read and the port carries no mutation surface, so a dest
// consumer implementing only this contract can serve the entity reads
// but cannot acquire a write it did not bargain for through it.
//
// W-wave consumer: W5 Mint A extended (the 9 W5 entity routes, per the
// supplemental arbiter ruling on eshu-hq/eshu-api#97) PLUS the W5c-e
// impact deployment-trace enrichment leg by explicit dual-pin cite:
// trace-deployment-chain and deployment-config-influence require this
// pin alongside ImpactReadAdmission for their TraceContext leg. The
// satellite records this pin in a follow-up; a satellite PR consuming
// an unminted pin is invalid. Arbiter review of this pin precedes
// satellite termination of the entity legs. Service and workload story
// ride this mint; the story served-spec bijection (TestSpecScope,
// evidence_boundaries present for get_workload_story and absent for
// get_service_story per EvidenceBoundariesFor at the minted HEAD)
// closes at satellite-record time, not in this pin.
//
// Narrow by construction, recorded not silently assumed: the
// twenty-seven methods re-declare the port signatures in this
// entity-scoped shape instead of embedding or aliasing
// querycontract.GraphQuery, querycontract.ContentStore,
// querycontract.CICDRunCorrelationStore,
// supplychain.ContainerImageIdentityStore,
// supplychain.SBOMAttestationAttachmentStore, the EntityNameSearcher
// facet, or the three read-model facet ports. This pin therefore
// grants nothing to the other families that read through those ports —
// the impact 13 beyond the dual-pin cite, the repository story, the
// supply-chain 18, and every other GraphQuery or ContentStore consumer
// keep their own per-route relay reasons until deliberate pins cover
// them. Citing CoreReadContractsVersion or SupplyChainReadAdmission as
// W5 coverage stays forbidden permanently.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - The content-fallback relationship build
//     (ContentRelationshipBuilder.BuildContentRelationships, the
//     GET entities/{entity_id}/context fallback): the production
//     satisfier is now the satellite-importable
//     entity/content/fallback.Builder (a stateless adapter over the
//     leaf's per-language extraction closure); the METHOD stays out —
//     do not freeze a builder method on this read pin — while its two
//     exercised reads (SearchEntitiesReferencingComponent,
//     ListRepoEntitiesByType) are frozen here, so a satisfier built to
//     this pin serves the fallback leg's reads. That fallback leg
//     terminates only when the satellite consumes the leaf satisfier,
//     and it keeps its standing until then.
//   - The optional image-candidate explainer facet
//     (ExplainContainerImageCandidate): StoryImageCandidateMissingExplanation
//     degrades to a generic detail when the store does not implement
//     it, so there is no shape to freeze.
//   - The RepositoryContextCoverage and RepositoryFilesLastIndexedAt
//     facets: the story coverage stage calls RepositoryCoverage
//     directly; the narrow-facet assertion lives on the context route,
//     not on any W5 path, so there is nothing to freeze here.
//   - GraphBackend, Profile, Logger, Instruments, and the fixed
//     failure messages are handler behavior, not port shape: there is
//     no method set to freeze, the same standing as QueryProfile in
//     the Compare and Code-flow pins.
//   - The grant checks (RepositoryAccessFilterFromContext plus the
//     scoped grant comparisons) and scoped/empty-grant parity are
//     proven per route at byte-parity time, not frozen here.
//   - No suppression leg exists on these routes; the Decision 2
//     suppression-state-filtering parity obligation stays on the
//     supply-chain finding/reconciliation legs this pin does not
//     touch.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type EntityReadAdmission interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
	RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error)
	GetEntityContent(ctx context.Context, entityID string) (*querycontract.EntityContent, error)
	GetFileContent(ctx context.Context, repoID, relativePath string) (*querycontract.FileContent, error)
	GetFileContents(ctx context.Context, repoID string, relativePaths []string) ([]querycontract.FileContent, error)
	NonBlankRepoFilePaths(ctx context.Context, repoID string, relativePaths []string) ([]string, error)
	SearchEntitiesByName(ctx context.Context, repoID, entityType, name string, limit int) ([]querycontract.EntityContent, error)
	SearchEntitiesByNameAnyRepo(ctx context.Context, entityType, name string, limit int) ([]querycontract.EntityContent, error)
	SearchEntitiesReferencingComponent(ctx context.Context, repoID, componentName string, limit int) ([]querycontract.EntityContent, error)
	SearchFileContentAnyRepo(ctx context.Context, pattern string, limit int) ([]querycontract.FileContent, error)
	SearchFileContentAnyRepoExactCase(ctx context.Context, pattern string, limit int) ([]querycontract.FileContent, error)
	ResolveRepository(ctx context.Context, selector string) (*querycontract.RepositoryCatalogEntry, error)
	ListRepoEntities(ctx context.Context, repoID string, limit int) ([]querycontract.EntityContent, error)
	ListRepoEntitiesByKeys(ctx context.Context, repoID string, keys []querycontract.EntityContentKey) ([]querycontract.EntityContent, error)
	ListRepoEntitiesByIDs(ctx context.Context, repoID string, entityIDs []string, limit int) ([]querycontract.EntityContent, error)
	ListRepoEntitiesByType(ctx context.Context, repoID, entityType string, limit int) ([]querycontract.EntityContent, error)
	MatchRepositories(ctx context.Context, selector string) ([]querycontract.RepositoryCatalogEntry, error)
	ListRepositories(ctx context.Context) ([]querycontract.RepositoryCatalogEntry, error)
	ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error)
	ListFrameworkRoutes(ctx context.Context, repoID string) ([]querycontract.FrameworkRouteEvidence, error)
	SearchEntityNames(ctx context.Context, search entity.EntityNameSearch) ([]querycontract.EntityContent, error)
	ListCICDRunCorrelations(ctx context.Context, filter querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error)
	ListContainerImageIdentities(ctx context.Context, filter supplychain.ContainerImageIdentityFilter) ([]supplychain.ContainerImageIdentityRow, error)
	ListSBOMAttestationAttachments(ctx context.Context, filter supplychain.SBOMAttestationAttachmentFilter) (supplychain.SBOMAttestationAttachmentPage, error)
	ServiceStoryTargetSupportEvidence(ctx context.Context, filter repositorycontract.ServiceStoryTargetSupportFilter) (repositorycontract.ServiceStoryTargetSupportReadModel, error)
	DocumentationFindings(ctx context.Context, filter querycontract.DocumentationFindingFilter) (querycontract.DocumentationFindingListReadModel, error)
	RepositoryDeploymentEvidence(ctx context.Context, repoID string) (repositorycontract.RepositoryDeploymentEvidenceReadModel, error)
}

// entityServiceStoryTargetSupportFacet names the narrow read-model
// surface the service-story target-support stage asserts: the service
// package's store interface is unexported, so this pin re-declares its
// method in the leaf querycontract/repository types the production
// ContentReader satisfies.
type entityServiceStoryTargetSupportFacet interface {
	ServiceStoryTargetSupportEvidence(context.Context, repositorycontract.ServiceStoryTargetSupportFilter) (repositorycontract.ServiceStoryTargetSupportReadModel, error)
}

// entityDeploymentEvidenceFacet names the narrow read-model surface
// the deployment-evidence fast path asserts: the repository handler
// family's DeploymentEvidenceStore cannot be embedded here (that
// package's interface is not this pin's scope to alias), so this pin
// re-declares its method in the leaf types the production ContentReader
// satisfies.
type entityDeploymentEvidenceFacet interface {
	RepositoryDeploymentEvidence(context.Context, string) (repositorycontract.RepositoryDeploymentEvidenceReadModel, error)
}

// The mirror assertion keeps the entity-scoped re-declaration honest,
// and it works in one direction only: removal, rename, or re-signing
// of any of the 27 mirrored methods on the staying handler ports
// fails compilation here. Shared-port addition deliberately does not
// widen this pin — a new port method is adopted only by deliberate
// re-declaration plus a frozen-list update. It pins no shared port —
// the assertion names the combination only as the shape this pin
// mirrors, and no other family may cite this pin as coverage (beyond
// the explicit Impact TraceContext dual-pin cite recorded above).
var _ EntityReadAdmission = interface {
	querycontract.GraphQuery
	querycontract.ContentStore
	querycontract.CICDRunCorrelationStore
	supplychain.ContainerImageIdentityStore
	supplychain.SBOMAttestationAttachmentStore
	entity.EntityNameSearcher
	entityServiceStoryTargetSupportFacet
	querycontract.DocumentationFindingsStore
	entityDeploymentEvidenceFacet
}(nil)

// The ContentRelationshipReader four-shape assertion keeps the fallback
// leg's reads honest: the entity pin already freezes
// SearchEntitiesReferencingComponent and ListRepoEntitiesByType, so a leaf
// satisfier built to this pin serves the content-fallback build. It works
// in one direction only — the pin carries twenty-three more methods the
// builder never calls — and it pins no shared port: the assertion names
// ContentRelationshipReader only as the shape this pin covers for the
// builder leg (precedent: codequery's CodeRelationshipReadAdmission pin).
var _ querycontract.ContentRelationshipReader = EntityReadAdmission(nil)
