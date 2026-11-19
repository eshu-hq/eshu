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
	contractrepository "github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
	"github.com/eshu-hq/eshu/go/internal/scope/selection"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// frozenRepositoryReadsReadAdmission pins the exact method set behind
// the versioned #9669 W5 admission contract for the repository list,
// inventory, and detail reads (GET /api/v0/catalog, repositories,
// repositories/by-language, repositories/language-inventory,
// repositories/{repo_id}/branches, content, context, coverage,
// freshness, stats, tree): the two bounded graph reads, the nine
// content reads, the catalog reads, the freshness read, and the two
// list annotations. The entries re-declare the store signatures in
// this repository-reads-scoped shape instead of embedding the shared
// stores, so no other route can cite this pin — a blanket or
// shared-port pin is forbidden permanently. A renamed, added,
// removed, or re-signed method fails here until that list is
// deliberately updated; bump
// RepositoryReadsReadAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// W-wave consumer: W5 repositories list/detail (11 oracle Repositories
// rows; the story route rides the sibling RepositoryStoryReadAdmission
// pin, never this one).
var frozenRepositoryReadsReadAdmission = []string{
	"CountRepositoriesByLanguage func(context.Context, []string, bool, []string, []string) (querycontract.RepositoryLanguageAggregate, error)",
	"GetFileContent func(context.Context, string, string) (*querycontract.FileContent, error)",
	"ListRepoFiles func(context.Context, string, int) ([]querycontract.FileContent, error)",
	"ListRepoFilesByLanguage func(context.Context, string, []string, string, int) ([]querycontract.FileContent, error)",
	"ListRepoFilesByPath func(context.Context, string, string, int) ([]querycontract.FileContent, error)",
	"ListServiceCatalogCorrelations func(context.Context, querycontract.ServiceCatalogCorrelationFilter) ([]querycontract.ServiceCatalogCorrelationRow, error)",
	"ListWorkloadIdentities func(context.Context, int) ([]repository.CatalogWorkloadIdentityEntry, bool, error)",
	"ReadPageRetirements func(context.Context, []string) (map[string]repository.PageRetirement, error)",
	"ReadPageSelectionsBySlug func(context.Context, []string) (selection.PageSelections, error)",
	"ReadRepositoriesByLanguage func(context.Context, []string, int, int, bool, []string, []string) (querycontract.RepositoryLanguageAggregate, []querycontract.RepositoryLanguageRepository, error)",
	"ReadRepositoryFreshness func(context.Context, string) (status.RepositoryFreshnessSnapshot, error)",
	"RepoFilePathContext func(context.Context, string, string) (bool, string, error)",
	"RepositoryCoverage func(context.Context, string) (querycontract.RepositoryContentCoverage, error)",
	"RepositoryLanguageInventory func(context.Context, int, int, bool, []string, []string) ([]querycontract.RepositoryLanguageInventoryRow, error)",
	"ResolveRepository func(context.Context, string) (*querycontract.RepositoryCatalogEntry, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"RunSingle func(context.Context, string, map[string]interface {}) (map[string]interface {}, error)",
}

// repositoryReadsReadAdmissionWitness proves
// RepositoryReadsReadAdmission is satisfiable with the minimal shape:
// the list, inventory, and detail reads the eleven routes admit
// through, no wider store surface.
type repositoryReadsReadAdmissionWitness struct{}

func (repositoryReadsReadAdmissionWitness) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (repositoryReadsReadAdmissionWitness) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (repositoryReadsReadAdmissionWitness) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	return nil, nil
}

func (repositoryReadsReadAdmissionWitness) ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error) {
	return nil, nil
}

func (repositoryReadsReadAdmissionWitness) ListRepoFilesByLanguage(context.Context, string, []string, string, int) ([]querycontract.FileContent, error) {
	return nil, nil
}

func (repositoryReadsReadAdmissionWitness) ListRepoFilesByPath(context.Context, string, string, int) ([]querycontract.FileContent, error) {
	return nil, nil
}

func (repositoryReadsReadAdmissionWitness) RepoFilePathContext(context.Context, string, string) (bool, string, error) {
	return false, "", nil
}

func (repositoryReadsReadAdmissionWitness) RepositoryCoverage(context.Context, string) (querycontract.RepositoryContentCoverage, error) {
	return querycontract.RepositoryContentCoverage{}, nil
}

func (repositoryReadsReadAdmissionWitness) ResolveRepository(context.Context, string) (*querycontract.RepositoryCatalogEntry, error) {
	return nil, nil
}

func (repositoryReadsReadAdmissionWitness) ReadRepositoriesByLanguage(context.Context, []string, int, int, bool, []string, []string) (querycontract.RepositoryLanguageAggregate, []querycontract.RepositoryLanguageRepository, error) {
	return querycontract.RepositoryLanguageAggregate{}, nil, nil
}

func (repositoryReadsReadAdmissionWitness) CountRepositoriesByLanguage(context.Context, []string, bool, []string, []string) (querycontract.RepositoryLanguageAggregate, error) {
	return querycontract.RepositoryLanguageAggregate{}, nil
}

func (repositoryReadsReadAdmissionWitness) RepositoryLanguageInventory(context.Context, int, int, bool, []string, []string) ([]querycontract.RepositoryLanguageInventoryRow, error) {
	return nil, nil
}

func (repositoryReadsReadAdmissionWitness) ListWorkloadIdentities(context.Context, int) ([]contractrepository.CatalogWorkloadIdentityEntry, bool, error) {
	return nil, false, nil
}

func (repositoryReadsReadAdmissionWitness) ListServiceCatalogCorrelations(context.Context, querycontract.ServiceCatalogCorrelationFilter) ([]querycontract.ServiceCatalogCorrelationRow, error) {
	return nil, nil
}

func (repositoryReadsReadAdmissionWitness) ReadRepositoryFreshness(context.Context, string) (status.RepositoryFreshnessSnapshot, error) {
	return status.RepositoryFreshnessSnapshot{}, nil
}

func (repositoryReadsReadAdmissionWitness) ReadPageSelectionsBySlug(context.Context, []string) (selection.PageSelections, error) {
	return selection.PageSelections{}, nil
}

func (repositoryReadsReadAdmissionWitness) ReadPageRetirements(context.Context, []string) (map[string]contractrepository.PageRetirement, error) {
	return nil, nil
}

var _ RepositoryReadsReadAdmission = repositoryReadsReadAdmissionWitness{}

// repositoryReadsReadAdmissionMethodSet reflects the live
// RepositoryReadsReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list. The iface parameter
// forces every caller — including the seeded-drift RED proof below —
// through this same guard comparison, so a passing suite proves the
// guard itself can see drift rather than proving a private copy can.
func repositoryReadsReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestRepositoryReadsReadAdmissionContractIsVersioned pins the #9669
// W5 admission-contract version for the repository list, inventory,
// and detail reads. The frozen method set below is the enforcement:
// an added, removed, renamed, or re-signed method fails this test
// until the frozen list is deliberately updated. Bump
// RepositoryReadsReadAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
func TestRepositoryReadsReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if RepositoryReadsReadAdmissionContractVersion != "v0" {
		t.Errorf("RepositoryReadsReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", RepositoryReadsReadAdmissionContractVersion, "v0")
	}
	got := repositoryReadsReadAdmissionMethodSet(t, (*RepositoryReadsReadAdmission)(nil))
	if len(got) != len(frozenRepositoryReadsReadAdmission) {
		t.Fatalf("RepositoryReadsReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenRepositoryReadsReadAdmission, len(frozenRepositoryReadsReadAdmission), RepositoryReadsReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenRepositoryReadsReadAdmission[i] {
			t.Errorf("RepositoryReadsReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenRepositoryReadsReadAdmission[i], RepositoryReadsReadAdmissionContractVersion)
		}
	}
}

// TestRepositoryReadsReadAdmissionWireShapesAreSealed proves the seal
// on the pin: exactly the seventeen re-declared list and detail reads,
// with no story, mutation, snapshot, or ledger surface riding along.
// The frozen method-set contract test pins the exact names; this test
// pins the exclusion the arbiter ruling demands. The story route rides
// the sibling RepositoryStoryReadAdmission pin, never this one:
// minting it here would retroactively widen D4, which stays
// forbidden.
func TestRepositoryReadsReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*RepositoryReadsReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 17 {
		t.Fatalf("RepositoryReadsReadAdmission has %d methods, want exactly 17: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Story", "Delete", "Create", "Update", "Write", "Ledger", "Mutat",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("RepositoryReadsReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededRepositoryReadsDrift is a deliberately widened copy of the pin:
// the seventeen frozen list and detail reads plus a repository-branch
// write the list lane never issues. It never ships; it proves the
// frozen-set comparison below can see a one-method widening.
type seededRepositoryReadsDrift interface {
	RepositoryReadsReadAdmission
	RecordRepositoryBranch(context.Context, string) error
}

// TestRepositoryReadsReadAdmissionSeededDriftIsDetected is the RED half
// of the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes
// on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestRepositoryReadsReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := repositoryReadsReadAdmissionMethodSet(t, (*seededRepositoryReadsDrift)(nil))
	if reflect.DeepEqual(got, frozenRepositoryReadsReadAdmission) {
		t.Fatal("seeded widening (RecordRepositoryBranch) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenRepositoryReadsReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenRepositoryReadsReadAdmission)+1)
	}
}
