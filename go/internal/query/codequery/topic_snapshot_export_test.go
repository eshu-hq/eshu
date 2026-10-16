// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestCodeTopicRouteAnswersSnapshotExportRefusalAsPermanent503 is the #7957
// route proof: a content reader that refused shared-snapshot export reaches
// the client of POST /api/v0/code/topics/investigate as a 503
// backend_unavailable with a fixed message and details.reason, no Retry-After
// or retry_after_seconds, and an Error request span, in both negotiation
// shapes. It swaps queryHandlerTracer, so it is not parallel.
func TestCodeTopicRouteAnswersSnapshotExportRefusalAsPermanent503(t *testing.T) {
	for _, accept := range []string{querycontract.EnvelopeMIMEType, ""} {
		t.Run(fmt.Sprintf("accept=%q", accept), func(t *testing.T) {
			store := &topicInvestigationContentStore{err: fmt.Errorf("investigate: %w", db.ErrSnapshotExportUnqualified)}
			handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)

			spans := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			previous := queryHandlerTracer
			queryHandlerTracer = provider.Tracer("code-topic-snapshot-export-test")
			t.Cleanup(func() { queryHandlerTracer = previous })
			ctx, span := provider.Tracer("test").Start(context.Background(), "request")
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/topics/investigate",
				bytes.NewBufferString(`{"topic":"auth"}`)).WithContext(ctx)
			if accept != "" {
				req.Header.Set("Accept", accept)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			span.End()

			if len(store.requests) != 1 {
				t.Fatalf("content reads = %d, want 1 (no retry)", len(store.requests))
			}
			assertSnapshotExportRefusal(t, rec, accept != "")
			marked := false
			for _, ended := range spans.Ended() {
				if ended.Name() == telemetry.SpanQueryCodeTopicInvestigation && ended.Status().Code == codes.Error && len(ended.Events()) > 0 {
					marked = true
				}
			}
			if !marked {
				t.Fatalf("%s span not marked Error with the refusal", telemetry.SpanQueryCodeTopicInvestigation)
			}
		})
	}
}

// assertSnapshotExportRefusal checks the stable #7957 permanent-503 contract on
// one response.
func assertSnapshotExportRefusal(t *testing.T, rec *httptest.ResponseRecorder, envelope bool) {
	t.Helper()
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q on a permanent refusal", got)
	}
	want := querycontract.ErrSnapshotExportRefused.Error()
	if !envelope {
		var plain map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &plain); err != nil || plain["detail"] != want {
			t.Fatalf("plain body = %s (%v), want detail %q", rec.Body.String(), err, want)
		}
		return
	}
	var body querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode envelope: %v; body=%s", err, rec.Body.String())
	}
	if body.Error == nil || body.Error.Code != querycontract.ErrorCodeBackendUnavailable || body.Error.Message != want ||
		body.Error.Details["reason"] != querycontract.SnapshotExportUnqualifiedReason {
		t.Fatalf("envelope error = %#v", body.Error)
	}
	if _, hinted := body.Error.Details["retry_after_seconds"]; hinted {
		t.Fatalf("envelope carries a retry hint: %#v", body.Error.Details)
	}
}
