package reducer

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestGCPResourceMaterializationRecordsPrometheusSignals(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	handler := GCPResourceMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(),
			gcpResourceEnvelope(map[string]any{"asset_type": "compute.googleapis.com/Disk"}),
		}},
		NodeWriter:  &recordingCloudResourceNodeWriter{},
		Instruments: inst,
	}

	if _, err := handler.Handle(context.Background(), gcpResourceIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	factAttrs := map[string]string{
		telemetry.MetricDimensionDomain:   string(DomainGCPResourceMaterialization),
		telemetry.MetricDimensionFactKind: facts.GCPCloudResourceFactKind,
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_gcp_materialization_facts_total", factAttrs); got != 2 {
		t.Fatalf("gcp resource fact count = %d, want 2", got)
	}
	writeAttrs := map[string]string{
		telemetry.MetricDimensionDomain: string(DomainGCPResourceMaterialization),
		telemetry.MetricDimensionKind:   "node",
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_gcp_materialization_graph_writes_total", writeAttrs); got != 1 {
		t.Fatalf("gcp resource graph writes = %d, want 1", got)
	}
	durationAttrs := map[string]string{
		telemetry.MetricDimensionDomain:     string(DomainGCPResourceMaterialization),
		telemetry.MetricDimensionWritePhase: "graph_write",
	}
	if got := reducerHistogramCount(t, rm, "eshu_dp_gcp_materialization_duration_seconds", durationAttrs); got != 1 {
		t.Fatalf("gcp resource graph_write duration count = %d, want 1", got)
	}
}

func TestGCPRelationshipMaterializationRecordsPrometheusSignals(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(),
			gcpDiskResource(),
			gcpInstanceToDisk("supported"),
			gcpInstanceToDisk("unsupported"),
		}},
		EdgeWriter:           &recordingCloudResourceEdgeWriter{},
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
		Instruments:          inst,
	}

	if _, err := handler.Handle(context.Background(), gcpRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	resourceFactAttrs := map[string]string{
		telemetry.MetricDimensionDomain:   string(DomainGCPRelationshipMaterialization),
		telemetry.MetricDimensionFactKind: facts.GCPCloudResourceFactKind,
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_gcp_materialization_facts_total", resourceFactAttrs); got != 2 {
		t.Fatalf("gcp relationship resource fact count = %d, want 2", got)
	}
	relationshipFactAttrs := map[string]string{
		telemetry.MetricDimensionDomain:   string(DomainGCPRelationshipMaterialization),
		telemetry.MetricDimensionFactKind: facts.GCPCloudRelationshipFactKind,
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_gcp_materialization_facts_total", relationshipFactAttrs); got != 2 {
		t.Fatalf("gcp relationship fact count = %d, want 2", got)
	}
	writeAttrs := map[string]string{
		telemetry.MetricDimensionDomain: string(DomainGCPRelationshipMaterialization),
		telemetry.MetricDimensionKind:   "edge",
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_gcp_materialization_graph_writes_total", writeAttrs); got != 1 {
		t.Fatalf("gcp relationship graph writes = %d, want 1", got)
	}
	durationAttrs := map[string]string{
		telemetry.MetricDimensionDomain:     string(DomainGCPRelationshipMaterialization),
		telemetry.MetricDimensionWritePhase: "graph_write",
	}
	if got := reducerHistogramCount(t, rm, "eshu_dp_gcp_materialization_duration_seconds", durationAttrs); got != 1 {
		t.Fatalf("gcp relationship graph_write duration count = %d, want 1", got)
	}
}

func TestGCPMaterializationSkipsNoOpGraphWriteDurations(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	resourceHandler := GCPResourceMaterializationHandler{
		FactLoader:  &stubFactLoader{},
		NodeWriter:  &recordingCloudResourceNodeWriter{},
		Instruments: inst,
	}
	if _, err := resourceHandler.Handle(context.Background(), gcpResourceIntent()); err != nil {
		t.Fatalf("resource Handle returned error: %v", err)
	}

	relationshipHandler := GCPRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{},
		EdgeWriter:           &recordingCloudResourceEdgeWriter{},
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
		Instruments:          inst,
	}
	if _, err := relationshipHandler.Handle(context.Background(), gcpRelationshipIntent()); err != nil {
		t.Fatalf("relationship Handle returned error: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	for _, attrs := range []map[string]string{
		{
			telemetry.MetricDimensionDomain:     string(DomainGCPResourceMaterialization),
			telemetry.MetricDimensionWritePhase: "graph_write",
		},
		{
			telemetry.MetricDimensionDomain:     string(DomainGCPRelationshipMaterialization),
			telemetry.MetricDimensionWritePhase: "graph_write",
		},
		{
			telemetry.MetricDimensionDomain:     string(DomainGCPRelationshipMaterialization),
			telemetry.MetricDimensionWritePhase: "retract",
		},
	} {
		if reducerHistogramHasAttrs(rm, "eshu_dp_gcp_materialization_duration_seconds", attrs) {
			t.Fatalf("no-op execution emitted duration sample with attrs %v", attrs)
		}
	}
}

func TestGCPMaterializationSignalsReachPrometheusExposition(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	ctx := context.Background()
	bootstrap, err := telemetry.NewBootstrap("reducer")
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	providers, err := telemetry.NewProviders(ctx, bootstrap)
	if err != nil {
		t.Fatalf("NewProviders() error = %v", err)
	}
	defer func() {
		_ = providers.Shutdown(ctx)
	}()

	inst, err := telemetry.NewInstruments(providers.MeterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	resourceHandler := GCPResourceMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(),
		}},
		NodeWriter:  &recordingCloudResourceNodeWriter{},
		Instruments: inst,
	}
	if _, err := resourceHandler.Handle(ctx, gcpResourceIntent()); err != nil {
		t.Fatalf("resource Handle returned error: %v", err)
	}

	relationshipHandler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(),
			gcpDiskResource(),
			gcpInstanceToDisk("supported"),
		}},
		EdgeWriter:           &recordingCloudResourceEdgeWriter{},
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
		Instruments:          inst,
	}
	if _, err := relationshipHandler.Handle(ctx, gcpRelationshipIntent()); err != nil {
		t.Fatalf("relationship Handle returned error: %v", err)
	}

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	providers.PrometheusHandler.ServeHTTP(rec, req)
	body := rec.Body.String()

	assertPrometheusMetricLine(
		t, body, "eshu_dp_gcp_materialization_facts_total",
		`domain="gcp_resource_materialization"`,
		`fact_kind="gcp_cloud_resource"`,
		`service_name="reducer"`,
	)
	assertPrometheusMetricLine(
		t, body, "eshu_dp_gcp_materialization_graph_writes_total",
		`domain="gcp_relationship_materialization"`,
		`kind="edge"`,
	)
	assertPrometheusMetricLine(
		t, body, "eshu_dp_gcp_materialization_duration_seconds_count",
		`domain="gcp_relationship_materialization"`,
		`write_phase="graph_write"`,
	)
}

func gcpResourceIntent() Intent {
	return Intent{
		IntentID:     "intent-gcp-resources-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainGCPResourceMaterialization,
		EntityKeys:   []string{"gcp_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func reducerHistogramHasAttrs(rm metricdata.ResourceMetrics, metricName string, wantAttrs map[string]string) bool {
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != metricName {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				continue
			}
			for _, dp := range histogram.DataPoints {
				if hasAttrs(dp.Attributes.ToSlice(), wantAttrs) && dp.Count > 0 {
					return true
				}
			}
		}
	}
	return false
}

func assertPrometheusMetricLine(t *testing.T, body, metricName string, labels ...string) {
	t.Helper()

	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "#") || !strings.Contains(line, metricName) {
			continue
		}
		matches := true
		for _, label := range labels {
			if !strings.Contains(line, label) {
				matches = false
				break
			}
		}
		if matches {
			return
		}
	}

	t.Fatalf("Prometheus exposition missing %s with labels %v\n%s", metricName, labels, body)
}
