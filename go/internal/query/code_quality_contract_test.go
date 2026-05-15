package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleCodeQualityInspectionFindsLongFunctionsWithHandles(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				for _, want := range []string{
					"MATCH (e:Function)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)",
					"repo.id = $repo_id",
					"line_count >= $min_lines",
					"ORDER BY line_count DESC, e.name, e.id",
					"SKIP $offset",
					"LIMIT $limit",
				} {
					if !strings.Contains(cypher, want) {
						t.Fatalf("cypher = %q, want %q", cypher, want)
					}
				}
				if got, want := params["repo_id"], "repo-payments"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if got, want := params["min_lines"], 20; got != want {
					t.Fatalf("params[min_lines] = %#v, want %#v", got, want)
				}
				if got, want := params["limit"], 3; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"entity_id": "function:checkout", "name": "checkout",
						"labels": []any{"Function"}, "file_path": "src/payments.ts",
						"repo_id": "repo-payments", "repo_name": "payments-service",
						"language": "typescript", "start_line": int64(10), "end_line": int64(42),
						"line_count": int64(33), "argument_count": int64(2), "complexity": int64(8),
					},
					{
						"entity_id": "function:refund", "name": "refund",
						"labels": []any{"Function"}, "file_path": "src/refunds.ts",
						"repo_id": "repo-payments", "repo_name": "payments-service",
						"language": "typescript", "start_line": int64(5), "end_line": int64(28),
						"line_count": int64(24), "argument_count": int64(1), "complexity": int64(5),
					},
					{
						"entity_id": "function:overflow", "name": "overflow",
						"labels": []any{"Function"}, "file_path": "src/overflow.ts",
						"repo_id": "repo-payments", "repo_name": "payments-service",
						"language": "typescript", "start_line": int64(1), "end_line": int64(22),
						"line_count": int64(22), "argument_count": int64(1), "complexity": int64(4),
					},
				}, nil
			},
		},
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/quality/inspect",
		bytes.NewBufferString(`{"check":"function_length","repo_id":"repo-payments","min_lines":20,"limit":2}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleCodeQualityInspection(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := envelope.Truth.Capability, "code_quality.refactoring"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("results = %#v, want two rows", data["results"])
	}
	first := results[0].(map[string]any)
	if got, want := first["entity_id"], "function:checkout"; got != want {
		t.Fatalf("first entity_id = %#v, want %#v", got, want)
	}
	if got, want := first["line_count"], float64(33); got != want {
		t.Fatalf("first line_count = %#v, want %#v", got, want)
	}
	sourceHandle, ok := first["source_handle"].(map[string]any)
	if !ok {
		t.Fatalf("source_handle type = %T, want map", first["source_handle"])
	}
	if got, want := sourceHandle["repo_id"], "repo-payments"; got != want {
		t.Fatalf("source_handle.repo_id = %#v, want %#v", got, want)
	}
	if got, want := sourceHandle["file_path"], "src/payments.ts"; got != want {
		t.Fatalf("source_handle.file_path = %#v, want %#v", got, want)
	}
	if got, want := sourceHandle["start_line"], float64(10); got != want {
		t.Fatalf("source_handle.start_line = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}

func TestHandleCodeQualityInspectionLocalLightweightReturnsStructuredUnsupportedCapability(t *testing.T) {
	t.Parallel()

	graphCalled := false
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				graphCalled = true
				return nil, nil
			},
		},
		Profile: ProfileLocalLightweight,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/quality/inspect",
		bytes.NewBufferString(`{"check":"complexity","limit":1}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleCodeQualityInspection(rec, req)

	if got, want := rec.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if graphCalled {
		t.Fatal("graph query was called, want capability gate before graph reads")
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Error == nil {
		t.Fatal("envelope.Error = nil, want unsupported capability error")
	}
	if got, want := envelope.Error.Code, ErrorCodeUnsupportedCapability; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if got, want := envelope.Error.Capability, codeQualityCapability; got != want {
		t.Fatalf("capability = %q, want %q", got, want)
	}
	if envelope.Error.Profiles == nil {
		t.Fatal("error profiles = nil, want current and required profiles")
	}
	if got, want := envelope.Error.Profiles.Current, ProfileLocalLightweight; got != want {
		t.Fatalf("current profile = %q, want %q", got, want)
	}
	if got, want := envelope.Error.Profiles.Required, ProfileLocalAuthoritative; got != want {
		t.Fatalf("required profile = %q, want %q", got, want)
	}
}

func TestHandleCodeQualityInspectionFindsFunctionsByArgumentCount(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				for _, want := range []string{
					"parameter_count as argument_count",
					"argument_count >= $min_arguments",
					"ORDER BY argument_count DESC, e.name, e.id",
				} {
					if !strings.Contains(cypher, want) {
						t.Fatalf("cypher = %q, want %q", cypher, want)
					}
				}
				if got, want := params["min_arguments"], 5; got != want {
					t.Fatalf("params[min_arguments] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"entity_id": "function:buildPayment", "name": "buildPayment",
						"labels": []any{"Function"}, "file_path": "src/payments.go",
						"repo_id": "repo-payments", "repo_name": "payments-service",
						"language": "go", "start_line": int64(30), "end_line": int64(40),
						"line_count": int64(11), "argument_count": int64(6), "complexity": int64(3),
					},
				}, nil
			},
		},
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/quality/inspect",
		bytes.NewBufferString(`{"check":"argument_count","repo_id":"repo-payments","min_arguments":5,"limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleCodeQualityInspection(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	results := data["results"].([]any)
	first := results[0].(map[string]any)
	if got, want := first["argument_count"], float64(6); got != want {
		t.Fatalf("argument_count = %#v, want %#v", got, want)
	}
}
