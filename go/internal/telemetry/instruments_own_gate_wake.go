// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricDimensionSite labels eshu_dp_reducer_own_gate_wake_total with where
// the #7597 own-gate wake ran. The value set is closed: OwnGateWakeSiteActivation
// or OwnGateWakeSiteFail.
const MetricDimensionSite = "site"

// Own-gate wake sites (#7597).
const (
	// OwnGateWakeSiteActivation is the wake inside a relationship-generation
	// activation transaction.
	OwnGateWakeSiteActivation = "activation"
	// OwnGateWakeSiteFail is the check a consumer's own counting dead letter
	// runs after its Fail commits.
	OwnGateWakeSiteFail = "fail"
)

// Own-gate wake outcomes (#7597), recorded on the existing outcome dimension.
const (
	// OwnGateWakeOutcomeReopened means the wake statement reopened at least one
	// dead-lettered own-gate consumer.
	OwnGateWakeOutcomeReopened = "reopened"
	// OwnGateWakeOutcomeNoop means the wake statement ran and reopened nobody.
	OwnGateWakeOutcomeNoop = "noop"
	// OwnGateWakeOutcomeError means the wake (or, at activation, its in-flight
	// lock) failed. At activation the transaction rolled back; at a Fail the
	// dead letter stayed committed and the error was logged, not returned.
	OwnGateWakeOutcomeError = "error"
)

// AttrOwnGateWakeSite returns a site attribute. v must be one of the
// OwnGateWakeSite* constants.
func AttrOwnGateWakeSite(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSite, v)
}

// registerOwnGateWakeInstruments registers the #7597 own-gate wake counter on
// inst. One increment per wake statement, never per row: labels are the
// closed site and outcome sets, with no scope or work-item id.
func registerOwnGateWakeInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ReducerOwnGateWake, err = meter.Int64Counter(
		"eshu_dp_reducer_own_gate_wake_total",
		metric.WithDescription("Own-gate consumer wake statements by site (activation: inside a relationship-generation activation; fail: after a consumer's own counting dead letter) and outcome (reopened, noop, error) (#7597)"),
	); err != nil {
		return fmt.Errorf("register ReducerOwnGateWake counter: %w", err)
	}
	return nil
}

// registerOwnGateWake appends the own-gate wake site metric dimension (#7597).
func registerOwnGateWake() {
	metricDimensionKeys = append(metricDimensionKeys, MetricDimensionSite)
}
