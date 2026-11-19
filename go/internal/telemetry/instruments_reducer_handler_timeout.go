// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// HandlerTimeoutBounds provides the configured per-domain handler bounds for
// the timeout gauge (#10866). Implementations read operator configuration
// (never live queue or graph state): the gauge value changes only on
// redeploy, so the callback must be cheap and infallible under load.
type HandlerTimeoutBounds interface {
	// HandlerTimeoutBounds returns the bound in seconds per domain for every
	// known domain. Zero means unbounded.
	HandlerTimeoutBounds(ctx context.Context) (map[string]float64, error)
}

// RegisterReducerHandlerTimeoutGauge registers the
// eshu_dp_reducer_handler_timeout_seconds gauge backed by bounds. One
// callback reads the bounds once per scrape and observes the gauge once per
// known domain, so the series set is the closed domain set with no
// per-intent cardinality. A nil observer registers nothing and reports
// success, so lanes without a configured reducer service keep today's
// instruments byte-identical.
func RegisterReducerHandlerTimeoutGauge(inst *Instruments, meter metric.Meter, bounds HandlerTimeoutBounds) error {
	if inst == nil {
		return errors.New("instruments are required")
	}
	if meter == nil {
		return errors.New("meter is required")
	}
	if bounds == nil {
		return nil
	}

	var err error
	inst.ReducerHandlerTimeoutSeconds, err = meter.Float64ObservableGauge(
		"eshu_dp_reducer_handler_timeout_seconds",
		metric.WithDescription("Configured per-domain reducer handler bound in seconds; zero means unbounded"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("register ReducerHandlerTimeoutSeconds gauge: %w", err)
	}
	if _, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		configured, err := bounds.HandlerTimeoutBounds(ctx)
		if err != nil {
			return fmt.Errorf("observe reducer handler timeout bounds: %w", err)
		}
		for domain, bound := range configured {
			if bound < 0 {
				bound = 0
			}
			o.ObserveFloat64(inst.ReducerHandlerTimeoutSeconds, bound,
				metric.WithAttributes(
					attribute.String(MetricDimensionDomain, domain),
				),
			)
		}
		return nil
	}, inst.ReducerHandlerTimeoutSeconds); err != nil {
		return fmt.Errorf("register ReducerHandlerTimeoutSeconds callback: %w", err)
	}
	return nil
}
