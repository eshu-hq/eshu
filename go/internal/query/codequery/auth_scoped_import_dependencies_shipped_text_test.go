// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// #5167 code-family batch 2a: the shipped-text guard for the seven builders
// behind POST /api/v0/code/imports/investigate. The route tests drive a
// statement interpreter, so they prove behaviour; this proves the text each
// builder emits and where it sits, which is what a rewrite would silently drop.
//
// The placement assertion is the substantive half. Every builder here pages --
// four with SKIP/LIMIT, three with LIMIT $scan_limit and Go-side paging -- so a
// grant that landed after the bound would let an out-of-grant repository spend
// the page or the 25,000-row scan budget.

// importGrantBuilder is one builder, the request that exercises it, and the
// Repository aliases its pattern binds.
type importGrantBuilder struct {
	name    string
	aliases []string
	build   func(codemodel.ImportDependencyRequest) string
}

func importGrantBuilders() []importGrantBuilder {
	scopes := []map[string]any{{"repo_id": codeGrantGrantedRepo, "path": "/proof/src/api.py"}}
	return []importGrantBuilder{
		{
			name:    "codemodel.DirectImportRowsCypher",
			aliases: []string{"repo"},
			build:   func(req codemodel.ImportDependencyRequest) string { return codemodel.DirectImportRowsCypher(req) },
		},
		{
			name:    "codemodel.PackageImportRowsCypher",
			aliases: []string{"repo"},
			build:   func(req codemodel.ImportDependencyRequest) string { return codemodel.PackageImportRowsCypher(req, nil) },
		},
		{
			name:    "packageImportRowsCypher_source_module",
			aliases: []string{"repo"},
			build: func(req codemodel.ImportDependencyRequest) string {
				return codemodel.PackageImportRowsCypher(req, scopes)
			},
		},
		{
			name:    "codemodel.SourceModuleFilesCypher",
			aliases: []string{"repo"},
			build:   func(req codemodel.ImportDependencyRequest) string { return codemodel.SourceModuleFilesCypher(req) },
		},
		{
			name:    "codemodel.TargetModuleFilesCypher",
			aliases: []string{"repo"},
			build:   func(req codemodel.ImportDependencyRequest) string { return codemodel.TargetModuleFilesCypher(req) },
		},
		{
			name:    "codemodel.SourceModuleImportRowsCypher",
			aliases: []string{"repo"},
			build: func(req codemodel.ImportDependencyRequest) string {
				return codemodel.SourceModuleImportRowsCypher(req, scopes)
			},
		},
		{
			name:    "codemodel.FileImportCycleEdgeRowsCypher",
			aliases: []string{"repo"},
			build: func(req codemodel.ImportDependencyRequest) string {
				return codemodel.FileImportCycleEdgeRowsCypher(req)
			},
		},
		{
			name:    "codemodel.CrossModuleCallRowsCypher",
			aliases: []string{"source_repo", "target_repo"},
			build: func(req codemodel.ImportDependencyRequest) string {
				return codemodel.CrossModuleCallRowsCypher(req, scopes, scopes)
			},
		},
	}
}

func TestImportDependencyBuildersBindTheGrantInTheShippedCypher(t *testing.T) {
	t.Parallel()

	scoped := codemodel.ImportDependencyRequest{
		SourceFile: "src/api.py",
		Access:     repositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}},
	}
	for _, builder := range importGrantBuilders() {
		t.Run(builder.name, func(t *testing.T) {
			t.Parallel()

			normalized := querycontract.NormalizeCypherWhitespace(builder.build(scoped))
			for _, alias := range builder.aliases {
				want := "(" + alias + ".id IN $allowed_repository_ids OR " + alias + ".id IN $allowed_scope_ids)"
				at := strings.Index(normalized, want)
				if at < 0 {
					t.Fatalf("%s missing the grant on %s:\n%s", builder.name, alias, normalized)
				}
				for _, bound := range []string{" RETURN ", " ORDER BY ", " SKIP ", " LIMIT "} {
					boundAt := strings.Index(normalized, bound)
					if boundAt >= 0 && at > boundAt {
						t.Fatalf("%s emits the grant after %q, so the page or the scan budget is spent before it applies:\n%s", builder.name, strings.TrimSpace(bound), normalized)
					}
				}
			}
		})
	}
}

func TestImportDependencyBuildersCarryNoGrantForAnUnscopedCaller(t *testing.T) {
	t.Parallel()

	unscoped := codemodel.ImportDependencyRequest{
		SourceFile: "src/api.py",
		Access:     repositoryAccessFilter{AllScopes: true},
	}
	for _, builder := range importGrantBuilders() {
		t.Run(builder.name, func(t *testing.T) {
			t.Parallel()

			if got := builder.build(unscoped); strings.Contains(got, "allowed_repository_ids") {
				t.Fatalf("%s carries a grant condition for an unscoped caller:\n%s", builder.name, querycontract.NormalizeCypherWhitespace(got))
			}
		})
	}
}

// TestImportDependencyParamsBindTheGrantArrays pins the other half: a grant
// condition whose parameters never arrive fails at execution rather than
// filtering.
func TestImportDependencyParamsBindTheGrantArrays(t *testing.T) {
	t.Parallel()

	params := ImportDependencyParams(codemodel.ImportDependencyRequest{
		SourceFile: "src/api.py",
		Access:     repositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}},
	})
	if !querycontract.GraphParamContains(params, "allowed_repository_ids", codeGrantGrantedRepo) {
		t.Fatalf("params[allowed_repository_ids] = %#v, want the caller's granted ids", params["allowed_repository_ids"])
	}
	if _, ok := params["allowed_scope_ids"]; !ok {
		t.Fatalf("params = %#v, want allowed_scope_ids bound alongside allowed_repository_ids", params)
	}

	unscoped := ImportDependencyParams(codemodel.ImportDependencyRequest{
		SourceFile: "src/api.py",
		Access:     repositoryAccessFilter{AllScopes: true},
	})
	if _, ok := unscoped["allowed_repository_ids"]; ok {
		t.Fatalf("unscoped params carry grant arrays: %#v", unscoped)
	}
}
