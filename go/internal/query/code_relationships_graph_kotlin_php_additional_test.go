package query

import (
	"context"
	"testing"
)

func TestHandleRelationshipsReturnsGraphBackedKotlinCompanionObjectReceiverCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-kotlin-companion-1",
					"name":       "usage",
					"labels":     []any{"Function"},
					"file_path":  "src/Person.kt",
					"repo_id":    "repo-1",
					"repo_name":  "comprehensive",
					"language":   "kotlin",
					"start_line": int64(10),
					"end_line":   int64(14),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "create",
							"target_id":   "function-kotlin-companion-2",
						},
					},
					"incoming": []any{},
				}, nil
			},
		},
	}
	assertGraphBackedSingleCallResponse(t, handler, "function-kotlin-companion-1", "kotlin", "create")
}

func TestHandleRelationshipsReturnsGraphBackedKotlinGenericNullableReceiverCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-kotlin-generic-nullable-1",
					"name":       "usage",
					"labels":     []any{"Function"},
					"file_path":  "src/Box.kt",
					"repo_id":    "repo-1",
					"repo_name":  "comprehensive",
					"language":   "kotlin",
					"start_line": int64(10),
					"end_line":   int64(14),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-kotlin-generic-nullable-2",
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
		"function-kotlin-generic-nullable-1",
		"kotlin",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedKotlinSameFileFunctionReturnTypeAliasCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-kotlin-return-alias-1",
					"name":       "usage",
					"labels":     []any{"Function"},
					"file_path":  "src/Usage.kt",
					"repo_id":    "repo-1",
					"repo_name":  "comprehensive",
					"language":   "kotlin",
					"start_line": int64(12),
					"end_line":   int64(16),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-kotlin-return-alias-2",
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
		"function-kotlin-return-alias-1",
		"kotlin",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedPHPPropertyChainAliasCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-php-property-chain-1",
					"name":       "run",
					"labels":     []any{"Function"},
					"file_path":  "src/Config.php",
					"repo_id":    "repo-1",
					"repo_name":  "payments",
					"language":   "php",
					"start_line": int64(8),
					"end_line":   int64(12),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-php-property-chain-2",
						},
					},
					"incoming": []any{},
				}, nil
			},
		},
	}
	assertGraphBackedSingleCallResponse(t, handler, "function-php-property-chain-1", "php", "info")
}

func TestHandleRelationshipsReturnsGraphBackedPHPDirectFreeFunctionReturnReceiverCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-php-direct-free-1",
					"name":       "run",
					"labels":     []any{"Function"},
					"file_path":  "src/App.php",
					"repo_id":    "repo-1",
					"repo_name":  "payments",
					"language":   "php",
					"start_line": int64(6),
					"end_line":   int64(10),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-php-direct-free-2",
						},
					},
					"incoming": []any{},
				}, nil
			},
		},
	}
	assertGraphBackedSingleCallResponse(t, handler, "function-php-direct-free-1", "php", "info")
}

func TestHandleRelationshipsReturnsGraphBackedPHPTypedParameterReceiverCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-php-typed-parameter-1",
					"name":       "run",
					"labels":     []any{"Function"},
					"file_path":  "src/Config.php",
					"repo_id":    "repo-1",
					"repo_name":  "payments",
					"language":   "php",
					"start_line": int64(10),
					"end_line":   int64(14),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-php-typed-parameter-2",
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
		"function-php-typed-parameter-1",
		"php",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedPHPNewExpressionReceiverCalls(t *testing.T) {
	t.Parallel()

	assertGraphBackedSingleCallResponse(t, newPHPGraphBackedSingleCallHandler("function-php-new-expression-1"), "function-php-new-expression-1", "php", "info")
}

func TestHandleRelationshipsReturnsGraphBackedPHPSameFileFreeFunctionReturnAliasReceiverCalls(t *testing.T) {
	t.Parallel()

	assertGraphBackedSingleCallResponse(t, newPHPGraphBackedSingleCallHandler("function-php-free-function-alias-1"), "function-php-free-function-alias-1", "php", "info")
}

func TestHandleRelationshipsReturnsGraphBackedPHPSameFileFreeFunctionReturnPropertyChainAliasCalls(t *testing.T) {
	t.Parallel()

	assertGraphBackedSingleCallResponse(t, newPHPGraphBackedSingleCallHandler("function-php-free-function-property-chain-1"), "function-php-free-function-property-chain-1", "php", "info")
}

func TestHandleRelationshipsReturnsGraphBackedPHPSameFileMethodReturnPropertyChainAliasCalls(t *testing.T) {
	t.Parallel()

	assertGraphBackedSingleCallResponse(t, newPHPGraphBackedSingleCallHandler("function-php-method-property-chain-1"), "function-php-method-property-chain-1", "php", "info")
}

func TestHandleRelationshipsReturnsGraphBackedPHPStaticPropertyReceiverChains(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		entityID string
	}{
		{name: "self", entityID: "function-php-self-static-property-1"},
		{name: "parent", entityID: "function-php-parent-static-property-1"},
		{name: "static", entityID: "function-php-static-property-1"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertGraphBackedSingleCallResponse(t, newPHPGraphBackedSingleCallHandler(tc.entityID), tc.entityID, "php", "info")
		})
	}
}

func TestHandleRelationshipsReturnsGraphBackedPHPParentAndStaticPropertyReceiverAccessChains(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		entityID string
	}{
		{name: "parent", entityID: "function-php-parent-property-access-1"},
		{name: "static", entityID: "function-php-static-property-access-1"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertGraphBackedSingleCallResponse(t, newPHPGraphBackedSingleCallHandler(tc.entityID), tc.entityID, "php", "info")
		})
	}
}

func newPHPGraphBackedSingleCallHandler(entityID string) *CodeHandler {
	return &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         entityID,
					"name":       "run",
					"labels":     []any{"Function"},
					"file_path":  "src/Config.php",
					"repo_id":    "repo-1",
					"repo_name":  "payments",
					"language":   "php",
					"start_line": int64(1),
					"end_line":   int64(2),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   entityID + "-callee",
						},
					},
					"incoming": []any{},
				}, nil
			},
		},
	}
}
