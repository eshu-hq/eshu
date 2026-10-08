// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func newTestProviders(t *testing.T) *Providers {
	t.Helper()
	_ = os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	b, err := NewBootstrap("test-service")
	require.NoError(t, err)
	providers, err := NewProviders(context.Background(), b)
	require.NoError(t, err)
	t.Cleanup(func() { _ = providers.Shutdown(context.Background()) })
	return providers
}

func scrapeMetrics(h http.Handler) (int, string) {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Code, rec.Body.String()
}

// TestScrapeSurvivesAttributeSetsThatVaryAcrossCalls pins the negative result of
// the #7723 investigation: one instrument recorded with different attribute key
// sets is NOT a gather error. A fix keyed on label-dimension drift would be
// chasing a defect that does not exist.
func TestScrapeSurvivesAttributeSetsThatVaryAcrossCalls(t *testing.T) {
	providers := newTestProviders(t)
	ctx := context.Background()
	hist, err := providers.MeterProvider.Meter("eshu/vary-test").Float64Histogram(
		"eshu_dp_vary_probe_seconds",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 1),
	)
	require.NoError(t, err)
	hist.Record(ctx, 0.2, metric.WithAttributes(attribute.String("route", "a")))
	hist.Record(ctx, 0.2, metric.WithAttributes(attribute.String("route", "a"), attribute.String("outcome", "ok")))
	hist.Record(ctx, 0.2)

	code, body := scrapeMetrics(providers.PrometheusHandler)
	require.Equal(t, http.StatusOK, code, body)
	require.Contains(t, body, `eshu_dp_vary_probe_seconds_count{`)
}

// TestScrapeSurvivesAttributesThatShadowResourceLabels is the regression for
// the persistent bare 500 on /metrics: a metric attribute named service.name or
// service.namespace sanitizes to the same Prometheus label as the resource
// constant label, so client_golang rejects the series ("duplicate label names
// in constant and variable labels") and, with the default error handling, fails
// the entire scrape. The resource already carries both values, so the colliding
// attribute must never reach the exporter, and the series must still render.
func TestScrapeSurvivesAttributesThatShadowResourceLabels(t *testing.T) {
	providers := newTestProviders(t)
	ctx := context.Background()
	meter := providers.MeterProvider.Meter("eshu/shadow-test")

	counter, err := meter.Int64Counter("eshu_dp_shadow_probe_total")
	require.NoError(t, err)
	hist, err := meter.Float64Histogram("eshu_dp_shadow_probe_seconds",
		metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(0.1, 1))
	require.NoError(t, err)

	for _, key := range []string{"service.namespace", "service.name", "service_namespace", "service_name"} {
		counter.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", "ok"), attribute.String(key, "caller-value")))
		hist.Record(ctx, 0.2, metric.WithAttributes(attribute.String("outcome", "ok"), attribute.String(key, "caller-value")))
	}

	code, body := scrapeMetrics(providers.PrometheusHandler)
	require.Equal(t, http.StatusOK, code, body)
	require.Contains(t, body, `eshu_dp_shadow_probe_total{`)
	require.Contains(t, body, `eshu_dp_shadow_probe_seconds_count{`)
	// The catch-all view must not discard the explicit bucket boundaries an
	// instrument was registered with.
	require.Contains(t, body, `eshu_dp_shadow_probe_seconds_bucket{`)
	require.Contains(t, body, `le="0.1"`)
	require.NotContains(t, body, `le="5"`, "default OTEL boundaries replaced the instrument's own")
	require.NotContains(t, body, "caller-value", "the resource labels are authoritative; a caller value must not replace them")
	require.Contains(t, body, `service_namespace="eshu"`)
	require.Contains(t, body, `service_name="test-service"`)
	// All four spellings collapse onto one series per instrument.
	require.Equal(t, 1, strings.Count(body, "eshu_dp_shadow_probe_total{"), body)
}

type recordingClock struct{ now time.Time }

func (c *recordingClock) Now() time.Time { return c.now }

func newGatherErrorFixture(t *testing.T, collector prometheus.Collector) (http.Handler, *bytes.Buffer, *sdkmetric.ManualReader, *recordingClock) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	counter, err := mp.Meter("eshu/gather-test").Int64Counter(MetricsGatherErrorsMetricName)
	require.NoError(t, err)

	registry := prometheus.NewRegistry()
	good := prometheus.NewGauge(prometheus.GaugeOpts{Name: "eshu_dp_good_series", Help: "healthy series"})
	good.Set(7)
	registry.MustRegister(good)
	registry.MustRegister(collector)

	var logs bytes.Buffer
	b, err := NewBootstrap("test-service")
	require.NoError(t, err)
	logger := NewLoggerWithWriter(b, "metrics", "test", &logs)
	clock := &recordingClock{now: time.Unix(1_700_000_000, 0)}
	return newMetricsHandler(registry, logger, counter, clock.Now), &logs, reader, clock
}

type invalidSeriesCollector struct{ err error }

func (c invalidSeriesCollector) Describe(chan<- *prometheus.Desc) {}
func (c invalidSeriesCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.NewInvalidMetric(prometheus.NewInvalidDesc(c.err), c.err)
}

func gatherErrorCount(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != MetricsGatherErrorsMetricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "data = %T", m.Data)
			var total int64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total
		}
	}
	return 0
}

// TestMetricsHandlerServesHealthySeriesAndReportsGatherError proves one bad
// series no longer blinds the whole scrape, and that the loss is loud: the
// healthy family is served with 200, the failing instrument is named in an
// error log, and a counter rises so an alert can fire.
func TestMetricsHandlerServesHealthySeriesAndReportsGatherError(t *testing.T) {
	bad := errors.New(`collected metric "eshu_dp_bad_series" has a label named "x" whose value is not utf8`)
	handler, logs, reader, _ := newGatherErrorFixture(t, invalidSeriesCollector{err: bad})

	code, body := scrapeMetrics(handler)
	require.Equal(t, http.StatusOK, code, body)
	require.Contains(t, body, "eshu_dp_good_series 7")
	require.NotContains(t, body, "eshu_dp_bad_series", "the failing series must be absent, never rendered wrong")

	require.Contains(t, logs.String(), `"event_name":"`+EventMetricsGatherFailed+`"`)
	require.Contains(t, logs.String(), "eshu_dp_bad_series", "the log must name the failing instrument")
	require.Equal(t, int64(1), gatherErrorCount(t, reader))
}

// TestMetricsHandlerRateLimitsRepeatedGatherErrorLogs keeps a persistent
// failure from writing one error line per scrape while the counter still
// counts every failing scrape.
func TestMetricsHandlerRateLimitsRepeatedGatherErrorLogs(t *testing.T) {
	bad := errors.New(`collected metric "eshu_dp_bad_series" is broken`)
	handler, logs, reader, clock := newGatherErrorFixture(t, invalidSeriesCollector{err: bad})

	for range 3 {
		scrapeMetrics(handler)
	}
	require.Equal(t, 1, strings.Count(logs.String(), EventMetricsGatherFailed), logs.String())
	require.Equal(t, int64(3), gatherErrorCount(t, reader))

	clock.now = clock.now.Add(gatherErrorLogInterval + time.Second)
	scrapeMetrics(handler)
	require.Equal(t, 2, strings.Count(logs.String(), EventMetricsGatherFailed), logs.String())
	require.Equal(t, int64(4), gatherErrorCount(t, reader))
}

// TestMetricsHandlerFailsWhenNothingCanBeGathered keeps the bare-failure
// contract for a registry that yields no metric at all, so a dead exporter is
// not mistaken for an empty-but-healthy scrape.
func TestMetricsHandlerFailsWhenNothingCanBeGathered(t *testing.T) {
	registry := prometheus.NewRegistry()
	registry.MustRegister(invalidSeriesCollector{err: errors.New("broken")})
	b, err := NewBootstrap("test-service")
	require.NoError(t, err)
	var logs bytes.Buffer
	handler := newMetricsHandler(registry, NewLoggerWithWriter(b, "metrics", "test", &logs), nil, time.Now)

	code, _ := scrapeMetrics(handler)
	require.Equal(t, http.StatusInternalServerError, code)
	require.Contains(t, logs.String(), EventMetricsGatherFailed)
}

// TestCleanScrapeExposesGatherErrorSeries pins that the gather-error counter
// exists from provider creation. Prometheus increase() cannot see a series that
// first appears already above zero, so an alert on the counter would miss the
// first failure unless a zero baseline is exported before any error happens.
func TestCleanScrapeExposesGatherErrorSeries(t *testing.T) {
	providers := newTestProviders(t)

	code, body := scrapeMetrics(providers.PrometheusHandler)
	require.Equal(t, http.StatusOK, code, body)
	require.Regexp(t, `(?m)^`+MetricsGatherErrorsMetricName+`\{[^}]*\} 0$`, body)
}
