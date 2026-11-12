// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
)

// frozenEntityReadAdmission pins the exact method set behind the
// versioned #9669 W5 admission contract for the entity family (Mint A:
// the 8 W5 entity routes only). The two graph reads (Run, RunSingle),
// the nine ContentStore reads the family closure proves exercised
// (direct handler reads plus the transitive selector catalog reads,
// the type-asserted EntityNameSearcher facet, and the CI/CD static
// workflow file list), and the three reducer-store reads (CI/CD run
// correlations, container-image identities, SBOM/attestation
// attachments). The entries re-declare the port signatures in this
// entity-scoped shape instead of embedding the shared or family ports,
// so no other family can cite this pin — a blanket or shared-port pin
// is forbidden permanently. A renamed, added, removed, or re-signed
// method fails here until that list is deliberately updated; bump
// EntityReadAdmissionContractVersion alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers
// can pin the shape, keeping the eshu-api extraction boundary from
// drifting silently.
//
// W-wave consumer: W5 Mint A ONLY (per the arbiter W5 ruling,
// APPROVED-WITH-CONDITIONS). The satellite records this pin in a
// follow-up; a satellite PR consuming an unminted pin is invalid.
// Arbiter review of this pin precedes satellite termination of the
// entity legs.
var frozenEntityReadAdmission = []string{
	"GetEntityContent func(context.Context, string) (*querycontract.EntityContent, error)",
	"ListCICDRunCorrelations func(context.Context, querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error)",
	"ListContainerImageIdentities func(context.Context, chain.ContainerImageIdentityFilter) ([]chain.ContainerImageIdentityRow, error)",
	"ListRepoEntitiesByIDs func(context.Context, string, []string, int) ([]querycontract.EntityContent, error)",
	"ListRepoEntitiesByKeys func(context.Context, string, []querycontract.EntityContentKey) ([]querycontract.EntityContent, error)",
	"ListRepoFiles func(context.Context, string, int) ([]querycontract.FileContent, error)",
	"ListRepositories func(context.Context) ([]querycontract.RepositoryCatalogEntry, error)",
	"ListSBOMAttestationAttachments func(context.Context, chain.SBOMAttestationAttachmentFilter) (chain.SBOMAttestationAttachmentPage, error)",
	"MatchRepositories func(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error)",
	"ResolveRepository func(context.Context, string) (*querycontract.RepositoryCatalogEntry, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"RunSingle func(context.Context, string, map[string]interface {}) (map[string]interface {}, error)",
	"SearchEntitiesByName func(context.Context, string, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntitiesByNameAnyRepo func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntityNames func(context.Context, entity.EntityNameSearch) ([]querycontract.EntityContent, error)",
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
// behind the witness: it answers the nine pinned ContentStore reads
// plus the type-asserted EntityNameSearcher facet.
type entityReadAdmissionContentStub struct{}

func (entityReadAdmissionContentStub) GetEntityContent(context.Context, string) (*querycontract.EntityContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) SearchEntitiesByName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) SearchEntitiesByNameAnyRepo(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) ResolveRepository(context.Context, string) (*querycontract.RepositoryCatalogEntry, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) ListRepoEntitiesByKeys(context.Context, string, []querycontract.EntityContentKey) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (entityReadAdmissionContentStub) ListRepoEntitiesByIDs(context.Context, string, []string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
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

func (entityReadAdmissionContentStub) SearchEntityNames(context.Context, entity.EntityNameSearch) ([]querycontract.EntityContent, error) {
	return nil, nil
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
// satisfiable: the graph, content, and reducer reads compose jointly,
// so no single store carries all fifteen methods. It composes
// test-only stubs rather than inventing a production satisfier — the
// seam stays unwired.
type entityReadAdmissionWitness struct {
	entityReadAdmissionGraphStub
	entityReadAdmissionContentStub
	entityReadAdmissionReducerStub
}

var _ EntityReadAdmission = entityReadAdmissionWitness{}

// entityReadAdmissionMethodSet reflects the live EntityReadAdmission
// method set into sorted "name signature" strings for comparison with
// the frozen list.
func entityReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*EntityReadAdmission)(nil)).Elem()
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
	got := entityReadAdmissionMethodSet(t)
	if len(got) != len(frozenEntityReadAdmission) {
		t.Fatalf("EntityReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenEntityReadAdmission, len(frozenEntityReadAdmission), EntityReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenEntityReadAdmission[i] {
			t.Errorf("EntityReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenEntityReadAdmission[i], EntityReadAdmissionContractVersion)
		}
	}
}
