// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
)

// frozenRepositoryStoryReadAdmission pins the exact method set behind
// the versioned #9669 W5 admission contract for the repository story
// read (GET /api/v0/repositories/{repo_id}/story, per the supplemental
// arbiter ruling on eshu-hq/eshu-api#97). The two graph reads (Run,
// RunSingle), the eight ContentStore reads the story closure proves
// exercised (selector catalog, coverage, semantic lists, narrative
// hydration, artifact hydration, the config needed-content check),
// the CI/CD run-correlation read, and the four narrow read-model
// facets the story stages assert (read-model summary, target support,
// target documentation, deployment evidence). The entries re-declare
// the port signatures in this story-scoped shape instead of embedding
// the shared or family ports, so no other route can cite this pin — a
// blanket or shared-port pin is forbidden permanently. Minting a
// whole-Repositories pin to cover this one route would retroactively
// widen D4, which stays forbidden. A renamed, added, removed, or
// re-signed method fails here until that list is deliberately updated;
// bump RepositoryStoryReadAdmissionContractVersion alongside (manually
// — the bump is convention, the frozen list is the enforcement) so
// dest consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// W-wave consumer: W5f story single (the repository story route ONLY;
// the impact packet single rides its own packet pin and neither route
// may cite the other's pin). The satellite records this pin in a
// follow-up; a satellite PR consuming an unminted pin is invalid.
// Arbiter review of this pin precedes satellite termination of the
// story leg. The D4 transport record covers the 11 terminated
// repository GETs and explicitly excludes story — it must not be cited
// for this route.
var frozenRepositoryStoryReadAdmission = []string{
	"DocumentationFindings func(context.Context, querycontract.DocumentationFindingFilter) (querycontract.DocumentationFindingListReadModel, error)",
	"GetFileContent func(context.Context, string, string) (*querycontract.FileContent, error)",
	"GetFileContents func(context.Context, string, []string) ([]querycontract.FileContent, error)",
	"ListCICDRunCorrelations func(context.Context, querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error)",
	"ListRepoEntities func(context.Context, string, int) ([]querycontract.EntityContent, error)",
	"ListRepoEntitiesByTypes func(context.Context, string, []string, int) ([]querycontract.EntityContent, error)",
	"ListRepoFiles func(context.Context, string, int) ([]querycontract.FileContent, error)",
	"MatchRepositories func(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error)",
	"NonBlankRepoFilePaths func(context.Context, string, []string) ([]string, error)",
	"RepositoryCoverage func(context.Context, string) (querycontract.RepositoryContentCoverage, error)",
	"RepositoryDeploymentEvidence func(context.Context, string) (repository.RepositoryDeploymentEvidenceReadModel, error)",
	"RepositoryReadModelSummary func(context.Context, string) (repository.RepositoryReadModelSummary, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"RunSingle func(context.Context, string, map[string]interface {}) (map[string]interface {}, error)",
	"ServiceStoryTargetSupportEvidence func(context.Context, repository.ServiceStoryTargetSupportFilter) (repository.ServiceStoryTargetSupportReadModel, error)",
}

// repositoryStoryReadAdmissionGraphStub is the test-only graph
// satisfier behind the witness: it answers the two pinned graph
// reads (selector fallback plus story traversals).
type repositoryStoryReadAdmissionGraphStub struct{}

func (repositoryStoryReadAdmissionGraphStub) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (repositoryStoryReadAdmissionGraphStub) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// repositoryStoryReadAdmissionContentStub is the test-only content
// satisfier behind the witness: it answers the eight pinned
// ContentStore reads.
type repositoryStoryReadAdmissionContentStub struct{}

func (repositoryStoryReadAdmissionContentStub) MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error) {
	return nil, nil
}

func (repositoryStoryReadAdmissionContentStub) RepositoryCoverage(context.Context, string) (querycontract.RepositoryContentCoverage, error) {
	return querycontract.RepositoryContentCoverage{}, nil
}

func (repositoryStoryReadAdmissionContentStub) ListRepoEntities(context.Context, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (repositoryStoryReadAdmissionContentStub) ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error) {
	return nil, nil
}

func (repositoryStoryReadAdmissionContentStub) ListRepoEntitiesByTypes(context.Context, string, []string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (repositoryStoryReadAdmissionContentStub) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	return nil, nil
}

func (repositoryStoryReadAdmissionContentStub) GetFileContents(context.Context, string, []string) ([]querycontract.FileContent, error) {
	return nil, nil
}

func (repositoryStoryReadAdmissionContentStub) NonBlankRepoFilePaths(context.Context, string, []string) ([]string, error) {
	return nil, nil
}

// repositoryStoryReadAdmissionReducerStub is the test-only reducer
// satisfier behind the witness: it answers the pinned CI/CD run
// correlation read.
type repositoryStoryReadAdmissionReducerStub struct{}

func (repositoryStoryReadAdmissionReducerStub) ListCICDRunCorrelations(context.Context, querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error) {
	return nil, nil
}

// repositoryStoryReadAdmissionFacetStub is the test-only read-model
// satisfier behind the witness: it answers the four pinned narrow
// facets the story stages assert.
type repositoryStoryReadAdmissionFacetStub struct{}

func (repositoryStoryReadAdmissionFacetStub) RepositoryReadModelSummary(context.Context, string) (repository.RepositoryReadModelSummary, error) {
	return repository.RepositoryReadModelSummary{}, nil
}

func (repositoryStoryReadAdmissionFacetStub) ServiceStoryTargetSupportEvidence(context.Context, repository.ServiceStoryTargetSupportFilter) (repository.ServiceStoryTargetSupportReadModel, error) {
	return repository.ServiceStoryTargetSupportReadModel{}, nil
}

func (repositoryStoryReadAdmissionFacetStub) DocumentationFindings(context.Context, querycontract.DocumentationFindingFilter) (querycontract.DocumentationFindingListReadModel, error) {
	return querycontract.DocumentationFindingListReadModel{}, nil
}

func (repositoryStoryReadAdmissionFacetStub) RepositoryDeploymentEvidence(context.Context, string) (repository.RepositoryDeploymentEvidenceReadModel, error) {
	return repository.RepositoryDeploymentEvidenceReadModel{}, nil
}

// repositoryStoryReadAdmissionWitness proves
// RepositoryStoryReadAdmission is satisfiable: the graph, content,
// reducer, and read-model facet reads compose jointly, so no single
// store carries all fifteen methods. It composes test-only stubs
// rather than inventing a production satisfier — the seam stays
// unwired.
type repositoryStoryReadAdmissionWitness struct {
	repositoryStoryReadAdmissionGraphStub
	repositoryStoryReadAdmissionContentStub
	repositoryStoryReadAdmissionReducerStub
	repositoryStoryReadAdmissionFacetStub
}

var _ RepositoryStoryReadAdmission = repositoryStoryReadAdmissionWitness{}

// repositoryStoryReadAdmissionResignedWitness carries a deliberately
// re-signed GetFileContents (an extra trailing parameter). It MUST NOT
// satisfy RepositoryStoryReadAdmission: the seeded rejection test below
// proves a signature drift on any pinned method breaks the pin instead
// of sliding through silently.
type repositoryStoryReadAdmissionResignedWitness struct {
	repositoryStoryReadAdmissionWitness
}

func (repositoryStoryReadAdmissionResignedWitness) GetFileContents(context.Context, string, []string, int) ([]querycontract.FileContent, error) {
	return nil, nil
}

// repositoryStoryReadAdmissionMethodSet reflects the live
// RepositoryStoryReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list. The iface parameter
// forces every caller — including the seeded-drift RED proof below —
// through this same guard comparison, so a passing suite proves the
// guard itself can see drift rather than proving a private copy can.
func repositoryStoryReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestRepositoryStoryReadAdmissionContractIsVersioned pins the #9669 W5
// admission-contract version for the repository story read. The frozen
// method set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is
// deliberately updated. Bump
// RepositoryStoryReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
func TestRepositoryStoryReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if RepositoryStoryReadAdmissionContractVersion != "v0" {
		t.Errorf("RepositoryStoryReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", RepositoryStoryReadAdmissionContractVersion, "v0")
	}
	got := repositoryStoryReadAdmissionMethodSet(t, (*RepositoryStoryReadAdmission)(nil))
	if len(got) != len(frozenRepositoryStoryReadAdmission) {
		t.Fatalf("RepositoryStoryReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenRepositoryStoryReadAdmission, len(frozenRepositoryStoryReadAdmission), RepositoryStoryReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenRepositoryStoryReadAdmission[i] {
			t.Errorf("RepositoryStoryReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenRepositoryStoryReadAdmission[i], RepositoryStoryReadAdmissionContractVersion)
		}
	}
}

// TestRepositoryStoryReadAdmissionRejectsResignedMethod is the seeded
// RED half of the contract proof: a witness whose GetFileContents
// carries one extra parameter MUST NOT satisfy the pin. If a re-signed
// method ever slides through, this test fails and the frozen shape
// above can no longer be trusted to catch drift.
func TestRepositoryStoryReadAdmissionRejectsResignedMethod(t *testing.T) {
	t.Parallel()

	pin := reflect.TypeOf((*RepositoryStoryReadAdmission)(nil)).Elem()
	if reflect.TypeOf(repositoryStoryReadAdmissionResignedWitness{}).Implements(pin) {
		t.Errorf("repositoryStoryReadAdmissionResignedWitness implements RepositoryStoryReadAdmission: a re-signed GetFileContents must break the pin")
	}
	if !reflect.TypeOf(repositoryStoryReadAdmissionWitness{}).Implements(pin) {
		t.Errorf("repositoryStoryReadAdmissionWitness does not implement RepositoryStoryReadAdmission: the minimal shape must satisfy the pin")
	}
}

// TestRepositoryStoryReadAdmissionWireShapesAreSealed proves the seal
// on the pin: exactly the fifteen re-declared story reads, with no
// list-all, mutation, snapshot, or ledger surface riding along. The
// frozen method-set contract test pins the exact names; this test
// pins the exclusion the arbiter ruling demands. Minting a
// whole-Repositories pin to cover this one route would retroactively
// widen D4, which stays forbidden: the count below is the enforcement.
func TestRepositoryStoryReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*RepositoryStoryReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 15 {
		t.Fatalf("RepositoryStoryReadAdmission has %d methods, want exactly 15: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"ListRepositories", "Delete", "Create", "Update", "Write", "Snapshot", "Ledger",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("RepositoryStoryReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededRepositoryStoryDrift is a deliberately widened copy of the
// pin: the fifteen frozen story reads plus a repository-catalog list,
// which the story lane never issues (the catalog leg rides the
// sibling repository-reads pin). It never ships; it proves the
// frozen-set comparison below can see a one-method widening.
type seededRepositoryStoryDrift interface {
	RepositoryStoryReadAdmission
	ListRepositories(context.Context) ([]string, error)
}

// TestRepositoryStoryReadAdmissionSeededDriftIsDetected is the RED
// half of the seeded-drift pair: the same reflect comparison the
// contract test runs must report a mismatch on the planted widening.
// It passes on a clean tree precisely because the guard fires on
// drift — a guard that cannot see the seeded method is no guard.
func TestRepositoryStoryReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := repositoryStoryReadAdmissionMethodSet(t, (*seededRepositoryStoryDrift)(nil))
	if reflect.DeepEqual(got, frozenRepositoryStoryReadAdmission) {
		t.Fatal("seeded widening (ListRepositories) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenRepositoryStoryReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenRepositoryStoryReadAdmission)+1)
	}
}
