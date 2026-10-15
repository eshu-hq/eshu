// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestInvestigateCodeTopicDeniesUnqualifiedSnapshotExportWithoutFallback is
// the #7957 consumer regression: a refused shared-snapshot set is returned
// unchanged with no probe, no single-statement fallback, and a denied
// execution mode on the postgres.query span.
func TestInvestigateCodeTopicDeniesUnqualifiedSnapshotExportWithoutFallback(t *testing.T) {
	recorder := &codeTopicParallelRecorder{}
	pool := openCodeTopicParallelDB(t, recorder)
	reader := NewContentReaderWithReadStore(codeTopicTestSnapshotStore{
		ReadStore: postgres.NewSQLReadStore(pool), handle: pool,
		beginErr: fmt.Errorf("runtime refusal: %w", db.ErrSnapshotExportUnqualified),
	})
	spans := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	reader.tracer = provider.Tracer("code-topic-denial-test")

	terms := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	rows, err := reader.InvestigateCodeTopic(t.Context(), codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 26})
	if !errors.Is(err, db.ErrSnapshotExportUnqualified) || len(rows) != 0 {
		t.Fatalf("rows=%#v err=%v, want ErrSnapshotExportUnqualified", rows, err)
	}
	recorder.mu.Lock()
	probes, serial, imports := recorder.probes, recorder.serialQueries, recorder.imports
	recorder.mu.Unlock()
	if probes != 0 || serial != 0 || imports != 0 {
		t.Fatalf("denied read ran SQL: probes=%d serial=%d imports=%d", probes, serial, imports)
	}
	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want the one postgres.query span", len(ended))
	}
	attrs := map[string]string{}
	for _, kv := range ended[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value.String()
	}
	if attrs["code_topic.execution_mode"] != "denied" || attrs["code_topic.denial_reason"] != "snapshot_export_unqualified" {
		t.Fatalf("span attributes = %v, want execution_mode=denied and denial_reason=snapshot_export_unqualified", attrs)
	}
	if _, fallback := attrs["code_topic.parallel_fallback_reason"]; fallback {
		t.Fatalf("denied read recorded a fallback reason: %v", attrs)
	}
	if ended[0].Status().Code != codes.Error || len(ended[0].Events()) == 0 {
		t.Fatalf("span status = %v events = %d, want Error with the recorded refusal", ended[0].Status(), len(ended[0].Events()))
	}
}
