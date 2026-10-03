// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestListImpactFindingsReaderTimeoutAnswersRetryable503 is the #7548
// regression proof. The findings read and the cloud-runtime probe read wrote
// every store error as a fixed 500 carrying the Go error text, so a stale or
// timed-out guarded PostgreSQL reader (#7523) reached clients as a non-retryable
// 500 instead of the 503 backend_unavailable with Retry-After that
// querycontract.WriteGraphReadError answers on the kubernetes-runtime and
// runtime-context branches of the same handler. A non-timeout reader failure
// stays a 500, and a mapped 503 emits no stage_failed record (the #7546
// contract: only handler-owned 500s are logged).
func TestListImpactFindingsReaderTimeoutAnswersRetryable503(t *testing.T) {
	t.Parallel()

	branches := []struct {
		name  string
		stage string
		build func(err error) *Handler
	}{
		{
			name:  "findings read",
			stage: "impact_findings_query",
			build: func(err error) *Handler {
				return &Handler{ImpactFindings: failingImpactFindingsStore{err: err}}
			},
		},
		{
			name:  "cloud runtime probe read",
			stage: "cloud_runtime_evidence",
			build: func(err error) *Handler {
				digest := failedStageRow().SubjectDigest
				cloudGraph := &graph.FakeCloudRuntimeGraph{RowsByDigest: map[string][]map[string]any{
					digest: {cloudResourceGraphRow("uid-x", digest, "arn-x")},
				}}
				return &Handler{
					Neo4j:                  cloudGraph,
					ImpactFindings:         &graph.FakeRuntimeContextFindingStore{Rows: []impact.FindingRow{failedStageRow()}},
					CloudResourceInventory: &stubCloudInventory{err: err, rowsByDigest: cloudGraph.RowsByDigest},
				}
			},
		},
	}
	verdicts := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"reader stale", fmt.Errorf("read findings: %w", db.ErrReaderStale), http.StatusServiceUnavailable},
		{
			"reader pool wait timeout",
			fmt.Errorf("read findings: %w", errors.Join(db.ErrReaderUnavailable, context.DeadlineExceeded)),
			http.StatusServiceUnavailable,
		},
		{"bare reader unavailable", db.ErrReaderUnavailable, http.StatusInternalServerError},
	}

	for _, branch := range branches {
		for _, verdict := range verdicts {
			t.Run(branch.name+"/"+verdict.name, func(t *testing.T) {
				t.Parallel()

				var logBuf bytes.Buffer
				handler := branch.build(verdict.err)
				handler.Logger = slog.New(slog.NewJSONHandler(&logBuf, nil))

				rec := serveImpactFindingsEnvelope(t, handler)

				if rec.Code != verdict.wantStatus {
					t.Fatalf("status = %d, want %d; body = %s", rec.Code, verdict.wantStatus, rec.Body.String())
				}
				failed := recordsWithEvent(decodeLogRecords(t, &logBuf), "supply_chain_query.stage_failed")
				if verdict.wantStatus == http.StatusInternalServerError {
					if rec.Header().Get("Retry-After") != "" {
						t.Fatalf("Retry-After = %q on a non-retryable 500, want none", rec.Header().Get("Retry-After"))
					}
					if len(failed) != 1 || failed[0]["stage"] != branch.stage {
						t.Fatalf("stage_failed records = %#v, want exactly one for stage %q", failed, branch.stage)
					}
					return
				}
				if len(failed) != 0 {
					t.Fatalf("stage_failed records = %d on a mapped 503, want 0; log=%s", len(failed), logBuf.String())
				}
				if seconds, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || seconds < 1 {
					t.Fatalf("Retry-After = %q, want a positive integer", rec.Header().Get("Retry-After"))
				}
				var body struct {
					Error *querycontract.ErrorEnvelope `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == nil {
					t.Fatalf("body = %s, want an error envelope: %v", rec.Body.String(), err)
				}
				if body.Error.Code != querycontract.ErrorCodeBackendUnavailable {
					t.Fatalf("error code = %q, want %q", body.Error.Code, querycontract.ErrorCodeBackendUnavailable)
				}
				if body.Error.Capability != ImpactFindingsCapability {
					t.Fatalf("error capability = %q, want %q", body.Error.Capability, ImpactFindingsCapability)
				}
			})
		}
	}
}

// serveImpactFindingsEnvelope serves the impact-findings route asking for the
// stable response envelope, so a mapped verdict exposes its error code.
func serveImpactFindingsEnvelope(t *testing.T, handler *Handler) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, failedStageRoute, nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
