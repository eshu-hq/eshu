// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eshusearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const supersededCounterName = "eshu_dp_search_document_generation_superseded_total"

// alwaysCurrentGeneration is the fake freshness check every non-supersede test
// uses: the generation never moves.
func alwaysCurrentGeneration(context.Context, string, string) (bool, error) {
	return true, nil
}

// threeEntityPages returns three loader pages of one curated entity each.
func threeEntityPages() []SearchDocumentProjectionInput {
	page := func(id, name string) SearchDocumentProjectionInput {
		return SearchDocumentProjectionInput{ContentEntities: []searchdocs.ContentEntity{
			{EntityID: id, RepoID: "repo-1", EntityType: "Function", EntityName: name, SourceCache: "func " + name + "(){}"},
		}}
	}
	return []SearchDocumentProjectionInput{page("e-1", "A"), page("e-2", "B"), page("e-3", "C")}
}

// supersedeAfterInserts returns a freshness check that reports the generation
// current until the writer has accepted n pages, then superseded. It models the
// projector activating a newer generation between two page callbacks.
func supersedeAfterInserts(writer *capturingSearchDocWriter, n int, calls *int) reducercontract.GenerationFreshnessCheck {
	return func(_ context.Context, scopeID, generationID string) (bool, error) {
		*calls++
		if scopeID != "scope-1" || generationID != "gen-1" {
			return false, errors.New("check called with wrong scope or generation")
		}
		return len(writer.insertedPages) < n, nil
	}
}

func supersedeTelemetry(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("eshusearch-supersede"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

// supersededCounterByPhase collects the #7458 counter as phase -> total.
func supersededCounterByPhase(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != supersededCounterName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", supersededCounterName, m.Data)
			}
			for _, dp := range sum.DataPoints {
				phase, _ := dp.Attributes.Value(attribute.Key("phase"))
				if attrLen := dp.Attributes.Len(); attrLen != 1 {
					t.Fatalf("%s carries %d attributes, want only phase", supersededCounterName, attrLen)
				}
				out[phase.AsString()] += dp.Value
			}
		}
	}
	return out
}

// TestEshuSearchDocumentHandlerAbandonsSupersededGenerationMidStream is the
// #7458 regression: once the generation is superseded after page 1, the handler
// must not write pages 2 and 3, must not Cancel (the retire DELETE is the cost
// being removed) and must not Finalize.
func TestEshuSearchDocumentHandlerAbandonsSupersededGenerationMidStream(t *testing.T) {
	t.Parallel()

	instruments, reader := supersedeTelemetry(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	loader := &fakePagedSearchDocLoader{pages: threeEntityPages()}
	writer := &capturingSearchDocWriter{result: EshuSearchDocumentWriteResult{CanonicalWrites: 3}}
	checks := 0
	handler := EshuSearchDocumentHandler{
		Loader:          loader,
		Writer:          writer,
		GenerationCheck: supersedeAfterInserts(writer, 1, &checks),
		Instruments:     instruments,
		Logger:          logger,
	}

	result, err := handler.Handle(context.Background(), searchDocIntent())
	if err != nil {
		t.Fatalf("Handle error = %v, want nil (superseded is an acked result)", err)
	}
	if got := len(writer.insertedPages); got != 1 {
		t.Fatalf("InsertPage calls = %d, want exactly 1 (stop after the supersede is seen)", got)
	}
	if writer.finalizeCalls != 0 {
		t.Fatalf("Finalize calls = %d, want 0", writer.finalizeCalls)
	}
	if writer.cancelCalls != 0 {
		t.Fatalf("Cancel calls = %d, want 0 (Cancel is the retire the fence removes)", writer.cancelCalls)
	}
	if result.Status != reducercontract.ResultStatusSuperseded {
		t.Fatalf("Status = %q, want %q", result.Status, reducercontract.ResultStatusSuperseded)
	}
	if result.CanonicalWrites != 1 {
		t.Errorf("CanonicalWrites = %d, want 1 (documents actually written)", result.CanonicalWrites)
	}
	want := "eshu search document projection abandoned: generation superseded phase=page pages_written=1 documents_written=1"
	if result.EvidenceSummary != want {
		t.Errorf("EvidenceSummary = %q, want %q", result.EvidenceSummary, want)
	}
	if result.IntentID != "intent-1" || result.Domain != DomainEshuSearchDocument {
		t.Errorf("result identity = %q/%q", result.IntentID, result.Domain)
	}
	if checks != 2 {
		t.Errorf("checks = %d, want 2 (page 1 current, page 2 superseded)", checks)
	}
	if got := supersededCounterByPhase(t, reader); got["page"] != 1 || got["finalize"] != 0 || len(got) != 1 {
		t.Errorf("%s = %v, want {page:1}", supersededCounterName, got)
	}
	assertSupersedeLog(t, logs.String(), "page", 1, 1)
}

// TestEshuSearchDocumentHandlerAbandonsSupersededGenerationBeforeFinalize covers
// the check after the stream: the last page landed, then the generation moved,
// so the retire and stats statements must not run.
func TestEshuSearchDocumentHandlerAbandonsSupersededGenerationBeforeFinalize(t *testing.T) {
	t.Parallel()

	instruments, reader := supersedeTelemetry(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	loader := &fakePagedSearchDocLoader{pages: threeEntityPages()}
	writer := &capturingSearchDocWriter{result: EshuSearchDocumentWriteResult{CanonicalWrites: 3}}
	checks := 0
	handler := EshuSearchDocumentHandler{
		Loader:          loader,
		Writer:          writer,
		GenerationCheck: supersedeAfterInserts(writer, 3, &checks),
		Instruments:     instruments,
		Logger:          logger,
	}

	result, err := handler.Handle(context.Background(), searchDocIntent())
	if err != nil {
		t.Fatalf("Handle error = %v, want nil", err)
	}
	if got := len(writer.insertedPages); got != 3 {
		t.Fatalf("InsertPage calls = %d, want 3", got)
	}
	if writer.finalizeCalls != 0 || writer.cancelCalls != 0 {
		t.Fatalf("finalize=%d cancel=%d, want 0/0", writer.finalizeCalls, writer.cancelCalls)
	}
	if result.Status != reducercontract.ResultStatusSuperseded {
		t.Fatalf("Status = %q, want superseded", result.Status)
	}
	if result.CanonicalWrites != 3 {
		t.Errorf("CanonicalWrites = %d, want 3", result.CanonicalWrites)
	}
	want := "eshu search document projection abandoned: generation superseded phase=finalize pages_written=3 documents_written=3"
	if result.EvidenceSummary != want {
		t.Errorf("EvidenceSummary = %q, want %q", result.EvidenceSummary, want)
	}
	if checks != 4 {
		t.Errorf("checks = %d, want 4 (one per page plus one before Finalize)", checks)
	}
	if got := supersededCounterByPhase(t, reader); got["finalize"] != 1 || got["page"] != 0 || len(got) != 1 {
		t.Errorf("%s = %v, want {finalize:1}", supersededCounterName, got)
	}
	assertSupersedeLog(t, logs.String(), "finalize", 3, 3)
}

// TestEshuSearchDocumentHandlerChecksBeforeFirstPage proves the first page is
// fenced too: a generation superseded before any page writes nothing and is
// counted at phase=page with zero pages written.
func TestEshuSearchDocumentHandlerChecksBeforeFirstPage(t *testing.T) {
	t.Parallel()

	instruments, reader := supersedeTelemetry(t)
	loader := &fakePagedSearchDocLoader{pages: threeEntityPages()}
	writer := &capturingSearchDocWriter{}
	checks := 0
	handler := EshuSearchDocumentHandler{
		Loader:          loader,
		Writer:          writer,
		GenerationCheck: supersedeAfterInserts(writer, 0, &checks),
		Instruments:     instruments,
	}

	result, err := handler.Handle(context.Background(), searchDocIntent())
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if len(writer.insertedPages) != 0 || writer.finalizeCalls != 0 || writer.cancelCalls != 0 {
		t.Fatalf("inserted=%d finalize=%d cancel=%d, want all 0", len(writer.insertedPages), writer.finalizeCalls, writer.cancelCalls)
	}
	if result.Status != reducercontract.ResultStatusSuperseded || result.CanonicalWrites != 0 {
		t.Fatalf("result = %+v, want superseded with 0 writes", result)
	}
	if got := supersededCounterByPhase(t, reader); got["page"] != 1 {
		t.Errorf("%s = %v, want page:1", supersededCounterName, got)
	}
}

// TestEshuSearchDocumentHandlerCurrentGenerationStillFinalizes proves a
// generation that stays active writes every page, checks once per page plus once
// before Finalize, and never touches the superseded counter.
func TestEshuSearchDocumentHandlerCurrentGenerationStillFinalizes(t *testing.T) {
	t.Parallel()

	instruments, reader := supersedeTelemetry(t)
	checks := 0
	loader := &fakePagedSearchDocLoader{pages: threeEntityPages()}
	writer := &capturingSearchDocWriter{result: EshuSearchDocumentWriteResult{CanonicalWrites: 3}}
	handler := EshuSearchDocumentHandler{
		Loader: loader,
		Writer: writer,
		GenerationCheck: func(context.Context, string, string) (bool, error) {
			checks++
			return true, nil
		},
		Instruments: instruments,
	}

	result, err := handler.Handle(context.Background(), searchDocIntent())
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded || writer.finalizeCalls != 1 || len(writer.insertedPages) != 3 {
		t.Fatalf("status=%q finalize=%d pages=%d, want succeeded/1/3", result.Status, writer.finalizeCalls, len(writer.insertedPages))
	}
	if checks != 4 {
		t.Errorf("checks = %d, want 4 (3 pages + 1 before Finalize)", checks)
	}
	if got := supersededCounterByPhase(t, reader); len(got) != 0 {
		t.Errorf("%s = %v, want no points", supersededCounterName, got)
	}
}

// TestEshuSearchDocumentHandlerFailsClosedOnCheckError proves a freshness
// lookup error is not read as "current" or "superseded": it takes the existing
// stream-error path, which cancels the partial write and fails the item.
func TestEshuSearchDocumentHandlerFailsClosedOnCheckError(t *testing.T) {
	t.Parallel()

	instruments, reader := supersedeTelemetry(t)
	boom := errors.New("freshness lookup boom")
	loader := &fakePagedSearchDocLoader{pages: threeEntityPages()}
	writer := &capturingSearchDocWriter{}
	handler := EshuSearchDocumentHandler{
		Loader: loader,
		Writer: writer,
		GenerationCheck: func(context.Context, string, string) (bool, error) {
			if len(writer.insertedPages) >= 1 {
				return false, boom
			}
			return true, nil
		},
		Instruments: instruments,
	}

	_, err := handler.Handle(context.Background(), searchDocIntent())
	if !errors.Is(err, boom) {
		t.Fatalf("Handle error = %v, want it to wrap the check error", err)
	}
	if writer.cancelCalls != 1 {
		t.Fatalf("Cancel calls = %d, want 1 (existing stream-error path)", writer.cancelCalls)
	}
	if writer.finalizeCalls != 0 || len(writer.insertedPages) != 1 {
		t.Fatalf("finalize=%d pages=%d, want 0/1", writer.finalizeCalls, len(writer.insertedPages))
	}
	if got := supersededCounterByPhase(t, reader); len(got) != 0 {
		t.Errorf("%s = %v, want no points on a check error", supersededCounterName, got)
	}
}

// TestEshuSearchDocumentHandlerFailsClosedOnFinalizeCheckError covers the
// post-stream check erroring: the item fails and the partial write is not
// finalized.
func TestEshuSearchDocumentHandlerFailsClosedOnFinalizeCheckError(t *testing.T) {
	t.Parallel()

	boom := errors.New("freshness lookup boom")
	loader := &fakePagedSearchDocLoader{pages: threeEntityPages()[:1]}
	writer := &capturingSearchDocWriter{}
	handler := EshuSearchDocumentHandler{
		Loader: loader,
		Writer: writer,
		GenerationCheck: func(context.Context, string, string) (bool, error) {
			if len(writer.insertedPages) >= 1 {
				return false, boom
			}
			return true, nil
		},
	}

	_, err := handler.Handle(context.Background(), searchDocIntent())
	if !errors.Is(err, boom) {
		t.Fatalf("Handle error = %v, want it to wrap the check error", err)
	}
	if writer.finalizeCalls != 0 {
		t.Fatalf("Finalize calls = %d, want 0", writer.finalizeCalls)
	}
}

// TestEshuSearchDocumentHandlerRequiresGenerationCheck proves a nil check is a
// construction error, like a nil loader or writer, and that nothing is written.
func TestEshuSearchDocumentHandlerRequiresGenerationCheck(t *testing.T) {
	t.Parallel()

	writer := &capturingSearchDocWriter{}
	handler := EshuSearchDocumentHandler{Loader: &fakePagedSearchDocLoader{pages: threeEntityPages()}, Writer: writer}
	_, err := handler.Handle(context.Background(), searchDocIntent())
	if err == nil {
		t.Fatal("Handle error = nil, want a construction error for a nil GenerationCheck")
	}
	if !strings.Contains(err.Error(), "generation check") {
		t.Errorf("error = %q, want it to name the generation check", err)
	}
	if writer.begins != 0 {
		t.Errorf("Begin calls = %d, want 0 (construction error precedes any write)", writer.begins)
	}
}

func assertSupersedeLog(t *testing.T, raw, phase string, pages, docs int) {
	t.Helper()
	var record map[string]any
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		var candidate map[string]any
		if err := json.Unmarshal([]byte(line), &candidate); err != nil {
			continue
		}
		if msg, _ := candidate["msg"].(string); strings.Contains(msg, "abandoned") {
			record = candidate
		}
	}
	if record == nil {
		t.Fatalf("no abandon log record in %q", raw)
	}
	if record["level"] != "INFO" {
		t.Errorf("log level = %v, want INFO (a routine supersede is not an error)", record["level"])
	}
	for key, want := range map[string]any{
		"scope_id":          "scope-1",
		"generation_id":     "gen-1",
		"domain":            string(DomainEshuSearchDocument),
		"phase":             phase,
		"pages_written":     float64(pages),
		"documents_written": float64(docs),
	} {
		if record[key] != want {
			t.Errorf("log %s = %v, want %v", key, record[key], want)
		}
	}
	if _, ok := record["duration_seconds"]; !ok {
		t.Error("log missing duration_seconds")
	}
	if record[telemetry.LogKeyPipelinePhase] != telemetry.PhaseReduction {
		t.Errorf("log %s = %v, want %s", telemetry.LogKeyPipelinePhase, record[telemetry.LogKeyPipelinePhase], telemetry.PhaseReduction)
	}
}
