// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Providers holds the initialized OTEL SDK providers and their shutdown function.
type Providers struct {
	TracerProvider    *sdktrace.TracerProvider
	MeterProvider     *sdkmetric.MeterProvider
	PrometheusHandler http.Handler
	Shutdown          func(context.Context) error
}

// ProviderOption configures provider creation.
type ProviderOption func(*providerConfig)

type providerConfig struct {
	// Future options can be added here
}

// NewProviders creates OTEL SDK providers from the bootstrap configuration.
// It creates trace and metric exporters based on OTEL_EXPORTER_OTLP_ENDPOINT.
// When the endpoint is empty, OTLP exporters are skipped (local dev safe).
// Prometheus exporter is always created for /metrics endpoint, with the Go
// and process collectors and the eshu_dp_gomemlimit_bytes gauge registered,
// so every runtime exports its memory signals without a per-main call.
func NewProviders(ctx context.Context, b Bootstrap, opts ...ProviderOption) (*Providers, error) {
	if err := b.Validate(); err != nil {
		return nil, fmt.Errorf("invalid bootstrap: %w", err)
	}

	cfg := &providerConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Create resource with plain attributes to avoid semconv schema URL
	// conflicts between default detectors and explicit semconv versions.
	res := resource.NewWithAttributes(
		"",
		attribute.String("service.name", b.ServiceName),
		attribute.String("service.namespace", b.ServiceNamespace),
	)

	// Check if OTLP endpoint is configured
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	hasOTLP := otlpEndpoint != ""

	// Create trace provider
	traceProvider, err := createTraceProvider(ctx, res, hasOTLP)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace provider: %w", err)
	}

	// The gather-error log writes JSON to stderr with the bootstrap service
	// attributes.
	metricsLogger := NewLogger(b, "telemetry", "metrics")

	// Create meter provider with both OTLP and Prometheus exporters
	meterProvider, promHandler, err := createMeterProvider(ctx, res, hasOTLP, metricsLogger)
	if err != nil {
		if traceProvider != nil {
			_ = traceProvider.Shutdown(ctx)
		}
		return nil, fmt.Errorf("failed to create meter provider: %w", err)
	}

	// Create combined shutdown function
	shutdown := func(ctx context.Context) error {
		var errs []error
		if traceProvider != nil {
			if err := traceProvider.Shutdown(ctx); err != nil {
				errs = append(errs, fmt.Errorf("trace provider shutdown: %w", err))
			}
		}
		if meterProvider != nil {
			if err := meterProvider.Shutdown(ctx); err != nil {
				errs = append(errs, fmt.Errorf("meter provider shutdown: %w", err))
			}
		}
		return errors.Join(errs...)
	}

	// Every runtime builds its /metrics here, so the memory-limit gauge is
	// registered here too rather than in each main (#9567: the reducer, api,
	// mcp-server, and collectors never registered it).
	if err := RecordGOMEMLIMIT(meterProvider.Meter(DefaultSignalName)); err != nil {
		_ = shutdown(ctx)
		return nil, fmt.Errorf("register memory-limit gauge: %w", err)
	}

	otel.SetTracerProvider(traceProvider)
	otel.SetMeterProvider(meterProvider)

	return &Providers{
		TracerProvider:    traceProvider,
		MeterProvider:     meterProvider,
		PrometheusHandler: promHandler,
		Shutdown:          shutdown,
	}, nil
}

func createTraceProvider(ctx context.Context, res *resource.Resource, hasOTLP bool) (*sdktrace.TracerProvider, error) {
	var opts []sdktrace.TracerProviderOption
	opts = append(opts, sdktrace.WithResource(res))

	if hasOTLP {
		exporter, err := otlptracegrpc.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
		}
		opts = append(opts, sdktrace.WithBatcher(exporter))
	}

	return sdktrace.NewTracerProvider(opts...), nil
}

func createMeterProvider(ctx context.Context, res *resource.Resource, hasOTLP bool, metricsLogger *slog.Logger) (*sdkmetric.MeterProvider, http.Handler, error) {
	var readers []sdkmetric.Reader

	// Always create Prometheus exporter for /metrics endpoint.
	//
	// WithResourceAsConstantLabels propagates the named OTEL resource
	// attributes onto every emitted metric as Prometheus labels. Without
	// this option the exporter leaves resource metadata on the separate
	// target_info gauge only, which means a Grafana template variable
	// scoped to a data-plane metric (e.g.
	// label_values(eshu_dp_facts_emitted_total, service_name) in
	// docs/dashboards/ingester.json) returns empty. Every Eshu dashboard
	// under docs/dashboards/ relies on service_name and service_namespace
	// being available as direct labels for filtering; see issue #154 for
	// the audit that revealed the gap. The allow-keys filter is
	// intentionally narrow so we do not accidentally leak every resource
	// attribute (host, process, sdk version, etc.) into the metric
	// label set.
	registry := prometheus.NewRegistry()
	// Go runtime and process memory series (go_memstats_*, go_gc_*,
	// process_resident_memory_bytes) sit beside the OTEL output on the same
	// private registry. Without them an operator cannot see a heap burst
	// against GOMEMLIMIT and the container limit (#9567). They carry no
	// resource labels; scrape-target labels identify the service.
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	promExporter, err := otelprom.New(
		otelprom.WithRegisterer(registry),
		otelprom.WithResourceAsConstantLabels(
			attribute.NewAllowKeysFilter(prometheusResourceLabelKeys...),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
	}
	readers = append(readers, promExporter)

	// Add OTLP metric exporter if endpoint is configured
	if hasOTLP {
		metricExporter, err := otlpmetricgrpc.New(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create OTLP metric exporter: %w", err)
		}
		readers = append(readers, sdkmetric.NewPeriodicReader(metricExporter))
	}

	opts := []sdkmetric.Option{sdkmetric.WithResource(res), sdkmetric.WithView(reservedAttributeView())}
	for _, reader := range readers {
		opts = append(opts, sdkmetric.WithReader(reader))
	}

	meterProvider := sdkmetric.NewMeterProvider(opts...)

	gatherErrors, err := registerMetricsGatherErrors(meterProvider.Meter("eshu/telemetry"))
	if err != nil {
		_ = meterProvider.Shutdown(ctx)
		return nil, nil, err
	}

	return meterProvider, newMetricsHandler(registry, metricsLogger, gatherErrors, time.Now), nil
}
