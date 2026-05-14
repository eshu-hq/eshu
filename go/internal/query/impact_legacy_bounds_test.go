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

func TestFindBlastRadiusUsesRequestedLimitAndReportsTruncation(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("cypher = %q, want server-side LIMIT parameter", cypher)
				}
				if got, want := params["limit"], 3; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{"repo": "payments", "tier": "edge"},
					{"repo": "billing", "tier": "backend"},
					{"repo": "ledger", "tier": "backend"},
				}, nil
			},
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/blast-radius",
		bytes.NewBufferString(`{"target":"payment","target_type":"repository","limit":2}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.findBlastRadius(rec, req)

	data := decodeImpactEnvelopeData(t, rec)
	affected, ok := data["affected"].([]any)
	if !ok || len(affected) != 2 {
		t.Fatalf("affected = %#v, want two rows", data["affected"])
	}
	if got, want := data["limit"], float64(2); got != want {
		t.Fatalf("limit = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}

func TestTraceResourceToCodeUsesRequestedLimitAndReportsTruncation(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("cypher = %q, want server-side LIMIT parameter", cypher)
				}
				if got, want := params["limit"], 2; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{"start_id": "resource:queue", "start_name": "queue", "start_labels": []any{"CloudResource"}, "repo_id": "repo-a", "repo_name": "api", "depth": int64(1)},
					{"start_id": "resource:queue", "start_name": "queue", "start_labels": []any{"CloudResource"}, "repo_id": "repo-b", "repo_name": "worker", "depth": int64(2)},
				}, nil
			},
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-resource-to-code",
		bytes.NewBufferString(`{"start":"resource:queue","max_depth":4,"limit":1}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.traceResourceToCode(rec, req)

	data := decodeImpactEnvelopeData(t, rec)
	paths, ok := data["paths"].([]any)
	if !ok || len(paths) != 1 {
		t.Fatalf("paths = %#v, want one row", data["paths"])
	}
	if got, want := data["limit"], float64(1); got != want {
		t.Fatalf("limit = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}

func TestFindChangeSurfaceUsesRequestedLimitAndReportsTruncation(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("cypher = %q, want server-side LIMIT parameter", cypher)
				}
				if !strings.Contains(cypher, "$environment") {
					t.Fatalf("cypher = %q, want environment predicate before LIMIT", cypher)
				}
				if got, want := params["limit"], 2; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				if got, want := params["environment"], "prod"; got != want {
					t.Fatalf("params[environment] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{"id": "service:a", "name": "a", "labels": []any{"Workload"}, "environment": "prod", "depth": int64(1)},
					{"id": "service:b", "name": "b", "labels": []any{"Workload"}, "environment": "prod", "depth": int64(2)},
				}, nil
			},
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{"id": "service:start", "name": "start"}, nil
			},
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/change-surface",
		bytes.NewBufferString(`{"target":"service:start","environment":"prod","limit":1}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.findChangeSurface(rec, req)

	data := decodeImpactEnvelopeData(t, rec)
	impacted, ok := data["impacted"].([]any)
	if !ok || len(impacted) != 1 {
		t.Fatalf("impacted = %#v, want one row", data["impacted"])
	}
	if got, want := data["limit"], float64(1); got != want {
		t.Fatalf("limit = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}

func decodeImpactEnvelopeData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", envelope.Data)
	}
	return data
}
