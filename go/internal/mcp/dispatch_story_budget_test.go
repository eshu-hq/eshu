// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/answer"
	"github.com/eshu-hq/eshu/go/internal/query/service"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// serviceStoryLargeHandler serves the large-service story the way the story
// route does: it builds the dossier from the every-list-at-cap fixture, shapes
// it with the real exported shaper for the requested evidence_detail, and
// writes the canonical envelope with the answer packet companion.
func serviceStoryLargeHandler(t *testing.T, wantDetail string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		detail := querycontract.QueryParam(r, "evidence_detail")
		if detail != wantDetail {
			t.Errorf("forwarded evidence_detail = %q, want %q", detail, wantDetail)
		}
		data := service.BuildServiceStoryResponse("checkout-service", testutil.OversizedServiceDossierContext())
		truth := querycontract.BuildTruthEnvelope(querycontract.ProfileProduction, "platform_impact.context_overview",
			querycontract.TruthBasisHybrid, "resolved from service dossier and platform evidence")
		truth.Omissions = querycontract.ApplyStoryEvidenceDetail(data, detail)
		querycontract.WriteSuccess(w, r, http.StatusOK, answer.ServiceStoryAnswerData("checkout-service", data, truth), truth)
	})
}

// dispatchServiceStory dispatches get_service_story with the default response
// budget.
func dispatchServiceStory(t *testing.T, handler http.Handler, args map[string]any) *dispatchResult {
	t.Helper()
	result, err := dispatchToolWithOptions(
		context.Background(), handler, "get_service_story", args, "",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		dispatchOptions{responseByteBudget: defaultToolResponseByteBudget},
	)
	if err != nil {
		t.Fatalf("dispatchToolWithOptions(get_service_story) error = %v", err)
	}
	return result
}

// TestServiceStoryHandlesAnsweredAsOneCopy drives get_service_story through
// the real dispatch path on the every-list-at-cap fixture (#8798). Under the MCP
// default (handles) the dual result is over the budget but one copy fits, so
// the dispatch answers with the resource copy alone: no error envelope, and the
// fallback counter records it. Under full even one copy is over the budget, so
// the dispatch returns mcp_response_over_budget, the failure #8798 reported.
func TestServiceStoryHandlesAnsweredAsOneCopy(t *testing.T) {
	// Not parallel: installs a process-global meter provider.
	reader := budgetMetricsForTest(t)

	handles := dispatchServiceStory(t, serviceStoryLargeHandler(t, querycontract.ContextEvidenceDetailHandles),
		map[string]any{"service_name": "checkout-service"})
	if handles.IsError || !handles.ResourceOnly {
		t.Fatalf("handles story IsError=%v ResourceOnly=%v, want a resource-copy success", handles.IsError, handles.ResourceOnly)
	}
	if handles.Envelope == nil || handles.Envelope.Error != nil {
		t.Fatalf("handles story envelope = %+v, want a success envelope, not %s", handles.Envelope, errorCodeResponseOverBudget)
	}
	if emitted := estimateResponseBytes(handles); emitted > defaultToolResponseByteBudget {
		t.Fatalf("handles story emitted %d bytes, want <= the %d-byte budget", emitted, defaultToolResponseByteBudget)
	}
	if got := collectResourceFallbacks(t, reader, "get_service_story"); got != 1 {
		t.Fatalf("resource fallback counter = %d, want 1", got)
	}
	if over, _, _ := collectBudgetMetrics(t, reader, "get_service_story"); over != 0 {
		t.Fatalf("over-budget counter = %d after the handles story, want 0", over)
	}

	full := dispatchServiceStory(t, serviceStoryLargeHandler(t, querycontract.ContextEvidenceDetailFull),
		map[string]any{"service_name": "checkout-service", "evidence_detail": querycontract.ContextEvidenceDetailFull})
	if !full.IsError || full.Envelope == nil || full.Envelope.Error == nil || full.Envelope.Error.Code != errorCodeResponseOverBudget {
		t.Fatalf("full story = %+v, want the %s envelope", full, errorCodeResponseOverBudget)
	}
	if over, _, _ := collectBudgetMetrics(t, reader, "get_service_story"); over != 1 {
		t.Fatalf("over-budget counter = %d after the full story, want 1", over)
	}
	if got := collectResourceFallbacks(t, reader, "get_service_story"); got != 1 {
		t.Fatalf("resource fallback counter = %d after the full story, want still 1", got)
	}
}
