// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
)

// EntityReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the entity family reads (the 8 W5
// entity routes). Canonical reads persist through explicit core-owned
// admission contracts: eshu-api keeps the HTTP shape and calls this
// versioned port, which the core-owned graph, content, and
// reducer-owned stores satisfy jointly, composed by the consumer. A
// breaking change to the read shape fails the frozen method-set test
// until that list is deliberately updated; bump this version alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
const EntityReadAdmissionContractVersion = "v0"

// EntityReadAdmission is the versioned admission-contract port the API
// handler will depend on instead of the five direct store handles. It
// declares exactly the fifteen reads the 8 W5 entity routes admit
// through — the graph reads (Run, RunSingle, mirroring
// querycontract.GraphQuery, exercised directly and through the
// selector catalog fallback), the nine content reads the family closure
// proves exercised (SearchEntitiesByName, SearchEntitiesByNameAnyRepo,
// GetEntityContent, ResolveRepository, ListRepoEntitiesByKeys,
// ListRepoEntitiesByIDs, MatchRepositories and ListRepositories via
// selector.ResolveExactForAccess, ListRepoFiles via the CI/CD static
// workflow evidence), the type-asserted EntityNameSearcher facet on the
// global resolve path (SearchEntityNames), the CI/CD run
// correlation read (ListCICDRunCorrelations via
// LoadRepositoryScopedCICDEvidence on the workload-story path), and
// the service-story supply-chain reads
// (ListContainerImageIdentities and ListSBOMAttestationAttachments) —
// so the boundary is held honest by the compile-time mirror assertion
// below plus the frozen method-set contract test. It will back the
// eshu-api extraction handoff for this family. The seam is read-only
// on purpose: every declared method is a bounded read and the port
// carries no mutation surface, so a dest consumer implementing only
// this contract can serve the entity reads but cannot acquire a write
// it did not bargain for through it.
//
// W-wave consumer: W5 Mint A ONLY (the 8 W5 entity routes, per the
// arbiter W5 ruling on eshu-hq/eshu#9669, APPROVED-WITH-CONDITIONS).
// The satellite records this pin in a follow-up; a satellite PR
// consuming an unminted pin is invalid. Arbiter review of this pin
// precedes satellite termination of the entity legs. Service and
// workload story ride this mint; the story served-spec bijection
// (TestSpecScope, evidence_boundaries present for get_workload_story
// and absent for get_service_story per EvidenceBoundariesFor at the
// minted HEAD) closes at satellite-record time, not in this pin.
//
// Narrow by construction, recorded not silently assumed: the fifteen
// methods re-declare the port signatures in this entity-scoped shape
// instead of embedding or aliasing querycontract.GraphQuery,
// querycontract.ContentStore, querycontract.CICDRunCorrelationStore,
// supplychain.ContainerImageIdentityStore,
// supplychain.SBOMAttestationAttachmentStore, or the EntityNameSearcher
// facet. This pin therefore grants nothing to the other families that
// read through those ports — the impact 13, the repository story, the
// supply-chain 18, and every other GraphQuery or ContentStore consumer
// keep their own per-route relay reasons until deliberate pins cover
// them. Citing CoreReadContractsVersion or SupplyChainReadAdmission as
// W5 coverage stays forbidden permanently.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - GET services/{service_name}/downstream-consumers (the 9th Mount
//     route) is not a W5 route; nothing in this pin covers it.
//   - The content-fallback relationship build
//     (ContentRelationshipBuilder.BuildContentRelationships, the
//     GET entities/{entity_id}/context fallback): the only production
//     satisfier is root's ContentIndexRelationshipBuilder, a
//     stateless adapter over root's unexported per-language extraction
//     closure with no satellite-consumable constructor. Do not freeze
//     a method the satellite cannot satisfy: that fallback leg
//     terminates only when core provides a satisfier, and it keeps its
//     standing until then.
//   - The optional image-candidate explainer facet
//     (ExplainContainerImageCandidate): StoryImageCandidateMissingExplanation
//     degrades to a generic detail when the store does not implement
//     it, so there is no shape to freeze.
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
	SearchEntitiesByName(ctx context.Context, repoID, entityType, name string, limit int) ([]querycontract.EntityContent, error)
	SearchEntitiesByNameAnyRepo(ctx context.Context, entityType, name string, limit int) ([]querycontract.EntityContent, error)
	ResolveRepository(ctx context.Context, selector string) (*querycontract.RepositoryCatalogEntry, error)
	ListRepoEntitiesByKeys(ctx context.Context, repoID string, keys []querycontract.EntityContentKey) ([]querycontract.EntityContent, error)
	ListRepoEntitiesByIDs(ctx context.Context, repoID string, entityIDs []string, limit int) ([]querycontract.EntityContent, error)
	MatchRepositories(ctx context.Context, selector string) ([]querycontract.RepositoryCatalogEntry, error)
	ListRepositories(ctx context.Context) ([]querycontract.RepositoryCatalogEntry, error)
	ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error)
	SearchEntityNames(ctx context.Context, search entity.EntityNameSearch) ([]querycontract.EntityContent, error)
	ListCICDRunCorrelations(ctx context.Context, filter querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error)
	ListContainerImageIdentities(ctx context.Context, filter supplychain.ContainerImageIdentityFilter) ([]supplychain.ContainerImageIdentityRow, error)
	ListSBOMAttestationAttachments(ctx context.Context, filter supplychain.SBOMAttestationAttachmentFilter) (supplychain.SBOMAttestationAttachmentPage, error)
}

// The mirror assertion keeps the entity-scoped re-declaration honest,
// and it works in one direction only: removal, rename, or re-signing
// of any of the 15 mirrored methods on the staying handler ports
// fails compilation here. Shared-port addition deliberately does not
// widen this pin — a new port method is adopted only by deliberate
// re-declaration plus a frozen-list update. It pins no shared port —
// the assertion names the combination only as the shape this pin
// mirrors, and no other family may cite this pin as coverage.
var _ EntityReadAdmission = interface {
	querycontract.GraphQuery
	querycontract.ContentStore
	querycontract.CICDRunCorrelationStore
	supplychain.ContainerImageIdentityStore
	supplychain.SBOMAttestationAttachmentStore
	entity.EntityNameSearcher
}(nil)
