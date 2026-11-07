// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// Benchmark input: one delta repository ("repo-bench") with a fixed,
// deterministic mix of changed Go files, plus one skipped non-delta file.
// Four import-free files and four plain-import files (two alias-free imports
// each) each carry four keyless same-package calls over two distinct callee
// names per group; the dot-, blank-, and named-import files, the file with no
// package identity, and the non-delta file each carry two calls that derive
// nothing. Totals: 13 files, 42 keyless edges, 4 distinct derived keys in the
// after state (Alpha, Beta from import-free files; Gamma, Delta from
// plain-import files), 2 in the pre-#10301 baseline (plain-import files were
// skipped wholesale).
//
// Run: cd go && go test ./internal/reducer/code/call/shared
// -run '^$' -bench 'SamePackageGoCalleeKeys' -benchtime=10000x -count=3

func benchDeltaSamePackageEnvelopes() ([]facts.Envelope, map[string]struct{}) {
	const repoID = "repo-bench"
	const importPath = "example.com/bench"
	plainImports := []any{
		map[string]any{"name": "fmt"},
		map[string]any{"name": "example.com/other"},
	}
	calls := func(names ...string) []any {
		edges := make([]any, 0, len(names))
		for _, name := range names {
			edges = append(edges, deltaSamePackageCall(name, name, "go"))
		}
		return edges
	}
	envelopes := []facts.Envelope{}
	for _, path := range []string{"a.go", "b.go", "c.go", "d.go"} {
		envelopes = append(envelopes,
			deltaSamePackageFile(repoID, "app/"+path, importPath,
				calls("Alpha", "Beta", "Alpha", "Beta"), nil))
	}
	for _, path := range []string{"p1.go", "p2.go", "p3.go", "p4.go"} {
		envelopes = append(envelopes,
			deltaSamePackageFile(repoID, "app/"+path, importPath,
				calls("Gamma", "Delta", "Gamma", "Delta"), plainImports))
	}
	// The alias-bearing files call a name no other file calls, so the
	// keys/op guard is sensitive to the gate itself: a broken gate that
	// derived through aliases would report 5 keys instead of 4.
	envelopes = append(envelopes,
		deltaSamePackageFile(repoID, "app/dot.go", importPath,
			calls("Epsilon", "Epsilon"), []any{
				map[string]any{"name": "example.com/other", "alias": "."},
			}),
		deltaSamePackageFile(repoID, "app/blank.go", importPath,
			calls("Epsilon", "Epsilon"), []any{
				map[string]any{"name": "example.com/other", "alias": "_"},
			}),
		deltaSamePackageFile(repoID, "app/aliased.go", importPath,
			calls("Epsilon", "Epsilon"), []any{
				map[string]any{"name": "example.com/other", "alias": "other"},
			}),
		deltaSamePackageFile(repoID, "app/nopkg.go", "",
			calls("Gamma", "Gamma"), nil),
		deltaSamePackageFile("repo-other", "app/main.go", importPath,
			calls("Alpha", "Alpha"), nil),
	)
	return envelopes, map[string]struct{}{repoID: {}}
}

// BenchmarkSamePackageGoCalleeKeysForDeltaRepos measures the after state:
// the #10301 gate derives keys for import-free AND plain-import files,
// short-circuiting on the first aliased import entry otherwise.
func BenchmarkSamePackageGoCalleeKeysForDeltaRepos(b *testing.B) {
	envelopes, delta := benchDeltaSamePackageEnvelopes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keys := SamePackageGoCalleeKeysForDeltaRepos(envelopes, delta)
		if len(keys) != 4 {
			b.Fatalf("derived %d keys, want 4", len(keys))
		}
	}
	b.ReportMetric(4, "keys/op")
	b.ReportMetric(42, "edges/op")
}

// preAliasGateSamePackageGoCalleeKeys is the verbatim pre-#10301 gate from
// base a218b94c42e (any file carrying imports is skipped wholesale),
// preserved here as the measurement baseline only. It takes the same input
// as the after state so the benchmark compares identical metric boundaries.
func preAliasGateSamePackageGoCalleeKeys(
	envelopes []facts.Envelope,
	deltaRepos map[string]struct{},
) []string {
	if len(deltaRepos) == 0 {
		return nil
	}
	var keys []string
	for _, env := range envelopes {
		if env.FactKind != factload.FactKindFile {
			continue
		}
		if _, onDelta := deltaRepos[payloadcore.PayloadStr(env.Payload, "repo_id")]; !onDelta {
			continue
		}
		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		importPath := strings.TrimSpace(payloadcore.AnyToString(fileData["package_import_path"]))
		if importPath == "" {
			continue
		}
		if len(payloadcore.MapSlice(fileData["imports"])) > 0 {
			continue
		}
		for _, edge := range payloadcore.MapSlice(fileData["function_calls"]) {
			if len(codeCallEdgeSymbolKeys(edge)) > 0 {
				continue
			}
			if strings.TrimSpace(payloadcore.AnyToString(edge["lang"])) != "go" {
				continue
			}
			name := strings.TrimSpace(payloadcore.AnyToString(edge["full_name"]))
			if name == "" || name != strings.TrimSpace(payloadcore.AnyToString(edge["name"])) {
				continue
			}
			if strings.ContainsAny(name, "./ \t\n") || strings.HasPrefix(name, "scip-") {
				continue
			}
			keys = append(keys, "scip-go gomod "+importPath+" "+name+"().")
		}
	}
	if len(keys) == 0 {
		return nil
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// BenchmarkPreAliasGateSamePackageGoCalleeKeys measures the baseline: the
// pre-#10301 gate on the same 13-file, 42-edge input. Plain-import files are
// skipped, so only Alpha and Beta derive keys.
func BenchmarkPreAliasGateSamePackageGoCalleeKeys(b *testing.B) {
	envelopes, delta := benchDeltaSamePackageEnvelopes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keys := preAliasGateSamePackageGoCalleeKeys(envelopes, delta)
		if len(keys) != 2 {
			b.Fatalf("derived %d keys, want 2", len(keys))
		}
	}
	b.ReportMetric(2, "keys/op")
	b.ReportMetric(42, "edges/op")
}
