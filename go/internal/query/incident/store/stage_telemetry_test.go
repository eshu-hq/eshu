// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestIncidentContextReadRecordsBoundedStageTimings(t *testing.T) {
	db, _ := openIncidentContextStoreTestDB(t, []incidentContextStoreQueryResult{
		{
			match:   "fact.fact_kind = 'incident.record'",
			columns: incidentContextFactColumns(),
			rows: [][]driver.Value{{
				"incident-fact", "scope-a", "generation-1", "reported", "", "PABC123",
				time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), "1.0.0",
				[]byte(`{"provider":"pagerduty","status":"triggered","title":"test incident"}`),
			}},
		},
		{match: "fact.fact_kind = 'incident.lifecycle_event'", columns: incidentContextFactColumns()},
	})

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, parent := provider.Tracer("incident-stage-test").Start(context.Background(), "request")
	_, err := NewStore(db).ReadIncidentContext(ctx, model.IncidentContextFilter{
		Provider: "pagerduty", ProviderIncidentID: "PABC123", Limit: 10,
	})
	parent.End()
	if err != nil {
		t.Fatalf("ReadIncidentContext() error = %v", err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want request span", len(spans))
	}
	stages := make(map[string]bool)
	for _, event := range spans[0].Events() {
		if event.Name != "query.incident_context.stage" {
			continue
		}
		var stage string
		var hasDuration, hasError bool
		for _, attr := range event.Attributes {
			switch string(attr.Key) {
			case "eshu.incident_context.stage":
				stage = attr.Value.AsString()
			case "eshu.incident_context.duration_ms":
				hasDuration = attr.Value.AsFloat64() >= 0
			case "eshu.incident_context.error":
				hasError = !attr.Value.AsBool()
			default:
				t.Errorf("unexpected incident stage attribute %q", attr.Key)
			}
		}
		if stage == "" || !hasDuration || !hasError {
			t.Errorf("incomplete timing for stage %q", stage)
		}
		stages[stage] = true
	}
	for _, stage := range []string{"anchor", "timeline", "changes", "routing", "runtime", "review"} {
		if !stages[stage] {
			t.Errorf("missing %q incident stage timing", stage)
		}
	}
}

func TestIncidentContextReadRecordsReachedStageOnError(t *testing.T) {
	db, _ := openIncidentContextStoreTestDB(t, []incidentContextStoreQueryResult{
		{
			match:   "fact.fact_kind = 'incident.record'",
			columns: incidentContextFactColumns(),
			rows: [][]driver.Value{{
				"incident-fact", "scope-a", "generation-1", "reported", "", "PABC123",
				time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), "1.0.0",
				[]byte(`{"provider":"pagerduty","status":"triggered","title":"test incident"}`),
			}},
		},
		{
			match: "fact.fact_kind = 'incident.lifecycle_event'",
			err:   errors.New("private timeline failure"),
		},
	})

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, parent := provider.Tracer("incident-stage-error-test").Start(context.Background(), "request")
	_, err := NewStore(db).ReadIncidentContext(ctx, model.IncidentContextFilter{
		Provider: "pagerduty", ProviderIncidentID: "PABC123", Limit: 10,
	})
	parent.End()
	if err == nil {
		t.Fatal("ReadIncidentContext() error = nil, want timeline error")
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want request span", len(spans))
	}
	events := spans[0].Events()
	if len(events) != 2 {
		t.Fatalf("stage events = %d, want anchor and timeline only", len(events))
	}
	for index, wantStage := range []string{"anchor", "timeline"} {
		if events[index].Name != "query.incident_context.stage" {
			t.Fatalf("event %d name = %q", index, events[index].Name)
		}
		var gotStage string
		var gotError bool
		for _, attr := range events[index].Attributes {
			switch string(attr.Key) {
			case "eshu.incident_context.stage":
				gotStage = attr.Value.AsString()
			case "eshu.incident_context.duration_ms":
				if attr.Value.AsFloat64() < 0 {
					t.Errorf("event %d has negative duration", index)
				}
			case "eshu.incident_context.error":
				gotError = attr.Value.AsBool()
			default:
				t.Errorf("event %d exposes unexpected attribute %q", index, attr.Key)
			}
		}
		if gotStage != wantStage || gotError != (index == 1) {
			t.Errorf("event %d = stage %q, error %t; want %q, %t", index, gotStage, gotError, wantStage, index == 1)
		}
	}
}
