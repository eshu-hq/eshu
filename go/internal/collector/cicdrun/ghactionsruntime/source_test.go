// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestNewClaimedSourceRejectsUnboundedTargets(t *testing.T) {
	t.Parallel()

	_, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              fakeClient{},
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token",
			AllowedRepositories: []string{"example/repo"},
			MaxRuns:             maxRunPages + 1,
			MaxJobs:             10,
			MaxArtifacts:        10,
		}},
	})
	if err == nil {
		t.Fatal("NewClaimedSource() error = nil, want max_runs rejection")
	}
}

func TestClaimedSourceCollectsGitHubActionsRunAndArtifacts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 7, 15, 0, 0, 0, time.UTC)
	client := fakeClient{page: RunPage{Snapshots: []RunSnapshot{{
		Workflow: map[string]any{
			"id":    42,
			"name":  "Publish",
			"path":  ".github/workflows/publish.yml",
			"state": "active",
		},
		Run: map[string]any{
			"id":             1001,
			"run_attempt":    2,
			"run_number":     88,
			"name":           "Publish",
			"event":          "push",
			"status":         "completed",
			"conclusion":     "success",
			"head_branch":    "main",
			"head_sha":       "0123456789abcdef0123456789abcdef01234567",
			"run_started_at": "2026-06-07T14:59:00Z",
			"updated_at":     "2026-06-07T15:00:00Z",
			"html_url":       "https://github.com/example/repo/actions/runs/1001",
			"repository": map[string]any{
				"full_name": "example/repo",
				"html_url":  "https://github.com/example/repo",
			},
			"actor": map[string]any{"login": "builder"},
		},
		Artifacts: []map[string]any{{
			"id":                   501,
			"name":                 "image-digest",
			"size_in_bytes":        128,
			"digest":               "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"archive_download_url": "https://api.github.com/repos/example/repo/actions/artifacts/501/zip?token=secret",
			"created_at":           "2026-06-07T15:00:01Z",
			"expires_at":           "2026-06-14T15:00:01Z",
			"workflow_run": map[string]any{
				"id":       1001,
				"head_sha": "0123456789abcdef0123456789abcdef01234567",
			},
		}},
	}}}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Now:                 func() time.Time { return observedAt },
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token",
			AllowedRepositories: []string{"example/repo"},
			SourceURI:           "https://github.com/example/repo",
			MaxRuns:             1,
			MaxJobs:             10,
			MaxArtifacts:        10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "ci-cd:github-actions:example/repo",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := collected.Scope.CollectorKind, scope.CollectorCICDRun; got != want {
		t.Fatalf("Scope.CollectorKind = %q, want %q", got, want)
	}
	if got, want := collected.Generation.ScopeID, "ci-cd:github-actions:example/repo"; got != want {
		t.Fatalf("Generation.ScopeID = %q, want %q", got, want)
	}

	envelopes := drainFacts(t, collected.Facts)
	requireFactKind(t, envelopes, facts.CICDRunFactKind)
	artifact := requireFactKind(t, envelopes, facts.CICDArtifactFactKind)
	if got, want := artifact.Payload["artifact_digest"], "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; got != want {
		t.Fatalf("artifact_digest = %#v, want %#v", got, want)
	}
	if got, want := artifact.Payload["download_url"], "https://api.github.com/repos/example/repo/actions/artifacts/501/zip"; got != want {
		t.Fatalf("download_url = %#v, want stripped URL %#v", got, want)
	}
}

func TestClaimedSourceClassifiesProviderErrors(t *testing.T) {
	t.Parallel()

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              fakeClient{err: ErrRateLimited},
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token",
			AllowedRepositories: []string{"example/repo"},
			MaxRuns:             1,
			MaxJobs:             10,
			MaxArtifacts:        10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	_, _, err = source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "ci-cd:github-actions:example/repo",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("NextClaimed() error = %v, want ErrRateLimited", err)
	}
}

func TestClaimedSourceRecordsProviderTelemetry(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("ci-cd-run-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	source := newTelemetryTestSource(t, fakeClient{page: RunPage{Snapshots: []RunSnapshot{telemetryTestSnapshot()}}}, instruments, tracerProvider)

	if _, _, err := source.NextClaimed(context.Background(), telemetryTestWorkItem()); err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}

	rm := collectCICDRunMetrics(t, reader)
	assertCICDRunCounterPoint(t, rm, "eshu_dp_ci_cd_run_provider_requests_total", map[string]string{
		telemetry.MetricDimensionProvider:    string(cicdrun.ProviderGitHubActions),
		telemetry.MetricDimensionStatusClass: "success",
	})
	assertCICDRunHistogramPoint(t, rm, "eshu_dp_ci_cd_run_fetch_duration_seconds", map[string]string{
		telemetry.MetricDimensionProvider:    string(cicdrun.ProviderGitHubActions),
		telemetry.MetricDimensionStatusClass: "success",
	})
	assertCICDRunCounterPoint(t, rm, "eshu_dp_ci_cd_run_facts_emitted_total", map[string]string{
		telemetry.MetricDimensionProvider: "github_actions",
		telemetry.MetricDimensionFactKind: facts.CICDRunFactKind,
	})
	assertCICDRunCounterPoint(t, rm, "eshu_dp_ci_cd_run_partial_generations_total", map[string]string{
		telemetry.MetricDimensionProvider: "github_actions",
		telemetry.MetricDimensionReason:   "jobs_truncated",
	})
	assertCICDRunCounterPoint(t, rm, "eshu_dp_ci_cd_run_partial_generations_total", map[string]string{
		telemetry.MetricDimensionProvider: "github_actions",
		telemetry.MetricDimensionReason:   "artifacts_truncated",
	})
	assertCICDRunCounterLabelsExclude(t, rm, "eshu_dp_ci_cd_run_provider_requests_total", "example/repo", "token-value")
	if !cicdRunSpanRecorded(spanRecorder, telemetry.SpanCICDRunObserve) {
		t.Fatalf("span %q was not recorded", telemetry.SpanCICDRunObserve)
	}
	if !cicdRunSpanRecorded(spanRecorder, telemetry.SpanCICDRunFetch) {
		t.Fatalf("span %q was not recorded", telemetry.SpanCICDRunFetch)
	}
}

func TestClaimedSourceRecordsRateLimitTelemetry(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("ci-cd-run-rate-limit-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	source := newTelemetryTestSource(t, fakeClient{err: RateLimitError{RetryAfter: 45 * time.Second}}, instruments, nil)

	_, _, err = source.NextClaimed(context.Background(), telemetryTestWorkItem())
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("NextClaimed() error = %v, want ErrRateLimited", err)
	}

	rm := collectCICDRunMetrics(t, reader)
	assertCICDRunCounterPoint(t, rm, "eshu_dp_ci_cd_run_provider_requests_total", map[string]string{
		telemetry.MetricDimensionProvider:    "github_actions",
		telemetry.MetricDimensionStatusClass: "rate_limited",
	})
	assertCICDRunCounterPoint(t, rm, "eshu_dp_ci_cd_run_rate_limited_total", map[string]string{
		telemetry.MetricDimensionProvider: "github_actions",
	})
}

type fakeClient struct {
	page RunPage
	err  error
}

func (f fakeClient) FetchRuns(context.Context, TargetConfig) (RunPage, error) {
	return f.page, f.err
}

func drainFacts(t *testing.T, ch <-chan facts.Envelope) []facts.Envelope {
	t.Helper()
	var out []facts.Envelope
	for envelope := range ch {
		out = append(out, envelope)
	}
	return out
}

func requireFactKind(t *testing.T, envelopes []facts.Envelope, factKind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind == factKind {
			return envelope
		}
	}
	t.Fatalf("missing fact kind %q in %#v", factKind, envelopes)
	return facts.Envelope{}
}

func newTelemetryTestSource(
	t *testing.T,
	client fakeClient,
	instruments *telemetry.Instruments,
	tracerProvider *sdktrace.TracerProvider,
) ClaimedSource {
	t.Helper()
	var tracer traceProvider
	if tracerProvider != nil {
		tracer = tracerProvider
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Tracer:              tracerFromProvider(tracer),
		Instruments:         instruments,
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token-value",
			AllowedRepositories: []string{"example/repo"},
			MaxRuns:             1,
			MaxJobs:             10,
			MaxArtifacts:        10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	return source
}

type traceProvider interface {
	Tracer(string, ...trace.TracerOption) trace.Tracer
}

func tracerFromProvider(provider traceProvider) trace.Tracer {
	if provider == nil {
		return nil
	}
	return provider.Tracer(telemetry.DefaultSignalName)
}

func telemetryTestWorkItem() workflow.WorkItem {
	return workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "ci-cd:github-actions:example/repo",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	}
}

func telemetryTestSnapshot() RunSnapshot {
	return RunSnapshot{
		Workflow: map[string]any{
			"id":   42,
			"name": "Publish",
		},
		Run: map[string]any{
			"id":             1001,
			"run_attempt":    1,
			"run_number":     88,
			"name":           "Publish",
			"event":          "push",
			"status":         "completed",
			"conclusion":     "success",
			"head_sha":       "0123456789abcdef0123456789abcdef01234567",
			"run_started_at": "2026-06-07T14:59:00Z",
			"updated_at":     "2026-06-07T15:00:00Z",
		},
		JobsPartial:      true,
		ArtifactsPartial: true,
		Artifacts: []map[string]any{{
			"id":            501,
			"name":          "image-digest",
			"digest":        "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"workflow_run":  map[string]any{"id": 1001},
			"created_at":    "2026-06-07T15:00:01Z",
			"expires_at":    "2026-06-14T15:00:01Z",
			"size_in_bytes": 128,
		}},
		Warnings: []map[string]any{{"reason": "provider_metadata_partial"}},
	}
}

func collectCICDRunMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	return rm
}

func assertCICDRunCounterPoint(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	attrs map[string]string,
) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, metricRecord := range sm.Metrics {
			if metricRecord.Name != name {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s has type %T, want Sum[int64]", name, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				if cicdRunAttributeSetContains(point.Attributes, attrs) && point.Value > 0 {
					return
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %v was not recorded", name, attrs)
}

func assertCICDRunHistogramPoint(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	attrs map[string]string,
) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, metricRecord := range sm.Metrics {
			if metricRecord.Name != name {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s has type %T, want Histogram[float64]", name, metricRecord.Data)
			}
			for _, point := range histogram.DataPoints {
				if cicdRunAttributeSetContains(point.Attributes, attrs) && point.Count > 0 {
					return
				}
			}
		}
	}
	t.Fatalf("histogram %s with attrs %v was not recorded", name, attrs)
}

func assertCICDRunCounterLabelsExclude(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	forbiddenValues ...string,
) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, metricRecord := range sm.Metrics {
			if metricRecord.Name != name {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s has type %T, want Sum[int64]", name, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				for _, kv := range point.Attributes.ToSlice() {
					for _, forbidden := range forbiddenValues {
						if kv.Value.AsString() == forbidden {
							t.Fatalf("metric %s label %s leaked forbidden value %q", name, kv.Key, forbidden)
						}
					}
				}
			}
		}
	}
}

func cicdRunAttributeSetContains(attrs attribute.Set, want map[string]string) bool {
	for key, wantValue := range want {
		var matched bool
		for _, kv := range attrs.ToSlice() {
			if string(kv.Key) == key && kv.Value.AsString() == wantValue {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func cicdRunSpanRecorded(recorder *tracetest.SpanRecorder, name string) bool {
	for _, span := range recorder.Ended() {
		if span.Name() == name {
			return true
		}
	}
	return false
}
