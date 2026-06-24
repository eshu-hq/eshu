package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestContentEntitySourceFileKindClassifier verifies that
// ContentEntitySourceFileKind maps every documented artifact_type to the
// correct bounded SourceFileKind* constant. The classifier is the gatekeeper
// that keeps eshu_dp_content_entity_emitted_total low-cardinality.
func TestContentEntitySourceFileKindClassifier(t *testing.T) {
	t.Parallel()

	cases := []struct {
		artifactType string
		wantKind     string
	}{
		// Empty → code (ordinary source file with no artifact_type)
		{"", SourceFileKindCode},

		// Package manifests and lockfiles
		{"package_manifest", SourceFileKindPackageManifest},
		{"go_module", SourceFileKindPackageManifest},
		{"go_sum", SourceFileKindPackageManifest},
		{"npm_lockfile", SourceFileKindPackageManifest},
		{"yarn_lockfile", SourceFileKindPackageManifest},
		{"pnpm_lockfile", SourceFileKindPackageManifest},
		{"cargo_manifest", SourceFileKindPackageManifest},
		{"cargo_lockfile", SourceFileKindPackageManifest},
		{"pyproject", SourceFileKindPackageManifest},
		{"requirements", SourceFileKindPackageManifest},
		{"pip_lockfile", SourceFileKindPackageManifest},
		{"pipfile", SourceFileKindPackageManifest},
		{"pipfile_lock", SourceFileKindPackageManifest},
		{"maven_pom", SourceFileKindPackageManifest},
		{"gradle_build", SourceFileKindPackageManifest},
		{"nuget", SourceFileKindPackageManifest},
		{"nuget_lock", SourceFileKindPackageManifest},
		{"composer", SourceFileKindPackageManifest},
		{"composer_lock", SourceFileKindPackageManifest},
		{"mix_lock", SourceFileKindPackageManifest},
		{"hex_manifest", SourceFileKindPackageManifest},
		{"pubspec", SourceFileKindPackageManifest},
		{"pubspec_lock", SourceFileKindPackageManifest},
		{"swift_package", SourceFileKindPackageManifest},
		{"swift_package_resolved", SourceFileKindPackageManifest},

		// Config / infra artifacts
		{"config", SourceFileKindConfig},
		{"generic_config", SourceFileKindConfig},
		{"generic_config_template", SourceFileKindConfig},
		{"dockerfile", SourceFileKindConfig},
		{"docker_compose", SourceFileKindConfig},
		{"terraform", SourceFileKindConfig},
		{"terraform_template_text", SourceFileKindConfig},
		{"helm_chart", SourceFileKindConfig},
		{"argocd", SourceFileKindConfig},
		{"kustomize", SourceFileKindConfig},
		{"nginx_config", SourceFileKindConfig},
		{"nginx_config_template", SourceFileKindConfig},
		{"apache_config", SourceFileKindConfig},
		{"apache_config_template", SourceFileKindConfig},
		{"ansible", SourceFileKindConfig},
		{"ansible_playbook", SourceFileKindConfig},
		{"ansible_role", SourceFileKindConfig},
		{"yaml_template", SourceFileKindConfig},
		{"github_actions_workflow", SourceFileKindConfig},
		{"cloudformation_serverless", SourceFileKindConfig},
		{"cloudformation", SourceFileKindConfig},

		// Anything else maps to "other" (not code, manifest, or config)
		{"some_new_artifact_type", SourceFileKindOther},
		{"unknown_kind", SourceFileKindOther},
	}

	for _, tc := range cases {
		t.Run(tc.artifactType+"→"+tc.wantKind, func(t *testing.T) {
			t.Parallel()
			got := ContentEntitySourceFileKind(tc.artifactType)
			if got != tc.wantKind {
				t.Fatalf("ContentEntitySourceFileKind(%q) = %q, want %q", tc.artifactType, got, tc.wantKind)
			}
		})
	}
}

// TestContentEntityEmittedCounterRecordsSourceFileKind verifies that
// eshu_dp_content_entity_emitted_total can be recorded with the
// source_file_kind and collector_kind dimensions and the values reach the
// SDK reader correctly. This is the counter that would have surfaced #3676
// instantly.
func TestContentEntityEmittedCounterRecordsSourceFileKind(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	if inst.ContentEntityEmitted == nil {
		t.Fatal("ContentEntityEmitted counter was not registered")
	}

	ctx := context.Background()

	// Simulate: 100 code entities, 250 lockfile/manifest entities (the #3676 explosion pattern)
	inst.ContentEntityEmitted.Add(ctx, 100, metric.WithAttributes(
		AttrSourceFileKind(SourceFileKindCode),
		AttrCollectorKind("bootstrap-index"),
	))
	inst.ContentEntityEmitted.Add(ctx, 250, metric.WithAttributes(
		AttrSourceFileKind(SourceFileKindPackageManifest),
		AttrCollectorKind("bootstrap-index"),
	))
	inst.ContentEntityEmitted.Add(ctx, 20, metric.WithAttributes(
		AttrSourceFileKind(SourceFileKindConfig),
		AttrCollectorKind("bootstrap-index"),
	))

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// Code entities
	if got := contentEntityCounterValue(t, rm, map[string]string{
		MetricDimensionSourceFileKind: SourceFileKindCode,
		MetricDimensionCollectorKind:  "bootstrap-index",
	}); got != 100 {
		t.Fatalf("content_entity_emitted_total[code] = %d, want 100", got)
	}
	// Package manifest entities (lockfile explosion signal)
	if got := contentEntityCounterValue(t, rm, map[string]string{
		MetricDimensionSourceFileKind: SourceFileKindPackageManifest,
		MetricDimensionCollectorKind:  "bootstrap-index",
	}); got != 250 {
		t.Fatalf("content_entity_emitted_total[package_manifest] = %d, want 250", got)
	}
	// Config entities
	if got := contentEntityCounterValue(t, rm, map[string]string{
		MetricDimensionSourceFileKind: SourceFileKindConfig,
		MetricDimensionCollectorKind:  "bootstrap-index",
	}); got != 20 {
		t.Fatalf("content_entity_emitted_total[config] = %d, want 20", got)
	}
}

// TestBootstrapPipelinePhaseDurationHistogramRecords verifies that
// eshu_dp_bootstrap_pipeline_phase_seconds can be recorded with the
// bootstrap_phase and collector_kind dimensions.
func TestBootstrapPipelinePhaseDurationHistogramRecords(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	if inst.BootstrapPipelinePhaseDuration == nil {
		t.Fatal("BootstrapPipelinePhaseDuration histogram was not registered")
	}

	ctx := context.Background()

	// Record all 5 named phases
	phases := []string{
		BootstrapPhaseCollection,
		BootstrapPhaseProjection,
		BootstrapPhaseRelationshipBackfill,
		BootstrapPhaseIaCReachability,
		BootstrapPhaseConfigStateDrift,
	}
	for _, phase := range phases {
		inst.BootstrapPipelinePhaseDuration.Record(ctx, 1.5, metric.WithAttributes(
			AttrBootstrapPhase(phase),
			AttrCollectorKind("bootstrap-index"),
		))
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	for _, phase := range phases {
		bootstrapPhaseHistogramPoint(t, rm, phase)
	}
}

// contentEntityCounterValue reads the cumulative value for
// eshu_dp_content_entity_emitted_total at the given attribute set.
func contentEntityCounterValue(t *testing.T, rm metricdata.ResourceMetrics, wantAttrs map[string]string) int64 {
	t.Helper()
	const metricName = "eshu_dp_content_entity_emitted_total"
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != metricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Int64 sum", metricName, m.Data)
			}
			for _, point := range sum.DataPoints {
				if attrsMatch(point.Attributes.ToSlice(), wantAttrs) {
					return point.Value
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %#v not found", metricName, wantAttrs)
	return 0
}

// bootstrapPhaseHistogramPoint verifies a data point exists for the named
// bootstrap phase in eshu_dp_bootstrap_pipeline_phase_seconds.
func bootstrapPhaseHistogramPoint(t *testing.T, rm metricdata.ResourceMetrics, phase string) {
	t.Helper()
	const metricName = "eshu_dp_bootstrap_pipeline_phase_seconds"
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != metricName {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data type = %T, want Histogram[float64]", metricName, m.Data)
			}
			for _, point := range hist.DataPoints {
				if attrsMatch(point.Attributes.ToSlice(), map[string]string{
					MetricDimensionBootstrapPhase: phase,
					MetricDimensionCollectorKind:  "bootstrap-index",
				}) {
					return
				}
			}
		}
	}
	t.Fatalf("metric %s for phase %q not found in ResourceMetrics", metricName, phase)
}

// attrsMatch returns true when all wantAttrs key/value pairs appear in attrs.
func attrsMatch(attrs []attribute.KeyValue, wantAttrs map[string]string) bool {
	for wantKey, wantVal := range wantAttrs {
		found := false
		for _, a := range attrs {
			if string(a.Key) == wantKey && a.Value.AsString() == wantVal {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
