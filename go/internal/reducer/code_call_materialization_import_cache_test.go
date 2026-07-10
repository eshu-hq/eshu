// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractAllCodeRelationshipRowsCachesRepositoryImportPaths(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-cache",
				"imports_map": map[string]any{
					"alpha": []any{"src/alpha.ts", "src/shared.ts"},
					"beta":  []any{"src/beta.ts", "src/shared.ts"},
				},
			},
		},
	}

	_, _, _, _, index, _ := extractAllCodeRelationshipRowsWithIndex(envelopes)
	got := append([]string(nil), index.repositoryImportPathsByRepo["repo-cache"]...)
	want := []string{"src/alpha.ts", "src/beta.ts", "src/shared.ts"}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cached repository import paths = %#v, want %#v", got, want)
	}
}

func TestCodeCallRepositoryImportPathsForResolutionFallsBackWithoutCache(t *testing.T) {
	t.Parallel()

	repositoryImports := map[string][]string{
		"alpha": {"src/alpha.ts", "src/shared.ts"},
		"beta":  {"src/beta.ts", "src/shared.ts"},
	}
	got := codeCallRepositoryImportPathsForResolution(
		codeEntityIndex{},
		"repo-cache",
		repositoryImports,
	)
	want := codeCallRepositoryImportPaths(repositoryImports)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback repository import paths = %#v, want %#v", got, want)
	}
}

func TestCodeCallRepositoryImportPathsForResolutionSkipsEmptyImports(t *testing.T) {
	t.Parallel()

	got := codeCallRepositoryImportPathsForResolution(codeEntityIndex{}, "repo-cache", nil)
	if got != nil {
		t.Fatalf("empty repository import paths = %#v, want nil", got)
	}
}

func BenchmarkExtractCodeCallRowsRepositoryImportBarrier(b *testing.B) {
	envelopes := codeCallRepositoryImportBarrierBenchmarkEnvelopes(5_000, 1_000)

	b.ResetTimer()
	for range b.N {
		_, rows := ExtractCodeCallRows(envelopes)
		if len(rows) != 0 {
			b.Fatalf("len(rows) = %d, want 0", len(rows))
		}
	}
}

func codeCallRepositoryImportBarrierBenchmarkEnvelopes(
	importPathCount int,
	callCount int,
) []facts.Envelope {
	importsMap := make(map[string]any, importPathCount+1)
	for i := 0; i < importPathCount; i++ {
		importsMap[fmt.Sprintf("symbol-%05d", i)] = []any{
			fmt.Sprintf("src/deps/dependency-%05d.js", i),
		}
	}
	importsMap["missing"] = []any{"src/target.js"}

	calls := make([]any, 0, callCount)
	for i := 0; i < callCount; i++ {
		calls = append(calls, map[string]any{
			"lang":        "javascript",
			"name":        "missing",
			"full_name":   "missing",
			"line_number": i + 2,
		})
	}

	return []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":     "repo-benchmark",
				"imports_map": importsMap,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-benchmark",
				"relative_path": "src/caller.js",
				"parsed_file_data": map[string]any{
					"path": "src/caller.js",
					"functions": []any{
						map[string]any{
							"uid":         "function:caller",
							"name":        "caller",
							"line_number": 1,
							"end_line":    callCount + 2,
						},
					},
					"imports": []any{
						map[string]any{
							"name":   "missing",
							"source": "./target",
						},
					},
					"function_calls": calls,
				},
			},
		},
	}
}
