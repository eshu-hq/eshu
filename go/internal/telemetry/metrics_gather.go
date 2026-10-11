// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const (
	// MetricsGatherErrorsMetricName counts registry gather errors. It rises
	// once per reported error per scrape, so a persistent defect shows as a
	// steady rate. The log reports a closed failure class, not metric data.
	MetricsGatherErrorsMetricName = "eshu_dp_metrics_gather_errors_total"

	// EventMetricsGatherFailed is the event_name of the error log written when
	// the Prometheus gatherer reports one or more failing series.
	EventMetricsGatherFailed = "telemetry.metrics.gather_failed"

	// gatherErrorLogInterval bounds how often a gather failure is
	// logged. A scrape every 15 seconds would otherwise write thousands of
	// lines a day for one broken instrument.
	gatherErrorLogInterval = 5 * time.Minute
	gatherFailureClass     = "prometheus_gather_error"
	gatherFailureMessage   = "prometheus registry gather failed"
)

// prometheusResourceLabelKeys are the OTEL resource attributes the Prometheus
// exporter copies onto every metric as constant labels. They are the single
// source for both the exporter's allow-keys filter and the attribute deny view
// below, so the two cannot drift apart.
var prometheusResourceLabelKeys = []attribute.Key{"service.name", "service.namespace"}

// reservedMetricAttributeKeys lists every attribute spelling that sanitizes to
// one of the resource constant labels: the dotted OTEL key and its underscore
// form. A metric point carrying one of them makes client_golang reject the
// series with "duplicate label names in constant and variable labels", which
// fails the whole scrape.
func reservedMetricAttributeKeys() []attribute.Key {
	keys := make([]attribute.Key, 0, 2*len(prometheusResourceLabelKeys))
	for _, key := range prometheusResourceLabelKeys {
		keys = append(keys, key, attribute.Key(strings.ReplaceAll(string(key), ".", "_")))
	}
	return keys
}

// reservedAttributeView drops the reserved resource-label attributes from every
// instrument before aggregation. The resource already carries the authoritative
// value as a constant label, so the point-level copy is redundant at best and a
// scrape-killing collision at worst. Series that differed only by that
// attribute merge into one.
func reservedAttributeView() sdkmetric.View {
	return sdkmetric.NewView(
		sdkmetric.Instrument{Name: "*"},
		sdkmetric.Stream{AttributeFilter: attribute.NewDenyKeysFilter(reservedMetricAttributeKeys()...)},
	)
}

// registerMetricsGatherErrors registers the gather-error counter on the
// provider's own meter. It lives with the handler it serves because the handler
// is built before any binary calls NewInstruments.
func registerMetricsGatherErrors(meter metric.Meter) (metric.Int64Counter, error) {
	// The name is a literal, not the constant, so the telemetry-coverage
	// verifier can see the registration.
	counter, err := meter.Int64Counter(
		"eshu_dp_metrics_gather_errors_total",
		metric.WithDescription("Registry gather errors on the /metrics scrape; the telemetry.metrics.gather_failed log reports a bounded failure class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register %s counter: %w", MetricsGatherErrorsMetricName, err)
	}
	// Export a zero baseline now. A counter first recorded at its first error
	// appears above zero, which Prometheus increase() cannot count, so the first
	// failure would not fire an alert on the counter.
	counter.Add(context.Background(), 0)
	return counter, nil
}

// newMetricsHandler serves the registry on /metrics. A gather error drops only
// the failing series: one broken instrument must not blind every healthy one,
// which the default error handling does with a bare 500 that never reaches a
// log. The loss is reported instead of hidden: each failing scrape is logged
// (rate limited) with a closed failure class and counted. A scrape that
// gathers nothing at all still fails with a 500, so a dead exporter is not
// mistaken for a healthy empty one.
func newMetricsHandler(registry *prometheus.Registry, logger *slog.Logger, errorCounter metric.Int64Counter, now func() time.Time) http.Handler {
	if now == nil {
		now = time.Now
	}
	gatherer := &reportingGatherer{inner: registry, logger: logger, counter: errorCounter, now: now}
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError})
}

// reportingGatherer wraps a gatherer and reports every gather error.
type reportingGatherer struct {
	inner   prometheus.Gatherer
	logger  *slog.Logger
	counter metric.Int64Counter
	now     func() time.Time

	mu         sync.Mutex
	lastLogged time.Time
}

// Gather implements prometheus.Gatherer. It returns the gathered families
// untouched; reporting never changes what is served.
func (g *reportingGatherer) Gather() ([]*dto.MetricFamily, error) {
	families, err := g.inner.Gather()
	if err != nil {
		g.report(err)
		// promhttp includes the returned error in a 500 response when no family
		// survived. Registry errors can embed complete label values.
		return families, errors.New(gatherFailureMessage)
	}
	return families, nil
}

func (g *reportingGatherer) report(err error) {
	count := int64(1)
	if multi, ok := err.(prometheus.MultiError); ok {
		count = int64(len(multi))
	}
	if g.counter != nil {
		g.counter.Add(context.Background(), count)
	}
	if g.logger == nil || !g.shouldLog() {
		return
	}
	g.logger.Error("metrics scrape dropped series that failed to gather",
		EventAttr(EventMetricsGatherFailed),
		slog.Int64("error_count", count),
		slog.String(LogKeyFailureClass, gatherFailureClass),
	)
}

// shouldLog allows at most one gather failure log per interval per handler.
// The counter still records every reported error on every scrape.
func (g *reportingGatherer) shouldLog() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if !g.lastLogged.IsZero() && now.Sub(g.lastLogged) < gatherErrorLogInterval {
		return false
	}
	g.lastLogged = now
	return true
}
