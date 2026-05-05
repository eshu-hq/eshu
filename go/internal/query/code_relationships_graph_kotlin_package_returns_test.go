package query

import (
	"context"
	"testing"
)

func TestHandleRelationshipsReturnsGraphBackedKotlinPackageAwareSiblingFunctionReturnTypeAliasCalls(
	t *testing.T,
) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-kotlin-return-package-sibling-1",
					"name":       "usage",
					"labels":     []any{"Function"},
					"file_path":  "src/feature/Usage.kt",
					"repo_id":    "repo-1",
					"repo_name":  "comprehensive",
					"language":   "kotlin",
					"start_line": int64(25),
					"end_line":   int64(29),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-kotlin-return-package-sibling-2",
						},
					},
					"incoming": []any{},
				}, nil
			},
		},
	}
	assertGraphBackedSingleCallResponse(
		t,
		handler,
		"function-kotlin-return-package-sibling-1",
		"kotlin",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedKotlinPackageAwareSiblingFunctionReturnTypesAcrossGrandparentDirectories(
	t *testing.T,
) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-kotlin-return-package-grandparent-1",
					"name":       "usage",
					"labels":     []any{"Function"},
					"file_path":  "src/feature/Usage.kt",
					"repo_id":    "repo-1",
					"repo_name":  "comprehensive",
					"language":   "kotlin",
					"start_line": int64(26),
					"end_line":   int64(30),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-kotlin-return-package-grandparent-2",
						},
					},
					"incoming": []any{},
				}, nil
			},
		},
	}
	assertGraphBackedSingleCallResponse(
		t,
		handler,
		"function-kotlin-return-package-grandparent-1",
		"kotlin",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedKotlinCrossFilePackageAwareFunctionReturnReceiverChains(
	t *testing.T,
) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-kotlin-return-crossfile-package-1",
					"name":       "usage",
					"labels":     []any{"Function"},
					"file_path":  "src/feature/Usage.kt",
					"repo_id":    "repo-1",
					"repo_name":  "comprehensive",
					"language":   "kotlin",
					"start_line": int64(29),
					"end_line":   int64(33),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-kotlin-return-crossfile-package-2",
						},
					},
					"incoming": []any{},
				}, nil
			},
		},
	}
	assertGraphBackedSingleCallResponse(
		t,
		handler,
		"function-kotlin-return-crossfile-package-1",
		"kotlin",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedKotlinPackageAwareDirectFunctionReturnReceiverChainsAcrossSiblingDirectories(
	t *testing.T,
) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-kotlin-return-direct-sibling-1",
					"name":       "usage",
					"labels":     []any{"Function"},
					"file_path":  "src/feature/Usage.kt",
					"repo_id":    "repo-1",
					"repo_name":  "comprehensive",
					"language":   "kotlin",
					"start_line": int64(30),
					"end_line":   int64(34),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-kotlin-return-direct-sibling-2",
						},
					},
					"incoming": []any{},
				}, nil
			},
		},
	}
	assertGraphBackedSingleCallResponse(
		t,
		handler,
		"function-kotlin-return-direct-sibling-1",
		"kotlin",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedKotlinPackageAwareDirectFunctionReturnReceiverChainsAcrossDeeperPackageDirectories(
	t *testing.T,
) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-kotlin-return-direct-deeper-1",
					"name":       "usage",
					"labels":     []any{"Function"},
					"file_path":  "src/feature/Usage.kt",
					"repo_id":    "repo-1",
					"repo_name":  "comprehensive",
					"language":   "kotlin",
					"start_line": int64(31),
					"end_line":   int64(35),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-kotlin-return-direct-deeper-2",
						},
					},
					"incoming": []any{},
				}, nil
			},
		},
	}
	assertGraphBackedSingleCallResponse(
		t,
		handler,
		"function-kotlin-return-direct-deeper-1",
		"kotlin",
		"info",
	)
}
