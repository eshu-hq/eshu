// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerDeltaUnionCodeCallJVMInstruments registers the delta-union outcome
// counters of the code-call symbol definition read (#10003 row 1) and the
// JVM reachability read (#10003 row 2) on inst. Both mirror the manifest
// dependency read's delta-union counter (#9991): one point per delta-active
// scope a read reached, by read and outcome.
func registerDeltaUnionCodeCallJVMInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.CodeCallSymbolDefinitionReadDeltaUnion, err = meter.Int64Counter(
		"eshu_dp_code_call_symbol_definition_read_delta_union_total",
		metric.WithDescription("Delta-base union decisions of code-call symbol definition reads, one per delta-active scope a read reached (union_resolve_failed: once per failed read), by read and outcome (#10003)"),
	); err != nil {
		return fmt.Errorf("register CodeCallSymbolDefinitionReadDeltaUnion counter: %w", err)
	}
	if inst.JVMReachabilityReadDeltaUnion, err = meter.Int64Counter(
		"eshu_dp_jvm_reachability_read_delta_union_total",
		metric.WithDescription("Delta-base union decisions of JVM reachability reads, one per delta-active scope a read reached (union_resolve_failed: once per failed read), by read and outcome (#10003)"),
	); err != nil {
		return fmt.Errorf("register JVMReachabilityReadDeltaUnion counter: %w", err)
	}
	return nil
}
