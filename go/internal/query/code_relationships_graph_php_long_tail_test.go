package query

import (
	"context"
	"testing"
)

func TestHandleRelationshipsReturnsGraphBackedPHPFreeFunctionReturnCallChainReceiverCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-php-free-chain-1",
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
							"target_id":   "function-php-free-chain-2",
						},
					},
					"incoming": []any{},
				}, nil
			},
		},
	}
	assertGraphBackedSingleCallResponse(t, handler, "function-php-free-chain-1", "php", "info")
}

func TestHandleRelationshipsReturnsGraphBackedPHPParenthesizedMethodReturnCallChainReceiverCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-php-parenthesized-1",
					"name":       "run",
					"labels":     []any{"Function"},
					"file_path":  "src/App.php",
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
							"target_id":   "function-php-parenthesized-2",
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
		"function-php-parenthesized-1",
		"php",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedPHPSelfAndStaticInstantiationReceiverCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-php-self-static-new-1",
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
							"target_id":   "function-php-self-static-new-2",
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
		"function-php-self-static-new-1",
		"php",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedPHPCrossFileChainedStaticFactoryReturnCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-php-static-factory-1",
					"name":       "run",
					"labels":     []any{"Function"},
					"file_path":  "src/App.php",
					"repo_id":    "repo-1",
					"repo_name":  "payments",
					"language":   "php",
					"start_line": int64(7),
					"end_line":   int64(11),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-php-static-factory-2",
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
		"function-php-static-factory-1",
		"php",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedPHPCrossFileMethodReturnCallChainRows(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-php-cross-method-chain-1",
					"name":       "run",
					"labels":     []any{"Function"},
					"file_path":  "src/App.php",
					"repo_id":    "repo-1",
					"repo_name":  "payments",
					"language":   "php",
					"start_line": int64(9),
					"end_line":   int64(13),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-php-cross-method-chain-2",
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
		"function-php-cross-method-chain-1",
		"php",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedPHPAnonymousClassReceiverCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-php-anonymous-class-1",
					"name":       "run",
					"labels":     []any{"Function"},
					"file_path":  "src/App.php",
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
							"target_id":   "function-php-anonymous-class-2",
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
		"function-php-anonymous-class-1",
		"php",
		"info",
	)
}
