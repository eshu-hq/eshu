// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	entityContextResolutionMetric = "eshu_dp_entity_context_resolution_total"
	resolvedBySpanAttr            = "eshu.entity_context.resolved_by"
	statementsTriedSpanAttr       = "eshu.entity_context.statements_tried"
)

// resolutionObservation is everything an operator can read about one
// GetEntityContext request: the HTTP answer, the server span attributes, the
// counter data points, and the structured log lines.
type resolutionObservation struct {
	rec       *httptest.ResponseRecorder
	spanAttrs map[string]attribute.Value
	counts    map[string]int64 // resolved_by label -> counter value
	logs      []map[string]any
}

// observeEntityContext drives GetEntityContext once on handler and collects
// the span, counter, and log signals. The handler gets the instruments and
// logger; the request carries a recording server span, as the API middleware
// supplies in production.
func observeEntityContext(t *testing.T, handler *Handler, entityID string) resolutionObservation {
	t.Helper()
	return observeEntityContextWithAuth(t, handler, entityID, auth.AuthContext{})
}

// observeEntityContextWithAuth is observeEntityContext for a caller with the
// given auth context; the zero value leaves the request unauthenticated.
func observeEntityContextWithAuth(t *testing.T, handler *Handler, entityID string, authCtx auth.AuthContext) resolutionObservation {
	t.Helper()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	t.Cleanup(func() { _ = meterProvider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("entity-context-resolution-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	handler.Instruments = instruments

	var logBuf bytes.Buffer
	handler.Logger = slog.New(slog.NewJSONHandler(&logBuf, nil))

	spans := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	ctx, span := tracerProvider.Tracer("entity-context-resolution-test").Start(context.Background(), "GET /entities/{id}/context")

	if authCtx.Mode != "" {
		ctx = auth.ContextWithAuthContext(ctx, authCtx)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/"+entityID+"/context", nil).WithContext(ctx)
	req.SetPathValue("entity_id", entityID)
	rec := httptest.NewRecorder()
	handler.GetEntityContext(rec, req)
	span.End()

	obs := resolutionObservation{
		rec:       rec,
		spanAttrs: map[string]attribute.Value{},
		counts:    map[string]int64{},
	}
	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	for _, kv := range ended[0].Attributes() {
		obs.spanAttrs[string(kv.Key)] = kv.Value
	}

	var collected metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != entityContextResolutionMetric {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Sum[int64]", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				label, ok := dp.Attributes.Value(attribute.Key("resolved_by"))
				if !ok {
					t.Fatalf("%s data point has no resolved_by attribute: %v", m.Name, dp.Attributes)
				}
				obs.counts[label.AsString()] += dp.Value
			}
		}
	}

	for _, line := range bytes.Split(bytes.TrimSpace(logBuf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		obs.logs = append(obs.logs, entry)
	}
	return obs
}

// assertResolved fails unless the request resolved by want after tried graph
// statements, on both the span and the counter, and the counter moved by
// exactly one.
func assertResolved(t *testing.T, obs resolutionObservation, want string, tried int) {
	t.Helper()
	got, ok := obs.spanAttrs[resolvedBySpanAttr]
	if !ok || got.AsString() != want {
		t.Fatalf("span %s = %v (present=%v), want %q", resolvedBySpanAttr, got, ok, want)
	}
	gotTried, ok := obs.spanAttrs[statementsTriedSpanAttr]
	if !ok || gotTried.AsInt64() != int64(tried) {
		t.Fatalf("span %s = %v (present=%v), want %d", statementsTriedSpanAttr, gotTried, ok, tried)
	}
	if len(obs.counts) != 1 || obs.counts[want] != 1 {
		t.Fatalf("%s = %v, want exactly {%s: 1}", entityContextResolutionMetric, obs.counts, want)
	}
}

func hitRow(id string) map[string]any {
	return map[string]any{
		"id":            id,
		"labels":        []any{"Function"},
		"name":          "Main",
		"relationships": []any{},
	}
}

// TestGetEntityContextResolvedByAnchorOnNeo4jFastPath: a row from the Neo4j
// CALL () anchor (statements[0]) is resolved_by=anchor after one statement.
func TestGetEntityContextResolvedByAnchorOnNeo4jFastPath(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		GraphBackend: querycontract.GraphBackendNeo4j,
		Neo4j: graph.FakeGraphReader{
			RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
				return hitRow("fn-1"), nil
			},
		},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	obs := observeEntityContext(t, handler, "fn-1")

	if obs.rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", obs.rec.Code, obs.rec.Body.String())
	}
	assertResolved(t, obs, "anchor", 1)
}

// TestGetEntityContextResolvedByAnchorOnNornicDBLoopLabel: a hit on the third
// labeled statement of the per-label loop is still an anchor hit; the count
// of statements tried shows how far down the list it was.
func TestGetEntityContextResolvedByAnchorOnNornicDBLoopLabel(t *testing.T) {
	t.Parallel()

	calls := 0
	handler := &Handler{
		Neo4j: graph.FakeGraphReader{
			RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
				calls++
				if calls < 3 {
					return nil, nil
				}
				return hitRow("fn-1"), nil
			},
		},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	obs := observeEntityContext(t, handler, "fn-1")

	if obs.rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", obs.rec.Code, obs.rec.Body.String())
	}
	assertResolved(t, obs, "anchor", 3)
}

// TestGetEntityContextResolvedByFallbackOnlyForTheFinalUnlabeledStatement:
// only a row from the last statement (the unlabeled MATCH (e)) is a fallback,
// on both dialects.
func TestGetEntityContextResolvedByFallbackOnlyForTheFinalUnlabeledStatement(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		backend querycontract.GraphBackend
		total   int
	}{
		{name: "neo4j", backend: querycontract.GraphBackendNeo4j, total: 2},
		{name: "nornicdb_loop", total: len(EntityContextAnchorLabels) + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := &Handler{
				GraphBackend: tc.backend,
				Neo4j: graph.FakeGraphReader{
					RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
						if !strings.Contains(cypher, entityContextUnlabeledAnchor) {
							return nil, nil
						}
						return hitRow("cr-1"), nil
					},
				},
				Profile: querycontract.ProfileLocalAuthoritative,
			}
			obs := observeEntityContext(t, handler, "cr-1")

			if obs.rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", obs.rec.Code, obs.rec.Body.String())
			}
			assertResolved(t, obs, "fallback", tc.total)
		})
	}
}

// TestGetEntityContextResolvedByContentWhenGraphMissesAndContentAnswers: every
// graph statement misses and the content store answers.
func TestGetEntityContextResolvedByContentWhenGraphMissesAndContentAnswers(t *testing.T) {
	t.Parallel()

	entity := querycontract.EntityContent{EntityID: "ce-1", RepoID: "repo-1", RelativePath: "a.yaml", EntityType: "K8sResource", EntityName: "web"}
	handler := &Handler{
		GraphBackend: querycontract.GraphBackendNeo4j,
		Neo4j: graph.FakeGraphReader{
			RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) { return nil, nil },
		},
		Content:              entityContextFakeContentStore{entity: &entity},
		ContentRelationships: scriptedContentRelationshipBuilder{},
		Profile:              querycontract.ProfileLocalAuthoritative,
	}
	obs := observeEntityContext(t, handler, "ce-1")

	if obs.rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", obs.rec.Code, obs.rec.Body.String())
	}
	assertResolved(t, obs, "content", 2)
}

// TestGetEntityContextResolvedByContentWithoutGraphReader: a deployment with
// no graph reader sends zero graph statements and the content store answers.
func TestGetEntityContextResolvedByContentWithoutGraphReader(t *testing.T) {
	t.Parallel()

	entity := querycontract.EntityContent{EntityID: "ce-1", RepoID: "repo-1", RelativePath: "a.yaml", EntityType: "K8sResource", EntityName: "web"}
	handler := &Handler{
		Content:              entityContextFakeContentStore{entity: &entity},
		ContentRelationships: scriptedContentRelationshipBuilder{},
		Profile:              querycontract.ProfileLocalAuthoritative,
	}
	obs := observeEntityContext(t, handler, "ce-1")

	if obs.rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", obs.rec.Code, obs.rec.Body.String())
	}
	assertResolved(t, obs, "content", 0)
}

// TestGetEntityContextResolvedByNoneOnATrueMiss: graph and content both miss,
// the request answers 404, and the true miss is counted as none.
func TestGetEntityContextResolvedByNoneOnATrueMiss(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		GraphBackend: querycontract.GraphBackendNeo4j,
		Neo4j: graph.FakeGraphReader{
			RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) { return nil, nil },
		},
		Content:              entityContextFakeContentStore{},
		ContentRelationships: scriptedContentRelationshipBuilder{},
		Profile:              querycontract.ProfileLocalAuthoritative,
	}
	obs := observeEntityContext(t, handler, "missing")

	if obs.rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", obs.rec.Code, obs.rec.Body.String())
	}
	assertResolved(t, obs, "none", 2)
}

// TestGetEntityContextGraphErrorHasNoResolvedBy: a request that ends in a
// graph error has no resolved_by (no counter, no span attribute) but still
// reports how many statements it sent, and the existing failure log line
// carries statements_tried and no resolved_by. The response is unchanged.
func TestGetEntityContextGraphErrorHasNoResolvedBy(t *testing.T) {
	t.Parallel()

	calls := 0
	handler := &Handler{
		Neo4j: graph.FakeGraphReader{
			RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
				calls++
				if calls < 2 {
					return nil, nil
				}
				return nil, errors.New("boom")
			},
		},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	obs := observeEntityContext(t, handler, "fn-1")

	if obs.rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (unchanged failure path); body=%s", obs.rec.Code, obs.rec.Body.String())
	}
	if v, ok := obs.spanAttrs[resolvedBySpanAttr]; ok {
		t.Fatalf("span carries %s=%v on an error", resolvedBySpanAttr, v)
	}
	if v, ok := obs.spanAttrs[statementsTriedSpanAttr]; !ok || v.AsInt64() != 2 {
		t.Fatalf("span %s = %v (present=%v), want 2", statementsTriedSpanAttr, v, ok)
	}
	if len(obs.counts) != 0 {
		t.Fatalf("%s = %v, want no data points on an error", entityContextResolutionMetric, obs.counts)
	}
	var failure map[string]any
	for _, line := range obs.logs {
		if line["failure_class"] == "graph_read_error" {
			failure = line
		}
	}
	if failure == nil {
		t.Fatalf("no anchor-loop failure log line; logs=%v", obs.logs)
	}
	if got, ok := failure["statements_tried"].(float64); !ok || got != 2 {
		t.Fatalf("failure log statements_tried = %v, want 2; line=%v", failure["statements_tried"], failure)
	}
	if _, ok := failure["resolved_by"]; ok {
		t.Fatalf("failure log carries resolved_by on an error; line=%v", failure)
	}
}

// TestGetEntityContextBackendAnchorMismatchLogCarriesStatementsTried: the
// existing id-mismatch warning gets statements_tried, and the discarded row
// is not credited to the graph: the request resolves by what answers next.
func TestGetEntityContextBackendAnchorMismatchLogCarriesStatementsTried(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		GraphBackend: querycontract.GraphBackendNeo4j,
		Neo4j: graph.FakeGraphReader{
			RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
				return hitRow("some-other-id"), nil
			},
		},
		Content:              entityContextFakeContentStore{},
		ContentRelationships: scriptedContentRelationshipBuilder{},
		Profile:              querycontract.ProfileLocalAuthoritative,
	}
	obs := observeEntityContext(t, handler, "fn-1")

	if obs.rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", obs.rec.Code, obs.rec.Body.String())
	}
	assertResolved(t, obs, "none", 1)
	var mismatch map[string]any
	for _, line := range obs.logs {
		if line["reason"] == "backend_anchor_mismatch" {
			mismatch = line
		}
	}
	if mismatch == nil {
		t.Fatalf("no backend_anchor_mismatch log line; logs=%v", obs.logs)
	}
	if got, ok := mismatch["statements_tried"].(float64); !ok || got != 1 {
		t.Fatalf("mismatch log statements_tried = %v, want 1; line=%v", mismatch["statements_tried"], mismatch)
	}
}

// TestGetEntityContextGrantDeniedGraphRowStillResolvesByTheGraph: a scoped
// caller whose granted repositories exclude the row's repository gets a 404,
// but the graph did answer, so the request is anchor, not a true miss.
func TestGetEntityContextGrantDeniedGraphRowStillResolvesByTheGraph(t *testing.T) {
	t.Parallel()

	row := hitRow("fn-1")
	row["repo_id"] = "repo-b"
	handler := &Handler{
		GraphBackend: querycontract.GraphBackendNeo4j,
		Neo4j: graph.FakeGraphReader{
			RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) { return row, nil },
		},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	metricsObs := observeEntityContextWithAuth(t, handler, "fn-1", auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-a"},
	})

	if metricsObs.rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", metricsObs.rec.Code, metricsObs.rec.Body.String())
	}
	assertResolved(t, metricsObs, "anchor", 1)
}

// TestGetEntityContextResolutionToleratesNilInstrumentsAndLogger: tests and
// the local profile build a Handler with neither Instruments nor Logger. Every
// path that now carries resolution signals (answer, graph error, id mismatch)
// must still answer as before, and the span attributes must still be set.
func TestGetEntityContextResolutionToleratesNilInstrumentsAndLogger(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		run        func(context.Context, string, map[string]any) (map[string]any, error)
		wantStatus int
		wantBy     string
	}{
		{
			name:       "answer",
			run:        func(context.Context, string, map[string]any) (map[string]any, error) { return hitRow("fn-1"), nil },
			wantStatus: http.StatusOK,
			wantBy:     "anchor",
		},
		{
			name:       "graph_error",
			run:        func(context.Context, string, map[string]any) (map[string]any, error) { return nil, errors.New("boom") },
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "id_mismatch",
			run:        func(context.Context, string, map[string]any) (map[string]any, error) { return hitRow("other"), nil },
			wantStatus: http.StatusNotFound,
			wantBy:     "none",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := &Handler{
				GraphBackend:         querycontract.GraphBackendNeo4j,
				Neo4j:                graph.FakeGraphReader{RunSingleFn: tc.run},
				Content:              entityContextFakeContentStore{},
				ContentRelationships: scriptedContentRelationshipBuilder{},
				Profile:              querycontract.ProfileLocalAuthoritative,
			}
			spans := tracetest.NewSpanRecorder()
			tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
			ctx, span := tracerProvider.Tracer("nil-tolerance-test").Start(context.Background(), "GET /entities/{id}/context")
			req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/fn-1/context", nil).WithContext(ctx)
			req.SetPathValue("entity_id", "fn-1")
			rec := httptest.NewRecorder()

			handler.GetEntityContext(rec, req)
			span.End()

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			attrs := map[string]attribute.Value{}
			for _, kv := range spans.Ended()[0].Attributes() {
				attrs[string(kv.Key)] = kv.Value
			}
			if _, ok := attrs[statementsTriedSpanAttr]; !ok {
				t.Fatalf("span lacks %s without Instruments/Logger", statementsTriedSpanAttr)
			}
			got, ok := attrs[resolvedBySpanAttr]
			if tc.wantBy == "" && ok {
				t.Fatalf("span carries %s=%v on an error", resolvedBySpanAttr, got)
			}
			if tc.wantBy != "" && got.AsString() != tc.wantBy {
				t.Fatalf("span %s = %v (present=%v), want %q", resolvedBySpanAttr, got, ok, tc.wantBy)
			}
		})
	}
}
