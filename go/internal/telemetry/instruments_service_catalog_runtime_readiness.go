// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerServiceCatalogRuntimeReadiness registers the #7816 counter of
// service catalog runtime readiness fence decisions, by domain and outcome.
func registerServiceCatalogRuntimeReadiness(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ServiceCatalogRuntimeReadiness, err = meter.Int64Counter(
		"eshu_dp_service_catalog_runtime_readiness_total",
		metric.WithDescription("Service catalog runtime readiness fence decisions by reducer domain and outcome "+
			"(ready/no_producer/deferred/bound_exceeded): whether the runtime evidence read waited on "+
			"in-flight workload_materialization work (#7816)"),
	); err != nil {
		return fmt.Errorf("register ServiceCatalogRuntimeReadiness counter: %w", err)
	}
	return nil
}
