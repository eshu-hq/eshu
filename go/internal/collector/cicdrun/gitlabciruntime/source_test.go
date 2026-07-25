// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitlabciruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

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
			ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/demo",
			ProjectPath:         "eshu-hq/demo",
			Token:               "token",
			AllowedProjectPaths: []string{"eshu-hq/demo"},
			MaxRuns:             maxPipelinePages + 1,
			MaxJobs:             10,
		}},
	})
	if err == nil {
		t.Fatal("NewClaimedSource() error = nil, want max_runs rejection")
	}
}

func TestClaimedSourceCollectsGitLabCIPipelineAndJobs(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 7, 15, 0, 0, 0, time.UTC)
	client := fakeClient{page: PipelinePage{Snapshots: []PipelineSnapshot{{
		Pipeline: map[string]any{
			"id":         7700,
			"iid":        42,
			"project_id": 1,
			"ref":        "main",
			"sha":        "1f2e3d4c5b6a79889706a5b4c3d2e1f00f1e2d3c",
			"status":     "success",
			"source":     "push",
			"created_at": "2026-06-07T14:58:00Z",
			"updated_at": "2026-06-07T15:00:00Z",
			"started_at": "2026-06-07T14:59:00Z",
			"web_url":    "https://gitlab.com/eshu-hq/demo/-/pipelines/7700",
			"user":       map[string]any{"username": "builder"},
		},
		Jobs: []map[string]any{{
			"id":          9001,
			"name":        "build",
			"stage":       "build",
			"status":      "success",
			"created_at":  "2026-06-07T14:58:10Z",
			"started_at":  "2026-06-07T14:58:20Z",
			"finished_at": "2026-06-07T14:59:50Z",
			"web_url":     "https://gitlab.com/eshu-hq/demo/-/jobs/9001",
			"artifacts": []any{
				map[string]any{
					"file_type":   "archive",
					"size":        128,
					"filename":    "artifacts.zip",
					"file_format": "zip",
				},
			},
		}},
	}}}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Now:                 func() time.Time { return observedAt },
		Targets: []TargetConfig{{
			ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/demo",
			ProjectPath:         "eshu-hq/demo",
			Token:               "token",
			AllowedProjectPaths: []string{"eshu-hq/demo"},
			SourceURI:           "https://gitlab.com/eshu-hq/demo",
			MaxRuns:             1,
			MaxJobs:             10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/demo",
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
	if got, want := collected.Generation.ScopeID, "gitlab-ci://gitlab.com/eshu-hq/demo"; got != want {
		t.Fatalf("Generation.ScopeID = %q, want %q", got, want)
	}

	envelopes := drainFacts(t, collected.Facts)
	run := requireFactKind(t, envelopes, facts.CICDRunFactKind)
	if got, want := run.Payload["provider"], "gitlab_ci"; got != want {
		t.Fatalf("run.Payload[provider] = %#v, want %#v", got, want)
	}
	if got, want := run.Payload["run_id"], "7700"; got != want {
		t.Fatalf("run.Payload[run_id] = %#v, want %#v", got, want)
	}
	if got, want := run.Payload["commit_sha"], "1f2e3d4c5b6a79889706a5b4c3d2e1f00f1e2d3c"; got != want {
		t.Fatalf("run.Payload[commit_sha] = %#v, want %#v", got, want)
	}
	job := requireFactKind(t, envelopes, facts.CICDJobFactKind)
	if got, want := job.Payload["job_id"], "9001"; got != want {
		t.Fatalf("job.Payload[job_id] = %#v, want %#v", got, want)
	}
	requireFactKind(t, envelopes, facts.CICDWarningFactKind)
}

func TestClaimedSourceClassifiesProviderErrors(t *testing.T) {
	t.Parallel()

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              fakeClient{err: ErrRateLimited},
		Targets: []TargetConfig{{
			ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/demo",
			ProjectPath:         "eshu-hq/demo",
			Token:               "token",
			AllowedProjectPaths: []string{"eshu-hq/demo"},
			MaxRuns:             1,
			MaxJobs:             10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	_, _, err = source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/demo",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("NextClaimed() error = %v, want ErrRateLimited", err)
	}
}

func TestClaimedSourceRejectsUnclaimedScope(t *testing.T) {
	t.Parallel()

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              fakeClient{},
		Targets: []TargetConfig{{
			ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/demo",
			ProjectPath:         "eshu-hq/demo",
			Token:               "token",
			AllowedProjectPaths: []string{"eshu-hq/demo"},
			MaxRuns:             1,
			MaxJobs:             10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	_, _, err = source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "gitlab-ci://gitlab.com/other/project",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	})
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want unconfigured target error")
	}
}

func TestClaimedSourceRecordsProviderTelemetry(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("ci-cd-run-gitlab-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client: fakeClient{page: PipelinePage{Snapshots: []PipelineSnapshot{{
			Pipeline: map[string]any{
				"id":         1,
				"ref":        "main",
				"sha":        "1f2e3d4c5b6a79889706a5b4c3d2e1f00f1e2d3c",
				"status":     "success",
				"source":     "push",
				"web_url":    "https://gitlab.com/eshu-hq/demo/-/pipelines/1",
				"created_at": "2026-06-07T14:58:00Z",
			},
			Jobs: nil,
		}}}},
		Instruments: instruments,
		Targets: []TargetConfig{{
			ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/demo",
			ProjectPath:         "eshu-hq/demo",
			Token:               "token",
			AllowedProjectPaths: []string{"eshu-hq/demo"},
			MaxRuns:             1,
			MaxJobs:             10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	_, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/demo",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	})
	if err != nil || !ok {
		t.Fatalf("NextClaimed() = (_, %v, %v), want (_, true, nil)", ok, err)
	}

	// Collect and assert the recorded points: without this the test only proves
	// the instrumented path does not panic, not that any metric was incremented
	// (a metric dropped from recordFetch/recordFacts would still pass).
	rm := collectGitLabCICDRunMetrics(t, reader)
	assertGitLabCICDRunCounterPoint(t, rm, "eshu_dp_ci_cd_run_provider_requests_total", map[string]string{
		telemetry.MetricDimensionProvider:    string(cicdrun.ProviderGitLabCI),
		telemetry.MetricDimensionStatusClass: "success",
	})
	assertGitLabCICDRunCounterPoint(t, rm, "eshu_dp_ci_cd_run_facts_emitted_total", map[string]string{
		telemetry.MetricDimensionProvider: string(cicdrun.ProviderGitLabCI),
		telemetry.MetricDimensionFactKind: facts.CICDRunFactKind,
	})
}

// collectGitLabCICDRunMetrics drains the manual reader so the assertions below
// run against the points the source actually recorded.
func collectGitLabCICDRunMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	return rm
}

// assertGitLabCICDRunCounterPoint fails unless the named counter carries a
// positive data point matching every attribute in attrs, so a dropped metric or
// a wrong provider/status dimension is a test failure rather than a silent pass.
func assertGitLabCICDRunCounterPoint(
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
				if gitLabCICDRunAttributesContain(point.Attributes, attrs) && point.Value > 0 {
					return
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %v was not recorded", name, attrs)
}

func gitLabCICDRunAttributesContain(attrs attribute.Set, want map[string]string) bool {
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

type fakeClient struct {
	page PipelinePage
	err  error
}

func (f fakeClient) FetchPipelines(context.Context, TargetConfig) (PipelinePage, error) {
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
