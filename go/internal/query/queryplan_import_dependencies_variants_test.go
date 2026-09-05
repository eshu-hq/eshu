// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

const (
	importDependencyQueryplanVariantFamilySHA256       = "b3180b961166b233f5e3e0155623e8335c86a44c7d8d23c67b007f6503c5b4fd"
	importDependencyQueryplanExpectedReachableRequests = 488
	importDependencyQueryplanExpectedVariantCount      = 280
)

func TestImportDependencyQueryplanVariantsStayComplete(t *testing.T) {
	t.Parallel()

	requests := reachableImportDependencyQueryplanRequests()
	if got, want := len(requests), importDependencyQueryplanExpectedReachableRequests; got != want {
		t.Fatalf("reachable request shapes = %d, want %d", got, want)
	}

	variants := importDependencyQueryplanVariants()
	if got, want := len(variants), importDependencyQueryplanExpectedVariantCount; got != want {
		t.Fatalf("production query variants = %d, want %d", got, want)
	}
	if got, want := queryplan.ProductionCypherFamilySHA256(variants), importDependencyQueryplanVariantFamilySHA256; got != want {
		t.Fatalf("import-dependency variant family SHA-256 = %s, want %s", got, want)
	}

	registered := make(map[string]struct{}, len(variants))
	for _, cypher := range variants {
		registered[cypher] = struct{}{}
	}
	for _, request := range requests {
		for _, query := range importDependencyQueryplanQueries(request) {
			if _, ok := registered[query.cypher]; !ok {
				t.Errorf("reachable request %s emitted unregistered %s query", importDependencyQueryplanRequestName(request), query.name)
			}
		}
	}
}

func reachableImportDependencyQueryplanRequests() []importDependencyRequest {
	queryTypes := []string{
		"imports_by_file",
		"importers",
		"module_dependencies",
		"package_imports",
		"file_import_cycles",
		"cross_module_calls",
	}
	// Both access states, the way resourceSelectorQueryplanVariants and
	// cloudResourceListQueryplanVariants already enumerate theirs. Since #5167
	// batch 2a every builder on this route renders the caller's repository
	// grant, so a scoped caller runs a different statement from a shared-key
	// one and the family has to profile both.
	accesses := []importDependencyQueryplanAccess{
		{name: "all", filter: repositoryAccessFilter{AllScopes: true}},
		{name: "scoped", filter: queryplanScopedRepositoryAccess()},
	}
	requests := make([]importDependencyRequest, 0, len(queryTypes)*len(accesses)*62)
	for _, access := range accesses {
		for _, queryType := range queryTypes {
			for scopeMask := 1; scopeMask < 1<<5; scopeMask++ {
				for languageMask := 0; languageMask < 2; languageMask++ {
					request := importDependencyQueryplanRequest(queryType, scopeMask, languageMask != 0, access)
					if err := request.validate(); err != nil {
						continue
					}
					requests = append(requests, request)
				}
			}
		}
	}
	return requests
}

// importDependencyQueryplanAccess is one caller class the family profiles.
type importDependencyQueryplanAccess struct {
	name   string
	filter repositoryAccessFilter
}

// importDependencyQueryplanAccessName reads the caller class back off a request
// rather than carrying a label on the production struct.
func importDependencyQueryplanAccessName(request importDependencyRequest) string {
	if request.access.Scoped() {
		return "scoped"
	}
	return "all"
}

func importDependencyQueryplanRequest(
	queryType string,
	scopeMask int,
	withLanguage bool,
	access importDependencyQueryplanAccess,
) importDependencyRequest {
	request := importDependencyRequest{
		QueryType: queryType,
		Limit:     10,
		access:    access.filter,
	}
	if scopeMask&1 != 0 {
		request.RepoID = "proof-repository"
	}
	if scopeMask&2 != 0 {
		request.SourceFile = "src/proof.py"
	}
	if scopeMask&4 != 0 {
		request.TargetFile = "src/target.py"
	}
	if scopeMask&8 != 0 {
		request.SourceModule = "proof.source"
	}
	if scopeMask&16 != 0 {
		request.TargetModule = "proof.target"
	}
	if withLanguage {
		request.Language = "python"
	}
	return request
}

type importDependencyQueryplanQuery struct {
	name   string
	cypher string
}

func importDependencyQueryplanQueries(request importDependencyRequest) []importDependencyQueryplanQuery {
	switch request.queryType() {
	case "file_import_cycles":
		return []importDependencyQueryplanQuery{{name: "cycle-edges", cypher: fileImportCycleEdgeRowsCypher(request)}}
	case "cross_module_calls":
		queries := make([]importDependencyQueryplanQuery, 0, 3)
		var sourceScopes []map[string]any
		if strings.TrimSpace(request.SourceModule) != "" {
			queries = append(queries, importDependencyQueryplanQuery{name: "source-membership", cypher: sourceModuleFilesCypher(request)})
			sourceScopes = []map[string]any{{"repo_id": "proof-repository", "path": "/proof/src/proof.py"}}
		}
		var targetScopes []map[string]any
		if strings.TrimSpace(request.TargetModule) != "" {
			queries = append(queries, importDependencyQueryplanQuery{name: "target-membership", cypher: targetModuleFilesCypher(request)})
			targetScopes = []map[string]any{{"repo_id": "proof-repository", "path": "/proof/src/target.py"}}
		}
		queries = append(queries, importDependencyQueryplanQuery{
			name:   "cross-module-calls",
			cypher: crossModuleCallRowsCypher(request, sourceScopes, targetScopes),
		})
		return queries
	default:
		queries := make([]importDependencyQueryplanQuery, 0, 2)
		var sourceScopes []map[string]any
		if strings.TrimSpace(request.SourceModule) != "" {
			queries = append(queries, importDependencyQueryplanQuery{name: "source-membership", cypher: sourceModuleFilesCypher(request)})
			sourceScopes = []map[string]any{{"repo_id": "proof-repository", "path": "/proof/src/proof.py"}}
		}
		switch {
		case request.queryType() == "package_imports":
			queries = append(queries, importDependencyQueryplanQuery{name: "package-imports", cypher: packageImportRowsCypher(request, sourceScopes)})
		case len(sourceScopes) > 0:
			queries = append(queries, importDependencyQueryplanQuery{name: "source-module-imports", cypher: sourceModuleImportRowsCypher(request, sourceScopes)})
		default:
			queries = append(queries, importDependencyQueryplanQuery{name: "direct-imports", cypher: directImportRowsCypher(request)})
		}
		return queries
	}
}

func importDependencyQueryplanVariants() map[string]string {
	requests := reachableImportDependencyQueryplanRequests()
	queryNames := make(map[string]string)
	variants := make(map[string]string)
	for _, request := range requests {
		requestName := importDependencyQueryplanRequestName(request)
		for _, query := range importDependencyQueryplanQueries(request) {
			if _, exists := queryNames[query.cypher]; exists {
				continue
			}
			name := "import-dependencies/" + requestName + "/" + query.name
			queryNames[query.cypher] = name
			variants[name] = query.cypher
		}
	}
	return variants
}

func importDependencyQueryplanRequestName(request importDependencyRequest) string {
	parts := []string{importDependencyQueryplanAccessName(request), request.queryType()}
	for _, filter := range []struct {
		name  string
		value string
	}{
		{name: "repo", value: request.RepoID},
		{name: "source-file", value: request.SourceFile},
		{name: "target-file", value: request.TargetFile},
		{name: "source-module", value: request.SourceModule},
		{name: "target-module", value: request.TargetModule},
		{name: "language", value: request.Language},
	} {
		if strings.TrimSpace(filter.value) != "" {
			parts = append(parts, filter.name)
		}
	}
	return strings.Join(parts, "+")
}
