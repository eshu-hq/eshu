// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
)

// fallbackBarrierIndex builds an EntityIndex for one repository whose files
// carry the given node_package_name values, mirroring what the parser stamps.
func fallbackBarrierIndex(packageNames ...string) shared.EntityIndex {
	envelopes := make([]facts.Envelope, 0, len(packageNames)+1)
	envelopes = append(envelopes, facts.Envelope{FactKind: "repository", Payload: map[string]any{"repo_id": "repo"}})
	for i, name := range packageNames {
		rel := fmt.Sprintf("src/file%d.ts", i)
		fileData := map[string]any{"path": rel}
		if name != "" {
			fileData["node_package_name"] = name
		}
		envelopes = append(envelopes, facts.Envelope{FactKind: "file", Payload: map[string]any{
			"repo_id":          "repo",
			"relative_path":    rel,
			"parsed_file_data": fileData,
		}})
	}
	return shared.BuildEntityIndex(envelopes)
}

func TestBlocksRepoFallback(t *testing.T) {
	t.Parallel()

	workspace := fallbackBarrierIndex("@acme/app", "@acme/widgets")
	externalOnly := fallbackBarrierIndex("acme-app")
	noManifest := fallbackBarrierIndex("")

	for _, tc := range []struct {
		name  string
		index shared.EntityIndex
		key   string
		want  bool
	}{
		{"unkeyed call never blocks", externalOnly, "", false},
		{"external key blocks", externalOnly, "package:@acme/format#formatPrice", true},
		{"workspace key keeps the fallback", workspace, "package:@acme/widgets#widget", false},
		{"key without a manifest anywhere blocks", noManifest, "package:@acme/format#formatPrice", true},
		{"malformed key without prefix fails open", externalOnly, "@acme/format#formatPrice", false},
		{"malformed key without export fails open", externalOnly, "package:@acme/format", false},
		{"malformed key without package fails open", externalOnly, "package:#formatPrice", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			call := map[string]any{}
			if tc.key != "" {
				call["package_export_symbol"] = tc.key
			}
			ctx := shared.ResolveContext{Index: tc.index, RepositoryID: "repo", Call: call}
			if got := BlocksRepoFallback(ctx); got != tc.want {
				t.Errorf("BlocksRepoFallback() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNodePackageKeyPackage(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		key  string
		want string
	}{
		{"package:@acme/format#formatPrice", "@acme/format"},
		{"package:bar#widget", "bar"},
		{"  package:bar#widget  ", "bar"},
		{"", ""},
		{"package:bar", ""},
		{"package:#widget", ""},
		{"package:bar#", ""},
		{"bar#widget", ""},
		{"scip-foo#bar", ""},
	} {
		if got := nodePackageKeyPackage(tc.key); got != tc.want {
			t.Errorf("nodePackageKeyPackage(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}
