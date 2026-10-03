// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const (
	failedStageRoute = "/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=10&repository_id=repository:r_217415d9"
	failedStageRepo  = "repository:r_217415d9"
	// failedStageLogErrorMax mirrors the 256-byte bound the stage_failed event
	// promises for its error attribute (#7546).
	failedStageLogErrorMax = 256
)

// failingImpactFindingsStore is the findings read port failing with a fixed
// error, standing in for the guarded Postgres reader (#7546).
type failingImpactFindingsStore struct{ err error }

func (s failingImpactFindingsStore) ListSupplyChainImpactFindings(
	context.Context,
	impact.FindingFilter,
) ([]impact.FindingRow, error) {
	return nil, s.err
}

// decodeLogRecords parses the JSON slog lines captured in buf.
func decodeLogRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		record := map[string]any{}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not JSON: %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func recordsWithEvent(records []map[string]any, event string) []map[string]any {
	var out []map[string]any
	for _, record := range records {
		if record["event_name"] == event {
			out = append(out, record)
		}
	}
	return out
}

func serveImpactFindings(t *testing.T, handler *Handler) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, failedStageRoute, nil))
	return rec
}

func failedStageRow() impact.FindingRow {
	row := osPackageFindingRowForRuntimeContext()
	row.RepositoryID = failedStageRepo
	return row
}

// TestListImpactFindingsLogsFailedStageOnHandlerOwned500 is the #7546
// regression proof: a handler-owned HTTP 500 on the impact-findings route
// left no attributable log (querycontract.WriteError never logs, and the
// findings stage completion carried no error attribute). Each 5xx branch must
// now emit exactly one ERROR supply_chain_query.stage_failed record with the
// stage, repository, and a bounded error, record the error on the handler
// span, and leave the wire response unchanged.
func TestListImpactFindingsLogsFailedStageOnHandlerOwned500(t *testing.T) {
	// Not parallel: swaps the package-global queryHandlerTracer.
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("impact-findings-stage-failed-test")
	t.Cleanup(func() { queryHandlerTracer = previousTracer })

	longASCII := strings.Repeat("x", 600)
	longMultiByte := strings.Repeat("é", 300) // 600 bytes, 2 bytes per rune

	tests := []struct {
		name       string
		build      func() *Handler
		stage      string
		wantDetail string
		wantLogErr string // exact text, or the bounded prefix when truncated
		wantSite   string // closed-set error_site; "" means "other"
		wantCause  string // closed-set error_cause; "" means "unknown"
		truncated  bool
	}{
		{
			name: "findings read error",
			build: func() *Handler {
				return &Handler{ImpactFindings: failingImpactFindingsStore{
					err: errors.New("PostgreSQL reader connection unavailable"),
				}}
			},
			stage:      "impact_findings_query",
			wantDetail: "PostgreSQL reader connection unavailable",
			wantLogErr: "PostgreSQL reader connection unavailable",
		},
		{
			name: "guarded reader borrow failure keeps its cause class",
			build: func() *Handler {
				return &Handler{ImpactFindings: failingImpactFindingsStore{err: fixedTextError{
					text: "PostgreSQL reader connection unavailable",
					cause: errors.Join(db.ErrReaderUnavailable,
						&net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}),
				}}}
			},
			stage:      "impact_findings_query",
			wantDetail: "PostgreSQL reader connection unavailable",
			wantLogErr: "PostgreSQL reader connection unavailable",
			wantSite:   "reader_unavailable",
			wantCause:  "conn_reset",
		},
		{
			name: "findings read error longer than the bound",
			build: func() *Handler {
				return &Handler{ImpactFindings: failingImpactFindingsStore{err: errors.New(longASCII)}}
			},
			stage:      "impact_findings_query",
			wantDetail: longASCII,
			wantLogErr: longASCII[:failedStageLogErrorMax],
			truncated:  true,
		},
		{
			name: "findings read error longer than the bound with multibyte runes",
			build: func() *Handler {
				return &Handler{ImpactFindings: failingImpactFindingsStore{err: errors.New(longMultiByte)}}
			},
			stage:      "impact_findings_query",
			wantDetail: longMultiByte,
			wantLogErr: strings.Repeat("é", failedStageLogErrorMax/2),
			truncated:  true,
		},
		{
			name: "cloud runtime probe error",
			build: func() *Handler {
				digest := failedStageRow().SubjectDigest
				cloudGraph := &graph.FakeCloudRuntimeGraph{RowsByDigest: map[string][]map[string]any{
					digest: {cloudResourceGraphRow("uid-x", digest, "arn-x")},
				}}
				return &Handler{
					Neo4j:                  cloudGraph,
					ImpactFindings:         &graph.FakeRuntimeContextFindingStore{Rows: []impact.FindingRow{failedStageRow()}},
					CloudResourceInventory: &stubCloudInventory{err: errors.New("ledger unavailable"), rowsByDigest: cloudGraph.RowsByDigest},
				}
			},
			stage:      "cloud_runtime_evidence",
			wantDetail: "supply-chain impact runtime evidence probe failed",
			wantLogErr: "ledger unavailable",
		},
		{
			name: "kubernetes runtime probe non-graph error",
			build: func() *Handler {
				digest := failedStageRow().SubjectDigest
				k8sGraph := &graph.FakeKubernetesRuntimeGraph{Rows: []map[string]any{{
					"matched_digest": digest, "workload_uid": "kw-1", "edge_scope_id": "scope-1", "edge_generation_id": "gen-1",
				}}}
				return &Handler{
					Neo4j:                       k8sGraph,
					ImpactFindings:              &graph.FakeRuntimeContextFindingStore{Rows: []impact.FindingRow{failedStageRow()}},
					KubernetesWorkloadInventory: &stubKubernetesWorkloadInventory{err: errors.New("owner ledger unavailable")},
				}
			},
			stage:      "kubernetes_runtime_evidence",
			wantDetail: "supply-chain impact kubernetes runtime evidence probe failed",
			wantLogErr: "filter current authorized kubernetes runtime workloads: owner ledger unavailable",
		},
		{
			name: "runtime context probe non-graph error",
			build: func() *Handler {
				return &Handler{ImpactFindings: &graph.FakeRuntimeContextFindingStore{
					Rows: []impact.FindingRow{failedStageRow()},
					Err:  errors.New("postgres: connection reset"),
				}}
			},
			stage:      "runtime_context",
			wantDetail: "supply-chain impact runtime context probe failed",
			wantLogErr: "postgres: connection reset",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spansBefore := len(recorder.Ended())
			var logBuf bytes.Buffer
			handler := tt.build()
			handler.Logger = slog.New(slog.NewJSONHandler(&logBuf, nil))

			rec := serveImpactFindings(t, handler)

			// (a) wire behavior is unchanged.
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
			if body["error"] != http.StatusText(http.StatusInternalServerError) || body["detail"] != tt.wantDetail {
				t.Fatalf("body = %#v, want error=%q detail=%q", body, http.StatusText(http.StatusInternalServerError), tt.wantDetail)
			}

			// (b) exactly one ERROR stage_failed record with bounded fields.
			records := decodeLogRecords(t, &logBuf)
			failed := recordsWithEvent(records, "supply_chain_query.stage_failed")
			if len(failed) != 1 {
				t.Fatalf("stage_failed records = %d, want 1; log=%s", len(failed), logBuf.String())
			}
			record := failed[0]
			if record["level"] != "ERROR" {
				t.Fatalf("stage_failed level = %v, want ERROR", record["level"])
			}
			if record["stage"] != tt.stage || record["repo_id"] != failedStageRepo ||
				record["operation"] != supplyChainImpactFindingsOperation {
				t.Fatalf("stage_failed identity = %#v, want stage=%q repo_id=%q operation=%q",
					record, tt.stage, failedStageRepo, supplyChainImpactFindingsOperation)
			}
			wantSite, wantCause := tt.wantSite, tt.wantCause
			if wantSite == "" {
				wantSite = "other"
			}
			if wantCause == "" {
				wantCause = "unknown"
			}
			if record["error_site"] != wantSite || record["error_cause"] != wantCause {
				t.Fatalf("stage_failed error_site/error_cause = %v/%v, want %s/%s",
					record["error_site"], record["error_cause"], wantSite, wantCause)
			}
			gotErr, _ := record["error"].(string)
			if gotErr != tt.wantLogErr {
				t.Fatalf("stage_failed error = %q, want %q", gotErr, tt.wantLogErr)
			}
			if len(gotErr) > failedStageLogErrorMax || !utf8.ValidString(gotErr) || strings.ContainsRune(gotErr, utf8.RuneError) {
				t.Fatalf("stage_failed error is not bounded valid UTF-8: len=%d", len(gotErr))
			}
			if tt.truncated && len(gotErr) == len(tt.wantDetail) {
				t.Fatalf("stage_failed error was not truncated")
			}

			// (c) the findings stage completion carries error=true on failure.
			if tt.stage == "impact_findings_query" {
				for _, completed := range recordsWithEvent(records, "supply_chain_query.stage_completed") {
					if completed["stage"] != "impact_findings_query" {
						continue
					}
					if completed["error"] != true {
						t.Fatalf("impact_findings_query completion error = %v, want true; record=%#v", completed["error"], completed)
					}
				}
			}

			// (f) the handler span records the error with Error status.
			var handlerSpan sdktrace.ReadOnlySpan
			for _, span := range recorder.Ended()[spansBefore:] {
				if span.Name() == telemetry.SpanQuerySupplyChainImpactFindings {
					handlerSpan = span
				}
			}
			if handlerSpan == nil {
				t.Fatalf("no ended %s span", telemetry.SpanQuerySupplyChainImpactFindings)
			}
			if handlerSpan.Status().Code != codes.Error {
				t.Fatalf("handler span status = %v, want Error", handlerSpan.Status().Code)
			}
			hasException := false
			for _, event := range handlerSpan.Events() {
				if event.Name == "exception" {
					hasException = true
				}
			}
			if !hasException {
				t.Fatalf("handler span has no recorded exception event; events=%#v", handlerSpan.Events())
			}
		})
	}
}

// TestListImpactFindingsStageCompletionErrorAttributeAndNoFailureOnSuccess
// locks the success side of #7546: the findings stage completion reports
// error=false on a healthy read, and a successful request emits no
// stage_failed record.
func TestListImpactFindingsStageCompletionErrorAttributeAndNoFailureOnSuccess(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	handler := &Handler{
		ImpactFindings: &graph.FakeRuntimeContextFindingStore{
			Rows:   []impact.FindingRow{failedStageRow()},
			ByRepo: map[string]impact.RuntimeContext{},
		},
		Logger: slog.New(slog.NewJSONHandler(&logBuf, nil)),
	}
	rec := serveImpactFindings(t, handler)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	records := decodeLogRecords(t, &logBuf)
	if failed := recordsWithEvent(records, "supply_chain_query.stage_failed"); len(failed) != 0 {
		t.Fatalf("stage_failed records = %d on success, want 0; log=%s", len(failed), logBuf.String())
	}
	found := false
	for _, completed := range recordsWithEvent(records, "supply_chain_query.stage_completed") {
		if completed["stage"] != "impact_findings_query" {
			continue
		}
		found = true
		if completed["error"] != false {
			t.Fatalf("impact_findings_query completion error = %v, want false; record=%#v", completed["error"], completed)
		}
	}
	if !found {
		t.Fatalf("no impact_findings_query completion record; log=%s", logBuf.String())
	}
}

// TestListImpactFindingsNilLoggerStillFailsWith500 proves the failure helper is
// nil-safe like the stage timer: a handler without a Logger keeps the same 500.
func TestListImpactFindingsNilLoggerStillFailsWith500(t *testing.T) {
	t.Parallel()

	handler := &Handler{ImpactFindings: failingImpactFindingsStore{err: errors.New("reader unavailable")}}
	rec := serveImpactFindings(t, handler)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "reader unavailable") {
		t.Fatalf("body = %s, want the unchanged detail", rec.Body.String())
	}
}
