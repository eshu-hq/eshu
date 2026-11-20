// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	repositorycontract "github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
)

// frozenEntityReadAdmission pins the exact method set behind the
// versioned #9669 W5 admission contract for the entity family (Mint A
// as extended for the 9 W5 entity routes, per the supplemental arbiter
// ruling on eshu-hq/eshu-api#97, with GET
// /api/v0/investigations/services/{service_name} recorded as the 9th
// admitted route and GET /api/v0/graph/entities explicitly OUT as
// W8b-owned satellite-side). The two graph reads (Run, RunSingle),
// the nineteen ContentStore reads the family closure proves exercised
// (direct handler reads, the transitive selector catalog reads, the
// type-asserted EntityNameSearcher facet, the CI/CD static workflow
// file list, the service evidence and framework-route reads, the
// content-fallback relationship-builder reads, the repository
// infrastructure panel read, and the service enrichment tail: consumer
// file search, provisioning entity list, artifact hydration, and the
// config needed-content check), the three reducer-store reads (CI/CD
// run correlations, container-image identities, SBOM/attestation
// attachments), the type-asserted content-reference search facet on
// the consumer-evidence path (SearchFileReferenceAnyRepo, wider bounds
// than the substring fallback), and the four narrow read-model facets
// the story, deployment-evidence, and read-model identity stages
// assert (target support, target documentation, deployment evidence,
// and the read-model summary behind the identity-only 200 vs
// absent-summary 404). The
// entries re-declare the port signatures in this entity-scoped shape
// instead of embedding the shared or family ports, so no other family
// can cite this pin — a blanket or shared-port pin is forbidden
// permanently. A renamed, added, removed, or re-signed method fails
// here until that list is deliberately updated; bump
// EntityReadAdmissionContractVersion alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers
// can pin the shape, keeping the eshu-api extraction boundary from
// drifting silently.
//
// W-wave consumer: W5 Mint A extended (the 9 W5 entity routes) PLUS
// the W5c-e impact deployment-trace enrichment leg by explicit dual-pin
// cite (per the same ruling: trace-deployment-chain and
// deployment-config-influence require this pin alongside
// ImpactReadAdmission for their TraceContext leg). The satellite
// records this pin in a follow-up; a satellite PR consuming an
// unminted pin is invalid. Arbiter review of this pin precedes
// satellite termination of the entity legs.
var frozenEntityReadAdmission = []string{
	"DocumentationFindings func(context.Context, querycontract.DocumentationFindingFilter) (querycontract.DocumentationFindingListReadModel, error)",
	"GetEntityContent func(context.Context, string) (*querycontract.EntityContent, error)",
	"GetFileContent func(context.Context, string, string) (*querycontract.FileContent, error)",
	"GetFileContents func(context.Context, string, []string) ([]querycontract.FileContent, error)",
	"ListCICDRunCorrelations func(context.Context, querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error)",
	"ListContainerImageIdentities func(context.Context, chain.ContainerImageIdentityFilter) ([]chain.ContainerImageIdentityRow, error)",
	"ListFrameworkRoutes func(context.Context, string) ([]querycontract.FrameworkRouteEvidence, error)",
	"ListRepoEntities func(context.Context, string, int) ([]querycontract.EntityContent, error)",
	"ListRepoEntitiesByIDs func(context.Context, string, []string, int) ([]querycontract.EntityContent, error)",
	"ListRepoEntitiesByKeys func(context.Context, string, []querycontract.EntityContentKey) ([]querycontract.EntityContent, error)",
	"ListRepoEntitiesByType func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
	"ListRepoEntitiesByTypes func(context.Context, string, []string, int) ([]querycontract.EntityContent, error)",
	"ListRepoFiles func(context.Context, string, int) ([]querycontract.FileContent, error)",
	"ListRepositories func(context.Context) ([]querycontract.RepositoryCatalogEntry, error)",
	"ListSBOMAttestationAttachments func(context.Context, chain.SBOMAttestationAttachmentFilter) (chain.SBOMAttestationAttachmentPage, error)",
	"MatchRepositories func(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error)",
	"NonBlankRepoFilePaths func(context.Context, string, []string) ([]string, error)",
	"RepositoryDeploymentEvidence func(context.Context, string) (repository.RepositoryDeploymentEvidenceReadModel, error)",
	"RepositoryReadModelSummary func(context.Context, string) (repository.RepositoryReadModelSummary, error)",
	"ResolveRepository func(context.Context, string) (*querycontract.RepositoryCatalogEntry, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"RunSingle func(context.Context, string, map[string]interface {}) (map[string]interface {}, error)",
	"SearchEntitiesByName func(context.Context, string, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntitiesByNameAnyRepo func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntitiesReferencingComponent func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntityNames func(context.Context, entity.EntityNameSearch) ([]querycontract.EntityContent, error)",
	"SearchFileContentAnyRepo func(context.Context, string, int) ([]querycontract.FileContent, error)",
	"SearchFileContentAnyRepoExactCase func(context.Context, string, int) ([]querycontract.FileContent, error)",
	"SearchFileReferenceAnyRepo func(context.Context, string, string, int) ([]querycontract.FileContent, bool, error)",
	"ServiceStoryTargetSupportEvidence func(context.Context, repository.ServiceStoryTargetSupportFilter) (repository.ServiceStoryTargetSupportReadModel, error)",
}

// entityReadAdmissionGraphStub is the test-only graph satisfier behind
// the witness: it answers the two pinned graph reads.
type entityReadAdmissionGraphStub struct {
	run       func(context.Context, string, map[string]any) ([]map[string]any, error)
	runSingle func(context.Context, string, map[string]any) (map[string]any, error)
}

func (f entityReadAdmissionGraphStub) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if f.run != nil {
		return f.run(ctx, cypher, params)
	}
	return nil, nil
}

func (f entityReadAdmissionGraphStub) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	if f.runSingle != nil {
		return f.runSingle(ctx, cypher, params)
	}
	return nil, nil
}

// entityReadAdmissionContentStub is the test-only content satisfier
// behind the witness: it answers the nineteen pinned ContentStore
// reads plus the type-asserted EntityNameSearcher and
// content-reference search facets.
type entityReadAdmissionContentStub struct{}

func (entityReadAdmissionContentStub) GetEntityContent(context.Context, string) (*querycontract.EntityContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) GetFileContents(context.Context, string, []string) ([]querycontract.FileContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) NonBlankRepoFilePaths(context.Context, string, []string) ([]string, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) SearchEntitiesByName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) SearchEntitiesByNameAnyRepo(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) SearchEntitiesReferencingComponent(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) SearchFileContentAnyRepo(context.Context, string, int) ([]querycontract.FileContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) SearchFileContentAnyRepoExactCase(context.Context, string, int) ([]querycontract.FileContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) ResolveRepository(context.Context, string) (*querycontract.RepositoryCatalogEntry, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) ListRepoEntities(context.Context, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) ListRepoEntitiesByKeys(context.Context, string, []querycontract.EntityContentKey) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) ListRepoEntitiesByIDs(context.Context, string, []string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) ListRepoEntitiesByType(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) ListRepoEntitiesByTypes(context.Context, string, []string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) SearchFileReferenceAnyRepo(context.Context, string, string, int) ([]querycontract.FileContent, bool, error) {
	return nil, false, nil
}

func (entityReadAdmissionContentStub) MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) ListRepositories(context.Context) ([]querycontract.RepositoryCatalogEntry, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) ListFrameworkRoutes(context.Context, string) ([]querycontract.FrameworkRouteEvidence, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) SearchEntityNames(context.Context, entity.EntityNameSearch) ([]querycontract.EntityContent, error) {
	return nil, nil
}

// entityReadAdmissionFacetStub is the test-only read-model satisfier
// behind the witness: it answers the four pinned narrow facets the
// story, deployment-evidence, and read-model identity stages assert
// (target support, target documentation, deployment evidence, and the
// read-model summary).
type entityReadAdmissionFacetStub struct{}

func (entityReadAdmissionFacetStub) ServiceStoryTargetSupportEvidence(context.Context, repositorycontract.ServiceStoryTargetSupportFilter) (repositorycontract.ServiceStoryTargetSupportReadModel, error) {
	return repositorycontract.ServiceStoryTargetSupportReadModel{}, nil
}

func (entityReadAdmissionFacetStub) DocumentationFindings(context.Context, querycontract.DocumentationFindingFilter) (querycontract.DocumentationFindingListReadModel, error) {
	return querycontract.DocumentationFindingListReadModel{}, nil
}

func (entityReadAdmissionFacetStub) RepositoryDeploymentEvidence(context.Context, string) (repositorycontract.RepositoryDeploymentEvidenceReadModel, error) {
	return repositorycontract.RepositoryDeploymentEvidenceReadModel{}, nil
}

func (entityReadAdmissionFacetStub) RepositoryReadModelSummary(context.Context, string) (repositorycontract.RepositoryReadModelSummary, error) {
	return repositorycontract.RepositoryReadModelSummary{}, nil
}

// entityReadAdmissionReducerStub is the test-only reducer satisfier
// behind the witness: it answers the three pinned reducer-store reads.
type entityReadAdmissionReducerStub struct{}

func (entityReadAdmissionReducerStub) ListCICDRunCorrelations(context.Context, querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error) {
	return nil, nil
}

func (entityReadAdmissionReducerStub) ListContainerImageIdentities(context.Context, supplychain.ContainerImageIdentityFilter) ([]supplychain.ContainerImageIdentityRow, error) {
	return nil, nil
}

func (entityReadAdmissionReducerStub) ListSBOMAttestationAttachments(context.Context, supplychain.SBOMAttestationAttachmentFilter) (supplychain.SBOMAttestationAttachmentPage, error) {
	return supplychain.SBOMAttestationAttachmentPage{}, nil
}

// entityReadAdmissionWitness proves EntityReadAdmission is
// satisfiable: the graph, content, reducer, and read-model facet reads
// compose jointly, so no single store carries all thirty methods.
// It composes test-only stubs rather than inventing a production
// satisfier — the seam stays unwired.
type entityReadAdmissionWitness struct {
	entityReadAdmissionGraphStub
	entityReadAdmissionContentStub
	entityReadAdmissionReducerStub
	entityReadAdmissionFacetStub
}

var _ EntityReadAdmission = entityReadAdmissionWitness{}

// entityReadAdmissionResignedWitness carries a deliberately re-signed
// GetFileContent (an extra trailing parameter). It MUST NOT satisfy
// EntityReadAdmission: the seeded rejection test below proves a
// signature drift on any pinned method breaks the pin instead of
// sliding through silently.
type entityReadAdmissionResignedWitness struct {
	entityReadAdmissionWitness
}

func (entityReadAdmissionResignedWitness) GetFileContent(context.Context, string, string, int) (*querycontract.FileContent, error) {
	return nil, nil
}

// entityReadAdmissionMethodSet reflects the live EntityReadAdmission
// method set into sorted "name signature" strings for comparison with
// the frozen list. The iface parameter forces every caller —
// including the seeded-drift RED proof below — through this same guard
// comparison, so a passing suite proves the guard itself can see drift
// rather than proving a private copy can.
func entityReadAdmissionMethodSet(t *testing.T, iface any) []string {
	t.Helper()
	rt := reflect.TypeOf(iface).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestEntityReadAdmissionContractIsVersioned pins the #9669 W5 Mint A
// admission-contract version for the entity family reads. The frozen
// method set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is
// deliberately updated. Bump EntityReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
func TestEntityReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if EntityReadAdmissionContractVersion != "v0" {
		t.Errorf("EntityReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", EntityReadAdmissionContractVersion, "v0")
	}
	got := entityReadAdmissionMethodSet(t, (*EntityReadAdmission)(nil))
	if len(got) != len(frozenEntityReadAdmission) {
		t.Fatalf("EntityReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenEntityReadAdmission, len(frozenEntityReadAdmission), EntityReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenEntityReadAdmission[i] {
			t.Errorf("EntityReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenEntityReadAdmission[i], EntityReadAdmissionContractVersion)
		}
	}
}

// TestEntityReadAdmissionRejectsResignedMethod is the seeded RED half
// of the contract proof: a witness whose GetFileContent carries one
// extra parameter MUST NOT satisfy the pin. If a re-signed method ever
// slides through, this test fails and the frozen shape above can no
// longer be trusted to catch drift.
func TestEntityReadAdmissionRejectsResignedMethod(t *testing.T) {
	t.Parallel()

	pin := reflect.TypeOf((*EntityReadAdmission)(nil)).Elem()
	if reflect.TypeOf(entityReadAdmissionResignedWitness{}).Implements(pin) {
		t.Errorf("entityReadAdmissionResignedWitness implements EntityReadAdmission: a re-signed GetFileContent must break the pin")
	}
	if !reflect.TypeOf(entityReadAdmissionWitness{}).Implements(pin) {
		t.Errorf("entityReadAdmissionWitness does not implement EntityReadAdmission: the minimal shape must satisfy the pin")
	}
}

// TestEntityReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the thirty re-declared entity reads, with no mutation,
// snapshot, ledger, or other-family surface riding along. The frozen
// method-set contract test pins the exact names; this test pins the
// exclusion the arbiter ruling demands.
func TestEntityReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*EntityReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 30 {
		t.Fatalf("EntityReadAdmission has %d methods, want exactly 30: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Delete", "Create", "Update", "Revoke", "Upsert", "Grant", "Snapshot", "Ledger",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("EntityReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededEntityDrift is a deliberately widened copy of the pin: the
// thirty frozen entity reads plus the graph-entities inventory list,
// which the entity pin keeps OUT by construction (that route rides
// the GraphEntityInventory pin). It never ships; it proves the
// frozen-set comparison below can see a one-method widening.
type seededEntityDrift interface {
	EntityReadAdmission
	ListGraphEntities(context.Context) ([]string, error)
}

// TestEntityReadAdmissionSeededDriftIsDetected is the RED half of the
// seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on
// a clean tree precisely because the guard fires on drift — a guard
// that cannot see the seeded method is no guard.
func TestEntityReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := entityReadAdmissionMethodSet(t, (*seededEntityDrift)(nil))
	if reflect.DeepEqual(got, frozenEntityReadAdmission) {
		t.Fatal("seeded widening (ListGraphEntities) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenEntityReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenEntityReadAdmission)+1)
	}
}
