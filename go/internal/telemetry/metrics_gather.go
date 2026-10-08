// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
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
	// MetricsGatherErrorsMetricName counts series the /metrics scrape could not
	// gather. It rises once per failing series per scrape, so a persistent
	// defect shows as a steady rate and an alert on increase() is enough. The
	// failing instrument is named in the matching error log, not in a label.
	MetricsGatherErrorsMetricName = "eshu_dp_metrics_gather_errors_total"

	// EventMetricsGatherFailed is the event_name of the error log written when
	// the Prometheus gatherer reports one or more failing series.
	EventMetricsGatherFailed = "telemetry.metrics.gather_failed"

	// gatherErrorLogInterval bounds how often an unchanged gather error is
	// logged. A scrape every 15 seconds would otherwise write thousands of
	// identical lines a day for one broken instrument.
	gatherErrorLogInterval = 5 * time.Minute

	// gatherErrorLogLimit caps the error text in a log line. The text names the
	// failing metrics and their label values, which are bounded, but a registry
	// with many broken series should not write an unbounded line.
	gatherErrorLogLimit = 2048
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
	counter, err := meter.Int64Counter(
		MetricsGatherErrorsMetricName,
		metric.WithDescription("Series the /metrics scrape could not gather; the failing instrument is named in the telemetry.metrics.gather_failed error log"),
	)
	if err != nil {
		return nil, fmt.Errorf("register %s counter: %w", MetricsGatherErrorsMetricName, err)
	}
	return counter, nil
}

// newMetricsHandler serves the registry on /metrics. A gather error drops only
// the failing series: one broken instrument must not blind every healthy one,
// which the default error handling does with a bare 500 that never reaches a
// log. The loss is reported instead of hidden: each failing scrape is logged
// (rate limited) with the offending metric text and counted. A scrape that
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
	lastText   string
	lastLogged time.Time
}

// Gather implements prometheus.Gatherer. It returns the gathered families
// untouched; reporting never changes what is served.
func (g *reportingGatherer) Gather() ([]*dto.MetricFamily, error) {
	families, err := g.inner.Gather()
	if err != nil {
		g.report(err)
		return families, fmt.Errorf("gather prometheus registry: %w", err)
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
	if g.logger == nil || !g.shouldLog(err.Error()) {
		return
	}
	text := err.Error()
	if len(text) > gatherErrorLogLimit {
		text = text[:gatherErrorLogLimit] + "...(truncated)"
	}
	g.logger.Error("metrics scrape dropped series that failed to gather",
		EventAttr(EventMetricsGatherFailed),
		slog.Int64("error_count", count),
		slog.String("error", text),
	)
}

// shouldLog reports whether this error text is new or the last identical report
// is older than gatherErrorLogInterval.
func (g *reportingGatherer) shouldLog(text string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if text == g.lastText && now.Sub(g.lastLogged) < gatherErrorLogInterval {
		return false
	}
	g.lastText = text
	g.lastLogged = now
	return true
}
