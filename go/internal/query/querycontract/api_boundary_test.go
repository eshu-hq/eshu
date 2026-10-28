// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"reflect"
	"sort"
	"testing"
)

// frozenCoreReadPorts pins the exact method set behind each versioned
// core read contract. A renamed, added, removed, or re-signed method fails
// here and must bump CoreReadContractsVersion in the same diff, so the
// eshu-api extraction boundary cannot drift silently.
var frozenCoreReadPorts = map[string][]string{
	"GraphQuery": {
		"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
		"RunSingle func(context.Context, string, map[string]interface {}) (map[string]interface {}, error)",
	},
	"ContentStore": {
		"CountRepositoriesByLanguage func(context.Context, []string, bool, []string, []string) (querycontract.RepositoryLanguageAggregate, error)",
		"GetEntityContent func(context.Context, string) (*querycontract.EntityContent, error)",
		"GetFileContent func(context.Context, string, string) (*querycontract.FileContent, error)",
		"GetFileContents func(context.Context, string, []string) ([]querycontract.FileContent, error)",
		"GetFileLines func(context.Context, string, string, int, int) (*querycontract.FileContent, error)",
		"ListFrameworkRoutes func(context.Context, string) ([]querycontract.FrameworkRouteEvidence, error)",
		"ListRepoEntities func(context.Context, string, int) ([]querycontract.EntityContent, error)",
		"ListRepoEntitiesByIDs func(context.Context, string, []string, int) ([]querycontract.EntityContent, error)",
		"ListRepoEntitiesByKeys func(context.Context, string, []querycontract.EntityContentKey) ([]querycontract.EntityContent, error)",
		"ListRepoEntitiesByPaths func(context.Context, string, []string, int) ([]querycontract.EntityContent, error)",
		"ListRepoEntitiesByType func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
		"ListRepoEntitiesByTypes func(context.Context, string, []string, int) ([]querycontract.EntityContent, error)",
		"ListRepoFiles func(context.Context, string, int) ([]querycontract.FileContent, error)",
		"ListRepoK8sSelectCandidates func(context.Context, string, int) ([]querycontract.K8sSelectCandidate, error)",
		"ListRepositories func(context.Context) ([]querycontract.RepositoryCatalogEntry, error)",
		"ListRepositoriesByLanguage func(context.Context, []string, int, int, bool, []string, []string) ([]querycontract.RepositoryLanguageRepository, error)",
		"MatchRepositories func(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error)",
		"MatchRepositoriesByIDs func(context.Context, []string) ([]querycontract.RepositoryCatalogEntry, error)",
		"NonBlankRepoFilePaths func(context.Context, string, []string) ([]string, error)",
		"ReadRepositoriesByLanguage func(context.Context, []string, int, int, bool, []string, []string) (querycontract.RepositoryLanguageAggregate, []querycontract.RepositoryLanguageRepository, error)",
		"RepositoryCoverage func(context.Context, string) (querycontract.RepositoryContentCoverage, error)",
		"RepositoryLanguageInventory func(context.Context, int, int, bool, []string, []string) ([]querycontract.RepositoryLanguageInventoryRow, error)",
		"ResolveRepository func(context.Context, string) (*querycontract.RepositoryCatalogEntry, error)",
		"SearchCodeCandidates func(context.Context, string, string, string, int, bool) ([]querycontract.EntityContent, []querycontract.EntityContent, error)",
		"SearchEntitiesByExactName func(context.Context, string, string, string, int) ([]querycontract.EntityContent, error)",
		"SearchEntitiesByExactNameAnyRepo func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
		"SearchEntitiesByLanguageAndType func(context.Context, string, string, string, string, int) ([]querycontract.EntityContent, error)",
		"SearchEntitiesByName func(context.Context, string, string, string, int) ([]querycontract.EntityContent, error)",
		"SearchEntitiesByNameAnyRepo func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
		"SearchEntitiesReferencingComponent func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
		"SearchEntityContent func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
		"SearchEntityContentAnyRepo func(context.Context, string, int) ([]querycontract.EntityContent, error)",
		"SearchFileContent func(context.Context, string, string, int) ([]querycontract.FileContent, error)",
		"SearchFileContentAnyRepo func(context.Context, string, int) ([]querycontract.FileContent, error)",
		"SearchFileContentAnyRepoExactCase func(context.Context, string, int) ([]querycontract.FileContent, error)",
	},
}

func portMethodSet(t *testing.T, name string, iface any) []string {
	t.Helper()
	rt := reflect.TypeOf(iface)
	if rt == nil {
		t.Fatalf("%s: want a pointer to the port interface, got %T", name, iface)
	}
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Interface {
		t.Fatalf("%s: want a pointer to the port interface, got %T", name, iface)
	}
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestCoreReadContractsVersioned pins the #8872 versioned core read-contract
// set consumed by the API surface: GraphQuery, ContentStore, and the Postgres
// ReadStore reader surface named on CoreReadContractsVersion. The version
// value plus the frozen method sets below are the enforcement: a breaking
// change to any port fails this test until the version bumps in the same
// diff.
func TestCoreReadContractsVersioned(t *testing.T) {
	t.Helper()
	t.Parallel()

	if CoreReadContractsVersion != "v0" {
		t.Errorf("CoreReadContractsVersion = %q, want %q: bump it deliberately with the port change", CoreReadContractsVersion, "v0")
	}
	for name, iface := range map[string]any{
		"GraphQuery":   (*GraphQuery)(nil),
		"ContentStore": (*ContentStore)(nil),
	} {
		want, ok := frozenCoreReadPorts[name]
		if !ok {
			t.Fatalf("no frozen method set for %s: add one with the port", name)
		}
		got := portMethodSet(t, name, iface)
		if len(got) != len(want) {
			t.Errorf("%s method set = %q (len %d), want frozen %q (len %d): bump %s with the port change", name, got, len(got), want, len(want), CoreReadContractsVersion)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s method set differs at index %d: got %q, want %q: bump %s with the port change", name, i, got[i], want[i], CoreReadContractsVersion)
			}
		}
	}
}
