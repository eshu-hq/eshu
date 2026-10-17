// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
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
