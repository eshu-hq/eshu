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

// registerServiceCatalogRuntimeRetrigger registers the #8169 counter of
// service catalog correlation re-triggers by a workload_materialization ACK,
// by producer domain and outcome.
func registerServiceCatalogRuntimeRetrigger(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ServiceCatalogRuntimeRetrigger, err = meter.Int64Counter(
		"eshu_dp_service_catalog_runtime_retrigger_total",
		metric.WithDescription("Service catalog correlation rows re-triggered by a succeeded runtime producer ACK, "+
			"by producer domain and outcome (reopened/replay_flagged add rows; none adds one per ACK that "+
			"matched no catalog row) (#8169)"),
	); err != nil {
		return fmt.Errorf("register ServiceCatalogRuntimeRetrigger counter: %w", err)
	}
	return nil
}
