// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const snapshotExportUnqualifiedMessage = "shared-snapshot read refused: the PostgreSQL reader build is not qualified for snapshot export"

func snapshotExportUnqualifiedErrors() map[string]error {
	return map[string]error{
		"bare sentinel":           db.ErrSnapshotExportUnqualified,
		"private runtime wrapper": privateWrapped{cause: db.ErrSnapshotExportUnqualified},
		"wrapped by caller":       fmt.Errorf("investigate code topic: %w", db.ErrSnapshotExportUnqualified),
		// The refusal is the cause even when the bounded budget also expired.
		"joined with graph deadline": errors.Join(ErrGraphReadDeadline, db.ErrSnapshotExportUnqualified),
	}
}

// TestMapGraphReadErrorSnapshotExportUnqualifiedIsPermanent503 pins the #7957
// verdict: 503 backend_unavailable, a fixed message, a closed reason, and no
// retry marker.
func TestMapGraphReadErrorSnapshotExportUnqualifiedIsPermanent503(t *testing.T) {
	t.Parallel()
	for name, err := range snapshotExportUnqualifiedErrors() {
		mapped, ok := mapGraphReadHTTPError(err)
		if !ok || mapped.status != http.StatusServiceUnavailable || mapped.code != ErrorCodeBackendUnavailable ||
			mapped.message != snapshotExportUnqualifiedMessage || mapped.retryable {
			t.Fatalf("%s: mapped = %+v ok=%v, want permanent 503 backend_unavailable", name, mapped, ok)
		}
		if len(mapped.details) != 1 || mapped.details["reason"] != "snapshot_export_unqualified" {
			t.Fatalf("%s: details = %v, want only reason=snapshot_export_unqualified", name, mapped.details)
		}
	}
	if mapped, ok := mapGraphReadHTTPError(db.ErrSnapshotReservationCapacity); ok {
		t.Fatalf("capacity marker claimed as a graph-read verdict: %+v", mapped)
	}
}

// TestWriteGraphReadErrorSnapshotExportUnqualifiedCarriesNoRetryHint writes
// the verdict through both negotiation shapes: no Retry-After, no
// retry_after_seconds, the fixed message, details.reason in the envelope, and
// the refusal recorded on the request span with Error status.
func TestWriteGraphReadErrorSnapshotExportUnqualifiedCarriesNoRetryHint(t *testing.T) {
	t.Parallel()
	for name, refusal := range snapshotExportUnqualifiedErrors() {
		for _, accept := range []string{EnvelopeMIMEType, ""} {
			t.Run(fmt.Sprintf("%s/accept=%q", name, accept), func(t *testing.T) {
				t.Parallel()
				spans := tracetest.NewSpanRecorder()
				provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
				ctx, span := provider.Tracer("test").Start(context.Background(), "request")
				req := httptest.NewRequest(http.MethodPost, "/api/v0/code/topics/investigate", nil).WithContext(ctx)
				if accept != "" {
					req.Header.Set("Accept", accept)
				}
				rec := httptest.NewRecorder()
				if !WriteGraphReadError(rec, req, refusal, "code_search.topic_investigation") {
					t.Fatal("refusal not claimed")
				}
				span.End()
				if rec.Code != http.StatusServiceUnavailable {
					t.Fatalf("status = %d, want 503", rec.Code)
				}
				if got := rec.Header().Get("Retry-After"); got != "" {
					t.Fatalf("Retry-After = %q on a permanent refusal", got)
				}
				ended := spans.Ended()
				if len(ended) != 1 || ended[0].Status().Code != codes.Error || len(ended[0].Events()) == 0 {
					t.Fatalf("request span not marked with the refusal: %+v", ended)
				}
				if accept == "" {
					var plain map[string]any
					if err := json.Unmarshal(rec.Body.Bytes(), &plain); err != nil || plain["detail"] != snapshotExportUnqualifiedMessage {
						t.Fatalf("plain body = %s (%v)", rec.Body.String(), err)
					}
					return
				}
				var envelope ResponseEnvelope
				if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if envelope.Error == nil || envelope.Error.Code != ErrorCodeBackendUnavailable ||
					envelope.Error.Message != snapshotExportUnqualifiedMessage || envelope.Error.Details["reason"] != "snapshot_export_unqualified" {
					t.Fatalf("envelope error = %#v", envelope.Error)
				}
				if _, hinted := envelope.Error.Details["retry_after_seconds"]; hinted {
					t.Fatalf("envelope carries a retry hint: %#v", envelope.Error.Details)
				}
			})
		}
	}
}
