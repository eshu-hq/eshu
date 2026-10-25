// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestContentStoreCounterSplitsByRedactorMode pins the redactor label #8833
// adds: with marker and skeleton both possible under redact, an operator must
// see which mode failed. A metadata path reports redactor="none".
func TestContentStoreCounterSplitsByRedactorMode(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("content-store-mode-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	var logs bytes.Buffer
	source := &GitSource{Instruments: instruments, Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	ctx := source.withStreamDropRecorder(context.Background())

	skeleton := parser.ContentStore{Policy: parser.ContentStoreRedact, Redactor: parser.RedactorSkeleton}
	snapshot := RepositorySnapshot{
		ContentFiles: []model.ContentFileSnapshot{
			{RelativePath: "a/.npmrc", Body: "_authToken=" + policySeed + "\n", Digest: "digest-a"},
			{RelativePath: "b/.yarnrc.yml", Body: "npmAuthToken: " + policySeed + "\n", Digest: "digest-b"},
			{RelativePath: "c/prod.tfvars", Body: "db_password = \"" + policySeed + "\"\n", Digest: "digest-c"},
			{RelativePath: "d/stage.tfvars", Body: "x = \"" + policySeed + "\xff\"\n", Digest: "digest-d"},
		},
		ContentStores: map[string]parser.ContentStore{
			"a/.npmrc":       {Policy: parser.ContentStoreMetadata},
			"b/.yarnrc.yml":  policyMarker("npmAuthToken"),
			"c/prod.tfvars":  skeleton,
			"d/stage.tfvars": skeleton,
		},
	}
	collected := buildStreamingGenerationWithContext(ctx, t.TempDir(),
		repositoryidentity.Metadata{ID: "repo://example/modes", Name: "modes"}, "run",
		time.Date(2026, time.October, 24, 9, 0, 0, 0, time.UTC), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	if got := contentFact(t, envelopes, "c/prod.tfvars").Payload["content_body"]; got != "db_password = REDACTED_password_literal\n" {
		t.Errorf("skeleton content_body = %q", got)
	}
	want := map[string]int64{
		"metadata/none/metadata":          1,
		"redact/marker/redacted":          1,
		"redact/skeleton/redacted":        1,
		"redact/skeleton/redactor_failed": 1,
	}
	got := contentStoreModeCounterValues(t, reader)
	for key, count := range want {
		if got[key] != count {
			t.Errorf("counter %s = %d, want %d (all: %v)", key, got[key], count, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected counter series: %v", got)
	}
	for _, want := range []string{"redact_skeleton_redactor_failed=1", "redact_marker_redacted=1", "metadata_metadata=1", "d/stage.tfvars"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log is missing %q: %q", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), policySeed) {
		t.Errorf("the log leaked the seeded credential: %q", logs.String())
	}
}

func contentStoreModeCounterValues(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	values := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_collector_content_store_files_total" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				policy, _ := point.Attributes.Value("policy")
				redactor, _ := point.Attributes.Value("redactor")
				outcome, _ := point.Attributes.Value("outcome")
				values[policy.AsString()+"/"+redactor.AsString()+"/"+outcome.AsString()] += point.Value
			}
		}
	}
	return values
}
