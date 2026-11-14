// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func prefixAllScopesAccess() querycontract.RepositoryAccessFilter {
	return querycontract.RepositoryAccessFilter{AllScopes: true}
}

func prefixPtr(value string) *string {
	return &value
}

// TestImportDependencyRequestRejectsEmptyPrefix proves an explicitly empty
// prefix fails closed: STARTS WITH ” matches every non-null Module name,
// which is a full-index scan the 25,000-row refusal cannot bound on the
// paged direct path.
func TestImportDependencyRequestRejectsEmptyPrefix(t *testing.T) {
	t.Parallel()

	for _, prefix := range []string{"", "   "} {
		req := ImportDependencyRequest{RepoID: "repo-1", TargetModulePrefix: prefixPtr(prefix)}
		if err := req.Validate(); err == nil {
			t.Fatalf("empty prefix %q validates, want rejection", prefix)
		} else if !strings.Contains(err.Error(), "target_module_prefix") {
			t.Fatalf("empty-prefix error = %q, want it to name target_module_prefix", err.Error())
		}
	}
}

// TestImportDependencyRequestAcceptsPrefixAsScope proves a non-empty prefix
// satisfies the scope filter on its own: the predicate seeks
// module_name_lookup, so the read stays bounded without another anchor.
func TestImportDependencyRequestAcceptsPrefixAsScope(t *testing.T) {
	t.Parallel()

	req := ImportDependencyRequest{TargetModulePrefix: prefixPtr("@scope/")}
	if err := req.Validate(); err != nil {
		t.Fatalf("prefix-only request rejected: %v", err)
	}
	if got := req.NormalizedTargetModulePrefix(); got != "@scope/" {
		t.Fatalf("normalized prefix = %q, want %q", got, "@scope/")
	}
	if !req.HasTargetModulePrefix() {
		t.Fatal("HasTargetModulePrefix = false, want true")
	}
	if got := (ImportDependencyRequest{}).NormalizedTargetModulePrefix(); got != "" {
		t.Fatalf("unset normalized prefix = %q, want empty", got)
	}
	if (ImportDependencyRequest{}).HasTargetModulePrefix() {
		t.Fatal("unset HasTargetModulePrefix = true, want false")
	}
}

// TestImportDependencyRequestRejectsPrefixPlusExact proves the exact anchor
// and the prefix anchor are mutually exclusive: one MATCH cannot carry both.
func TestImportDependencyRequestRejectsPrefixPlusExact(t *testing.T) {
	t.Parallel()

	req := ImportDependencyRequest{
		RepoID:             "repo-1",
		TargetModule:       "requests",
		TargetModulePrefix: prefixPtr("req"),
	}
	if err := req.Validate(); err == nil {
		t.Fatal("exact target_module plus prefix validates, want rejection")
	}
}

// TestImportDependencyRequestRejectsPrefixForCycles proves the prefix and
// external-only options are refused on the membership-resolved reads, which
// narrow modules through file scopes after the fetch instead of matching
// Module names in the statement.
func TestImportDependencyRequestRejectsPrefixForCycles(t *testing.T) {
	t.Parallel()

	prefix := ImportDependencyRequest{
		QueryType:          "file_import_cycles",
		RepoID:             "repo-1",
		TargetModulePrefix: prefixPtr("a."),
	}
	if err := prefix.Validate(); err == nil {
		t.Fatal("prefix on file_import_cycles validates, want rejection")
	}
	external := ImportDependencyRequest{
		QueryType:    "cross_module_calls",
		RepoID:       "repo-1",
		ExternalOnly: true,
	}
	if err := external.Validate(); err == nil {
		t.Fatal("external_only on cross_module_calls validates, want rejection")
	}
}

// TestImportDependencyPrefixJSONPresence proves the wire contract: an
// omitted prefix decodes to no filter, while an explicitly empty prefix
// decodes to a rejected filter. The pointer field keeps the two apart.
func TestImportDependencyPrefixJSONPresence(t *testing.T) {
	t.Parallel()

	var omitted ImportDependencyRequest
	if err := json.Unmarshal([]byte(`{"repo_id": "repo-1"}`), &omitted); err != nil {
		t.Fatalf("unmarshal omitted prefix: %v", err)
	}
	if omitted.HasTargetModulePrefix() {
		t.Fatal("omitted prefix must read as no filter")
	}
	if err := omitted.Validate(); err != nil {
		t.Fatalf("omitted prefix with repo scope rejected: %v", err)
	}

	var explicit ImportDependencyRequest
	if err := json.Unmarshal([]byte(`{"repo_id": "repo-1", "target_module_prefix": ""}`), &explicit); err != nil {
		t.Fatalf("unmarshal empty prefix: %v", err)
	}
	if explicit.TargetModulePrefix == nil {
		t.Fatal("explicit empty prefix must decode to non-nil")
	}
	if err := explicit.Validate(); err == nil {
		t.Fatal("explicit empty prefix validates, want rejection")
	}
}

// TestDirectImportPrefixRowsCypherShape proves the new direct builder seeks
// the module-name index with STARTS WITH instead of the exact-match node the
// pinned direct builder carries, while projecting the same row shape.
func TestDirectImportPrefixRowsCypherShape(t *testing.T) {
	t.Parallel()

	req := ImportDependencyRequest{
		RepoID:             "repo-1",
		TargetModulePrefix: prefixPtr("@scope/"),
		Access:             prefixAllScopesAccess(),
	}
	cypher := DirectImportPrefixRowsCypher(req)
	for _, want := range []string{
		"target_module.name STARTS WITH $target_module_prefix",
		"SKIP $offset\nLIMIT $limit",
		"target_module.name as target_module",
		"ORDER BY repo.id, source_file.relative_path, target_module.name",
	} {
		if !strings.Contains(cypher, want) {
			t.Errorf("prefix direct cypher missing %q:\n%s", want, cypher)
		}
	}
	if strings.Contains(cypher, "{name: $target_module}") {
		t.Errorf("prefix direct cypher must not carry the exact-match node:\n%s", cypher)
	}
}

// TestDirectImportPrefixRowsCypherExternalOnly proves external_only without
// a prefix excludes relative and absolute specifiers with no prefix
// parameter in the statement.
func TestDirectImportPrefixRowsCypherExternalOnly(t *testing.T) {
	t.Parallel()

	req := ImportDependencyRequest{RepoID: "repo-1", ExternalOnly: true, Access: prefixAllScopesAccess()}
	cypher := DirectImportPrefixRowsCypher(req)
	for _, want := range []string{
		"NOT target_module.name STARTS WITH '.'",
		"NOT target_module.name STARTS WITH '/'",
	} {
		if !strings.Contains(cypher, want) {
			t.Errorf("external-only cypher missing %q:\n%s", want, cypher)
		}
	}
	if strings.Contains(cypher, "$target_module_prefix") {
		t.Errorf("external-only cypher without prefix must not mention the prefix parameter:\n%s", cypher)
	}
}

// TestPackageImportPrefixRowsCypherShapes proves the new package builder
// mirrors the pinned package builder's two shapes: the scoped scan bound
// and the unscoped DISTINCT page.
func TestPackageImportPrefixRowsCypherShapes(t *testing.T) {
	t.Parallel()

	req := ImportDependencyRequest{
		QueryType:          "package_imports",
		RepoID:             "repo-1",
		TargetModulePrefix: prefixPtr("github.com/acme/"),
		Access:             prefixAllScopesAccess(),
	}
	scopes := []map[string]any{{"repo_id": "repo-1", "path": "a.go"}}
	scoped := PackageImportPrefixRowsCypher(req, scopes)
	if !strings.Contains(scoped, "LIMIT $scan_limit") {
		t.Errorf("scoped prefix package cypher missing the scan bound:\n%s", scoped)
	}
	if strings.Contains(scoped, "SKIP $offset") {
		t.Errorf("scoped prefix package cypher must not page:\n%s", scoped)
	}
	if !strings.Contains(scoped, "target_module.name STARTS WITH $target_module_prefix") {
		t.Errorf("scoped prefix package cypher missing the prefix predicate:\n%s", scoped)
	}

	paged := PackageImportPrefixRowsCypher(req, nil)
	for _, want := range []string{"SKIP $offset\nLIMIT $limit", "RETURN DISTINCT"} {
		if !strings.Contains(paged, want) {
			t.Errorf("paged prefix package cypher missing %q:\n%s", want, paged)
		}
	}
}

// TestMatchTargetModulePrefixFilter proves the Go-side filter used on the
// source-module scan path agrees with the Cypher predicate: prefix match on
// the raw name, external-only dropping dot- and slash-led specifiers.
func TestMatchTargetModulePrefixFilter(t *testing.T) {
	t.Parallel()

	prefix := ImportDependencyRequest{TargetModulePrefix: prefixPtr("acme.svc.")}
	for _, name := range []string{"acme.svc.mod", "acme.svc."} {
		if !MatchTargetModulePrefixFilter(prefix, name) {
			t.Errorf("prefix acme.svc. must match %q", name)
		}
	}
	for _, name := range []string{"acme.svc2.mod", "other.mod", "./acme.svc.mod"} {
		if MatchTargetModulePrefixFilter(prefix, name) {
			t.Errorf("prefix acme.svc. must not match %q", name)
		}
	}
	dotted := ImportDependencyRequest{TargetModulePrefix: prefixPtr("github.com/acme/")}
	if !MatchTargetModulePrefixFilter(dotted, "github.com/acme/svc") {
		t.Error("prefix github.com/acme/ must match the Go import path")
	}

	external := ImportDependencyRequest{ExternalOnly: true}
	for _, name := range []string{"requests", "@scope/pkg", "github.com/acme/svc", "acme.svc.mod"} {
		if !MatchTargetModulePrefixFilter(external, name) {
			t.Errorf("external-only must keep bare specifier %q", name)
		}
	}
	for _, name := range []string{"./x", "../y", "/abs/path"} {
		if MatchTargetModulePrefixFilter(external, name) {
			t.Errorf("external-only must drop relative/absolute specifier %q", name)
		}
	}
	if !MatchTargetModulePrefixFilter(ImportDependencyRequest{}, "anything") {
		t.Error("unset filter must match everything")
	}
}
