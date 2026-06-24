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
// ContentEntitySourceFileKind classifies content entities from the SAME signals
// the real parser/reducer path uses: package manifests are detected from
// entity_type "Variable" + config_kind "dependency" (artifact_type is empty for
// them), and config is detected from the artifact_type tokens inferArtifactType /
// persistedArtifactType actually emit. The classifier is the gatekeeper that
// keeps eshu_dp_content_entity_emitted_total low-cardinality.
func TestContentEntitySourceFileKindClassifier(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		entityType   string
		artifactType string
		configKind   string
		wantKind     string
	}{
		// Package manifests: the real signal is metadata, not artifact_type.
		// Dependency entities land in the parser "Variable" bucket with
		// config_kind="dependency" and carry NO artifact_type.
		{"go.mod dependency", "Variable", "", "dependency", SourceFileKindPackageManifest},
		{"package-lock dependency", "Variable", "", "dependency", SourceFileKindPackageManifest},
		{"maven pom dependency", "Variable", "", "dependency", SourceFileKindPackageManifest},

		// config_kind="dependency" but wrong entity_type is NOT a manifest
		// (the reducer requires entity_type=="Variable").
		{"dependency-config non-Variable", "Function", "", "dependency", SourceFileKindCode},
		// entity_type Variable but a non-dependency config_kind is not a manifest.
		{"Variable checksum config_kind", "Variable", "", "dependency_checksum", SourceFileKindCode},
		{"Variable env-var no config_kind", "Variable", "", "", SourceFileKindCode},

		// Ordinary source code: no artifact_type, no manifest metadata.
		{"plain code function", "Function", "", "", SourceFileKindCode},
		{"plain code class", "Class", "", "", SourceFileKindCode},
		// A plain (untemplated) YAML persists with empty artifact_type → code.
		{"plain yaml empty artifact_type", "Document", "", "", SourceFileKindCode},

		// Config / infra artifacts — only the tokens the parser actually emits
		// via inferArtifactType / persistedArtifactType.
		{"dockerfile", "Resource", "dockerfile", "", SourceFileKindConfig},
		{"docker_compose", "Service", "docker_compose", "", SourceFileKindConfig},
		{"github actions", "Job", "github_actions_workflow", "", SourceFileKindConfig},
		{"terraform_hcl", "Resource", "terraform_hcl", "", SourceFileKindConfig},
		{"terraform_template_text", "Resource", "terraform_template_text", "", SourceFileKindConfig},
		{"helm_helper_tpl", "Template", "helm_helper_tpl", "", SourceFileKindConfig},
		{"go_template_yaml", "Document", "go_template_yaml", "", SourceFileKindConfig},
		{"jinja_yaml", "Document", "jinja_yaml", "", SourceFileKindConfig},
		{"yaml_template", "Document", "yaml_template", "", SourceFileKindConfig},
		{"jinja_text_template", "Document", "jinja_text_template", "", SourceFileKindConfig},
		{"text_template", "Document", "text_template", "", SourceFileKindConfig},
		{"nginx_config", "Resource", "nginx_config", "", SourceFileKindConfig},
		{"nginx_config_template", "Resource", "nginx_config_template", "", SourceFileKindConfig},
		{"apache_config", "Resource", "apache_config", "", SourceFileKindConfig},
		{"apache_config_template", "Resource", "apache_config_template", "", SourceFileKindConfig},
		{"generic_config", "Resource", "generic_config", "", SourceFileKindConfig},
		{"generic_config_template", "Resource", "generic_config_template", "", SourceFileKindConfig},
		{"ansible_playbook", "Task", "ansible_playbook", "", SourceFileKindConfig},
		{"ansible_role", "Task", "ansible_role", "", SourceFileKindConfig},
		{"ansible_inventory", "Task", "ansible_inventory", "", SourceFileKindConfig},
		{"ansible_vars", "Task", "ansible_vars", "", SourceFileKindConfig},
		{"ansible_task_entrypoint", "Task", "ansible_task_entrypoint", "", SourceFileKindConfig},

		// Dead tokens the parser NEVER emits must NOT classify as config — they
		// fall through to "other" so a future parser change is visible, not
		// silently mislabeled.
		{"dead token terraform", "Resource", "terraform", "", SourceFileKindOther},
		{"dead token helm_chart", "Resource", "helm_chart", "", SourceFileKindOther},
		{"dead token argocd", "Resource", "argocd", "", SourceFileKindOther},
		{"dead token kustomize", "Resource", "kustomize", "", SourceFileKindOther},
		{"dead token cloudformation", "Resource", "cloudformation", "", SourceFileKindOther},
		{"unknown artifact_type", "Resource", "some_new_artifact_type", "", SourceFileKindOther},

		// Manifest signal wins even if an artifact_type is somehow also present.
		{"manifest beats artifact_type", "Variable", "dockerfile", "dependency", SourceFileKindPackageManifest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ContentEntitySourceFileKind(tc.entityType, tc.artifactType, tc.configKind)
			if got != tc.wantKind {
				t.Fatalf("ContentEntitySourceFileKind(entityType=%q, artifactType=%q, configKind=%q) = %q, want %q",
					tc.entityType, tc.artifactType, tc.configKind, got, tc.wantKind)
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

	// Record all named phases
	phases := []string{
		BootstrapPhaseCollection,
		BootstrapPhaseProjection,
		BootstrapPhaseRelationshipBackfill,
		BootstrapPhaseIaCReachability,
		BootstrapPhaseDeploymentReopen,
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
