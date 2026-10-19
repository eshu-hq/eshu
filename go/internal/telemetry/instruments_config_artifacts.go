// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

// Config-artifact instruments (#7126): the read-path candidate counter
// (#7951) and the per-repository derive counter and duration histogram
// (#8155). They share one file so the root package stays within the 40-file
// directory cap.

import (
	"fmt"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// Outcome values of eshu_dp_config_artifact_candidates_total (#7951). The set
// is closed.
const (
	// ConfigArtifactCandidateOutcomeHydrated counts candidates the load read
	// or blank-checked.
	ConfigArtifactCandidateOutcomeHydrated = "hydrated"
	// ConfigArtifactCandidateOutcomeUnhydrated counts candidates inside a list
	// window that the request budget left unread.
	ConfigArtifactCandidateOutcomeUnhydrated = "unhydrated"
	// ConfigArtifactCandidateOutcomeListTruncated counts related repositories
	// whose file list was cut at the 5,000-file window.
	ConfigArtifactCandidateOutcomeListTruncated = "list_truncated"
)

// registerConfigArtifactCandidates registers the #7951 counter of
// config-artifact candidates by outcome.
func registerConfigArtifactCandidates(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ConfigArtifactCandidates, err = meter.Int64Counter(
		"eshu_dp_config_artifact_candidates_total",
		metric.WithDescription("Total config-artifact candidates of the deployment-artifact read path, by outcome "+
			"(hydrated: read or blank-checked; unhydrated: inside a list window but left unread because the request budget ran out; "+
			"list_truncated: one per related repository whose file list was cut at the 5,000-file window)"),
	); err != nil {
		return fmt.Errorf("register ConfigArtifactCandidates counter: %w", err)
	}
	return nil
}

// Trigger values of the config-artifact derive metrics (#8155). The set is
// closed and matches artifacts.Trigger in
// go/internal/storage/postgres/config/artifacts.
const (
	// ConfigArtifactDeriveTriggerWrite is a derive the content Write runs as
	// its last step.
	ConfigArtifactDeriveTriggerWrite = "write"
	// ConfigArtifactDeriveTriggerBackfill is a reducer walker derive during
	// the first full walk.
	ConfigArtifactDeriveTriggerBackfill = "backfill"
	// ConfigArtifactDeriveTriggerRepair is a reducer walker derive of a
	// repository the dirty join named.
	ConfigArtifactDeriveTriggerRepair = "repair"
)

// Outcome values of eshu_dp_config_artifact_derives_total (#8155). The set is
// closed. The storage package owns the matching Go constants, including
// OutcomeCanceled (PR3b-2c-2); a parity test pins both sets.
const (
	// ConfigArtifactDeriveOutcomeOK is a derive that wrote rows and a state
	// row for its content version.
	ConfigArtifactDeriveOutcomeOK = "ok"
	// ConfigArtifactDeriveOutcomeSuperseded is a derive that rolled back
	// without writing because a newer version or extractor already owns the
	// state row.
	ConfigArtifactDeriveOutcomeSuperseded = "superseded"
	// ConfigArtifactDeriveOutcomeError is a derive that failed with an error
	// other than the classes below; the state row records the failure.
	ConfigArtifactDeriveOutcomeError = "error"
	// ConfigArtifactDeriveOutcomePanic is a derive that recovered a panic.
	ConfigArtifactDeriveOutcomePanic = "panic"
	// ConfigArtifactDeriveOutcomeTimeout is a derive that ran past its
	// deadline (ESHU_CONFIG_ARTIFACT_DERIVE_TIMEOUT).
	ConfigArtifactDeriveOutcomeTimeout = "timeout"
	// ConfigArtifactDeriveOutcomeCanceled is a derive whose caller context
	// was canceled (shutdown, supersede, or a lost claim). It is kept apart
	// from error so a shutdown wave does not read as a derive-error wave.
	ConfigArtifactDeriveOutcomeCanceled = "canceled"
	// ConfigArtifactDeriveOutcomeEncodingUnsupported is a derive refused
	// because the start-up encoding check failed, so no derive runs in that
	// process.
	ConfigArtifactDeriveOutcomeEncodingUnsupported = "encoding_unsupported"
	// ConfigArtifactDeriveOutcomeFenceMissing is a derive refused because the
	// start-up fence check did not vouch for the content version fence, so no
	// derive runs in that process.
	ConfigArtifactDeriveOutcomeFenceMissing = "fence_missing"
)

// configArtifactDeriveDurationBuckets are the boundaries, in seconds, of
// eshu_dp_config_artifact_derive_duration_seconds. 1 ms is the round-trip
// floor of a tiny repository; 2 s is the S1 per-repository cap, so le="2"
// reads that bar directly; 10 s is the default derive deadline; 15 s is the
// caller-facing worst case (the deadline plus the 5 s failure-record bound);
// 30 s covers an operator-raised deadline before +Inf.
var configArtifactDeriveDurationBuckets = []float64{
	0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 15, 30,
}

// ConfigArtifactDeriveTriggers returns a copy of the closed trigger set, in
// declaration order.
func ConfigArtifactDeriveTriggers() []string {
	return []string{
		ConfigArtifactDeriveTriggerWrite,
		ConfigArtifactDeriveTriggerBackfill,
		ConfigArtifactDeriveTriggerRepair,
	}
}

// ConfigArtifactDeriveOutcomes returns a copy of the closed outcome set, in
// declaration order.
func ConfigArtifactDeriveOutcomes() []string {
	return []string{
		ConfigArtifactDeriveOutcomeOK,
		ConfigArtifactDeriveOutcomeSuperseded,
		ConfigArtifactDeriveOutcomeError,
		ConfigArtifactDeriveOutcomePanic,
		ConfigArtifactDeriveOutcomeTimeout,
		ConfigArtifactDeriveOutcomeCanceled,
		ConfigArtifactDeriveOutcomeEncodingUnsupported,
		ConfigArtifactDeriveOutcomeFenceMissing,
	}
}

// ConfigArtifactDeriveDurationBuckets returns a copy of the derive duration
// histogram boundaries, in seconds.
func ConfigArtifactDeriveDurationBuckets() []float64 {
	return slices.Clone(configArtifactDeriveDurationBuckets)
}

// AttrTrigger returns a trigger attribute. v must be one of
// ConfigArtifactDeriveTriggers.
func AttrTrigger(v string) attribute.KeyValue {
	return attribute.String(contract.MetricDimensionTrigger, v)
}

// registerConfigArtifactDeriveInstruments registers the #8155 derive counter
// and duration histogram on inst. PR3b-2c-2 first emits them.
func registerConfigArtifactDeriveInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ConfigArtifactDerives, err = meter.Int64Counter(
		"eshu_dp_config_artifact_derives_total",
		metric.WithDescription("Per-repository config-artifact derives by trigger (write, backfill, repair) and outcome "+
			"(ok, superseded, error, panic, timeout, canceled, encoding_unsupported, fence_missing) (#8155). "+
			"No repository label"),
	); err != nil {
		return fmt.Errorf("register ConfigArtifactDerives counter: %w", err)
	}
	if inst.ConfigArtifactDeriveDuration, err = meter.Float64Histogram(
		"eshu_dp_config_artifact_derive_duration_seconds",
		metric.WithDescription("Wall time of one per-repository config-artifact derive, by trigger (#8155). "+
			"The caller can wait up to the derive deadline plus the 5 s failure-record bound"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(configArtifactDeriveDurationBuckets...),
	); err != nil {
		return fmt.Errorf("register ConfigArtifactDeriveDuration histogram: %w", err)
	}
	return nil
}

// registerConfigArtifactDerives appends the trigger dimension and the derive
// span (#8155). outcome is already in the root registry.
func registerConfigArtifactDerives() {
	metricDimensionKeys = append(metricDimensionKeys, contract.MetricDimensionTrigger)
	spanNames = append(spanNames, contract.SpanConfigArtifactsDerive)
}
