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

// TestLegacyContentFilesPathAppliesThePolicyAndCountsOutcomes drives the
// in-memory (ContentFiles) branch of streamFacts, proves it applies the same
// policy as the two-phase branch, and reads the operator counter and log the
// production path recorded.
func TestLegacyContentFilesPathAppliesThePolicyAndCountsOutcomes(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("content-store-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	var logs bytes.Buffer
	source := &GitSource{Instruments: instruments, Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	ctx := source.withStreamDropRecorder(context.Background())

	snapshot := RepositorySnapshot{
		ContentFiles: []model.ContentFileSnapshot{
			{RelativePath: "a/.npmrc", Body: "_authToken=" + policySeed + "\n", Digest: "digest-a"},
			{RelativePath: "b/.yarnrc.yml", Body: "npmAuthToken: " + policySeed + "\n", Digest: "digest-b"},
			{RelativePath: "c/.yarnrc.yml", Body: "npmAuthToken: " + policySeed + "\xff\n", Digest: "digest-c"},
			{RelativePath: "d/.yarnrc.yml", Body: "nodeLinker: node-modules\n", Digest: "digest-d"},
			{RelativePath: "plain.txt", Body: "unchanged " + policySeed + "\n", Digest: "digest-e"},
		},
		ContentStores: map[string]parser.ContentStore{
			"a/.npmrc":      {Policy: parser.ContentStoreMetadata},
			"b/.yarnrc.yml": policyMarker("npmAuthToken"),
			"c/.yarnrc.yml": policyMarker("npmAuthToken"),
			"d/.yarnrc.yml": policyMarker("npmAuthToken"),
		},
	}
	collected := buildStreamingGenerationWithContext(ctx, t.TempDir(),
		repositoryidentity.Metadata{ID: "repo://example/policy", Name: "policy"}, "run",
		time.Date(2026, time.October, 24, 9, 0, 0, 0, time.UTC), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	bodies := map[string]string{"a/.npmrc": "", "b/.yarnrc.yml": "npmAuthToken: REDACTED_api_token\n", "c/.yarnrc.yml": "", "d/.yarnrc.yml": "nodeLinker: node-modules\n"}
	for path, want := range bodies {
		if got := contentFact(t, envelopes, path).Payload["content_body"]; got != want {
			t.Errorf("%s content_body = %q, want %q", path, got, want)
		}
	}
	if got := contentFact(t, envelopes, "plain.txt").Payload["content_body"]; got != "unchanged "+policySeed+"\n" {
		t.Errorf("an unbound path must store verbatim, got %q", got)
	}
	if got := contentFact(t, envelopes, "c/.yarnrc.yml").Payload["content_digest"]; got != "digest-c" {
		t.Errorf("the fallback row keeps the source digest, got %v", got)
	}

	want := map[string]int64{
		"metadata/metadata":      1,
		"redact/redacted":        1,
		"redact/redactor_failed": 1,
		"redact/clean":           1,
	}
	got := contentStoreCounterValues(t, reader)
	for key, count := range want {
		if got[key] != count {
			t.Errorf("counter %s = %d, want %d (all: %v)", key, got[key], count, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected counter series: %v", got)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "fell back to metadata rows") {
		t.Errorf("a redactor failure must log a WARN, got %q", logs.String())
	}
	for _, want := range []string{"c/.yarnrc.yml", "invalid_utf8", "failed_reason_invalid_utf8=1"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("the WARN log must name the failed path and the bounded reason class: missing %q in %q", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), policySeed) {
		t.Errorf("the log leaked the seeded credential: %q", logs.String())
	}
}

func contentStoreCounterValues(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
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
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("counter data is %T, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				policy, _ := point.Attributes.Value("policy")
				outcome, _ := point.Attributes.Value("outcome")
				values[policy.AsString()+"/"+outcome.AsString()] += point.Value
			}
		}
	}
	return values
}
