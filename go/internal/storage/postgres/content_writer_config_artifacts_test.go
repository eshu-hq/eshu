// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

// configArtifactFakeDeriver records the requests ContentWriter.Write derives
// and answers with a canned result. It implements ConfigArtifactDeriver.
type configArtifactFakeDeriver struct {
	mu       sync.Mutex
	requests []artifacts.Request
	result   artifacts.Result
}

func (f *configArtifactFakeDeriver) DeriveRepository(_ context.Context, req artifacts.Request) artifacts.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	out := f.result
	out.RepoID = req.RepoID
	out.Trigger = req.Trigger
	return out
}

func (f *configArtifactFakeDeriver) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *configArtifactFakeDeriver) lastRequest() artifacts.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

// configArtifactDeriveSeries collects eshu_dp_config_artifact_derives_total by
// trigger and outcome, and counts the derive duration points by trigger.
func configArtifactDeriveSeries(t *testing.T, reader *sdkmetric.ManualReader) (map[[2]string]int64, map[string]int64) {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("collect: %v", err)
	}
	counts := map[[2]string]int64{}
	durations := map[string]int64{}
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "eshu_dp_config_artifact_derives_total":
				for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
					trigger, _ := point.Attributes.Value("trigger")
					outcome, _ := point.Attributes.Value("outcome")
					counts[[2]string{trigger.AsString(), outcome.AsString()}] += point.Value
				}
			case "eshu_dp_config_artifact_derive_duration_seconds":
				for _, point := range m.Data.(metricdata.Histogram[float64]).DataPoints {
					trigger, _ := point.Attributes.Value("trigger")
					durations[trigger.AsString()] += int64(point.Count)
				}
			}
		}
	}
	return counts, durations
}

func configArtifactWriteMaterialization() content.Materialization {
	return content.Materialization{
		RepoID:       "repo-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Records:      []content.Record{{Path: "a.tf", Body: "resource {}"}},
	}
}

func TestContentWriterWriteDerivesConfigArtifactsAfterInfraDerive(t *testing.T) {
	t.Parallel()

	database := withTransactions(&fakeExecQueryer{})
	fake := &configArtifactFakeDeriver{result: artifacts.Result{Outcome: artifacts.OutcomeOK, Rows: 3}}
	writer := NewContentWriter(database).WithConfigArtifactDeriver(fake)

	if _, err := writer.Write(context.Background(), configArtifactWriteMaterialization()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := fake.requestCount(); got != 1 {
		t.Fatalf("derive requests = %d, want 1", got)
	}
	req := fake.lastRequest()
	if req.RepoID != "repo-1" || req.ScopeID != "scope-1" || req.GenerationID != "gen-1" || req.Trigger != artifacts.TriggerWrite {
		t.Fatalf("derive request = %+v, want repo-1/scope-1/gen-1/write", req)
	}
}

func TestContentWriterWriteSkipsConfigArtifactDeriveWithoutDeriver(t *testing.T) {
	t.Parallel()

	instruments, reader := newDeriveMeter(t)
	writer := NewContentWriter(withTransactions(&fakeExecQueryer{})).WithInstruments(instruments)

	if _, err := writer.Write(context.Background(), configArtifactWriteMaterialization()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	counts, durations := configArtifactDeriveSeries(t, reader)
	if len(counts) != 0 || len(durations) != 0 {
		t.Fatalf("derive series = %v durations = %v, want no config-artifact derive without a deriver", counts, durations)
	}
}

func TestContentWriterWriteSucceedsWhenConfigArtifactDeriveFails(t *testing.T) {
	t.Parallel()

	instruments, reader := newDeriveMeter(t)
	database := withTransactions(&fakeExecQueryer{})
	fake := &configArtifactFakeDeriver{result: artifacts.Result{Outcome: artifacts.OutcomeError, ErrorClass: "error", Err: errors.New("boom")}}
	writer := NewContentWriter(database).WithInstruments(instruments).WithConfigArtifactDeriver(fake)

	if _, err := writer.Write(context.Background(), configArtifactWriteMaterialization()); err != nil {
		t.Fatalf("Write() error = %v, want nil: a failed config-artifact derive never fails the Write", err)
	}
	counts, durations := configArtifactDeriveSeries(t, reader)
	if counts[[2]string{"write", "error"}] != 1 {
		t.Fatalf("derive series = %v, want one write/error", counts)
	}
	if durations["write"] != 1 {
		t.Fatalf("derive durations = %v, want one write point", durations)
	}
}

func TestContentWriterWriteDoesNotDeriveConfigArtifactsWhenInfraDeriveFails(t *testing.T) {
	t.Parallel()

	database := withTransactions(&fakeExecQueryer{})
	database.txExecErr = errors.New("lock timeout")
	fake := &configArtifactFakeDeriver{result: artifacts.Result{Outcome: artifacts.OutcomeOK}}
	writer := NewContentWriter(database).WithConfigArtifactDeriver(fake)

	if _, err := writer.Write(context.Background(), configArtifactWriteMaterialization()); err == nil {
		t.Fatal("Write() error = nil, want the infra derive failure surfaced")
	}
	if got := fake.requestCount(); got != 0 {
		t.Fatalf("derive requests = %d, want 0: the config-artifact derive runs only after a successful infra derive", got)
	}
}

func TestContentWriterConfigArtifactDeriveEmitsSpan(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")
	instruments, reader := newDeriveMeter(t)
	fake := &configArtifactFakeDeriver{result: artifacts.Result{Outcome: artifacts.OutcomeOK, Rows: 3, ContentVersion: 7}}
	writer := NewContentWriter(withTransactions(&fakeExecQueryer{})).
		WithInstruments(instruments).
		WithConfigArtifactDeriver(fake).
		WithConfigArtifactTracer(tracer)

	if _, err := writer.Write(context.Background(), configArtifactWriteMaterialization()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	if got := ended[0].Name(); got != "config_artifacts.derive" {
		t.Fatalf("span name = %q, want config_artifacts.derive", got)
	}
	attrs := map[string]string{}
	for _, attr := range ended[0].Attributes() {
		attrs[string(attr.Key)] = attr.Value.String()
	}
	if attrs["repo_id"] != "repo-1" || attrs["trigger"] != "write" || attrs["outcome"] != "ok" {
		t.Fatalf("span attributes = %v, want repo_id=repo-1 trigger=write outcome=ok", attrs)
	}
	counts, _ := configArtifactDeriveSeries(t, reader)
	if counts[[2]string{"write", "ok"}] != 1 {
		t.Fatalf("derive series = %v, want one write/ok", counts)
	}
}

func TestNewConfigArtifactWriteDeriverDisabledBuildsNothing(t *testing.T) {
	t.Parallel()

	inner := &fakeExecQueryer{}
	deriver, err := NewConfigArtifactWriteDeriver(
		context.Background(),
		withTransactions(inner),
		func(key string) string {
			if key == "ESHU_CONFIG_ARTIFACT_DERIVE_ENABLED" {
				return "false"
			}
			return ""
		},
		nil,
	)
	if err != nil {
		t.Fatalf("NewConfigArtifactWriteDeriver() error = %v", err)
	}
	if deriver != nil {
		t.Fatal("NewConfigArtifactWriteDeriver() = non-nil, want nil when the derive is disabled")
	}
	inner.mu.Lock()
	defer inner.mu.Unlock()
	if len(inner.execs) != 0 || len(inner.queries) != 0 {
		t.Fatalf("database calls = %d execs %d queries, want none before any derive", len(inner.execs), len(inner.queries))
	}
}

func TestContentWriterWriteSkipsDeriveFromDisabledHelper(t *testing.T) {
	t.Parallel()

	// Regression for the typed-nil deriver: the disabled helper must hand
	// the writer a nil interface (not a typed-nil *Deriver), so Write
	// skips the derive instead of panicking on the nil receiver.
	deriver, err := NewConfigArtifactWriteDeriver(
		context.Background(),
		withTransactions(&fakeExecQueryer{}),
		func(key string) string {
			if key == "ESHU_CONFIG_ARTIFACT_DERIVE_ENABLED" {
				return "false"
			}
			return ""
		},
		nil,
	)
	if err != nil {
		t.Fatalf("NewConfigArtifactWriteDeriver() error = %v", err)
	}
	instruments, reader := newDeriveMeter(t)
	writer := NewContentWriter(withTransactions(&fakeExecQueryer{})).
		WithInstruments(instruments).
		WithConfigArtifactDeriver(deriver)

	if _, err := writer.Write(context.Background(), configArtifactWriteMaterialization()); err != nil {
		t.Fatalf("Write() error = %v, want nil with a disabled derive helper", err)
	}
	counts, durations := configArtifactDeriveSeries(t, reader)
	if len(counts) != 0 || len(durations) != 0 {
		t.Fatalf("derive series = %v durations = %v, want no config-artifact derive", counts, durations)
	}
}

func TestNewConfigArtifactWriteDeriverRejectsInvalidTimeout(t *testing.T) {
	t.Parallel()

	_, err := NewConfigArtifactWriteDeriver(
		context.Background(),
		withTransactions(&fakeExecQueryer{}),
		func(key string) string {
			if key == "ESHU_CONFIG_ARTIFACT_DERIVE_TIMEOUT" {
				return "forever"
			}
			return ""
		},
		nil,
	)
	if err == nil {
		t.Fatal("NewConfigArtifactWriteDeriver() error = nil, want the bad timeout to fail startup")
	}
}

func TestNewConfigArtifactWriteDeriverRejectsPoolWithoutTransactions(t *testing.T) {
	t.Parallel()

	// fakeExecQueryer has no Begin: no content transaction can derive through
	// it, so wiring must fail instead of attaching a deriver that can never
	// write.
	_, err := NewConfigArtifactWriteDeriver(
		context.Background(),
		&fakeExecQueryer{},
		func(string) string { return "" },
		nil,
	)
	if err == nil {
		t.Fatal("NewConfigArtifactWriteDeriver() error = nil, want failure for a pool that cannot open transactions")
	}
}
