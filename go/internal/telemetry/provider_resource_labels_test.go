package telemetry

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
)

// TestPrometheusExposesServiceLabelsOnMetrics locks in the contract that
// Eshu data-plane metrics carry service.name and service.namespace as
// Prometheus labels rather than burying them on the separate target_info
// gauge.
//
// Background: every dashboard under docs/dashboards/ defines `service` and
// `namespace` template variables that query
// label_values(eshu_dp_*, service_name) and label_values(eshu_dp_*, service_namespace).
// Before issue #154 landed, the OTEL Prometheus exporter was constructed
// without WithResourceAsConstantLabels, so those labels existed only on
// target_info. Both Grafana dropdowns were silently empty and every panel
// that filtered by {service_name=~"$service"} or
// {service_namespace=~"$namespace"} returned no data.
//
// This test fails the build if a future refactor of provider.go drops
// the WithResourceAsConstantLabels call or narrows the allow-keys filter
// below these two attributes.
func TestPrometheusExposesServiceLabelsOnMetrics(t *testing.T) {
	_ = os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	b, err := NewBootstrap("test-service")
	require.NoError(t, err)

	ctx := context.Background()
	providers, err := NewProviders(ctx, b)
	require.NoError(t, err)
	defer func() {
		_ = providers.Shutdown(ctx)
	}()

	meter := providers.MeterProvider.Meter("eshu/resource-labels-test")
	counter, err := meter.Int64Counter("eshu_dp_resource_labels_probe")
	require.NoError(t, err)
	counter.Add(ctx, 1, metric.WithAttributes())

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	providers.PrometheusHandler.ServeHTTP(rec, req)
	body := rec.Body.String()

	counterLines := metricLinesContaining(body, "eshu_dp_resource_labels_probe")
	require.NotEmpty(t, counterLines, "counter never reached the /metrics output")

	require.True(t,
		anyLineContains(counterLines, `service_name="test-service"`),
		"service_name label missing from data-plane counter; dashboards filtering by {service_name=~\"$service\"} will return empty data.\n  counter lines: %v",
		counterLines,
	)
	require.True(t,
		anyLineContains(counterLines, `service_namespace="eshu"`),
		"service_namespace label missing from data-plane counter; dashboards filtering by {service_namespace=~\"$namespace\"} will return empty data.\n  counter lines: %v",
		counterLines,
	)
}

// metricLinesContaining returns only data lines (not # HELP / # TYPE) of the
// Prometheus exposition body that mention needle.
func metricLinesContaining(body, needle string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, needle) {
			out = append(out, line)
		}
	}
	return out
}

func anyLineContains(lines []string, needle string) bool {
	for _, line := range lines {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}
