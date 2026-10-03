// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestListImpactFindingsMappedVerdictsStaySilentAndUnchanged pins the other
// half of the #7546 contract: an error that querycontract.WriteGraphReadError
// maps to the bounded 503/504 envelope (graph outage, graph deadline, a stale
// or timed-out guarded reader) is NOT a handler-owned 500, so it must keep its
// status and must not emit a stage_failed record or mark the handler span as
// failed. A regression that moved failStage above WriteGraphReadError would
// double-log these verdicts and stay green without this test.
func TestListImpactFindingsMappedVerdictsStaySilentAndUnchanged(t *testing.T) {
	// Not parallel: swaps the package-global queryHandlerTracer.
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("impact-findings-mapped-verdicts-test")
	t.Cleanup(func() { queryHandlerTracer = previousTracer })

	verdicts := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"graph unavailable", querycontract.ErrGraphUnavailable, http.StatusServiceUnavailable},
		{"graph read deadline", querycontract.ErrGraphReadDeadline, http.StatusGatewayTimeout},
		{"reader stale", db.ErrReaderStale, http.StatusServiceUnavailable},
		{
			"reader pool wait timeout",
			errors.Join(db.ErrReaderUnavailable, context.DeadlineExceeded),
			http.StatusServiceUnavailable,
		},
	}
	branches := []struct {
		name  string
		build func(err error) *Handler
	}{
		{
			name: "kubernetes runtime probe",
			build: func(err error) *Handler {
				digest := failedStageRow().SubjectDigest
				k8sGraph := &graph.FakeKubernetesRuntimeGraph{Rows: []map[string]any{{
					"matched_digest": digest, "workload_uid": "kw-1", "edge_scope_id": "scope-1", "edge_generation_id": "gen-1",
				}}}
				return &Handler{
					Neo4j:                       k8sGraph,
					ImpactFindings:              &graph.FakeRuntimeContextFindingStore{Rows: []impact.FindingRow{failedStageRow()}},
					KubernetesWorkloadInventory: &stubKubernetesWorkloadInventory{err: err},
				}
			},
		},
		{
			name: "runtime context probe",
			build: func(err error) *Handler {
				return &Handler{ImpactFindings: &graph.FakeRuntimeContextFindingStore{
					Rows: []impact.FindingRow{failedStageRow()},
					Err:  err,
				}}
			},
		},
	}

	for _, branch := range branches {
		for _, verdict := range verdicts {
			t.Run(branch.name+"/"+verdict.name, func(t *testing.T) {
				spansBefore := len(recorder.Ended())
				var logBuf bytes.Buffer
				handler := branch.build(verdict.err)
				handler.Logger = slog.New(slog.NewJSONHandler(&logBuf, nil))

				rec := serveImpactFindings(t, handler)

				if rec.Code != verdict.wantStatus {
					t.Fatalf("status = %d, want %d; body = %s", rec.Code, verdict.wantStatus, rec.Body.String())
				}
				records := decodeLogRecords(t, &logBuf)
				if failed := recordsWithEvent(records, "supply_chain_query.stage_failed"); len(failed) != 0 {
					t.Fatalf("stage_failed records = %d on a mapped %d verdict, want 0; log=%s",
						len(failed), verdict.wantStatus, logBuf.String())
				}
				for _, span := range recorder.Ended()[spansBefore:] {
					if span.Name() == telemetry.SpanQuerySupplyChainImpactFindings && span.Status().Code == codes.Error {
						t.Fatalf("handler span marked Error for a mapped %d verdict", verdict.wantStatus)
					}
				}
			})
		}
	}
}
