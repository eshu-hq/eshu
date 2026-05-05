package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleRelationshipsReturnsGraphBackedKotlinConstructorRootReceiverChains(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-kotlin-constructor-root-1",
					"name":       "usage",
					"labels":     []any{"Function"},
					"file_path":  "src/Usage.kt",
					"repo_id":    "repo-1",
					"repo_name":  "comprehensive",
					"language":   "kotlin",
					"start_line": int64(9),
					"end_line":   int64(13),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-kotlin-constructor-root-2",
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
		"function-kotlin-constructor-root-1",
		"kotlin",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedKotlinDottedPropertyAliasChains(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-kotlin-dotted-alias-1",
					"name":       "usage",
					"labels":     []any{"Function"},
					"file_path":  "src/Usage.kt",
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
							"target_id":   "function-kotlin-dotted-alias-2",
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
		"function-kotlin-dotted-alias-1",
		"kotlin",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedKotlinTypedPropertyChainCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-kotlin-typed-chain-1",
					"name":       "usage",
					"labels":     []any{"Function"},
					"file_path":  "src/Usage.kt",
					"repo_id":    "repo-1",
					"repo_name":  "comprehensive",
					"language":   "kotlin",
					"start_line": int64(10),
					"end_line":   int64(13),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-kotlin-typed-chain-2",
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
		"function-kotlin-typed-chain-1",
		"kotlin",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedPHPImportedTypeAliasReceiverCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-php-imported-type-alias-1",
					"name":       "run",
					"labels":     []any{"Function"},
					"file_path":  "src/ConfigRunner.php",
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
							"target_id":   "function-php-imported-type-alias-2",
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
		"function-php-imported-type-alias-1",
		"php",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedPHPNullsafeReceiverChains(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-php-nullsafe-1",
					"name":       "run",
					"labels":     []any{"Function"},
					"file_path":  "src/Session.php",
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
							"target_id":   "function-php-nullsafe-2",
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
		"function-php-nullsafe-1",
		"php",
		"info",
	)
}

func TestHandleRelationshipsReturnsGraphBackedPHPCrossFileReturnTypeAliasedCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-php-return-type-alias-1",
					"name":       "run",
					"labels":     []any{"Function"},
					"file_path":  "src/App.php",
					"repo_id":    "repo-1",
					"repo_name":  "payments",
					"language":   "php",
					"start_line": int64(3),
					"end_line":   int64(6),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CALLS",
							"target_name": "info",
							"target_id":   "function-php-return-type-alias-2",
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
		"function-php-return-type-alias-1",
		"php",
		"info",
	)
}

func assertGraphBackedSingleCallResponse(
	t *testing.T,
	handler *CodeHandler,
	entityID string,
	language string,
	targetName string,
) {
	t.Helper()

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"entity_id":"`+entityID+`","direction":"outgoing","relationship_type":"CALLS"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["language"], language; got != want {
		t.Fatalf("resp[language] = %#v, want %#v", got, want)
	}
	outgoing, ok := resp["outgoing"].([]any)
	if !ok || len(outgoing) != 1 {
		t.Fatalf("resp[outgoing] = %#v, want one graph-backed relationship", resp["outgoing"])
	}
	relationship, ok := outgoing[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[outgoing][0] type = %T, want map[string]any", outgoing[0])
	}
	if got, want := relationship["type"], "CALLS"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_name"], targetName; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
}
