// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestHandleImportDependencyInvestigationReturnsCrossModuleCalls(t *testing.T) {
	t.Parallel()

	callCount := 0
	handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(
		_ context.Context,
		cypher string,
		params map[string]any,
	) ([]map[string]any, error) {
		callCount++
		switch callCount {
		case 1:
			if !strings.Contains(cypher, "(source_module:Module {name: $source_module})<-[:CONTAINS]-(source_file:File)") {
				t.Fatalf("source membership cypher = %q, want connected source module path", cypher)
			}
			return []map[string]any{{
				"repo_id": "repo-1", "repo_name": "proof", "source_path": "/proof/src/api.py",
				"source_file": "src/api.py", "source_module": "payments.api",
			}}, nil
		case 2:
			if !strings.Contains(cypher, "(target_module:Module {name: $target_module})<-[:CONTAINS]-(target_file:File)") {
				t.Fatalf("target membership cypher = %q, want connected target module path", cypher)
			}
			return []map[string]any{{
				"repo_id": "repo-1", "repo_name": "proof", "target_path": "/proof/src/service.py",
				"target_file": "src/service.py", "target_module": "payments.service",
			}}, nil
		case 3:
			if got := strings.Count(cypher, "MATCH "); got != 1 {
				t.Fatalf("call query MATCH count = %d, want one connected path", got)
			}
			if !strings.Contains(cypher, "(caller:Function)-[rel:CALLS]->(callee:Function)") {
				t.Fatalf("call cypher = %q, want bounded CALLS path", cypher)
			}
			if got, want := params["source_paths"], []string{"/proof/src/api.py"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("params[source_paths] = %#v, want %#v", got, want)
			}
			if got, want := params["target_paths"], []string{"/proof/src/service.py"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("params[target_paths] = %#v, want %#v", got, want)
			}
			return []map[string]any{{
				"source_repo_id": "repo-1", "target_repo_id": "repo-1", "repo_name": "proof",
				"source_path": "/proof/src/api.py", "target_path": "/proof/src/service.py",
				"source_file": "src/api.py", "target_file": "src/service.py", "source_name": "charge",
				"source_id": "function-charge", "target_name": "persist", "target_id": "function-persist",
				"source_module": "payments.api", "target_module": "payments.service", "call_kind": "direct",
			}}, nil
		default:
			t.Fatalf("unexpected graph call %d", callCount)
			return nil, nil
		}
	}}}

	response := serveImportDependencyRequest(
		t,
		handler,
		`{"query_type":"cross_module_calls","repo_id":"repo-1","source_module":"payments.api","target_module":"payments.service","limit":10}`,
	)
	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	calls, ok := body["cross_module_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("cross_module_calls = %#v, want one call", body["cross_module_calls"])
	}
	call, ok := calls[0].(map[string]any)
	if !ok {
		t.Fatalf("cross_module_calls[0] = %#v, want object", calls[0])
	}
	for _, internalPath := range []string{"source_path", "target_path"} {
		if _, leaked := call[internalPath]; leaked {
			t.Fatalf("cross-module call leaked internal absolute %s: %#v", internalPath, call)
		}
	}
	if _, ok := body["dependencies"]; ok {
		t.Fatalf("response includes non-canonical dependencies key: %#v", body["dependencies"])
	}
	if got, want := callCount, 3; got != want {
		t.Fatalf("graph call count = %d, want %d", got, want)
	}
}

func TestHandleImportDependencyInvestigationRejectsUnscopedRequests(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Neo4j: fakeGraphReader{}}
	response := serveImportDependencyRequest(t, handler, `{"query_type":"imports_by_file","limit":25}`)
	if got, want := response.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, response.Body.String())
	}
}

func TestHandleImportDependencyInvestigationRejectsNegativeLimit(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Neo4j: fakeGraphReader{}}
	response := serveImportDependencyRequest(t, handler, `{"query_type":"imports_by_file","repo_id":"repo-1","limit":-1}`)
	if got, want := response.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, response.Body.String())
	}
}
