// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestChangePlanningRoutesAnswerSnapshotExportRefusalAsPermanent503 is the
// #7957 proof for the change-surface consumer: every impact route that reads
// code-topic evidence passes the shared-snapshot refusal up unchanged and
// answers it through WriteGraphReadError as a permanent 503 with no retry
// hint, in both negotiation shapes.
func TestChangePlanningRoutesAnswerSnapshotExportRefusalAsPermanent503(t *testing.T) {
	t.Parallel()
	for _, route := range []string{
		"/api/v0/impact/change-surface/investigate",
		"/api/v0/impact/pre-change",
		"/api/v0/impact/developer-change-plan",
	} {
		for _, accept := range []string{EnvelopeMIMEType, ""} {
			t.Run(fmt.Sprintf("%s/accept=%q", route, accept), func(t *testing.T) {
				t.Parallel()
				store := &topicInvestigationContentStore{err: fmt.Errorf("runtime refusal: %w", db.ErrSnapshotExportUnqualified)}
				handler := &ImpactHandler{Content: store, Profile: ProfileLocalAuthoritative}
				mux := http.NewServeMux()
				handler.Mount(mux)
				body := `{"repo_id":"repo-1","topic":"auth","changed_paths":["src/auth.go"],"limit":1}`
				req := httptest.NewRequest(http.MethodPost, route, bytes.NewBufferString(body))
				if accept != "" {
					req.Header.Set("Accept", accept)
				}
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)

				if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "" {
					t.Fatalf("status=%d Retry-After=%q, want permanent 503; body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
				}
				want := querycontract.ErrSnapshotExportRefused.Error()
				if accept == "" {
					var plain map[string]any
					if err := json.Unmarshal(rec.Body.Bytes(), &plain); err != nil || plain["detail"] != want {
						t.Fatalf("plain body = %s", rec.Body.String())
					}
					return
				}
				var envelope querycontract.ResponseEnvelope
				if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if envelope.Error == nil || envelope.Error.Code != querycontract.ErrorCodeBackendUnavailable || envelope.Error.Message != want ||
					envelope.Error.Details["reason"] != querycontract.SnapshotExportUnqualifiedReason {
					t.Fatalf("envelope error = %#v", envelope.Error)
				}
				if _, hinted := envelope.Error.Details["retry_after_seconds"]; hinted {
					t.Fatalf("envelope carries a retry hint: %#v", envelope.Error.Details)
				}
			})
		}
	}
}
